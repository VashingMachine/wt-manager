package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// cachedPullRequest persists the fields RemotePullRequest keeps out of JSON.
type cachedPullRequest struct {
	core.RemotePullRequest
	HeadSHA     string `json:"headSha,omitempty"`
	Diff        string `json:"diff,omitempty"`
	ChecksKnown bool   `json:"checksKnown,omitempty"`
}

func (c cachedPullRequest) restore() core.RemotePullRequest {
	pr := c.RemotePullRequest
	pr.HeadSHA = c.HeadSHA
	pr.Diff = c.Diff
	pr.ChecksKnown = c.ChecksKnown
	return pr
}

func toCachedPullRequest(pr core.RemotePullRequest) cachedPullRequest {
	return cachedPullRequest{RemotePullRequest: pr, HeadSHA: pr.HeadSHA, Diff: pr.Diff, ChecksKnown: pr.ChecksKnown}
}

func (s *Service) PeekPullRequestIndex(cfg core.Config) ([]core.PullRequest, time.Time, bool) {
	if cfg.RepoSlug == "" {
		return nil, time.Time{}, false
	}
	var prs []core.PullRequest
	fetchedAt, ok := s.cache.Get(bucketPRIndex, cfg.RepoSlug, &prs)
	return prs, fetchedAt, ok
}

func (s *Service) LoadPullRequestIndex(ctx context.Context, cfg core.Config, refresh bool) ([]core.PullRequest, string, error) {
	ctx = s.commandContext(ctx)
	cached, fetchedAt, cachedOK := s.PeekPullRequestIndex(cfg)
	if !refresh && cachedOK && isFresh(fetchedAt, prIndexTTL) {
		return cached, "", nil
	}
	if err := s.github.Available(cfg); err != nil {
		return nil, "", err
	}
	result, err := s.flights.do(bucketPRIndex+"\x00"+cfg.RepoSlug, func() (any, error) {
		prs, err := s.github.PullRequestIndex(ctx, cfg)
		if err == nil {
			_ = s.cache.Put(bucketPRIndex, cfg.RepoSlug, prs)
		}
		return prs, err
	})
	if err != nil {
		if cachedOK {
			return cached, fmt.Sprintf("PR refresh failed, showing %s", cachedLabel("PRs", fetchedAt)), nil
		}
		return nil, "", err
	}
	return result.([]core.PullRequest), "", nil
}

func (s *Service) PeekRemotePullRequests(cfg core.Config) ([]core.RemotePullRequest, time.Time, bool) {
	if cfg.RepoSlug == "" {
		return nil, time.Time{}, false
	}
	var cached []cachedPullRequest
	fetchedAt, ok := s.cache.Get(bucketPRList, cfg.RepoSlug, &cached)
	if !ok {
		return nil, time.Time{}, false
	}
	prs := make([]core.RemotePullRequest, len(cached))
	for i, entry := range cached {
		prs[i] = entry.restore()
	}
	s.overlayChecks(cfg, prs)
	return prs, fetchedAt, true
}

func (s *Service) LoadRemotePullRequests(ctx context.Context, cfg core.Config) ([]core.RemotePullRequest, error) {
	ctx = s.commandContext(ctx)
	result, err := s.flights.do(bucketPRList+"\x00"+cfg.RepoSlug, func() (any, error) {
		return s.github.PullRequests(ctx, cfg)
	})
	if err != nil {
		return nil, err
	}
	shared := result.([]core.RemotePullRequest)
	prs := make([]core.RemotePullRequest, len(shared))
	copy(prs, shared)
	cached := make([]cachedPullRequest, len(prs))
	for i, pr := range prs {
		// Check runs live in their own bucket, keyed by commit.
		pr.StatusCheckRollup = nil
		cached[i] = toCachedPullRequest(pr)
	}
	_ = s.cache.Put(bucketPRList, cfg.RepoSlug, cached)
	s.overlayChecks(cfg, prs)
	return prs, nil
}

func (s *Service) overlayChecks(cfg core.Config, prs []core.RemotePullRequest) {
	for i := range prs {
		if prs[i].HeadSHA == "" {
			continue
		}
		var checks []core.StatusCheck
		if _, ok := s.cache.Get(bucketPRChecks, checksKey(cfg, prs[i].HeadSHA), &checks); ok {
			prs[i].StatusCheckRollup = checks
			prs[i].ChecksKnown = true
		}
	}
}

func checksKey(cfg core.Config, sha string) string {
	return cfg.RepoSlug + "@" + sha
}

func checksTTL(checks []core.StatusCheck) time.Duration {
	if len(checks) == 0 {
		return checksEmptyTTL
	}
	for _, check := range checks {
		if !strings.EqualFold(check.Status, "completed") {
			return checksPendingTTL
		}
	}
	return checksCompleteTTL
}

func (s *Service) LoadRemotePullRequestChecks(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error) {
	ctx = s.commandContext(ctx)
	if sha == "" {
		return nil, errors.New("head commit is required")
	}
	key := checksKey(cfg, sha)
	var cached []core.StatusCheck
	fetchedAt, cachedOK := s.cache.Get(bucketPRChecks, key, &cached)
	if cachedOK && isFresh(fetchedAt, checksTTL(cached)) {
		return cached, nil
	}
	checks, err := s.fetchChecks(ctx, cfg, sha)
	if err != nil {
		if cachedOK {
			return cached, nil
		}
		return nil, err
	}
	return checks, nil
}

// fetchChecks reads check runs from GitHub under the shared request limit and
// caches them for the list and detail views alike.
func (s *Service) fetchChecks(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error) {
	result, err := s.flights.do(bucketPRChecks+"\x00"+checksKey(cfg, sha), func() (any, error) {
		select {
		case s.ghLimit <- struct{}{}:
			defer func() { <-s.ghLimit }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		checks, err := s.github.Checks(ctx, cfg, sha)
		if err == nil {
			_ = s.cache.Put(bucketPRChecks, checksKey(cfg, sha), checks)
		}
		return checks, err
	})
	if err != nil {
		return nil, err
	}
	return result.([]core.StatusCheck), nil
}

func prDetailKey(cfg core.Config, number int) string {
	return cfg.RepoSlug + "#" + strconv.Itoa(number)
}

func (s *Service) LoadRemotePullRequestDetail(ctx context.Context, cfg core.Config, number int) (core.RemotePullRequest, error) {
	ctx = s.commandContext(ctx)
	pr, err := s.github.PullRequestDetail(ctx, cfg, number, s.LoadRemotePullRequestChecks)
	if err != nil {
		return core.RemotePullRequest{}, err
	}
	_ = s.cache.Put(bucketPRDetail, prDetailKey(cfg, number), toCachedPullRequest(pr))
	return pr, nil
}

func (s *Service) PeekRemotePullRequestDetail(cfg core.Config, number int) (core.RemotePullRequest, time.Time, bool) {
	if cfg.RepoSlug == "" || number <= 0 {
		return core.RemotePullRequest{}, time.Time{}, false
	}
	var cached cachedPullRequest
	fetchedAt, ok := s.cache.Get(bucketPRDetail, prDetailKey(cfg, number), &cached)
	if !ok {
		return core.RemotePullRequest{}, time.Time{}, false
	}
	return cached.restore(), fetchedAt, true
}

func (s *Service) ApproveRemotePullRequest(ctx context.Context, cfg core.Config, number int) error {
	ctx = s.commandContext(ctx)
	if err := s.github.Approve(ctx, cfg, number); err != nil {
		return err
	}
	// The cached review decision is now wrong; keep it paintable but refetch.
	s.cache.Expire(bucketPRDetail, prDetailKey(cfg, number))
	return nil
}

func (s *Service) LoadGitHubCurrentUser(ctx context.Context, cfg core.Config) (string, error) {
	ctx = s.commandContext(ctx)
	var login string
	fetchedAt, ok := s.cache.Get(bucketGitHubUser, "login", &login)
	if ok && login != "" && isFresh(fetchedAt, githubUserTTL) {
		return login, nil
	}
	fresh, err := s.github.CurrentUser(ctx, cfg)
	if err != nil {
		if ok && login != "" {
			return login, nil
		}
		return "", err
	}
	_ = s.cache.Put(bucketGitHubUser, "login", fresh)
	return fresh, nil
}

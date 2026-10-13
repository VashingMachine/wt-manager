package services

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cache layout. Buckets group one kind of data; keys identify the repository
// (or JIRA site) and the item, so profiles sharing a cache file never collide.
const (
	bucketWorktreeList   = "worktree-list"   // repo path -> discovered worktrees
	bucketWorktreeStatus = "worktree-status" // repo path + worktree path -> status, commit
	bucketPRIndex        = "pr-index"        // repo slug -> PRs used to label branches
	bucketPRList         = "pr-list"         // repo slug -> PR Radar list
	bucketPRChecks       = "pr-checks"       // repo slug + head SHA -> check runs
	bucketPRDetail       = "pr-detail"       // repo slug + number -> PR detail and diff
	bucketGitHubUser     = "github-user"     // "login" -> authenticated user
)

const (
	// prIndexTTL bounds how long branch labels reuse a `gh pr list`; refreshes
	// within it (after creating a worktree, entering tickets) cost nothing.
	prIndexTTL    = 2 * time.Minute
	githubUserTTL = 24 * time.Hour
	// Completed check runs for a commit rarely change, while queued or
	// in-progress ones do, so they are revisited much sooner.
	checksCompleteTTL = 15 * time.Minute
	checksPendingTTL  = 30 * time.Second
	checksEmptyTTL    = 2 * time.Minute
	// fetchReuseWindow lets worktree creations queued behind each other share
	// one `git fetch` instead of each paying the network round trip.
	fetchReuseWindow = 15 * time.Second
)

// cachedPullRequest persists the fields RemotePullRequest keeps out of JSON.
type cachedPullRequest struct {
	RemotePullRequest
	HeadSHA     string `json:"headSha,omitempty"`
	Diff        string `json:"diff,omitempty"`
	ChecksKnown bool   `json:"checksKnown,omitempty"`
}

func (c cachedPullRequest) restore() RemotePullRequest {
	pr := c.RemotePullRequest
	pr.HeadSHA = c.HeadSHA
	pr.Diff = c.Diff
	pr.ChecksKnown = c.ChecksKnown
	return pr
}

func toCachedPullRequest(pr RemotePullRequest) cachedPullRequest {
	return cachedPullRequest{RemotePullRequest: pr, HeadSHA: pr.HeadSHA, Diff: pr.Diff, ChecksKnown: pr.ChecksKnown}
}

type cachedWorktreeStatus struct {
	Status RepoStatus
	Commit CommitInfo
}

func worktreeStatusKey(cfg Config, path string) string {
	return cfg.ActiveProfile.RepositoryPath + "\x00" + path
}

// --- worktrees -------------------------------------------------------------

func (s *Service) DiscoverWorktrees(ctx context.Context, cfg Config, showAll bool) ([]Worktree, error) {
	ctx = s.commandContext(ctx)
	all, err := discoverWorktrees(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_ = s.cache.Put(bucketWorktreeList, cfg.ActiveProfile.RepositoryPath, all)
	worktrees := filterManaged(all, showAll)
	s.overlayWorktreeStatus(cfg, worktrees)
	sortWorktrees(worktrees)
	return worktrees, nil
}

func (s *Service) PeekWorktrees(cfg Config, showAll bool) ([]Worktree, bool) {
	var all []Worktree
	if _, ok := s.cache.Get(bucketWorktreeList, cfg.ActiveProfile.RepositoryPath, &all); !ok || len(all) == 0 {
		return nil, false
	}
	worktrees := filterManaged(all, showAll)
	s.overlayWorktreeStatus(cfg, worktrees)
	sortWorktrees(worktrees)
	return worktrees, true
}

// overlayWorktreeStatus fills in the last known status of each worktree so the
// table shows real states while git re-reads them.
func (s *Service) overlayWorktreeStatus(cfg Config, worktrees []Worktree) {
	for i := range worktrees {
		wt := &worktrees[i]
		if note := missingWorktreeNote(*wt); note != "" {
			wt.LoadError = note
			continue
		}
		var cached cachedWorktreeStatus
		if fetchedAt, ok := s.cache.Get(bucketWorktreeStatus, worktreeStatusKey(cfg, wt.Path), &cached); ok {
			wt.Status = cached.Status
			wt.Commit = cached.Commit
			if !isFresh(fetchedAt, time.Minute) {
				// "5 minutes ago" was true when it was read, not now.
				wt.Commit.Relative = ""
			}
			continue
		}
		wt.StatusPending = true
	}
}

func (s *Service) LoadWorktreeStatus(ctx context.Context, cfg Config, wt Worktree) (Worktree, error) {
	ctx = s.commandContext(ctx)
	select {
	case s.gitLimit <- struct{}{}:
		defer func() { <-s.gitLimit }()
	case <-ctx.Done():
		return wt, ctx.Err()
	}
	warnings := readWorktreeStatus(ctx, &wt)
	if wt.LoadError == "" && len(warnings) == 0 {
		_ = s.cache.Put(bucketWorktreeStatus, worktreeStatusKey(cfg, wt.Path), cachedWorktreeStatus{Status: wt.Status, Commit: wt.Commit})
	}
	if len(warnings) > 0 {
		return wt, errors.New(strings.Join(warnings, "; "))
	}
	return wt, nil
}

// CollectWorktrees is the one-shot form of the progressive load the TUI runs:
// discovery, cached PR labels, then every status in parallel.
func (s *Service) CollectWorktrees(ctx context.Context, cfg Config, showAll bool) ([]Worktree, string, error) {
	worktrees, err := s.DiscoverWorktrees(ctx, cfg, showAll)
	if err != nil {
		return nil, "", err
	}
	var warnings []string
	prs, prWarning, prErr := s.LoadPullRequestIndex(ctx, cfg, false)
	switch {
	case prErr != nil:
		warnings = append(warnings, "PR lookup unavailable: gh auth or repo access failed")
	case prWarning != "":
		warnings = append(warnings, prWarning)
	}

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)
	for i := range worktrees {
		wg.Go(func() {
			loaded, err := s.LoadWorktreeStatus(ctx, cfg, worktrees[i])
			worktrees[i] = loaded
			if err != nil {
				mu.Lock()
				warnings = append(warnings, err.Error())
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	attachPullRequests(worktrees, prs)
	return worktrees, strings.Join(warnings, " | "), nil
}

// CollectTicketWorktrees gathers what ticket rows show. Discovery is enough:
// cached statuses come along for free and nothing else is displayed.
func (s *Service) CollectTicketWorktrees(ctx context.Context, cfg Config) ([]Worktree, string, error) {
	worktrees, err := s.DiscoverWorktrees(ctx, cfg, true)
	return worktrees, "", err
}

// --- pull requests ---------------------------------------------------------

func requireGitHub(cfg Config) error {
	if cfg.RepoSlug == "" {
		return errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh not found; run setup after installing GitHub CLI")
	}
	return nil
}

func (s *Service) PeekPullRequestIndex(cfg Config) ([]PullRequest, time.Time, bool) {
	if cfg.RepoSlug == "" {
		return nil, time.Time{}, false
	}
	var prs []PullRequest
	fetchedAt, ok := s.cache.Get(bucketPRIndex, cfg.RepoSlug, &prs)
	return prs, fetchedAt, ok
}

func (s *Service) LoadPullRequestIndex(ctx context.Context, cfg Config, refresh bool) ([]PullRequest, string, error) {
	ctx = s.commandContext(ctx)
	cached, fetchedAt, cachedOK := s.PeekPullRequestIndex(cfg)
	if !refresh && cachedOK && isFresh(fetchedAt, prIndexTTL) {
		return cached, "", nil
	}
	if err := requireGitHub(cfg); err != nil {
		return nil, "", err
	}
	result, err := s.flights.do(bucketPRIndex+"\x00"+cfg.RepoSlug, func() (any, error) {
		prs, err := fetchPullRequestIndex(ctx, cfg)
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
	return result.([]PullRequest), "", nil
}

func (s *Service) PeekRemotePullRequests(cfg Config) ([]RemotePullRequest, time.Time, bool) {
	if cfg.RepoSlug == "" {
		return nil, time.Time{}, false
	}
	var cached []cachedPullRequest
	fetchedAt, ok := s.cache.Get(bucketPRList, cfg.RepoSlug, &cached)
	if !ok {
		return nil, time.Time{}, false
	}
	prs := make([]RemotePullRequest, len(cached))
	for i, entry := range cached {
		prs[i] = entry.restore()
	}
	s.overlayChecks(cfg, prs)
	return prs, fetchedAt, true
}

func (s *Service) LoadRemotePullRequests(ctx context.Context, cfg Config) ([]RemotePullRequest, error) {
	ctx = s.commandContext(ctx)
	result, err := s.flights.do(bucketPRList+"\x00"+cfg.RepoSlug, func() (any, error) {
		return loadRemotePullRequests(ctx, cfg)
	})
	if err != nil {
		return nil, err
	}
	shared := result.([]RemotePullRequest)
	prs := make([]RemotePullRequest, len(shared))
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

func (s *Service) overlayChecks(cfg Config, prs []RemotePullRequest) {
	for i := range prs {
		if prs[i].HeadSHA == "" {
			continue
		}
		var checks []StatusCheck
		if _, ok := s.cache.Get(bucketPRChecks, checksKey(cfg, prs[i].HeadSHA), &checks); ok {
			prs[i].StatusCheckRollup = checks
			prs[i].ChecksKnown = true
		}
	}
}

func checksKey(cfg Config, sha string) string {
	return cfg.RepoSlug + "@" + sha
}

func checksTTL(checks []StatusCheck) time.Duration {
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

func (s *Service) LoadRemotePullRequestChecks(ctx context.Context, cfg Config, sha string) ([]StatusCheck, error) {
	ctx = s.commandContext(ctx)
	if sha == "" {
		return nil, errors.New("head commit is required")
	}
	key := checksKey(cfg, sha)
	var cached []StatusCheck
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
func (s *Service) fetchChecks(ctx context.Context, cfg Config, sha string) ([]StatusCheck, error) {
	result, err := s.flights.do(bucketPRChecks+"\x00"+checksKey(cfg, sha), func() (any, error) {
		select {
		case s.ghLimit <- struct{}{}:
			defer func() { <-s.ghLimit }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		checks, err := loadRemotePullRequestChecks(ctx, cfg, sha)
		if err == nil {
			_ = s.cache.Put(bucketPRChecks, checksKey(cfg, sha), checks)
		}
		return checks, err
	})
	if err != nil {
		return nil, err
	}
	return result.([]StatusCheck), nil
}

func prDetailKey(cfg Config, number int) string {
	return cfg.RepoSlug + "#" + strconv.Itoa(number)
}

func (s *Service) LoadRemotePullRequestDetail(ctx context.Context, cfg Config, number int) (RemotePullRequest, error) {
	ctx = s.commandContext(ctx)
	pr, err := loadRemotePullRequestDetailWith(ctx, cfg, number, s.LoadRemotePullRequestChecks)
	if err != nil {
		return RemotePullRequest{}, err
	}
	_ = s.cache.Put(bucketPRDetail, prDetailKey(cfg, number), toCachedPullRequest(pr))
	return pr, nil
}

func (s *Service) PeekRemotePullRequestDetail(cfg Config, number int) (RemotePullRequest, time.Time, bool) {
	if cfg.RepoSlug == "" || number <= 0 {
		return RemotePullRequest{}, time.Time{}, false
	}
	var cached cachedPullRequest
	fetchedAt, ok := s.cache.Get(bucketPRDetail, prDetailKey(cfg, number), &cached)
	if !ok {
		return RemotePullRequest{}, time.Time{}, false
	}
	return cached.restore(), fetchedAt, true
}

func (s *Service) ApproveRemotePullRequest(ctx context.Context, cfg Config, number int) error {
	ctx = s.commandContext(ctx)
	if err := approveRemotePullRequest(ctx, cfg, number); err != nil {
		return err
	}
	// The cached review decision is now wrong; keep it paintable but refetch.
	s.cache.Expire(bucketPRDetail, prDetailKey(cfg, number))
	return nil
}

func (s *Service) LoadGitHubCurrentUser(ctx context.Context, cfg Config) (string, error) {
	ctx = s.commandContext(ctx)
	var login string
	fetchedAt, ok := s.cache.Get(bucketGitHubUser, "login", &login)
	if ok && login != "" && isFresh(fetchedAt, githubUserTTL) {
		return login, nil
	}
	fresh, err := loadGitHubCurrentUser(ctx, cfg)
	if err != nil {
		if ok && login != "" {
			return login, nil
		}
		return "", err
	}
	_ = s.cache.Put(bucketGitHubUser, "login", fresh)
	return fresh, nil
}

// --- request coordination --------------------------------------------------

// flightGroup collapses concurrent identical fetches into one: a background
// prefetch and the view the user just opened share the same request.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flight
}

type flight struct {
	done  chan struct{}
	value any
	err   error
}

func (g *flightGroup) do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flight{}
	}
	if call, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-call.done
		return call.value, call.err
	}
	call := &flight{done: make(chan struct{})}
	g.calls[key] = call
	g.mu.Unlock()

	call.value, call.err = fn()
	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	close(call.done)
	return call.value, call.err
}

// fetchCoalescer skips a `git fetch` when the same remote was fetched within
// fetchReuseWindow, which is what queued worktree creations look like.
type fetchCoalescer struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func (c *fetchCoalescer) fetch(ctx context.Context, repoPath string, remoteName string, force bool) bool {
	key := repoPath + "\x00" + remoteName
	c.mu.Lock()
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if at, ok := c.last[key]; ok && !force && now().Sub(at) < fetchReuseWindow {
		c.mu.Unlock()
		return false
	}
	c.mu.Unlock()

	fetchRemote(ctx, repoPath, remoteName, force)

	c.mu.Lock()
	if c.last == nil {
		c.last = map[string]time.Time{}
	}
	c.last[key] = now()
	c.mu.Unlock()
	return true
}

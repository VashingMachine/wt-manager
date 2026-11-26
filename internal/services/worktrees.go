package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

type cachedWorktreeStatus struct {
	Status core.RepoStatus
	Commit core.CommitInfo
}

func worktreeStatusKey(cfg core.Config, path string) string {
	return cfg.ActiveProfile.RepositoryPath + "\x00" + path
}

func (s *Service) DiscoverWorktrees(ctx context.Context, cfg core.Config, showAll bool) ([]core.Worktree, error) {
	ctx = s.commandContext(ctx)
	all, err := s.git.DiscoverWorktrees(ctx, cfg)
	if err != nil {
		return nil, err
	}
	_ = s.cache.Put(bucketWorktreeList, cfg.ActiveProfile.RepositoryPath, all)
	worktrees := core.FilterManaged(all, showAll)
	s.overlayWorktreeStatus(cfg, worktrees)
	core.SortWorktrees(worktrees)
	core.OrderByStack(worktrees)
	return worktrees, nil
}

func (s *Service) PeekWorktrees(cfg core.Config, showAll bool) ([]core.Worktree, bool) {
	var all []core.Worktree
	if _, ok := s.cache.Get(bucketWorktreeList, cfg.ActiveProfile.RepositoryPath, &all); !ok || len(all) == 0 {
		return nil, false
	}
	// Labels depend on the project, which may have gained repositories since.
	core.LabelWorktrees(cfg, all)
	worktrees := core.FilterManaged(all, showAll)
	s.overlayWorktreeStatus(cfg, worktrees)
	core.SortWorktrees(worktrees)
	core.OrderByStack(worktrees)
	return worktrees, true
}

// overlayWorktreeStatus fills in the last known status of each worktree so the
// table shows real states while git re-reads them.
func (s *Service) overlayWorktreeStatus(cfg core.Config, worktrees []core.Worktree) {
	for i := range worktrees {
		wt := &worktrees[i]
		if note := core.MissingWorktreeNote(*wt); note != "" {
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

func (s *Service) LoadWorktreeStatus(ctx context.Context, cfg core.Config, wt core.Worktree) (core.Worktree, error) {
	ctx = s.commandContext(ctx)
	select {
	case s.gitLimit <- struct{}{}:
		defer func() { <-s.gitLimit }()
	case <-ctx.Done():
		return wt, ctx.Err()
	}
	warnings := s.git.ReadWorktreeStatus(ctx, &wt)
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
func (s *Service) CollectWorktrees(ctx context.Context, cfg core.Config, showAll bool) ([]core.Worktree, string, error) {
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
	core.AttachPullRequests(worktrees, prs)
	return worktrees, strings.Join(warnings, " | "), nil
}

// CollectTicketWorktrees gathers what ticket rows show. Discovery is enough:
// cached statuses come along for free and nothing else is displayed.
func (s *Service) CollectTicketWorktrees(ctx context.Context, cfg core.Config) ([]core.Worktree, string, error) {
	worktrees, err := s.DiscoverWorktrees(ctx, cfg, true)
	return worktrees, "", err
}

func (s *Service) CreateWorktree(ctx context.Context, cfg core.Config, branch string, path string) (bool, error) {
	ctx = s.commandContext(ctx)
	return s.git.CreateWorktree(ctx, cfg, branch, path)
}

func (s *Service) CreateStackedWorktree(ctx context.Context, cfg core.Config, branch string, path string, parent string) error {
	ctx = s.commandContext(ctx)
	return s.git.CreateStackedWorktree(ctx, cfg, branch, path, parent)
}

func (s *Service) SetStackParent(ctx context.Context, cfg core.Config, wt core.Worktree, parent string) error {
	ctx = s.commandContext(ctx)
	return s.git.SetStackParent(ctx, cfg, wt, parent)
}

func (s *Service) RestackWorktree(ctx context.Context, cfg core.Config, wt core.Worktree, options core.RestackOptions) (core.RestackResult, error) {
	ctx = s.commandContext(ctx)
	return s.git.RestackWorktree(ctx, cfg, wt, options, func(ctx context.Context, branch string) bool {
		return s.branchMergedOnGitHub(ctx, cfg, branch)
	})
}

// branchMergedOnGitHub asks GitHub about one branch. The PR labels cover only
// the latest pull requests of a repository, and in a busy one a parent merged
// a few days ago is no longer among them.
func (s *Service) branchMergedOnGitHub(ctx context.Context, cfg core.Config, branch string) bool {
	if s.github.Available(cfg) != nil {
		return false
	}
	select {
	case s.ghLimit <- struct{}{}:
		defer func() { <-s.ghLimit }()
	case <-ctx.Done():
		return false
	}
	prs, err := s.github.BranchPullRequests(ctx, cfg, branch)
	if err != nil {
		return false
	}
	return core.BranchMerged(prs, branch)
}

func (s *Service) SweepCandidates(ctx context.Context, cfg core.Config, olderThan time.Duration) ([]core.SweepCandidate, error) {
	ctx = s.commandContext(ctx)
	return s.git.SweepCandidates(ctx, cfg, olderThan, time.Now())
}

func (s *Service) RemoveWorktree(ctx context.Context, cfg core.Config, wt core.Worktree) error {
	ctx = s.commandContext(ctx)
	return s.git.RemoveWorktree(ctx, cfg, wt)
}

func (s *Service) EnsurePullRequestWorktree(ctx context.Context, cfg core.Config, pullRequest core.RemotePullRequest) (core.Worktree, bool, error) {
	ctx = s.commandContext(ctx)
	return s.git.EnsurePullRequestWorktree(ctx, cfg, pullRequest)
}

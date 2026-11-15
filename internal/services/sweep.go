package services

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// Sweeping removes worktrees that are finished in practice: nothing
// uncommitted in them and nobody has worked there for a while. The branch is
// never touched, so every commit stays reachable and the worktree can be
// created again at any time.

type SweepCandidate = core.SweepCandidate

// sweepCandidates lists the worktrees of the repository cfg is scoped to that
// sweep may remove. Status is read fresh for each of them: a cached "clean"
// is not a reason to delete anything.
func sweepCandidates(ctx context.Context, cfg Config, olderThan time.Duration, now time.Time) ([]SweepCandidate, error) {
	worktrees, err := discoverWorktrees(ctx, cfg)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-olderThan)

	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		candidates []SweepCandidate
		limit      = make(chan struct{}, 8)
	)
	for _, wt := range worktrees {
		if !sweepable(wt) {
			continue
		}
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			lastActivity := worktreeLastActivity(ctx, wt.Path)
			// A worktree whose age cannot be read is never assumed to be old.
			if lastActivity.IsZero() || lastActivity.After(cutoff) {
				return
			}
			if rebaseInProgress(wt.Path) {
				return
			}
			if warnings := readWorktreeStatus(ctx, &wt); len(warnings) > 0 || wt.LoadError != "" || wt.Status.Dirty {
				return
			}
			mu.Lock()
			candidates = append(candidates, SweepCandidate{Worktree: wt, LastActivity: lastActivity})
			mu.Unlock()
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].LastActivity.Equal(candidates[j].LastActivity) {
			return candidates[i].LastActivity.Before(candidates[j].LastActivity)
		}
		return candidates[i].Worktree.Path < candidates[j].Worktree.Path
	})
	return candidates, nil
}

// sweepable rules out what sweep never removes whatever its age: the main
// checkout, worktrees outside the managed folder, locked ones, entries whose
// folder is gone, and detached worktrees, whose commits no branch may hold.
func sweepable(wt Worktree) bool {
	return wt.Managed && !wt.IsMain && !wt.Locked && !wt.Missing && !wt.Prunable && !wt.Detached && wt.Branch != ""
}

// worktreeLastActivity is when HEAD last moved in a worktree: its creation, a
// commit, checkout, pull, merge or rebase. Reading files or running `git
// status` does not count, which is the point: a clean worktree nothing was
// committed to since has no work left in it.
func worktreeLastActivity(ctx context.Context, worktreePath string) time.Time {
	gitDir := worktreeGitDir(worktreePath)
	for _, name := range []string{filepath.Join("logs", "HEAD"), "HEAD"} {
		if info, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			return info.ModTime()
		}
	}
	// Without a reflog the last commit is the only trace left.
	output, err := runCommand(ctx, worktreePath, "git", "-C", worktreePath, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(output), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

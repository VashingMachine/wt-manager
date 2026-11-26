package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

// ageWorktree makes a worktree look untouched for the given time by dating
// the last movement of its HEAD back.
func ageWorktree(t *testing.T, path string, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age)
	if err := os.Chtimes(filepath.Join(worktreeGitDir(path), "logs", "HEAD"), at, at); err != nil {
		t.Fatalf("age %s: %v", path, err)
	}
}

func sweepNames(candidates []core.SweepCandidate) []string {
	var names []string
	for _, candidate := range candidates {
		names = append(names, candidate.Worktree.Name)
	}
	return names
}

const day = 24 * time.Hour

// Assures that sweep offers only clean worktrees untouched for longer than the limit, oldest first, and never the main checkout, a locked, detached or unmanaged worktree, or one with uncommitted work.
func TestSweepCandidatesAreCleanAndOld(t *testing.T) {
	cfg := testkit.GitRepo(t)
	ctx := context.Background()
	repo := cfg.ActiveProfile.RepositoryPath
	add := func(name string, age time.Duration) string {
		path := core.CreateWorktreePath(cfg, name)
		if _, err := createWorktreeWith(ctx, cfg, name, path, noFetch); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		ageWorktree(t, path, age)
		return path
	}

	add("old", 10*day)
	add("older", 30*day)
	add("recent", 2*day)
	testkit.WriteRepoFile(t, add("old-untracked", 10*day), "notes.txt", "wip\n")
	testkit.WriteRepoFile(t, add("old-modified", 10*day), "README.md", "changed\n")
	testkit.RunGit(t, repo, "worktree", "lock", add("old-locked", 10*day))
	testkit.RunGit(t, add("old-detached", 10*day), "checkout", "--detach")
	ageWorktree(t, core.CreateWorktreePath(cfg, "old-detached"), 10*day)

	outside := filepath.Join(filepath.Dir(cfg.ActiveProfile.WorktreesDir), "elsewhere")
	testkit.RunGit(t, repo, "worktree", "add", "-b", "old-unmanaged", outside)
	ageWorktree(t, outside, 10*day)
	ageWorktree(t, repo, 10*day)

	candidates, err := sweepCandidates(ctx, cfg, 7*day, time.Now())
	if err != nil {
		t.Fatalf("sweepCandidates() error = %v", err)
	}
	if got, want := sweepNames(candidates), []string{"older", "old"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if age := time.Since(candidates[0].LastActivity); age < 29*day || age > 31*day {
		t.Fatalf("last activity of the oldest candidate is %s ago, want about 30 days", age)
	}
}

// Assures that a commit in a worktree counts as activity, so work done yesterday is not swept although the worktree was created long ago.
func TestSweepActivityFollowsCommits(t *testing.T) {
	cfg := testkit.GitRepo(t)
	ctx := context.Background()
	path := core.CreateWorktreePath(cfg, "busy")
	if _, err := createWorktreeWith(ctx, cfg, "busy", path, noFetch); err != nil {
		t.Fatal(err)
	}
	ageWorktree(t, path, 30*day)
	testkit.CommitFile(t, path, "work.txt", "work\n", "fresh work")

	candidates, err := sweepCandidates(ctx, cfg, 7*day, time.Now())
	if err != nil || len(candidates) != 0 {
		t.Fatalf("sweepCandidates() = %v, %v; want nothing", sweepNames(candidates), err)
	}
}

// Assures that a swept candidate is removed without force and its branch stays, so nothing committed is lost.
func TestSweptWorktreeKeepsItsBranch(t *testing.T) {
	cfg := testkit.GitRepo(t)
	ctx := context.Background()
	path := core.CreateWorktreePath(cfg, "finished")
	if _, err := createWorktreeWith(ctx, cfg, "finished", path, noFetch); err != nil {
		t.Fatal(err)
	}
	testkit.CommitFile(t, path, "work.txt", "work\n", "finished work")
	ageWorktree(t, path, 10*day)

	candidates, err := sweepCandidates(ctx, cfg, 7*day, time.Now())
	if err != nil || len(candidates) != 1 {
		t.Fatalf("sweepCandidates() = %v, %v", sweepNames(candidates), err)
	}
	if err := removeWorktree(ctx, cfg, candidates[0].Worktree); err != nil {
		t.Fatalf("removeWorktree() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree folder still exists: %v", err)
	}
	if !gitRefExists(ctx, cfg.ActiveProfile.RepositoryPath, "refs/heads/finished") {
		t.Fatal("sweeping deleted the branch")
	}
}

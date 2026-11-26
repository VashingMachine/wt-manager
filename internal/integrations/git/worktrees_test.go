package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/fspath"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

func TestRepoRootFromNestedDir(t *testing.T) {
	cfg := testkit.GitRepo(t)
	nested := filepath.Join(cfg.ActiveProfile.RepositoryPath, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	repo, ok := repoRootFromDir(context.Background(), nested)
	if !ok {
		t.Fatal("repoRootFromDir() ok = false")
	}
	if fspath.Canonical(repo) != fspath.Canonical(cfg.ActiveProfile.RepositoryPath) {
		t.Fatalf("repoRootFromDir() = %q, want %q", repo, cfg.ActiveProfile.RepositoryPath)
	}
}

func TestCreateWorktreeExistingLocalBranch(t *testing.T) {
	cfg := testkit.GitRepo(t)
	testkit.RunGit(t, cfg.ActiveProfile.RepositoryPath, "branch", "local-feature")

	path := core.CreateWorktreePath(cfg, "local-feature")
	existing, err := createWorktree(context.Background(), cfg, "local-feature", path)
	if err != nil {
		t.Fatalf("createWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	testkit.AssertGitBranch(t, path, "local-feature")
}

func TestCreateWorktreeRemoteTrackingBranch(t *testing.T) {
	cfg := testkit.GitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	testkit.WriteRepoFile(t, repo, "remote.txt", "remote\n")
	testkit.RunGit(t, repo, "checkout", "-b", "remote-feature")
	testkit.RunGit(t, repo, "add", ".")
	testkit.RunGit(t, repo, "commit", "-m", "remote feature")
	testkit.RunGit(t, repo, "push", "origin", "remote-feature")
	testkit.RunGit(t, repo, "checkout", "main")
	testkit.RunGit(t, repo, "branch", "-D", "remote-feature")

	path := core.CreateWorktreePath(cfg, "remote-feature")
	existing, err := createWorktree(context.Background(), cfg, "remote-feature", path)
	if err != nil {
		t.Fatalf("createWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	testkit.AssertGitBranch(t, path, "remote-feature")
	upstream := strings.TrimSpace(testkit.RunGit(t, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"))
	if upstream != "origin/remote-feature" {
		t.Fatalf("upstream = %q, want origin/remote-feature", upstream)
	}
}

func TestCreateWorktreeNewBranchFallback(t *testing.T) {
	cfg := testkit.GitRepo(t)

	path := core.CreateWorktreePath(cfg, "new-feature")
	existing, err := createWorktree(context.Background(), cfg, "new-feature", path)
	if err != nil {
		t.Fatalf("createWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	testkit.AssertGitBranch(t, path, "new-feature")
}

func TestCreateWorktreeKnownPathSelectsExisting(t *testing.T) {
	cfg := testkit.GitRepo(t)
	path := core.CreateWorktreePath(cfg, "known-feature")
	if existing, err := createWorktree(context.Background(), cfg, "known-feature", path); err != nil || existing {
		t.Fatalf("initial createWorktree() existing=%v error=%v", existing, err)
	}

	existing, err := createWorktree(context.Background(), cfg, "known-feature", path)
	if err != nil {
		t.Fatalf("createWorktree() error = %v", err)
	}
	if !existing {
		t.Fatal("existing = false, want true")
	}
}

func TestEnsurePullRequestWorktreeFetchesPRRef(t *testing.T) {
	cfg := testkit.GitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	testkit.WriteRepoFile(t, repo, "pr.txt", "pr\n")
	testkit.RunGit(t, repo, "checkout", "-b", "remote-pr-branch")
	testkit.RunGit(t, repo, "add", ".")
	testkit.RunGit(t, repo, "commit", "-m", "pr branch")
	testkit.RunGit(t, repo, "push", "origin", "HEAD:refs/pull/42/head")
	testkit.RunGit(t, repo, "checkout", "main")

	pr := testkit.RemotePR(42, "OPEN", "alice", "PR branch", "fork-branch")
	wt, existing, err := ensurePullRequestWorktree(context.Background(), cfg, pr)
	if err != nil {
		t.Fatalf("ensurePullRequestWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	testkit.AssertGitBranch(t, wt.Path, "pr-42-fork-branch")
	if _, err := os.Stat(filepath.Join(wt.Path, "pr.txt")); err != nil {
		t.Fatalf("PR file missing from worktree: %v", err)
	}
}

func TestEnsurePullRequestWorktreeReusesExisting(t *testing.T) {
	cfg := testkit.GitRepo(t)
	pr := testkit.RemotePR(42, "OPEN", "alice", "PR branch", "existing-pr")
	path := core.CreateWorktreePath(cfg, "pr-42-existing-pr")
	testkit.RunGit(t, cfg.ActiveProfile.RepositoryPath, "branch", "pr-42-existing-pr")
	testkit.RunGit(t, cfg.ActiveProfile.RepositoryPath, "worktree", "add", path, "pr-42-existing-pr")

	wt, existing, err := ensurePullRequestWorktree(context.Background(), cfg, pr)
	if err != nil {
		t.Fatalf("ensurePullRequestWorktree() error = %v", err)
	}
	if !existing {
		t.Fatal("existing = false, want true")
	}
	if fspath.Canonical(wt.Path) != fspath.Canonical(path) {
		t.Fatalf("path = %q, want %q", wt.Path, path)
	}
}

func TestEnsurePullRequestWorktreePrefersExistingHeadBranchWorktree(t *testing.T) {
	cfg := testkit.GitRepo(t)
	branch := "feature-existing"
	testkit.RunGit(t, cfg.ActiveProfile.RepositoryPath, "branch", branch)
	existingPath := core.CreateWorktreePath(cfg, branch)
	testkit.RunGit(t, cfg.ActiveProfile.RepositoryPath, "worktree", "add", existingPath, branch)

	pr := testkit.RemotePR(42, "OPEN", "alice", "Existing branch PR", branch)
	wt, existing, err := ensurePullRequestWorktree(context.Background(), cfg, pr)
	if err != nil {
		t.Fatalf("ensurePullRequestWorktree() error = %v", err)
	}
	if !existing {
		t.Fatal("existing = false, want true")
	}
	if fspath.Canonical(wt.Path) != fspath.Canonical(existingPath) {
		t.Fatalf("path = %q, want existing branch worktree %q", wt.Path, existingPath)
	}
	if fallback := core.CreateWorktreePath(cfg, "pr-42-"+branch); knownWorktreePath(context.Background(), cfg.ActiveProfile.RepositoryPath, fallback) {
		t.Fatalf("created fallback PR worktree %q instead of reusing %q", fallback, existingPath)
	}
}

func TestCreateWorktreeConflictingPath(t *testing.T) {
	cfg := testkit.GitRepo(t)
	path := core.CreateWorktreePath(cfg, "conflict-feature")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if _, err := createWorktree(context.Background(), cfg, "conflict-feature", path); err == nil {
		t.Fatal("createWorktree() error = nil, want error")
	}
}

// Assures that creating a worktree for an existing local branch does not wait on a network fetch.
func TestCreateWorktreeForLocalBranchSkipsFetch(t *testing.T) {
	cfg := testkit.GitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	testkit.RunGit(t, repo, "branch", "local-only")
	fetches := 0
	fetch := func(context.Context, string, string, bool) bool { fetches++; return true }

	if _, err := createWorktreeWith(context.Background(), cfg, "local-only", core.CreateWorktreePath(cfg, "local-only"), fetch); err != nil {
		t.Fatalf("createWorktreeWith() error = %v", err)
	}
	if fetches != 0 {
		t.Fatalf("fetches = %d, want none for a local branch", fetches)
	}
	if _, err := createWorktreeWith(context.Background(), cfg, "brand-new", core.CreateWorktreePath(cfg, "brand-new"), fetch); err != nil {
		t.Fatalf("createWorktreeWith() error = %v", err)
	}
	if fetches == 0 {
		t.Fatal("a new branch was created without fetching the remote first")
	}
}

// Assures that queued worktree creations reuse a fetch from moments ago, unless forced.
func TestFetchCoalescerReusesRecentFetch(t *testing.T) {
	now := time.Now()
	coalescer := fetchCoalescer{now: func() time.Time { return now }}
	repo := t.TempDir()
	ctx := context.Background()
	if !coalescer.fetch(ctx, repo, "origin", false) {
		t.Fatal("first fetch was skipped")
	}
	if coalescer.fetch(ctx, repo, "origin", false) {
		t.Fatal("fetch within the reuse window was not skipped")
	}
	if !coalescer.fetch(ctx, repo, "origin", true) {
		t.Fatal("forced fetch was skipped")
	}
	now = now.Add(fetchReuseWindow + time.Second)
	if !coalescer.fetch(ctx, repo, "origin", false) {
		t.Fatal("fetch after the reuse window was skipped")
	}
}

// Assures that a reused fetch never hides a remote branch pushed after it: creation still tracks the remote branch.
func TestCreateWorktreeRefetchesWhenReusedFetchMissesBranch(t *testing.T) {
	cfg := testkit.GitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	testkit.WriteRepoFile(t, repo, "late.txt", "late\n")
	testkit.RunGit(t, repo, "checkout", "-b", "late-branch")
	testkit.RunGit(t, repo, "add", ".")
	testkit.RunGit(t, repo, "commit", "-m", "late")
	testkit.RunGit(t, repo, "push", "origin", "late-branch")
	testkit.RunGit(t, repo, "checkout", "main")
	testkit.RunGit(t, repo, "branch", "-D", "late-branch")
	testkit.RunGit(t, repo, "update-ref", "-d", "refs/remotes/origin/late-branch")

	client := New()
	client.fetches.last = map[string]time.Time{repo + "\x00origin": time.Now()}
	path := core.CreateWorktreePath(cfg, "late-branch")
	if _, err := client.CreateWorktree(context.Background(), cfg, "late-branch", path); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	upstream := strings.TrimSpace(testkit.RunGit(t, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"))
	if upstream != "origin/late-branch" {
		t.Fatalf("upstream = %q, want origin/late-branch", upstream)
	}
}

// Assures that a repository's remotes are listed, so the remote question can default to one that exists.
func TestGitRemotesListsRepositoryRemotes(t *testing.T) {
	cfg := testkit.GitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	testkit.RunGit(t, repo, "remote", "add", "upstream", "git@github.com:upstream/app.git")
	remotes := gitRemotes(context.Background(), repo)
	if strings.Join(remotes, ",") != "origin,upstream" {
		t.Fatalf("remotes = %v, want origin and upstream", remotes)
	}
	if got := gitRemotes(context.Background(), t.TempDir()); len(got) != 0 {
		t.Fatalf("remotes outside a repository = %v", got)
	}
}

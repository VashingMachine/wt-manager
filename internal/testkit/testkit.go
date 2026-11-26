// Package testkit holds the fixtures tests of several packages share: a
// throwaway git repository with a remote, scripted executables that stand in
// for gh or acli on PATH, and small assertions. Only tests import it.
package testkit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// GitRepo creates a repository with one commit on main, a bare remote named
// origin it is pushed to, and an empty worktrees folder, and returns a Config
// scoped to it.
func GitRepo(t *testing.T) core.Config {
	t.Helper()

	root := t.TempDir()
	repo := filepath.Join(root, "app")
	worktrees := filepath.Join(root, "worktrees")
	remote := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	if err := os.MkdirAll(worktrees, 0o755); err != nil {
		t.Fatalf("MkdirAll(worktrees) error = %v", err)
	}

	RunGit(t, root, "init", "--bare", remote)
	RunGit(t, repo, "init")
	RunGit(t, repo, "config", "user.email", "test@example.com")
	RunGit(t, repo, "config", "user.name", "Test User")
	RunGit(t, repo, "checkout", "-b", "main")
	WriteRepoFile(t, repo, "README.md", "hello\n")
	RunGit(t, repo, "add", ".")
	RunGit(t, repo, "commit", "-m", "initial")
	RunGit(t, repo, "remote", "add", "origin", remote)
	RunGit(t, repo, "push", "-u", "origin", "main")
	RunGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	RunGit(t, repo, "remote", "set-head", "origin", "-a")

	return core.Config{
		HomeDir: root,
		ActiveProfile: core.RepositoryProfile{
			Name:           "test",
			RepositoryPath: repo,
			WorktreesDir:   worktrees,
			RemoteName:     "origin",
		},
	}
}

// WriteRepoFile writes a file inside a repository or worktree.
func WriteRepoFile(t *testing.T, repo string, name string, content string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

// RunGit runs git in dir and fails the test when the command fails.
func RunGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=safe.bareRepository",
		"GIT_CONFIG_VALUE_0=all",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(output)))
	}
	return string(output)
}

// AssertGitBranch fails the test unless repo has branch checked out.
func AssertGitBranch(t *testing.T, repo string, branch string) {
	t.Helper()
	got := strings.TrimSpace(RunGit(t, repo, "branch", "--show-current"))
	if got != branch {
		t.Fatalf("branch = %q, want %q", got, branch)
	}
}

// RemotePR builds a pull request as the PR list shows it.
func RemotePR(number int, state string, author string, title string, branch string) core.RemotePullRequest {
	return core.RemotePullRequest{
		Number:      number,
		Title:       title,
		URL:         fmt.Sprintf("https://github.com/owner/repo/pull/%d", number),
		State:       state,
		HeadRefName: branch,
		BaseRefName: "main",
		Author:      core.GitHubActor{Login: author},
		UpdatedAt:   "2026-05-05T12:00:00Z",
	}
}

// WithCWD runs the rest of the test from dir.
func WithCWD(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s) error = %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore cwd error = %v", err)
		}
	})
}

// WriteExecutable writes a script that tests put on PATH in place of a real tool.
func WriteExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

// CommitFile writes and commits one file, and returns the new commit.
func CommitFile(t *testing.T, dir string, name string, content string, message string) string {
	t.Helper()
	WriteRepoFile(t, dir, name, content)
	RunGit(t, dir, "add", ".")
	RunGit(t, dir, "commit", "-m", message)
	return strings.TrimSpace(RunGit(t, dir, "rev-parse", "HEAD"))
}

// GitLines returns the non-empty output lines of a git command.
func GitLines(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	output := strings.TrimSpace(RunGit(t, dir, args...))
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

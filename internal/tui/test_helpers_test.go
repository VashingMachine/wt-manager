package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testGitRepo(t *testing.T) Config {
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

	runGit(t, root, "init", "--bare", remote)
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "checkout", "-b", "main")
	writeRepoFile(t, repo, "README.md", "hello\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-u", "origin", "main")
	runGit(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	runGit(t, repo, "remote", "set-head", "origin", "-a")

	return Config{
		HomeDir: root,
		ActiveProfile: RepositoryProfile{
			Name:           "test",
			RepositoryPath: repo,
			WorktreesDir:   worktrees,
			RemoteName:     "origin",
		},
	}
}

func writeRepoFile(t *testing.T, repo string, name string, content string) {
	t.Helper()
	path := filepath.Join(repo, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(output)))
	}
	return string(output)
}

func remotePRFixture(number int, state string, author string, title string, branch string) RemotePullRequest {
	return RemotePullRequest{
		Number:      number,
		Title:       title,
		URL:         fmt.Sprintf("https://github.com/owner/repo/pull/%d", number),
		State:       state,
		HeadRefName: branch,
		BaseRefName: "main",
		Author:      GitHubActor{Login: author},
		UpdatedAt:   "2026-05-05T12:00:00Z",
	}
}

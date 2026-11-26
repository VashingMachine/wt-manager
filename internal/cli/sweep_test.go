package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// sweepSandbox is the "shop" project with one repository and three worktrees:
// "old" (untouched for 20 days), "old-dirty" (as old, with an uncommitted
// file) and "recent" (untouched for 2 days). The config sweeps after 7 days.
func sweepSandbox(t *testing.T) (sandbox, string) {
	t.Helper()
	s := newSandbox(t)
	repo := s.repo(t, "api")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "config", "user.name", "Test User")
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "initial")
	for name, days := range map[string]int{"old": 20, "old-dirty": 20, "recent": 2} {
		git(t, repo, "worktree", "add", "-q", "-b", name, s.worktree(name))
		at := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
		if err := os.Chtimes(filepath.Join(repo, ".git", "worktrees", name, "logs", "HEAD"), at, at); err != nil {
			t.Fatalf("age %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.worktree("old-dirty"), "notes.txt"), []byte("wip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := core.DefaultAppConfig()
	app.DefaultProfile = "shop"
	app.Profiles = []core.RepositoryProfile{{Name: "shop", RepositoryPath: "~/projects/api", WorktreesDir: "~/worktrees/api", RemoteName: "origin"}}
	s.writeConfig(t, app)
	return s, repo
}

func (s sandbox) worktree(name string) string {
	return filepath.Join(s.home, "worktrees", "api", name)
}

func (s sandbox) sweep(t *testing.T, workingDir string, answers string, args ...string) (int, string) {
	t.Helper()
	var out strings.Builder
	code := Run(Options{
		Args:       append([]string{"sweep"}, args...),
		In:         strings.NewReader(answers),
		Out:        &out,
		HomeDir:    s.home,
		WorkingDir: workingDir,
		Services:   s.svc,
	})
	return code, out.String()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Assures that a dry run lists only the clean worktree past the configured age and removes nothing.
func TestSweepDryRunListsWithoutRemoving(t *testing.T) {
	s, repo := sweepSandbox(t)

	code, out := s.sweep(t, repo, "", "--dry-run")
	if code != 0 || !strings.Contains(out, "untouched for 7+ days") || !strings.Contains(out, "~/worktrees/api/old\n") || !strings.Contains(out, "20d") || !strings.Contains(out, "Dry run") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if strings.Contains(out, "old-dirty") || strings.Contains(out, "recent") {
		t.Fatalf("a dirty or recent worktree was offered:\n%s", out)
	}
	if !exists(s.worktree("old")) {
		t.Fatal("a dry run removed a worktree")
	}
}

// Assures that --yes removes the old clean worktree without asking, keeps its branch, and leaves dirty and recent worktrees alone.
func TestSweepYesRemovesOldCleanWorktrees(t *testing.T) {
	s, repo := sweepSandbox(t)

	code, out := s.sweep(t, repo, "", "--yes")
	if code != 0 || !strings.Contains(out, "removed  ~/worktrees/api/old") || strings.Contains(out, "[y/N]") {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if exists(s.worktree("old")) || !exists(s.worktree("old-dirty")) || !exists(s.worktree("recent")) {
		t.Fatalf("worktrees after sweep: old=%v old-dirty=%v recent=%v", exists(s.worktree("old")), exists(s.worktree("old-dirty")), exists(s.worktree("recent")))
	}
	git(t, repo, "rev-parse", "--verify", "--quiet", "refs/heads/old")
}

// Assures that without --yes the command asks first and removes nothing unless the answer is yes.
func TestSweepAsksBeforeRemoving(t *testing.T) {
	s, repo := sweepSandbox(t)

	code, out := s.sweep(t, repo, "\n")
	if code != 0 || !strings.Contains(out, "Remove 1 worktree? Their branches stay. [y/N]") || !strings.Contains(out, "Nothing was removed") || !exists(s.worktree("old")) {
		t.Fatalf("declined sweep: exit %d, old exists=%v, output:\n%s", code, exists(s.worktree("old")), out)
	}
	code, out = s.sweep(t, repo, "y\n")
	if code != 0 || exists(s.worktree("old")) {
		t.Fatalf("confirmed sweep: exit %d, old exists=%v, output:\n%s", code, exists(s.worktree("old")), out)
	}
}

// Assures that --days overrides the configured age, and that the configured age applies when the command runs outside the project.
func TestSweepAgeFromFlagOrConfig(t *testing.T) {
	s, _ := sweepSandbox(t)

	code, out := s.sweep(t, s.home, "", "--dry-run", "--days", "1")
	if code != 0 || !strings.Contains(out, "untouched for 1+ days") || !strings.Contains(out, "~/worktrees/api/recent") {
		t.Fatalf("--days 1: exit %d, output:\n%s", code, out)
	}
	code, out = s.sweep(t, s.home, "", "--dry-run", "--days", "60")
	if code != 0 || !strings.Contains(out, "Nothing to sweep in shop") || !strings.Contains(out, "60 days") {
		t.Fatalf("--days 60: exit %d, output:\n%s", code, out)
	}

	app := s.readConfig(t)
	app.Sweep.OlderThanDays = 30
	s.writeConfig(t, app)
	code, out = s.sweep(t, s.home, "", "--dry-run")
	if code != 0 || !strings.Contains(out, "Nothing to sweep in shop") || !strings.Contains(out, "30 days") {
		t.Fatalf("configured 30 days: exit %d, output:\n%s", code, out)
	}
}

// Assures that an unknown project or argument is reported instead of sweeping something else.
func TestSweepRejectsUnknownProjectAndArguments(t *testing.T) {
	s, repo := sweepSandbox(t)

	code, out := s.sweep(t, repo, "", "--yes", "--project", "nope")
	if code != 1 || !strings.Contains(out, `no project named "nope"`) || !exists(s.worktree("old")) {
		t.Fatalf("unknown project: exit %d, output:\n%s", code, out)
	}
	code, out = s.sweep(t, repo, "", "--force")
	if code != 2 || !strings.Contains(out, "wt-manager sweep") || !exists(s.worktree("old")) {
		t.Fatalf("unknown flag: exit %d, output:\n%s", code, out)
	}
}

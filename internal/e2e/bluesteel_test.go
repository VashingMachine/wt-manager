package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
)

const e2eEnv = "WT_MANAGER_E2E"
const bluesteelProfile = "bluesteel"

func TestBluesteelProfileWithRealGitAndGitHubCLI(t *testing.T) {
	if os.Getenv(e2eEnv) != "1" {
		t.Skipf("set %s=1 to run real git/gh bluesteel e2e tests", e2eEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	requireExecutable(t, "git")
	requireExecutable(t, "gh")
	requireCommand(t, ctx, "gh", "auth", "status")

	svc := services.NewService()
	cfg := loadBluesteelConfig(t, svc)

	if cfg.RepoSlug == "" {
		t.Fatalf("profile %q has no githubRepo and it could not be inferred", bluesteelProfile)
	}
	if _, err := os.Stat(cfg.ActiveProfile.RepositoryPath); err != nil {
		t.Fatalf("repositoryPath %s is unavailable: %v", cfg.ActiveProfile.RepositoryPath, err)
	}
	if _, err := os.Stat(cfg.ActiveProfile.WorktreesDir); err != nil {
		t.Fatalf("worktreesDir %s is unavailable: %v", cfg.ActiveProfile.WorktreesDir, err)
	}

	repoRoot, ok := svc.RepoRootFromDir(ctx, cfg.ActiveProfile.RepositoryPath)
	if !ok {
		t.Fatalf("repositoryPath %s is not a git repository", cfg.ActiveProfile.RepositoryPath)
	}
	if svc.CanonicalPath(repoRoot) != svc.CanonicalPath(cfg.ActiveProfile.RepositoryPath) {
		t.Fatalf("repo root = %q, want %q", repoRoot, cfg.ActiveProfile.RepositoryPath)
	}

	worktrees, warning, err := svc.CollectWorktrees(ctx, cfg, false)
	if err != nil {
		t.Fatalf("CollectWorktrees() error = %v", err)
	}
	if warning != "" {
		t.Logf("CollectWorktrees() warning: %s", warning)
	}
	if len(worktrees) == 0 {
		t.Fatal("CollectWorktrees() returned no managed worktrees")
	}
	assertHasMainWorktree(t, worktrees, cfg.ActiveProfile.RepositoryPath)

	currentUser, err := svc.LoadGitHubCurrentUser(ctx, cfg)
	if err != nil {
		t.Fatalf("LoadGitHubCurrentUser() error = %v", err)
	}
	if strings.TrimSpace(currentUser) == "" {
		t.Fatal("LoadGitHubCurrentUser() returned an empty login")
	}

	prs, err := svc.LoadRemotePullRequests(ctx, cfg)
	if err != nil {
		t.Fatalf("LoadRemotePullRequests() error = %v", err)
	}
	if len(prs) == 0 {
		t.Fatal("LoadRemotePullRequests() returned no PRs for bluesteel")
	}
	for _, pr := range prs {
		if pr.Number == 0 || pr.Title == "" || pr.URL == "" || pr.Author.Login == "" {
			t.Fatalf("remote PR has missing core fields: %#v", pr)
		}
		if pr.State != "OPEN" && pr.State != "CLOSED" && pr.State != "MERGED" {
			t.Fatalf("remote PR state = %q, want OPEN/CLOSED/MERGED for PR #%d", pr.State, pr.Number)
		}
	}

	detail, err := svc.LoadRemotePullRequestDetail(ctx, cfg, prs[0].Number)
	if err != nil {
		t.Fatalf("LoadRemotePullRequestDetail(%d) error = %v", prs[0].Number, err)
	}
	if detail.Number != prs[0].Number || detail.Title == "" || detail.URL == "" {
		t.Fatalf("PR detail mismatch: list=%#v detail=%#v", prs[0], detail)
	}
	if detail.HeadRefName == "" || detail.BaseRefName == "" {
		t.Fatalf("PR detail missing branch refs: %#v", detail)
	}
	if detail.Diff == "" {
		t.Fatalf("PR detail has empty diff for PR #%d", detail.Number)
	}
	if detail.ChangedFiles > 0 && len(detail.Files) == 0 {
		t.Fatalf("PR detail changedFiles=%d but Files is empty", detail.ChangedFiles)
	}
}

func loadBluesteelConfig(t *testing.T, svc *services.Service) core.Config {
	t.Helper()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	configPath := filepath.Join(homeDir, ".wt-manager.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read %s: %v", configPath, err)
	}

	var appConfig core.AppConfig
	if err := json.Unmarshal(raw, &appConfig); err != nil {
		t.Fatalf("decode %s: %v", configPath, err)
	}
	appConfig, _, err = svc.NormalizeAppConfig(appConfig)
	if err != nil {
		t.Fatalf("NormalizeAppConfig() error = %v", err)
	}

	profile := svc.FindProfile(appConfig.Profiles, bluesteelProfile)
	if profile == nil {
		t.Fatalf("profile %q missing from %s", bluesteelProfile, configPath)
	}
	resolved := svc.ResolveProfilePaths(*profile, homeDir)
	repoSlug := resolved.GitHubRepo
	if repoSlug == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		repoSlug = svc.InferGitHubRepo(ctx, resolved)
	}

	return core.Config{
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		App:           appConfig,
		ActiveProfile: resolved,
		RepoSlug:      repoSlug,
		ConfigExists:  true,
	}
}

func requireExecutable(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("%s not found in PATH: %v", name, err)
	}
}

func requireCommand(t *testing.T, ctx context.Context, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}

func assertHasMainWorktree(t *testing.T, worktrees []core.Worktree, repoPath string) {
	t.Helper()
	for _, wt := range worktrees {
		if wt.IsMain && wt.Path == repoPath {
			return
		}
	}
	for _, wt := range worktrees {
		if wt.IsMain {
			t.Fatalf("main worktree path = %q, want %q", wt.Path, repoPath)
		}
	}
	t.Fatalf("no main worktree found in %s", fmtWorktreePaths(worktrees))
}

func fmtWorktreePaths(worktrees []core.Worktree) string {
	paths := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		paths = append(paths, fmt.Sprintf("%s:%s", wt.Name, wt.Path))
	}
	return strings.Join(paths, ", ")
}

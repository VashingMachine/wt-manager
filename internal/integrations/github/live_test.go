package github

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/integrations/configfile"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

const refreshTimeout = 45 * time.Second

func TestLiveGitHubProfileRemotePullRequests(t *testing.T) {
	cfg := liveProfileConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	prs, err := loadRemotePullRequests(ctx, cfg)
	if err != nil {
		t.Fatalf("loadRemotePullRequests() live error = %v", err)
	}
	if len(prs) == 0 {
		t.Fatalf("loadRemotePullRequests() returned no PRs for profile %s", cfg.ActiveProfile.Name)
	}
	for _, pr := range prs {
		if pr.Number == 0 || pr.Title == "" || pr.URL == "" || pr.Author.Login == "" {
			t.Fatalf("live PR has missing core fields: %#v", pr)
		}
		if pr.State != "OPEN" && pr.State != "CLOSED" && pr.State != "MERGED" {
			t.Fatalf("live PR state = %q, want OPEN/CLOSED/MERGED for PR #%d", pr.State, pr.Number)
		}
	}
}

func TestLiveGitHubProfileRemotePullRequestDetail(t *testing.T) {
	cfg := liveProfileConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	prs, err := loadRemotePullRequests(ctx, cfg)
	if err != nil {
		t.Fatalf("loadRemotePullRequests() live error = %v", err)
	}
	if len(prs) == 0 {
		t.Fatalf("loadRemotePullRequests() returned no PRs for profile %s", cfg.ActiveProfile.Name)
	}

	detail, err := loadRemotePullRequestDetail(ctx, cfg, prs[0].Number)
	if err != nil {
		t.Fatalf("loadRemotePullRequestDetail(%d) live error = %v", prs[0].Number, err)
	}
	if detail.Number != prs[0].Number || detail.Title == "" || detail.URL == "" {
		t.Fatalf("live PR detail mismatch: list=%#v detail=%#v", prs[0], detail)
	}
	if detail.HeadRefName == "" || detail.BaseRefName == "" {
		t.Fatalf("live PR detail missing branches: %#v", detail)
	}
	if detail.Diff == "" {
		t.Fatalf("live PR detail has empty diff for PR #%d", detail.Number)
	}
	if detail.ChangedFiles > 0 && len(detail.Files) == 0 {
		t.Fatalf("live PR detail changedFiles=%d but Files is empty", detail.ChangedFiles)
	}
}

func liveProfileConfig(t *testing.T) core.Config {
	t.Helper()
	if os.Getenv("WT_MANAGER_LIVE_GH") != "1" {
		t.Skip("set WT_MANAGER_LIVE_GH=1 to run live gh/profile integration tests")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skipf("gh not found: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := shell.Run(ctx, "", "gh", "auth", "status"); err != nil {
		t.Skipf("gh is not authenticated: %v", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	appConfig, configPath, existed, err := configfile.New().Load(homeDir)
	if err != nil {
		t.Fatalf("configfile Load() error = %v", err)
	}
	if !existed {
		t.Fatalf("%s does not exist", configPath)
	}
	profileName := strings.TrimSpace(os.Getenv("WT_MANAGER_LIVE_PROFILE"))
	if profileName == "" {
		profileName = "app"
	}
	profile := core.FindProfile(appConfig.Profiles, profileName)
	if profile == nil {
		t.Fatalf("%s profile missing from %s", profileName, configPath)
	}
	resolved := core.ResolveProfilePaths(*profile, homeDir)
	if resolved.GitHubRepo == "" {
		t.Fatalf("%s profile has no githubRepo in %s", profileName, configPath)
	}
	if _, err := os.Stat(resolved.RepositoryPath); err != nil {
		t.Fatalf("%s repositoryPath %s is unavailable: %v", profileName, resolved.RepositoryPath, err)
	}

	return core.Config{
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		App:           appConfig,
		ActiveProfile: resolved,
		RepoSlug:      resolved.GitHubRepo,
		ConfigExists:  true,
	}
}

package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

func TestDefaultConfigSetupModes(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	testkit.RunGit(t, repo, "init")
	testkit.RunGit(t, repo, "config", "user.email", "test@example.com")
	testkit.RunGit(t, repo, "config", "user.name", "Test User")
	testkit.WriteRepoFile(t, repo, "README.md", "hello\n")
	testkit.RunGit(t, repo, "add", ".")
	testkit.RunGit(t, repo, "commit", "-m", "initial")

	t.Setenv("HOME", root)
	testkit.WithCWD(t, repo)

	cfg, err := NewService().DefaultConfig(false)
	if err != nil {
		t.Fatalf("NewService().DefaultConfig() error = %v", err)
	}
	if !cfg.SetupNeeded || cfg.SetupRepo == "" {
		t.Fatalf("NewService().DefaultConfig() should enter setup for missing config in repo, got %#v", cfg)
	}

	appConfig := core.DefaultAppConfig()
	appConfig.DefaultProfile = "repo"
	appConfig.Profiles = []core.RepositoryProfile{{Name: "repo", RepositoryPath: repo, WorktreesDir: filepath.Join(root, "worktrees"), RemoteName: "origin"}}
	if err := NewService().WriteAppConfig(filepath.Join(root, ".wt-manager.json"), appConfig); err != nil {
		t.Fatalf("NewService().WriteAppConfig() error = %v", err)
	}

	cfg, err = NewService().DefaultConfig(false)
	if err != nil {
		t.Fatalf("NewService().DefaultConfig() configured error = %v", err)
	}
	if cfg.SetupNeeded {
		t.Fatalf("NewService().DefaultConfig() SetupNeeded = true for configured repo")
	}

	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("MkdirAll(other) error = %v", err)
	}
	testkit.RunGit(t, other, "init")
	testkit.WriteRepoFile(t, other, "README.md", "other\n")
	testkit.RunGit(t, other, "add", ".")
	testkit.RunGit(t, other, "config", "user.email", "test@example.com")
	testkit.RunGit(t, other, "config", "user.name", "Test User")
	testkit.RunGit(t, other, "commit", "-m", "initial")
	testkit.WithCWD(t, other)
	cfg, err = NewService().DefaultConfig(false)
	if err != nil {
		t.Fatalf("NewService().DefaultConfig() other error = %v", err)
	}
	if !cfg.SetupNeeded || !strings.Contains(cfg.SetupReason, "not configured") {
		t.Fatalf("NewService().DefaultConfig() should enter setup for unconfigured cwd repo, got %#v", cfg)
	}

	cfg, err = NewService().DefaultConfig(true)
	if err != nil {
		t.Fatalf("NewService().DefaultConfig(force) error = %v", err)
	}
	if !cfg.SetupNeeded || !strings.Contains(cfg.SetupReason, "Manual") {
		t.Fatalf("NewService().DefaultConfig(force) got %#v", cfg)
	}
}

// multiRepositoryConfig turns the test repository into a project with a second
// repository, resolved the way the app resolves the active project.
func multiRepositoryConfig(t *testing.T) core.Config {
	t.Helper()
	cfg := testkit.GitRepo(t)
	root := cfg.HomeDir
	web := filepath.Join(root, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatalf("MkdirAll(web) error = %v", err)
	}
	testkit.RunGit(t, web, "init")
	testkit.RunGit(t, web, "config", "user.email", "test@example.com")
	testkit.RunGit(t, web, "config", "user.name", "Test User")
	testkit.RunGit(t, web, "checkout", "-b", "main")
	testkit.WriteRepoFile(t, web, "README.md", "web\n")
	testkit.RunGit(t, web, "add", ".")
	testkit.RunGit(t, web, "commit", "-m", "initial")
	testkit.RunGit(t, web, "remote", "add", "origin", "git@github.com:owner/web.git")

	profile := core.RepositoryProfile{Name: "shop", Repositories: []core.Repository{
		{Name: "app", RepositoryPath: cfg.ActiveProfile.RepositoryPath, WorktreesDir: cfg.ActiveProfile.WorktreesDir},
		{Name: "web", RepositoryPath: web, WorktreesDir: cfg.ActiveProfile.WorktreesDir},
	}}
	app, _, err := core.NormalizeAppConfig(core.AppConfig{DefaultProfile: "shop", Profiles: []core.RepositoryProfile{profile}})
	if err != nil {
		t.Fatalf("core.NormalizeAppConfig() error = %v", err)
	}
	resolved := NewService().ResolveProject(context.Background(), app.Profiles[0], root)
	return core.Config{HomeDir: root, App: app, ActiveProfile: resolved, RepoSlug: resolved.GitHubRepo}
}

// Assures that every repository of a project gets its GitHub repository from its own remote.
func TestResolveProjectInfersEachRepositorySlug(t *testing.T) {
	cfg := multiRepositoryConfig(t)
	web := cfg.ActiveProfile.Repository("web")
	if web == nil || web.GitHubRepo != "owner/web" {
		t.Fatalf("web repository = %#v, want slug inferred from its remote", web)
	}
	if cfg.ForRepository("web").RepoSlug != "owner/web" {
		t.Fatal("scoping to web did not select its GitHub repository")
	}
}

// Assures that starting the app inside any repository of a project counts as configured.
func TestRepoConfiguredRecognizesEveryProjectRepository(t *testing.T) {
	cfg := multiRepositoryConfig(t)
	web := cfg.ActiveProfile.Repository("web").RepositoryPath
	if !repoConfigured(cfg.App, cfg.HomeDir, web) {
		t.Fatal("the second repository of a project was reported as not configured")
	}
	if repoConfigured(cfg.App, cfg.HomeDir, filepath.Join(cfg.HomeDir, "elsewhere")) {
		t.Fatal("an unrelated folder was reported as configured")
	}
}

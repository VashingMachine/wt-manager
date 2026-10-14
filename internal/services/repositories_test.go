package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// multiRepositoryConfig turns the test repository into a project with a second
// repository, resolved the way the app resolves the active project.
func multiRepositoryConfig(t *testing.T) Config {
	t.Helper()
	cfg := testGitRepo(t)
	root := cfg.HomeDir
	web := filepath.Join(root, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatalf("MkdirAll(web) error = %v", err)
	}
	runGit(t, web, "init")
	runGit(t, web, "config", "user.email", "test@example.com")
	runGit(t, web, "config", "user.name", "Test User")
	runGit(t, web, "checkout", "-b", "main")
	writeRepoFile(t, web, "README.md", "web\n")
	runGit(t, web, "add", ".")
	runGit(t, web, "commit", "-m", "initial")
	runGit(t, web, "remote", "add", "origin", "git@github.com:owner/web.git")

	profile := RepositoryProfile{Name: "shop", Repositories: []core.Repository{
		{Name: "app", RepositoryPath: cfg.ActiveProfile.RepositoryPath, WorktreesDir: cfg.ActiveProfile.WorktreesDir},
		{Name: "web", RepositoryPath: web, WorktreesDir: cfg.ActiveProfile.WorktreesDir},
	}}
	app, _, err := normalizeAppConfig(AppConfig{DefaultProfile: "shop", Profiles: []RepositoryProfile{profile}})
	if err != nil {
		t.Fatalf("normalizeAppConfig() error = %v", err)
	}
	resolved := resolveProject(context.Background(), app.Profiles[0], root)
	return Config{HomeDir: root, App: app, ActiveProfile: resolved, RepoSlug: resolved.GitHubRepo}
}

// Assures that a profile listing repositories also keeps the repository from its flat fields, listed first, and that repository defaults are filled in.
func TestNormalizeAppConfigMovesFlatRepositoryIntoList(t *testing.T) {
	app, changed, err := normalizeAppConfig(AppConfig{Profiles: []RepositoryProfile{{
		Name:           "shop",
		RepositoryPath: "~/src/api",
		WorktreesDir:   "~/wt/api",
		GitHubRepo:     "owner/api",
		Repositories:   []core.Repository{{RepositoryPath: " ~/src/web ", WorktreesDir: "~/wt/web"}},
		LastRepository: "web",
	}}})
	if err != nil {
		t.Fatalf("normalizeAppConfig() error = %v", err)
	}
	profile := app.Profiles[0]
	if !changed || profile.RepositoryPath != "" || profile.WorktreesDir != "" || profile.GitHubRepo != "" || profile.RemoteName != "" {
		t.Fatalf("flat fields were not moved: changed=%v profile=%#v", changed, profile)
	}
	want := []core.Repository{
		{Name: "api", RepositoryPath: "~/src/api", WorktreesDir: "~/wt/api", RemoteName: "origin", GitHubRepo: "owner/api"},
		{Name: "web", RepositoryPath: "~/src/web", WorktreesDir: "~/wt/web", RemoteName: "origin"},
	}
	if len(profile.Repositories) != 2 || profile.Repositories[0] != want[0] || profile.Repositories[1] != want[1] {
		t.Fatalf("repositories = %#v, want %#v", profile.Repositories, want)
	}
	if profile.LastRepository != "web" {
		t.Fatalf("LastRepository = %q, want the existing repository kept", profile.LastRepository)
	}
}

// Assures that invalid repository lists are rejected with an explanation, and an unknown last repository is forgotten.
func TestNormalizeAppConfigValidatesRepositories(t *testing.T) {
	for name, repositories := range map[string][]core.Repository{
		"duplicate name":    {{Name: "api", RepositoryPath: "/src/api", WorktreesDir: "/wt"}, {Name: "api", RepositoryPath: "/src/web", WorktreesDir: "/wt"}},
		"missing path":      {{Name: "api", WorktreesDir: "/wt"}},
		"missing worktrees": {{Name: "api", RepositoryPath: "/src/api"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := normalizeAppConfig(AppConfig{Profiles: []RepositoryProfile{{Name: "shop", Repositories: repositories}}})
			if err == nil || !strings.Contains(err.Error(), `profile "shop"`) {
				t.Fatalf("normalizeAppConfig() error = %v, want a repository error for shop", err)
			}
		})
	}

	app, changed, err := normalizeAppConfig(AppConfig{Profiles: []RepositoryProfile{{
		Name:           "shop",
		Repositories:   []core.Repository{{Name: "api", RepositoryPath: "/src/api", WorktreesDir: "/wt", RemoteName: "origin"}},
		LastRepository: "gone",
	}}})
	if err != nil || !changed || app.Profiles[0].LastRepository != "" {
		t.Fatalf("unknown last repository: changed=%v err=%v last=%q", changed, err, app.Profiles[0].LastRepository)
	}
}

// Assures that a resolved project lists every repository with expanded paths and scopes the flat fields to the first one.
func TestResolveProfilePathsListsRepositories(t *testing.T) {
	homeDir := t.TempDir()
	profile := resolveProfilePaths(RepositoryProfile{Name: "shop", Repositories: []core.Repository{
		{Name: "api", RepositoryPath: "~/src/api", WorktreesDir: "~/wt/api", RemoteName: "origin", GitHubRepo: "owner/api"},
		{Name: "web", RepositoryPath: "~/src/web", WorktreesDir: "~/wt/web"},
	}}, homeDir)
	if len(profile.Repositories) != 2 || profile.Repositories[1].RepositoryPath != filepath.Join(homeDir, "src", "web") || profile.Repositories[1].RemoteName != "origin" {
		t.Fatalf("repositories = %#v", profile.Repositories)
	}
	if profile.RepositoryPath != filepath.Join(homeDir, "src", "api") || profile.WorktreesDir != filepath.Join(homeDir, "wt", "api") || profile.GitHubRepo != "owner/api" {
		t.Fatalf("flat fields = %#v, want the first repository", profile)
	}

	single := resolveProfilePaths(RepositoryProfile{Name: "app", RepositoryPath: "~/src/app", WorktreesDir: "~/wt", RemoteName: "origin"}, homeDir)
	if len(single.Repositories) != 1 || single.Repositories[0].Name != "app" || single.Repositories[0].RepositoryPath != filepath.Join(homeDir, "src", "app") {
		t.Fatalf("single-repository project resolved to %#v", single.Repositories)
	}
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

// Assures that discovered worktrees record their repository and that main checkouts are named after their repository in multi-repository projects.
func TestDiscoverWorktreesLabelsRepository(t *testing.T) {
	cfg := multiRepositoryConfig(t)
	svc := NewService()
	for _, name := range []string{"app", "web"} {
		worktrees, err := svc.DiscoverWorktrees(context.Background(), cfg.ForRepository(name), false)
		if err != nil {
			t.Fatalf("DiscoverWorktrees(%s) error = %v", name, err)
		}
		if len(worktrees) != 1 || worktrees[0].Repository != name || worktrees[0].Name != name || !worktrees[0].IsMain {
			t.Fatalf("%s worktrees = %#v, want its main checkout labelled %s", name, worktrees, name)
		}
		cached, ok := svc.PeekWorktrees(cfg.ForRepository(name), false)
		if !ok || cached[0].Repository != name {
			t.Fatalf("cached %s worktrees = %#v", name, cached)
		}
	}

	single := testGitRepo(t)
	worktrees, err := svc.DiscoverWorktrees(context.Background(), single, false)
	if err != nil || len(worktrees) != 1 || worktrees[0].Name != "test" || worktrees[0].Repository != "app" {
		t.Fatalf("single-repository worktrees = %#v, err = %v; want main named after the profile", worktrees, err)
	}
}

// Assures that repositories sharing a worktrees folder get the repository name in new worktree folders, so one branch can exist in both.
func TestCreateWorktreePathPrefixesRepositoryInSharedFolder(t *testing.T) {
	cfg := multiRepositoryConfig(t)
	worktrees := cfg.ActiveProfile.WorktreesDir
	if got := createWorktreePath(cfg.ForRepository("web"), "APP-1/fix"); got != filepath.Join(worktrees, "web-APP-1-fix") {
		t.Fatalf("web path = %q", got)
	}
	appPath := createWorktreePath(cfg.ForRepository("app"), "APP-1/fix")
	webPath := createWorktreePath(cfg.ForRepository("web"), "APP-1/fix")
	for _, name := range []string{"app", "web"} {
		scoped := cfg.ForRepository(name)
		if _, err := createWorktree(context.Background(), scoped, "APP-1/fix", createWorktreePath(scoped, "APP-1/fix")); err != nil {
			t.Fatalf("createWorktree(%s) error = %v", name, err)
		}
	}
	assertGitBranch(t, appPath, "APP-1/fix")
	assertGitBranch(t, webPath, "APP-1/fix")

	separate := cfg
	separate.ActiveProfile.Repositories = append([]core.Repository(nil), cfg.ActiveProfile.Repositories...)
	separate.ActiveProfile.Repositories[1].WorktreesDir = filepath.Join(cfg.HomeDir, "web-worktrees")
	if got := createWorktreePath(separate.ForRepository("web"), "feature"); got != filepath.Join(cfg.HomeDir, "web-worktrees", "feature") {
		t.Fatalf("own folder path = %q, want no prefix", got)
	}
}

// Assures that reading the config for a reload normalizes it without rewriting the file the user is editing.
func TestReadAppConfigDoesNotRewriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".wt-manager.json")
	raw := `{"defaultProfile":"shop","profiles":[{"name":"shop","repositories":[{"repositoryPath":"/src/api","worktreesDir":"/wt"}]}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app, err := readAppConfig(path)
	if err != nil {
		t.Fatalf("readAppConfig() error = %v", err)
	}
	if repository := app.Profiles[0].Repositories[0]; repository.Name != "api" || repository.RemoteName != "origin" {
		t.Fatalf("repository = %#v, want defaults filled in", repository)
	}
	if data, _ := os.ReadFile(path); string(data) != raw {
		t.Fatalf("file was rewritten: %s", data)
	}
	if err := os.WriteFile(path, []byte(`{"profiles":[`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := readAppConfig(path); err == nil {
		t.Fatal("readAppConfig() accepted invalid JSON")
	}
}

// Assures that the config file is opened in the editor as a single file, keeping VS Code in its current window.
func TestOpenEditorFileOpensConfigPath(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(bin, "args.log")
	writeExecutable(t, filepath.Join(bin, "code"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+logPath+"\n")
	writeExecutable(t, filepath.Join(bin, "custom-editor"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+logPath+"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	config := filepath.Join(t.TempDir(), "my config.json")

	if err := openEditorFile(context.Background(), config, EditorTool{Name: "VS Code", Command: "code"}); err != nil {
		t.Fatalf("openEditorFile(code) error = %v", err)
	}
	if data, _ := os.ReadFile(logPath); strings.TrimSpace(string(data)) != config {
		t.Fatalf("code args = %q, want only the config path", data)
	}
	if err := openEditorFile(context.Background(), config, EditorTool{Name: "Custom", Command: "custom-editor --wait"}); err != nil {
		t.Fatalf("openEditorFile(custom) error = %v", err)
	}
	if data, _ := os.ReadFile(logPath); strings.TrimSpace(string(data)) != "--wait\n"+config {
		t.Fatalf("custom args = %q, want flags then the config path", data)
	}
}

// Assures that a repository's remotes are listed, so the remote question can default to one that exists.
func TestGitRemotesListsRepositoryRemotes(t *testing.T) {
	cfg := testGitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	runGit(t, repo, "remote", "add", "upstream", "git@github.com:upstream/app.git")
	remotes := gitRemotes(context.Background(), repo)
	if strings.Join(remotes, ",") != "origin,upstream" {
		t.Fatalf("remotes = %v, want origin and upstream", remotes)
	}
	if got := gitRemotes(context.Background(), t.TempDir()); len(got) != 0 {
		t.Fatalf("remotes outside a repository = %v", got)
	}
}

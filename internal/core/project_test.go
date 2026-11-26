package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveProfileExpandsPaths(t *testing.T) {
	homeDir := t.TempDir()
	appConfig := AppConfig{
		DefaultProfile: "solen",
		Profiles: []RepositoryProfile{{
			Name:           "solen",
			RepositoryPath: "~/solen/app",
			WorktreesDir:   "~/solen",
			RemoteName:     "origin",
		}},
	}

	profile, err := ActiveProfile(appConfig, homeDir)
	if err != nil {
		t.Fatalf("ActiveProfile() error = %v", err)
	}
	if profile.RepositoryPath != filepath.Join(homeDir, "solen", "app") {
		t.Fatalf("RepositoryPath = %q", profile.RepositoryPath)
	}
	if profile.WorktreesDir != filepath.Join(homeDir, "solen") {
		t.Fatalf("WorktreesDir = %q", profile.WorktreesDir)
	}
}

func TestCreateWorktreePathDashSlashes(t *testing.T) {
	cfg := Config{ActiveProfile: RepositoryProfile{WorktreesDir: "/tmp/worktrees"}}
	got := CreateWorktreePath(cfg, "feature/my-worktree")
	want := filepath.Join("/tmp/worktrees", "feature-my-worktree")
	if got != want {
		t.Fatalf("CreateWorktreePath() = %q, want %q", got, want)
	}
}

// Assures that a profile listing repositories also keeps the repository from its flat fields, listed first, and that repository defaults are filled in.
func TestNormalizeAppConfigMovesFlatRepositoryIntoList(t *testing.T) {
	app, changed, err := NormalizeAppConfig(AppConfig{Profiles: []RepositoryProfile{{
		Name:           "shop",
		RepositoryPath: "~/src/api",
		WorktreesDir:   "~/wt/api",
		GitHubRepo:     "owner/api",
		Repositories:   []Repository{{RepositoryPath: " ~/src/web ", WorktreesDir: "~/wt/web"}},
		LastRepository: "web",
	}}})
	if err != nil {
		t.Fatalf("NormalizeAppConfig() error = %v", err)
	}
	profile := app.Profiles[0]
	if !changed || profile.RepositoryPath != "" || profile.WorktreesDir != "" || profile.GitHubRepo != "" || profile.RemoteName != "" {
		t.Fatalf("flat fields were not moved: changed=%v profile=%#v", changed, profile)
	}
	want := []Repository{
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
	for name, repositories := range map[string][]Repository{
		"duplicate name":    {{Name: "api", RepositoryPath: "/src/api", WorktreesDir: "/wt"}, {Name: "api", RepositoryPath: "/src/web", WorktreesDir: "/wt"}},
		"missing path":      {{Name: "api", WorktreesDir: "/wt"}},
		"missing worktrees": {{Name: "api", RepositoryPath: "/src/api"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := NormalizeAppConfig(AppConfig{Profiles: []RepositoryProfile{{Name: "shop", Repositories: repositories}}})
			if err == nil || !strings.Contains(err.Error(), `profile "shop"`) {
				t.Fatalf("NormalizeAppConfig() error = %v, want a repository error for shop", err)
			}
		})
	}

	app, changed, err := NormalizeAppConfig(AppConfig{Profiles: []RepositoryProfile{{
		Name:           "shop",
		Repositories:   []Repository{{Name: "api", RepositoryPath: "/src/api", WorktreesDir: "/wt", RemoteName: "origin"}},
		LastRepository: "gone",
	}}})
	if err != nil || !changed || app.Profiles[0].LastRepository != "" {
		t.Fatalf("unknown last repository: changed=%v err=%v last=%q", changed, err, app.Profiles[0].LastRepository)
	}
}

// Assures that a resolved project lists every repository with expanded paths and scopes the flat fields to the first one.
func TestResolveProfilePathsListsRepositories(t *testing.T) {
	homeDir := t.TempDir()
	profile := ResolveProfilePaths(RepositoryProfile{Name: "shop", Repositories: []Repository{
		{Name: "api", RepositoryPath: "~/src/api", WorktreesDir: "~/wt/api", RemoteName: "origin", GitHubRepo: "owner/api"},
		{Name: "web", RepositoryPath: "~/src/web", WorktreesDir: "~/wt/web"},
	}}, homeDir)
	if len(profile.Repositories) != 2 || profile.Repositories[1].RepositoryPath != filepath.Join(homeDir, "src", "web") || profile.Repositories[1].RemoteName != "origin" {
		t.Fatalf("repositories = %#v", profile.Repositories)
	}
	if profile.RepositoryPath != filepath.Join(homeDir, "src", "api") || profile.WorktreesDir != filepath.Join(homeDir, "wt", "api") || profile.GitHubRepo != "owner/api" {
		t.Fatalf("flat fields = %#v, want the first repository", profile)
	}

	single := ResolveProfilePaths(RepositoryProfile{Name: "app", RepositoryPath: "~/src/app", WorktreesDir: "~/wt", RemoteName: "origin"}, homeDir)
	if len(single.Repositories) != 1 || single.Repositories[0].Name != "app" || single.Repositories[0].RepositoryPath != filepath.Join(homeDir, "src", "app") {
		t.Fatalf("single-repository project resolved to %#v", single.Repositories)
	}
}

// Assures that a project configured with flat fields is one repository named after its folder.
func TestRepositoryListUsesFlatFieldsForSingleRepositoryProjects(t *testing.T) {
	profile := RepositoryProfile{Name: "app", RepositoryPath: "~/projects/app/", WorktreesDir: "~/worktrees", RemoteName: "origin", GitHubRepo: "owner/app"}
	repositories := profile.RepositoryList()
	if len(repositories) != 1 {
		t.Fatalf("repositories = %#v, want one", repositories)
	}
	want := Repository{Name: "app", RepositoryPath: "~/projects/app/", WorktreesDir: "~/worktrees", RemoteName: "origin", GitHubRepo: "owner/app"}
	if repositories[0] != want {
		t.Fatalf("repository = %#v, want %#v", repositories[0], want)
	}
	if (RepositoryProfile{Name: "empty"}).RepositoryList() != nil {
		t.Fatal("a profile without repositories listed one")
	}
}

// Assures that scoping a config to a repository makes every per-repository field describe it, and unknown names change nothing.
func TestConfigForRepositoryScopesProfileAndSlug(t *testing.T) {
	cfg := Config{
		RepoSlug: "owner/api",
		ActiveProfile: RepositoryProfile{
			Name:           "shop",
			RepositoryPath: "/src/api",
			WorktreesDir:   "/wt/api",
			RemoteName:     "origin",
			GitHubRepo:     "owner/api",
			Repositories: []Repository{
				{Name: "api", RepositoryPath: "/src/api", WorktreesDir: "/wt/api", RemoteName: "origin", GitHubRepo: "owner/api"},
				{Name: "web", RepositoryPath: "/src/web", WorktreesDir: "/wt/web", RemoteName: "upstream", GitHubRepo: "owner/web"},
			},
		},
	}
	if !cfg.MultiRepository() || cfg.RepositoryName() != "api" {
		t.Fatalf("multi = %v, scoped = %q, want multi project scoped to api", cfg.MultiRepository(), cfg.RepositoryName())
	}
	web := cfg.ForRepository("web")
	profile := web.ActiveProfile
	if profile.RepositoryPath != "/src/web" || profile.WorktreesDir != "/wt/web" || profile.RemoteName != "upstream" || web.RepoSlug != "owner/web" || web.RepositoryName() != "web" {
		t.Fatalf("scoped config = %#v / %q", profile, web.RepoSlug)
	}
	if len(profile.Repositories) != 2 || cfg.ActiveProfile.RepositoryPath != "/src/api" {
		t.Fatal("scoping dropped sibling repositories or changed the original config")
	}
	if unknown := cfg.ForRepository("missing"); unknown.ActiveProfile.RepositoryPath != "/src/api" || unknown.RepoSlug != "owner/api" {
		t.Fatalf("unknown repository changed the scope: %#v", unknown.ActiveProfile)
	}
}

// Assures that repositories sharing a worktrees folder are detected, so their worktree folders can be kept apart.
func TestSharesWorktreesDir(t *testing.T) {
	profile := RepositoryProfile{Name: "shop", Repositories: []Repository{
		{Name: "api", RepositoryPath: "/src/api", WorktreesDir: "/wt"},
		{Name: "web", RepositoryPath: "/src/web", WorktreesDir: "/wt/"},
		{Name: "docs", RepositoryPath: "/src/docs", WorktreesDir: "/wt/docs"},
	}}
	cfg := Config{ActiveProfile: profile}
	if !cfg.ForRepository("api").SharesWorktreesDir() || !cfg.ForRepository("web").SharesWorktreesDir() {
		t.Fatal("api and web share /wt but were not detected")
	}
	if cfg.ForRepository("docs").SharesWorktreesDir() {
		t.Fatal("docs has its own folder but was reported as shared")
	}
	single := Config{ActiveProfile: RepositoryProfile{Name: "app", RepositoryPath: "/src/app", WorktreesDir: "/wt"}}
	if single.SharesWorktreesDir() {
		t.Fatal("a single-repository project cannot share its folder")
	}
}

// Assures that converting a project to the repositories form clears the flat fields and forgets a last repository that is gone.
func TestWithRepositoriesReplacesFlatFields(t *testing.T) {
	flat := RepositoryProfile{Name: "shop", RepositoryPath: "/src/api", WorktreesDir: "/wt", RemoteName: "origin", GitHubRepo: "owner/api", LastRepository: "old"}
	converted := flat.WithRepositories(append(flat.RepositoryList(), Repository{Name: "web", RepositoryPath: "/src/web", WorktreesDir: "/wt"}))
	if converted.RepositoryPath != "" || converted.WorktreesDir != "" || converted.RemoteName != "" || converted.GitHubRepo != "" {
		t.Fatalf("flat fields kept: %#v", converted)
	}
	if len(converted.Repositories) != 2 || converted.Repositories[0].Name != "api" || converted.Repositories[0].GitHubRepo != "owner/api" || converted.LastRepository != "" {
		t.Fatalf("converted = %#v", converted)
	}
}

// Assures that new repository names never collide with the project's existing ones.
func TestUniqueRepositoryName(t *testing.T) {
	profile := RepositoryProfile{Repositories: []Repository{{Name: "api"}, {Name: "api-2"}}}
	if got := profile.UniqueRepositoryName("api"); got != "api-3" {
		t.Fatalf("UniqueRepositoryName(api) = %q, want api-3", got)
	}
	if got := profile.UniqueRepositoryName("web"); got != "web" {
		t.Fatalf("UniqueRepositoryName(web) = %q, want web", got)
	}
}

// Assures that a new repository's suggested worktrees folder follows the project's layout.
func TestSuggestWorktreesDir(t *testing.T) {
	perRepository := []Repository{{Name: "api", RepositoryPath: "~/src/api", WorktreesDir: "~/worktrees/api"}}
	if got := SuggestWorktreesDir(perRepository, "web", "~/src/web"); got != "~/worktrees/web" {
		t.Fatalf("per-repository layout suggested %q", got)
	}
	shared := []Repository{{Name: "api", RepositoryPath: "~/src/api", WorktreesDir: "~/worktrees"}}
	if got := SuggestWorktreesDir(shared, "web", "~/src/web"); got != "~/worktrees" {
		t.Fatalf("shared layout suggested %q", got)
	}
	if got := SuggestWorktreesDir(nil, "web", "~/src/web"); got != "~/src" {
		t.Fatalf("new project suggested %q, want next to the repository", got)
	}
}

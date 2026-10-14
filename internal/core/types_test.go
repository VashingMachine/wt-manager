package core

import "testing"

// Assures that a ticket patch changes only the patched ticket and fields, and appending a comment never aliases the original comments.
func TestTicketPatchApply(t *testing.T) {
	comments := make([]TicketComment, 1, 4)
	comments[0] = TicketComment{Body: "first"}
	original := Ticket{Key: "APP-1", Status: "To Do", StatusCategory: StatusCategoryNew, Comments: comments}
	patched := original

	TicketPatch{Key: "APP-1", Status: "Doing", Comment: &TicketComment{Body: "second"}}.Apply(&patched)
	if patched.Status != "Doing" || patched.StatusCategory != StatusCategoryNew || len(patched.Comments) != 2 {
		t.Fatalf("patched = %#v, want new status, unchanged unknown category, appended comment", patched)
	}
	if extended := original.Comments[:2]; extended[1].Body == "second" {
		t.Fatal("appending a comment wrote into the original ticket's backing array")
	}

	other := Ticket{Key: "APP-2", Status: "To Do"}
	TicketPatch{Key: "APP-1", Status: "Done"}.Apply(&other)
	if other.Status != "To Do" {
		t.Fatal("patch changed a different ticket")
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

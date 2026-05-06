package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAppConfigMissingDoesNotWrite(t *testing.T) {
	homeDir := t.TempDir()
	oldConfigPath := filepath.Join(homeDir, ".bwt-manager.json")
	if err := os.WriteFile(oldConfigPath, []byte(`{"defaultAgent":"copilot"}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	appConfig, configPath, existed, err := loadAppConfig(homeDir)
	if err != nil {
		t.Fatalf("loadAppConfig() error = %v", err)
	}

	if existed {
		t.Fatal("existed = true, want false")
	}
	if configPath != filepath.Join(homeDir, ".wt-manager.json") {
		t.Fatalf("configPath = %q, want config under temp home", configPath)
	}
	if appConfig.DefaultAgent != "codex" {
		t.Fatalf("DefaultAgent = %q, want codex", appConfig.DefaultAgent)
	}
	for _, name := range []string{"codex", "opencode", "claude", "copilot"} {
		if findAgent(appConfig.Agents, name) == nil {
			t.Fatalf("expected %s agent in %#v", name, appConfig.Agents)
		}
	}
	if len(appConfig.Profiles) != 0 {
		t.Fatalf("Profiles = %#v, want none when no git repo is inferred", appConfig.Profiles)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("new config should not be written before setup, stat error = %v", err)
	}
	if _, err := os.Stat(oldConfigPath); err != nil {
		t.Fatalf("old config should be untouched: %v", err)
	}
}

func TestLoadOrCreateAppConfigNormalizesExistingConfig(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".wt-manager.json")
	raw := `{
  "defaultProfile": "",
  "profiles": [
    {"name":"solen","repositoryPath":"~/solen/app","worktreesDir":"~/solen","remoteName":"","githubRepo":""}
  ],
  "defaultAgent":"missing",
  "agents":[{"name":"opencode","command":"opencode"}]
}`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	appConfig, _, err := loadOrCreateAppConfig(homeDir)
	if err != nil {
		t.Fatalf("loadOrCreateAppConfig() error = %v", err)
	}

	if appConfig.DefaultProfile != "solen" {
		t.Fatalf("DefaultProfile = %q, want solen", appConfig.DefaultProfile)
	}
	if appConfig.DefaultAgent != "opencode" {
		t.Fatalf("DefaultAgent = %q, want opencode", appConfig.DefaultAgent)
	}
	if appConfig.Profiles[0].RemoteName != "origin" {
		t.Fatalf("RemoteName = %q, want origin", appConfig.Profiles[0].RemoteName)
	}
	if len(appConfig.Agents) != 1 || appConfig.Agents[0].Name != "opencode" {
		t.Fatalf("Agents = %#v, want existing configured agents only", appConfig.Agents)
	}
}

func TestDefaultConfigSetupModes(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	writeRepoFile(t, repo, "README.md", "hello\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")

	t.Setenv("HOME", root)
	withCWD(t, repo)

	cfg, err := defaultConfig(false)
	if err != nil {
		t.Fatalf("defaultConfig() error = %v", err)
	}
	if !cfg.SetupNeeded || cfg.SetupRepo == "" {
		t.Fatalf("defaultConfig() should enter setup for missing config in repo, got %#v", cfg)
	}

	appConfig := defaultAppConfig()
	appConfig.DefaultProfile = "repo"
	appConfig.Profiles = []RepositoryProfile{{Name: "repo", RepositoryPath: repo, WorktreesDir: filepath.Join(root, "worktrees"), RemoteName: "origin"}}
	if err := writeAppConfig(filepath.Join(root, ".wt-manager.json"), appConfig); err != nil {
		t.Fatalf("writeAppConfig() error = %v", err)
	}

	cfg, err = defaultConfig(false)
	if err != nil {
		t.Fatalf("defaultConfig() configured error = %v", err)
	}
	if cfg.SetupNeeded {
		t.Fatalf("defaultConfig() SetupNeeded = true for configured repo")
	}

	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("MkdirAll(other) error = %v", err)
	}
	runGit(t, other, "init")
	writeRepoFile(t, other, "README.md", "other\n")
	runGit(t, other, "add", ".")
	runGit(t, other, "config", "user.email", "test@example.com")
	runGit(t, other, "config", "user.name", "Test User")
	runGit(t, other, "commit", "-m", "initial")
	withCWD(t, other)
	cfg, err = defaultConfig(false)
	if err != nil {
		t.Fatalf("defaultConfig() other error = %v", err)
	}
	if !cfg.SetupNeeded || !strings.Contains(cfg.SetupReason, "not configured") {
		t.Fatalf("defaultConfig() should enter setup for unconfigured cwd repo, got %#v", cfg)
	}

	cfg, err = defaultConfig(true)
	if err != nil {
		t.Fatalf("defaultConfig(force) error = %v", err)
	}
	if !cfg.SetupNeeded || !strings.Contains(cfg.SetupReason, "Manual") {
		t.Fatalf("defaultConfig(force) got %#v", cfg)
	}
}

func TestLoadOrCreateAppConfigRejectsInvalidProfiles(t *testing.T) {
	tests := map[string]string{
		"duplicate": `{"defaultProfile":"solen","profiles":[{"name":"solen","repositoryPath":"/repo","worktreesDir":"/w","remoteName":"origin"},{"name":"solen","repositoryPath":"/repo2","worktreesDir":"/w2","remoteName":"origin"}]}`,
		"invalid":   `{"defaultProfile":"broken","profiles":[{"name":"broken","repositoryPath":"","worktreesDir":"/w","remoteName":"origin"}]}`,
		"missing":   `{"defaultProfile":"missing","profiles":[{"name":"solen","repositoryPath":"/repo","worktreesDir":"/w","remoteName":"origin"}]}`,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			homeDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(homeDir, ".wt-manager.json"), []byte(raw), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, _, err := loadOrCreateAppConfig(homeDir); err == nil {
				t.Fatal("loadOrCreateAppConfig() error = nil, want error")
			}
		})
	}
}

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

	profile, err := activeProfile(appConfig, homeDir)
	if err != nil {
		t.Fatalf("activeProfile() error = %v", err)
	}
	if profile.RepositoryPath != filepath.Join(homeDir, "solen", "app") {
		t.Fatalf("RepositoryPath = %q", profile.RepositoryPath)
	}
	if profile.WorktreesDir != filepath.Join(homeDir, "solen") {
		t.Fatalf("WorktreesDir = %q", profile.WorktreesDir)
	}
}

func TestRepoRootFromNestedDir(t *testing.T) {
	cfg := testGitRepo(t)
	nested := filepath.Join(cfg.ActiveProfile.RepositoryPath, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	repo, ok := repoRootFromDir(context.Background(), nested)
	if !ok {
		t.Fatal("repoRootFromDir() ok = false")
	}
	if canonicalPath(repo) != canonicalPath(cfg.ActiveProfile.RepositoryPath) {
		t.Fatalf("repoRootFromDir() = %q, want %q", repo, cfg.ActiveProfile.RepositoryPath)
	}
}

func TestGitHubSlugFromRemoteURL(t *testing.T) {
	tests := map[string]string{
		"git@github.com:owner/repo.git":       "owner/repo",
		"ssh://git@github.com/owner/repo.git": "owner/repo",
		"https://github.com/owner/repo.git":   "owner/repo",
		"http://github.com/owner/repo":        "owner/repo",
		"https://example.com/owner/repo.git":  "",
	}
	for input, want := range tests {
		if got := gitHubSlugFromRemoteURL(input); got != want {
			t.Fatalf("gitHubSlugFromRemoteURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCreateWorktreePathDashSlashes(t *testing.T) {
	cfg := Config{ActiveProfile: RepositoryProfile{WorktreesDir: "/tmp/worktrees"}}
	got := createWorktreePath(cfg, "feature/my-worktree")
	want := filepath.Join("/tmp/worktrees", "feature-my-worktree")
	if got != want {
		t.Fatalf("createWorktreePath() = %q, want %q", got, want)
	}
}

func TestCompactPath(t *testing.T) {
	homeDir := filepath.Join(string(os.PathSeparator), "Users", "me")
	got := compactPath(filepath.Join(homeDir, "projects", "repo"), homeDir)
	if got != "~/projects/repo" {
		t.Fatalf("compactPath() = %q, want ~/projects/repo", got)
	}
}

func TestCheckGitHubCLI(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		warnings := checkGitHubCLI(context.Background(), "owner/repo")
		if len(warnings) == 0 || !strings.Contains(warnings[0], "gh not found") {
			t.Fatalf("warnings = %#v", warnings)
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		bin := t.TempDir()
		writeExecutable(t, filepath.Join(bin, "gh"), "#!/bin/sh\nif [ \"$1\" = auth ]; then exit 1; fi\nexit 0\n")
		t.Setenv("PATH", bin)
		warnings := checkGitHubCLI(context.Background(), "owner/repo")
		if len(warnings) == 0 || !strings.Contains(warnings[0], "not authenticated") {
			t.Fatalf("warnings = %#v", warnings)
		}
	})

	t.Run("authenticated", func(t *testing.T) {
		bin := t.TempDir()
		writeExecutable(t, filepath.Join(bin, "gh"), "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", bin)
		warnings := checkGitHubCLI(context.Background(), "owner/repo")
		if len(warnings) != 0 {
			t.Fatalf("warnings = %#v, want none", warnings)
		}
	})
}

func TestFilterRemotePullRequests(t *testing.T) {
	prs := []RemotePullRequest{
		remotePRFixture(10, "OPEN", "alice", "Add auth cache", "feature/auth"),
		remotePRFixture(11, "CLOSED", "bob", "Remove cache", "bug/remove-cache"),
		remotePRFixture(12, "MERGED", "clara", "Update docs", "docs/readme"),
	}
	prs[0].Assignees = []GitHubActor{{Login: "dariusz"}}
	prs[1].ReviewRequests = []ReviewRequest{{RequestedReviewer: GitHubActor{Login: "dariusz"}}}
	prs[2].Labels = []PullLabel{{Name: "docs"}}

	got := filterRemotePullRequests(prs, "cache", "open", "")
	if len(got) != 1 || got[0].Number != 10 {
		t.Fatalf("filter open cache = %#v, want PR 10", got)
	}

	got = filterRemotePullRequests(prs, "", "all", "dariusz")
	if len(got) != 2 || got[0].Number != 10 || got[1].Number != 11 {
		t.Fatalf("filter user dariusz = %#v, want PRs 10 and 11", got)
	}

	got = filterRemotePullRequests(prs, "docs", "merged", "")
	if len(got) != 1 || got[0].Number != 12 {
		t.Fatalf("filter merged docs = %#v, want PR 12", got)
	}

	got = filterRemotePullRequestsWithAuthor(prs, "", "all", "", "alice")
	if len(got) != 1 || got[0].Number != 10 {
		t.Fatalf("filter author alice = %#v, want PR 10", got)
	}
}

func TestRemotePRUsers(t *testing.T) {
	prs := []RemotePullRequest{
		remotePRFixture(10, "OPEN", "alice", "Add auth cache", "feature/auth"),
		remotePRFixture(11, "OPEN", "bob", "Remove cache", "bug/remove-cache"),
	}
	prs[0].Assignees = []GitHubActor{{Login: "zoe"}}
	prs[1].ReviewRequests = []ReviewRequest{{RequestedReviewer: GitHubActor{Login: "alice"}}}

	got := strings.Join(remotePRUsers(prs), ",")
	for _, want := range []string{"alice", "bob", "zoe"} {
		if !strings.Contains(got, want) {
			t.Fatalf("remotePRUsers() = %q, missing %s", got, want)
		}
	}
	if strings.Count(got, "alice") != 1 {
		t.Fatalf("remotePRUsers() = %q, want alice once", got)
	}
}

func TestBuildPullRequestChatPrompt(t *testing.T) {
	pr := remotePRFixture(42, "OPEN", "alice", "Fix token refresh", "feature/token-refresh")
	pr.URL = "https://github.com/owner/repo/pull/42"
	pr.Body = "This changes token refresh behavior."
	pr.Files = []PullFile{{Path: "auth/session.go", Additions: 40, Deletions: 12}}
	pr.Comments = []PullComment{{Author: GitHubActor{Login: "reviewer"}, Body: "Please check retry behavior."}}
	pr.Diff = strings.Repeat("diff --git a/auth/session.go b/auth/session.go\n", 500)

	prompt := buildPullRequestChatPrompt(pr, "what is risky?")
	for _, want := range []string{"Question: what is risky?", "PR #42: Fix token refresh", "auth/session.go", "reviewer", "Diff:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q in %q", want, prompt)
		}
	}
	if len([]rune(prompt)) > 18000 {
		t.Fatalf("prompt length = %d, want truncation", len([]rune(prompt)))
	}
}

func TestRemotePullRequestFromREST(t *testing.T) {
	mergedAt := "2026-05-05T12:00:00Z"
	got := remotePullRequestFromREST(restPullRequest{
		Number:       42,
		Title:        "Fix auth refresh",
		HTMLURL:      "https://github.com/owner/repo/pull/42",
		State:        "closed",
		Body:         "body",
		Draft:        true,
		User:         restActor{Login: "alice"},
		Assignees:    []restActor{{Login: "bob"}},
		Labels:       []restLabel{{Name: "backend"}},
		Head:         restRef{Ref: "feature/auth", SHA: "abc"},
		Base:         restRef{Ref: "main"},
		ChangedFiles: 3,
		Additions:    100,
		Deletions:    20,
		CreatedAt:    "2026-05-04T12:00:00Z",
		UpdatedAt:    "2026-05-05T12:00:00Z",
		MergedAt:     &mergedAt,
	})

	if got.State != "MERGED" || got.Number != 42 || got.HeadRefName != "feature/auth" || got.Author.Login != "alice" {
		t.Fatalf("remotePullRequestFromREST() = %#v", got)
	}
	if got.HeadSHA != "abc" {
		t.Fatalf("HeadSHA = %q, want abc", got.HeadSHA)
	}
	if len(got.Assignees) != 1 || got.Assignees[0].Login != "bob" {
		t.Fatalf("Assignees = %#v", got.Assignees)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "backend" {
		t.Fatalf("Labels = %#v", got.Labels)
	}
}

func TestLiveGitHubBluesteelRemotePullRequests(t *testing.T) {
	cfg := liveBluesteelConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	prs, err := loadRemotePullRequests(ctx, cfg)
	if err != nil {
		t.Fatalf("loadRemotePullRequests() live error = %v", err)
	}
	if len(prs) == 0 {
		t.Fatal("loadRemotePullRequests() returned no PRs for bluesteel")
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

func TestLiveGitHubBluesteelRemotePullRequestDetail(t *testing.T) {
	cfg := liveBluesteelConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	prs, err := loadRemotePullRequests(ctx, cfg)
	if err != nil {
		t.Fatalf("loadRemotePullRequests() live error = %v", err)
	}
	if len(prs) == 0 {
		t.Fatal("loadRemotePullRequests() returned no PRs for bluesteel")
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

func TestRunAgentWithPrompt(t *testing.T) {
	bin := t.TempDir()
	agentPath := filepath.Join(bin, "fake-agent")
	writeExecutable(t, agentPath, "#!/bin/sh\ninput=\"\"\nwhile IFS= read -r line; do input=\"$input $line\"; done\ncase \"$input\" in *risk*) echo 'risk found';; *) echo 'no risk';; esac\n")
	t.Setenv("PATH", bin)

	output, err := runAgentWithPrompt(context.Background(), "", "fake-agent", "tell me risk\n")
	if err != nil {
		t.Fatalf("runAgentWithPrompt() error = %v", err)
	}
	if strings.TrimSpace(output) != "risk found" {
		t.Fatalf("output = %q, want risk found", output)
	}
}

func TestCreateWorktreeExistingLocalBranch(t *testing.T) {
	cfg := testGitRepo(t)
	runGit(t, cfg.ActiveProfile.RepositoryPath, "branch", "local-feature")

	path := createWorktreePath(cfg, "local-feature")
	existing, err := createWorktree(context.Background(), cfg, "local-feature", path)
	if err != nil {
		t.Fatalf("createWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	assertGitBranch(t, path, "local-feature")
}

func TestCreateWorktreeRemoteTrackingBranch(t *testing.T) {
	cfg := testGitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	writeRepoFile(t, repo, "remote.txt", "remote\n")
	runGit(t, repo, "checkout", "-b", "remote-feature")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "remote feature")
	runGit(t, repo, "push", "origin", "remote-feature")
	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "branch", "-D", "remote-feature")

	path := createWorktreePath(cfg, "remote-feature")
	existing, err := createWorktree(context.Background(), cfg, "remote-feature", path)
	if err != nil {
		t.Fatalf("createWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	assertGitBranch(t, path, "remote-feature")
	upstream := strings.TrimSpace(runGit(t, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"))
	if upstream != "origin/remote-feature" {
		t.Fatalf("upstream = %q, want origin/remote-feature", upstream)
	}
}

func TestCreateWorktreeNewBranchFallback(t *testing.T) {
	cfg := testGitRepo(t)

	path := createWorktreePath(cfg, "new-feature")
	existing, err := createWorktree(context.Background(), cfg, "new-feature", path)
	if err != nil {
		t.Fatalf("createWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	assertGitBranch(t, path, "new-feature")
}

func TestCreateWorktreeKnownPathSelectsExisting(t *testing.T) {
	cfg := testGitRepo(t)
	path := createWorktreePath(cfg, "known-feature")
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
	cfg := testGitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	writeRepoFile(t, repo, "pr.txt", "pr\n")
	runGit(t, repo, "checkout", "-b", "remote-pr-branch")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "pr branch")
	runGit(t, repo, "push", "origin", "HEAD:refs/pull/42/head")
	runGit(t, repo, "checkout", "main")

	pr := remotePRFixture(42, "OPEN", "alice", "PR branch", "fork-branch")
	wt, existing, err := ensurePullRequestWorktree(context.Background(), cfg, pr)
	if err != nil {
		t.Fatalf("ensurePullRequestWorktree() error = %v", err)
	}
	if existing {
		t.Fatal("existing = true, want false")
	}
	assertGitBranch(t, wt.Path, "pr-42-fork-branch")
	if _, err := os.Stat(filepath.Join(wt.Path, "pr.txt")); err != nil {
		t.Fatalf("PR file missing from worktree: %v", err)
	}
}

func TestEnsurePullRequestWorktreeReusesExisting(t *testing.T) {
	cfg := testGitRepo(t)
	pr := remotePRFixture(42, "OPEN", "alice", "PR branch", "existing-pr")
	path := createWorktreePath(cfg, "pr-42-existing-pr")
	runGit(t, cfg.ActiveProfile.RepositoryPath, "branch", "pr-42-existing-pr")
	runGit(t, cfg.ActiveProfile.RepositoryPath, "worktree", "add", path, "pr-42-existing-pr")

	wt, existing, err := ensurePullRequestWorktree(context.Background(), cfg, pr)
	if err != nil {
		t.Fatalf("ensurePullRequestWorktree() error = %v", err)
	}
	if !existing {
		t.Fatal("existing = false, want true")
	}
	if canonicalPath(wt.Path) != canonicalPath(path) {
		t.Fatalf("path = %q, want %q", wt.Path, path)
	}
}

func TestEnsurePullRequestWorktreePrefersExistingHeadBranchWorktree(t *testing.T) {
	cfg := testGitRepo(t)
	branch := "feature-existing"
	runGit(t, cfg.ActiveProfile.RepositoryPath, "branch", branch)
	existingPath := createWorktreePath(cfg, branch)
	runGit(t, cfg.ActiveProfile.RepositoryPath, "worktree", "add", existingPath, branch)

	pr := remotePRFixture(42, "OPEN", "alice", "Existing branch PR", branch)
	wt, existing, err := ensurePullRequestWorktree(context.Background(), cfg, pr)
	if err != nil {
		t.Fatalf("ensurePullRequestWorktree() error = %v", err)
	}
	if !existing {
		t.Fatal("existing = false, want true")
	}
	if canonicalPath(wt.Path) != canonicalPath(existingPath) {
		t.Fatalf("path = %q, want existing branch worktree %q", wt.Path, existingPath)
	}
	if fallback := createWorktreePath(cfg, "pr-42-"+branch); knownWorktreePath(context.Background(), cfg.ActiveProfile.RepositoryPath, fallback) {
		t.Fatalf("created fallback PR worktree %q instead of reusing %q", fallback, existingPath)
	}
}

func TestCreateWorktreeConflictingPath(t *testing.T) {
	cfg := testGitRepo(t)
	path := createWorktreePath(cfg, "conflict-feature")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if _, err := createWorktree(context.Background(), cfg, "conflict-feature", path); err == nil {
		t.Fatal("createWorktree() error = nil, want error")
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("/tmp/worktree with 'quote'")
	want := `'/tmp/worktree with '\''quote'\'''`
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func TestAppleScriptString(t *testing.T) {
	got := appleScriptString("say \"hi\"\nnext")
	want := `"say \"hi\"\nnext"`
	if got != want {
		t.Fatalf("appleScriptString() = %q, want %q", got, want)
	}
}

func TestGhosttyTabScript(t *testing.T) {
	script := ghosttyTabScript("cd '/tmp/worktree' && copilot")
	for _, want := range []string{"tell application \"Ghostty\" to activate", "keystroke \"t\" using command down", "keystroke commandText", "key code 36"} {
		if !strings.Contains(script, want) {
			t.Fatalf("ghosttyTabScript() missing %q in %q", want, script)
		}
	}
	if !strings.Contains(script, "copilot") {
		t.Fatalf("ghosttyTabScript() should include selected agent command in %q", script)
	}
}

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

func assertGitBranch(t *testing.T, repo string, branch string) {
	t.Helper()
	got := strings.TrimSpace(runGit(t, repo, "branch", "--show-current"))
	if got != branch {
		t.Fatalf("branch = %q, want %q", got, branch)
	}
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

func liveBluesteelConfig(t *testing.T) Config {
	t.Helper()
	if os.Getenv("WT_MANAGER_LIVE_GH") != "1" {
		t.Skip("set WT_MANAGER_LIVE_GH=1 to run live gh/bluesteel integration tests")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skipf("gh not found: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := runCommand(ctx, "", "gh", "auth", "status"); err != nil {
		t.Skipf("gh is not authenticated: %v", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}
	appConfig, configPath, existed, err := loadAppConfig(homeDir)
	if err != nil {
		t.Fatalf("loadAppConfig() error = %v", err)
	}
	if !existed {
		t.Fatalf("%s does not exist", configPath)
	}
	profile := findProfile(appConfig.Profiles, "bluesteel")
	if profile == nil {
		t.Fatalf("bluesteel profile missing from %s", configPath)
	}
	resolved := resolveProfilePaths(*profile, homeDir)
	if resolved.GitHubRepo == "" {
		t.Fatalf("bluesteel profile has no githubRepo in %s", configPath)
	}
	if _, err := os.Stat(resolved.RepositoryPath); err != nil {
		t.Fatalf("bluesteel repositoryPath %s is unavailable: %v", resolved.RepositoryPath, err)
	}

	return Config{
		HomeDir:       homeDir,
		ConfigPath:    configPath,
		App:           appConfig,
		ActiveProfile: resolved,
		RepoSlug:      resolved.GitHubRepo,
		ConfigExists:  true,
	}
}

func withCWD(t *testing.T, dir string) {
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

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

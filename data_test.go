package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestSetupAgentDetection(t *testing.T) {
	lookup := func(command string) (string, error) {
		if command == "codex" || command == "claude" {
			return "/bin/" + command, nil
		}
		return "", os.ErrNotExist
	}
	options := detectSetupAgents([]AgentTool{{Name: "custom", Command: "custom --flag"}}, "custom", lookup)
	selected := agentNames(selectedSetupAgents(options))
	got := strings.Join(selected, ",")
	for _, want := range []string{"codex", "claude", "custom"} {
		if !strings.Contains(got, want) {
			t.Fatalf("selected agents = %q, missing %s", got, want)
		}
	}
}

func TestDirectoryPickerTypingJDoesNotMoveSelection(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, "projects"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	picker := newDirectoryPicker(homeDir, setupDirectorySeeds(homeDir), homeDir)
	picker.input.SetValue("~/pro")
	picker.input.CursorEnd()
	picker.refilter()
	picker.cursor = 0

	next, _ := picker.Update(teaKey("j"))
	if got := next.input.Value(); got != "~/proj" {
		t.Fatalf("input value = %q, want ~/proj", got)
	}
	if next.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", next.cursor)
	}
}

func TestSetupWizardSelectsCreatedProjectsFolderWhenTypingJ(t *testing.T) {
	homeDir := t.TempDir()
	repo := filepath.Join(homeDir, "solen", "app")
	projects := filepath.Join(homeDir, "projects")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("MkdirAll(projects) error = %v", err)
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	writeRepoFile(t, repo, "README.md", "hello\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "initial")

	setupCfg := Config{
		HomeDir:    homeDir,
		ConfigPath: filepath.Join(homeDir, ".wt-manager.json"),
		App:        defaultAppConfig(),
		SetupRepo:  repo,
	}
	setup := newSetupModel(setupCfg, true)
	if setup.step != setupStepProfile {
		t.Fatalf("initial step = %v, want profile", setup.step)
	}

	setup = updateSetupForTest(t, setup, teaKey("enter"))
	if setup.step != setupStepWorktrees {
		t.Fatalf("step = %v, want worktrees", setup.step)
	}
	setup.worktreePicker.input.SetValue("")
	setup.worktreePicker.input.CursorEnd()
	setup.worktreePicker.refilter()
	for _, key := range strings.Split("~/projects", "") {
		setup = updateSetupForTest(t, setup, teaKey(key))
	}
	if got := setup.worktreePicker.input.Value(); got != "~/projects" {
		t.Fatalf("worktree input = %q, want ~/projects", got)
	}
	if len(setup.worktreePicker.filtered) == 0 {
		t.Fatal("worktree picker has no matches for ~/projects")
	}
	if canonicalPath(setup.worktreePicker.filtered[0]) != canonicalPath(projects) {
		t.Fatalf("first match = %q, want %q", setup.worktreePicker.filtered[0], projects)
	}

	setup = updateSetupForTest(t, setup, teaKey("enter"))
	if setup.step != setupStepAgents {
		t.Fatalf("step = %v, want agents", setup.step)
	}
	if canonicalPath(setup.worktreesDir) != canonicalPath(projects) {
		t.Fatalf("worktreesDir = %q, want %q", setup.worktreesDir, projects)
	}
	for i := range setup.agentOptions {
		setup.agentOptions[i].Selected = setup.agentOptions[i].Name == "codex"
	}
	setup = updateSetupForTest(t, setup, teaKey("enter"))
	if setup.step != setupStepGitHub {
		t.Fatalf("step = %v, want github review", setup.step)
	}

	next, cmd := setup.Update(teaKey("enter"))
	if cmd == nil {
		t.Fatalf("setup save command is nil, status = %s", next.status)
	}
	result, ok := cmd().(setupResult)
	if !ok {
		t.Fatalf("setup save returned %T, want setupResult", cmd())
	}
	if result.Err != nil {
		t.Fatalf("setup result error = %v", result.Err)
	}
	if result.Config.ActiveProfile.WorktreesDir != projects {
		t.Fatalf("ActiveProfile.WorktreesDir = %q, want %q", result.Config.ActiveProfile.WorktreesDir, projects)
	}
	persisted, _, err := loadOrCreateAppConfig(homeDir)
	if err != nil {
		t.Fatalf("load persisted config error = %v", err)
	}
	profile := findProfile(persisted.Profiles, "app")
	if profile == nil {
		t.Fatalf("persisted profile app missing in %#v", persisted.Profiles)
	}
	if profile.WorktreesDir != "~/projects" {
		t.Fatalf("persisted WorktreesDir = %q, want ~/projects", profile.WorktreesDir)
	}
}

func TestDirectoryPickerFindsProjectsForTypo(t *testing.T) {
	homeDir := t.TempDir()
	projects := filepath.Join(homeDir, "projects")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	picker := newDirectoryPicker(homeDir, setupDirectorySeeds(homeDir), homeDir)
	picker.input.SetValue("~/proects")
	picker.refilter()

	if len(picker.filtered) == 0 {
		t.Fatal("filtered = empty, want ~/projects match")
	}
	if canonicalPath(picker.filtered[0]) != canonicalPath(projects) {
		t.Fatalf("first match = %q, want %q", picker.filtered[0], projects)
	}
	if selected := picker.SelectedPath(); canonicalPath(selected) != canonicalPath(projects) {
		t.Fatalf("SelectedPath() = %q, want %q", selected, projects)
	}
}

func TestSetupWizardTypoSearchSelectsProjectsFolder(t *testing.T) {
	homeDir := t.TempDir()
	repo := filepath.Join(homeDir, "imaginary", "repo")
	projects := filepath.Join(homeDir, "projects")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(repo) error = %v", err)
	}
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("MkdirAll(projects) error = %v", err)
	}
	runGit(t, repo, "init")

	setup := newSetupModel(Config{
		HomeDir:    homeDir,
		ConfigPath: filepath.Join(homeDir, ".wt-manager.json"),
		App:        defaultAppConfig(),
		SetupRepo:  repo,
	}, true)
	setup = updateSetupForTest(t, setup, teaKey("enter"))
	setup.worktreePicker.input.SetValue("")
	setup.worktreePicker.input.CursorEnd()
	setup.worktreePicker.refilter()
	for _, key := range strings.Split("~/proects", "") {
		setup = updateSetupForTest(t, setup, teaKey(key))
	}

	if len(setup.worktreePicker.filtered) == 0 {
		t.Fatal("worktree picker has no matches for ~/proects")
	}
	if canonicalPath(setup.worktreePicker.filtered[0]) != canonicalPath(projects) {
		t.Fatalf("first match = %q, want %q", setup.worktreePicker.filtered[0], projects)
	}
	selected := setup.worktreePicker.SelectedPath()
	if canonicalPath(selected) != canonicalPath(projects) {
		t.Fatalf("SelectedPath() = %q, want %q", selected, projects)
	}
}

func TestDirectoryPickerUsesTypedMissingPathWhenNoMatch(t *testing.T) {
	homeDir := t.TempDir()
	picker := newDirectoryPicker(homeDir, []string{homeDir}, homeDir)
	missing := filepath.Join(homeDir, "new-worktrees")
	picker.input.SetValue(missing)
	picker.refilter()

	if selected := picker.SelectedPath(); selected != missing {
		t.Fatalf("SelectedPath() = %q, want typed missing path %q", selected, missing)
	}
}

func TestCompleteSetupWritesConfigAndCreatesWorktreesDir(t *testing.T) {
	cfg := testGitRepo(t)
	homeDir := cfg.HomeDir
	configPath := filepath.Join(homeDir, ".wt-manager.json")
	worktreesDir := filepath.Join(homeDir, "new-worktrees")
	setupCfg := Config{HomeDir: homeDir, ConfigPath: configPath, App: defaultAppConfig(), SetupRepo: cfg.ActiveProfile.RepositoryPath}
	setup := newSetupModel(setupCfg, true)
	setup.repoPath = cfg.ActiveProfile.RepositoryPath
	setup.worktreesDir = worktreesDir
	setup.profileInput.SetValue("app")
	for i := range setup.agentOptions {
		setup.agentOptions[i].Selected = setup.agentOptions[i].Name == "codex"
	}
	setup.step = setupStepGitHub

	next, cmd := setup.updateGitHubStep(teaKey("enter"))
	if cmd == nil {
		t.Fatalf("updateGitHubStep() cmd = nil, status = %s", next.status)
	}
	msg := cmd()
	result, ok := msg.(setupResult)
	if !ok {
		t.Fatalf("cmd() = %T, want setupResult", msg)
	}
	if result.Err != nil {
		t.Fatalf("setupResult error = %v", result.Err)
	}
	if _, err := os.Stat(worktreesDir); err != nil {
		t.Fatalf("worktrees dir not created: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if result.Config.ActiveProfile.Name != "app" {
		t.Fatalf("ActiveProfile.Name = %q, want app", result.Config.ActiveProfile.Name)
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

func updateSetupForTest(t *testing.T, setup setupModel, key tea.KeyMsg) setupModel {
	t.Helper()
	next, cmd := setup.Update(key)
	if cmd != nil {
		if result, ok := cmd().(setupResult); ok && result.Err != nil {
			t.Fatalf("setup command error = %v", result.Err)
		}
	}
	return next
}

func teaKey(value string) tea.KeyMsg {
	if value == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

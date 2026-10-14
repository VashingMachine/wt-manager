package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
)

func TestSetupAgentDetection(t *testing.T) {
	lookup := func(command string) (string, error) {
		if command == "codex" || command == "claude" {
			return "/bin/" + command, nil
		}
		return "", os.ErrNotExist
	}
	svc := services.NewService()
	options := detectSetupAgents(svc, []AgentTool{{Name: "custom", Command: "custom --flag"}}, "custom", lookup)
	selected := agentNames(selectedSetupAgents(options))
	got := strings.Join(selected, ",")
	for _, want := range []string{"codex", "claude", "custom"} {
		if !strings.Contains(got, want) {
			t.Fatalf("selected agents = %q, missing %s", got, want)
		}
	}
}

func TestDirectoryPickerTypingJDoesNotMoveSelection(t *testing.T) {
	svc := services.NewService()
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, "projects"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	picker := newDirectoryPicker(svc, homeDir, setupDirectorySeeds(svc, homeDir), homeDir)
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
	svc := services.NewService()
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
		App:        svc.DefaultAppConfig(),
		SetupRepo:  repo,
	}
	setup := newSetupModel(setupCfg, true, svc)
	if setup.step != setupStepProject {
		t.Fatalf("initial step = %v, want project", setup.step)
	}

	setup = updateSetupForTest(t, setup, teaKey("enter"))
	if setup.step != setupStepRepositories || len(setup.repositories) != 1 {
		t.Fatalf("step = %v with %d repositories, want the repository list with app", setup.step, len(setup.repositories))
	}
	setup = openRepositoryForEdit(t, setup, 0)
	for _, key := range strings.Split("~/projects", "") {
		setup = updateSetupForTest(t, setup, teaKey(key))
	}
	if got := setup.editor.worktrees.input.Value(); got != "~/projects" {
		t.Fatalf("worktree input = %q, want ~/projects", got)
	}
	if len(setup.editor.worktrees.filtered) == 0 {
		t.Fatal("worktree picker has no matches for ~/projects")
	}
	if svc.CanonicalPath(setup.editor.worktrees.filtered[0]) != svc.CanonicalPath(projects) {
		t.Fatalf("first match = %q, want %q", setup.editor.worktrees.filtered[0], projects)
	}

	setup = updateSetupForTest(t, setup, teaKey("enter"))
	if setup.editor != nil || svc.CanonicalPath(setup.repositories[0].WorktreesDir) != svc.CanonicalPath(projects) {
		t.Fatalf("editor open = %v, worktreesDir = %q, want %q", setup.editor != nil, setup.repositories[0].WorktreesDir, projects)
	}
	setup = updateSetupForTest(t, setup, tea.KeyMsg{Type: tea.KeyTab})
	if setup.step != setupStepAgents {
		t.Fatalf("step = %v, want agents", setup.step)
	}
	for i := range setup.agentOptions {
		setup.agentOptions[i].Selected = setup.agentOptions[i].Name == "codex"
	}
	setup = updateSetupForTest(t, setup, teaKey("enter"))
	if setup.step != setupStepJira {
		t.Fatalf("step = %v, want JIRA", setup.step)
	}

	next, cmd := setup.Update(teaKey("enter"))
	if cmd != nil || next.step != setupStepReview {
		t.Fatalf("JIRA did not continue to the review: step=%v cmd=%v", next.step, cmd)
	}
	next, cmd = next.Update(teaKey("enter"))
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
	profile := svc.FindProfile(result.Config.App.Profiles, "app")
	if profile == nil {
		t.Fatalf("persisted profile app missing in %#v", result.Config.App.Profiles)
	}
	if profile.WorktreesDir != "~/projects" {
		t.Fatalf("persisted WorktreesDir = %q, want ~/projects", profile.WorktreesDir)
	}
}

func TestDirectoryPickerFindsProjectsForTypo(t *testing.T) {
	svc := services.NewService()
	homeDir := t.TempDir()
	projects := filepath.Join(homeDir, "projects")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	picker := newDirectoryPicker(svc, homeDir, setupDirectorySeeds(svc, homeDir), homeDir)
	picker.input.SetValue("~/proects")
	picker.refilter()

	if len(picker.filtered) == 0 {
		t.Fatal("filtered = empty, want ~/projects match")
	}
	if svc.CanonicalPath(picker.filtered[0]) != svc.CanonicalPath(projects) {
		t.Fatalf("first match = %q, want %q", picker.filtered[0], projects)
	}
	if selected := picker.SelectedPath(); svc.CanonicalPath(selected) != svc.CanonicalPath(projects) {
		t.Fatalf("SelectedPath() = %q, want %q", selected, projects)
	}
}

func TestSetupWizardTypoSearchSelectsProjectsFolder(t *testing.T) {
	svc := services.NewService()
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
		App:        svc.DefaultAppConfig(),
		SetupRepo:  repo,
	}, true, svc)
	setup = updateSetupForTest(t, setup, teaKey("enter"))
	setup = openRepositoryForEdit(t, setup, 0)
	for _, key := range strings.Split("~/proects", "") {
		setup = updateSetupForTest(t, setup, teaKey(key))
	}

	if len(setup.editor.worktrees.filtered) == 0 {
		t.Fatal("worktree picker has no matches for ~/proects")
	}
	if svc.CanonicalPath(setup.editor.worktrees.filtered[0]) != svc.CanonicalPath(projects) {
		t.Fatalf("first match = %q, want %q", setup.editor.worktrees.filtered[0], projects)
	}
	selected := setup.editor.worktrees.SelectedPath()
	if svc.CanonicalPath(selected) != svc.CanonicalPath(projects) {
		t.Fatalf("SelectedPath() = %q, want %q", selected, projects)
	}
}

func TestDirectoryPickerUsesTypedMissingPathWhenNoMatch(t *testing.T) {
	svc := services.NewService()
	homeDir := t.TempDir()
	picker := newDirectoryPicker(svc, homeDir, []string{homeDir}, homeDir)
	missing := filepath.Join(homeDir, "new-worktrees")
	picker.input.SetValue(missing)
	picker.refilter()

	if selected := picker.SelectedPath(); selected != missing {
		t.Fatalf("SelectedPath() = %q, want typed missing path %q", selected, missing)
	}
}

func TestCompleteSetupWritesConfigAndCreatesWorktreesDir(t *testing.T) {
	svc := services.NewService()
	cfg := testGitRepo(t)
	homeDir := cfg.HomeDir
	configPath := filepath.Join(homeDir, ".wt-manager.json")
	worktreesDir := filepath.Join(homeDir, "new-worktrees")
	setupCfg := Config{HomeDir: homeDir, ConfigPath: configPath, App: svc.DefaultAppConfig(), SetupRepo: cfg.ActiveProfile.RepositoryPath}
	setup := newSetupModel(setupCfg, true, svc)
	setup.repositories[0].WorktreesDir = worktreesDir
	setup.projectInput.SetValue("app")
	for i := range setup.agentOptions {
		setup.agentOptions[i].Selected = setup.agentOptions[i].Name == "codex"
	}
	setup.step = setupStepJira

	next, cmd := setup.updateJiraStep(teaKey("enter"))
	if cmd != nil || next.step != setupStepReview {
		t.Fatalf("JIRA did not continue to the review: step=%v cmd=%v", next.step, cmd)
	}
	next, cmd = next.updateReviewStep(teaKey("enter"))
	if cmd == nil {
		t.Fatalf("updateReviewStep() cmd = nil, status = %s", next.status)
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

// openRepositoryForEdit opens the repository editor for the idx-th repository
// and focuses its emptied worktrees folder search.
func openRepositoryForEdit(t *testing.T, setup setupModel, idx int) setupModel {
	t.Helper()
	for setup.repoCursor > idx {
		setup = updateSetupForTest(t, setup, teaKey("k"))
	}
	setup = updateSetupForTest(t, setup, teaKey("enter"))
	if setup.editor == nil || setup.editor.choosingFolder {
		t.Fatalf("enter on a repository did not open its details")
	}
	setup = updateSetupForTest(t, setup, tea.KeyMsg{Type: tea.KeyTab})
	setup.editor.worktrees.input.SetValue("")
	setup.editor.worktrees.input.CursorEnd()
	setup.editor.worktrees.refilter()
	return setup
}

func updateSetupForTest(t *testing.T, setup setupModel, key tea.KeyMsg) setupModel {
	t.Helper()
	next, cmd := setup.Update(key)
	if cmd == nil {
		return next
	}
	// Setup results arrive at once; cursor blink timers are left to expire.
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		if result, ok := msg.(setupResult); ok && result.Err != nil {
			t.Fatalf("setup command error = %v", result.Err)
		}
	case <-time.After(100 * time.Millisecond):
	}
	return next
}

func teaKey(value string) tea.KeyMsg {
	if value == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

// jiraLinkServices resolves pasted ticket links to their space without acli.
type jiraLinkServices struct {
	offlineServices
}

func (jiraLinkServices) JiraSite(context.Context) (string, error) {
	return "example.atlassian.net", nil
}

func (jiraLinkServices) ResolveJiraBoard(_ context.Context, link string) (JiraBoard, error) {
	key := link[strings.LastIndex(link, "/")+1:]
	key = key[:strings.Index(key, "-")]
	return JiraBoard{URL: link, Name: key + " space", Projects: []string{key}}, nil
}

// Assures that the JIRA setup step accepts ticket links, adds each space once
// however many of its tickets are pasted, keeps different spaces apart, and
// opens tickets on all of them together.
func TestSetupJiraStepAddsSpacesFromTicketLinks(t *testing.T) {
	cfg := testGitRepo(t)
	svc := jiraLinkServices{offlineServices{services.NewService()}}
	setup := newSetupModel(Config{HomeDir: cfg.HomeDir, ConfigPath: filepath.Join(cfg.HomeDir, ".wt-manager.json"), App: svc.DefaultAppConfig(), SetupRepo: cfg.ActiveProfile.RepositoryPath}, true, svc)
	setup.step = setupStepJira

	for _, link := range []string{
		"https://example.atlassian.net/browse/OPS-42",
		"https://example.atlassian.net/browse/WEB-3",
		"https://example.atlassian.net/browse/OPS-43",
	} {
		setup.jiraBoardInput.SetValue(link)
		setup, _ = setup.updateJiraStep(teaKey("enter"))
		if setup.statusErr || !strings.Contains(setup.status, "space") {
			t.Fatalf("adding %s: status = %q (error %v)", link, setup.status, setup.statusErr)
		}
	}
	jira := setup.jiraConfig
	if jira == nil || jira.Site != "example.atlassian.net" || jira.DefaultBoard != core.JiraAllSources || len(jira.Boards) != 2 {
		t.Fatalf("JIRA config = %+v, want the OPS and WEB spaces", jira)
	}
	if jira.Boards[0].URL != "https://example.atlassian.net/browse/OPS-43" || jira.Boards[1].Projects[0] != "WEB" {
		t.Fatalf("spaces = %+v, want OPS refreshed in place and WEB kept", jira.Boards)
	}
	if view := strings.Join(setup.jiraView(), "\n"); !strings.Contains(view, "OPS space") || !strings.Contains(view, "[whole space]") {
		t.Fatalf("JIRA step view = %q", view)
	}
}

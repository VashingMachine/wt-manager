package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	"github.com/charmbracelet/x/ansi"
)

type wizardHome struct {
	home string
	svc  offlineServices
}

// offlineServices skips the GitHub and Atlassian CLI checks, which reach the
// network.
type offlineServices struct {
	*services.Service
}

func (offlineServices) CheckGitHubCLI(context.Context, string) []string { return nil }
func (offlineServices) CheckAtlassianCLI(context.Context) []string      { return nil }

func newWizardHome(t *testing.T) wizardHome {
	t.Helper()
	return wizardHome{home: t.TempDir(), svc: offlineServices{services.NewService()}}
}

// repo creates ~/projects/<name> as a git repository with a GitHub remote.
func (w wizardHome) repo(t *testing.T, name string) string {
	t.Helper()
	path := gitInit(t, filepath.Join(w.home, "projects", name))
	runGit(t, path, "remote", "add", "origin", "git@github.com:acme/"+name+".git")
	return path
}

func (w wizardHome) config(app AppConfig, setupRepo string) Config {
	cfg := Config{HomeDir: w.home, ConfigPath: filepath.Join(w.home, ".wt-manager.json"), App: app, SetupRepo: setupRepo, ConfigExists: len(app.Profiles) > 0}
	if len(app.Profiles) > 0 {
		cfg.ActiveProfile = w.svc.ResolveProfilePaths(app.Profiles[0], w.home)
	}
	return cfg
}

func (w wizardHome) shop(repositories ...core.Repository) AppConfig {
	app := w.svc.DefaultAppConfig()
	app.DefaultProfile = "shop"
	app.Profiles = []RepositoryProfile{{Name: "shop", Repositories: repositories, Jira: &JiraConfig{Site: "acme.atlassian.net", Boards: []JiraBoard{{ID: 1, Name: "Shop board"}}}}}
	normalized, _, err := w.svc.NormalizeAppConfig(app)
	if err != nil {
		panic(err)
	}
	return normalized
}

func wizardKeys(t *testing.T, setup setupModel, keys ...string) setupModel {
	t.Helper()
	for _, key := range keys {
		setup = updateSetupForTest(t, setup, teaKeyType(key))
	}
	return setup
}

// finishWizard selects codex, walks through JIRA and the review, and returns the saved result.
func finishWizard(t *testing.T, setup setupModel) setupResult {
	t.Helper()
	if setup.step != setupStepAgents {
		setup = wizardKeys(t, setup, "tab")
	}
	for i := range setup.agentOptions {
		setup.agentOptions[i].Selected = setup.agentOptions[i].Name == "codex"
	}
	setup = wizardKeys(t, setup, "enter", "enter")
	if setup.step != setupStepReview || setup.reviewErr != nil {
		t.Fatalf("step = %v, review error = %v", setup.step, setup.reviewErr)
	}
	_, cmd := setup.Update(teaKeyType("enter"))
	if cmd == nil {
		t.Fatalf("saving returned no command; status = %q", setup.status)
	}
	result, ok := cmd().(setupResult)
	if !ok || result.Err != nil {
		t.Fatalf("save result = %#v", result)
	}
	return result
}

// Assures that setup builds a project with several repositories: the repository it starts in is listed, more are added through a picker of git repositories, and their worktrees folders and GitHub repositories get defaults.
func TestSetupWizardSetsUpProjectWithSeveralRepositories(t *testing.T) {
	w := newWizardHome(t)
	backend := w.repo(t, "backend")
	w.repo(t, "frontend")
	setup := newSetupModel(w.config(w.svc.DefaultAppConfig(), backend), true, w.svc)
	if setup.step != setupStepProject || setup.projectInput.Value() != "backend" || !strings.Contains(setup.projectHint(), "new project") {
		t.Fatalf("project step = %v %q %q", setup.step, setup.projectInput.Value(), setup.projectHint())
	}
	setup.projectInput.SetValue("staffbase")
	setup = wizardKeys(t, setup, "enter")
	if setup.step != setupStepRepositories || len(setup.repositories) != 1 || setup.repositories[0].Name != "backend" {
		t.Fatalf("repositories step = %v %#v", setup.step, setup.repositories)
	}

	setup = wizardKeys(t, setup, "a")
	if setup.editor == nil || !setup.editor.choosingFolder || labels(w.svc.Service, w.home, setup.editor.folder.filtered) != "~/projects/frontend" {
		t.Fatalf("add offers %v, want only the frontend repository", setup.editor)
	}
	setup = wizardKeys(t, setup, "enter")
	if setup.editor.nameInput.Value() != "frontend" || setup.editor.github != "acme/frontend" {
		t.Fatalf("details = %q / %q", setup.editor.nameInput.Value(), setup.editor.github)
	}
	setup = wizardKeys(t, setup, "enter", "enter")
	if setup.editor != nil || len(setup.repositories) != 2 {
		t.Fatalf("after adding, repositories = %#v", setup.repositories)
	}
	if view := ansi.Strip(setup.View(100)); !strings.Contains(view, "frontend") || !strings.Contains(view, "Continue") {
		t.Fatalf("repositories view = %q", view)
	}

	result := finishWizard(t, wizardKeys(t, setup, "enter"))
	profile := result.Config.App.Profiles[0]
	if result.Config.App.DefaultProfile != "staffbase" || profile.Name != "staffbase" || len(profile.Repositories) != 2 || profile.RepositoryPath != "" {
		t.Fatalf("saved profile = %#v", profile)
	}
	if profile.Repositories[1].GitHubRepo != "acme/frontend" || profile.Repositories[0].GitHubRepo != "acme/backend" {
		t.Fatalf("GitHub repositories = %#v", profile.Repositories)
	}
	if !result.Config.MultiRepository() || !strings.Contains(result.Message, "backend, frontend") {
		t.Fatalf("result config = %#v, message %q", result.Config.ActiveProfile.Repositories, result.Message)
	}
	if _, err := os.Stat(result.Config.ConfigPath); err != nil {
		t.Fatalf("config was not written: %v", err)
	}
}

// Assures that setup for a configured repository edits its project: every repository is listed, JIRA is kept, and removing one from the list removes it from the project only.
func TestSetupWizardEditsExistingProject(t *testing.T) {
	w := newWizardHome(t)
	api := w.repo(t, "api")
	w.repo(t, "web")
	app := w.shop(core.Repository{Name: "api", RepositoryPath: "~/projects/api", WorktreesDir: "~/worktrees/api"}, core.Repository{Name: "web", RepositoryPath: "~/projects/web", WorktreesDir: "~/worktrees/web"})
	setup := newSetupModel(w.config(app, api), false, w.svc)
	if setup.editing != "shop" || setup.projectInput.Value() != "shop" || len(setup.repositories) != 2 || setup.jiraConfig == nil {
		t.Fatalf("seeded wizard = editing %q, %d repositories, jira %v", setup.editing, len(setup.repositories), setup.jiraConfig)
	}
	if view := ansi.Strip(setup.View(100)); !strings.Contains(view, "Edit project shop") || !strings.Contains(view, "Edits project shop (api, web)") {
		t.Fatalf("project view = %q", view)
	}
	setup = wizardKeys(t, setup, "enter", "k", "k", "d")
	if len(setup.repositories) != 1 || setup.repositories[0].Name != "api" {
		t.Fatalf("after removing web, repositories = %#v", setup.repositories)
	}
	result := finishWizard(t, setup)
	profile := result.Config.App.Profiles[0]
	if len(result.Config.App.Profiles) != 1 || profile.Name != "shop" || profile.Jira == nil || len(profile.RepositoryList()) != 1 || profile.RepositoryList()[0].Name != "api" {
		t.Fatalf("saved profile = %#v", profile)
	}
}

// Assures that naming an existing project during setup adds the repository setup started in to it, and that a new name renames the edited project.
func TestSetupWizardJoinsOrRenamesProjects(t *testing.T) {
	w := newWizardHome(t)
	api := w.repo(t, "api")
	docs := w.repo(t, "docs")
	app := w.shop(core.Repository{Name: "api", RepositoryPath: "~/projects/api", WorktreesDir: "~/worktrees/api"})

	join := newSetupModel(w.config(app, docs), true, w.svc)
	join.projectInput.SetValue("shop")
	if hint := join.projectHint(); hint != "Edits project shop (api) and adds docs" {
		t.Fatalf("hint = %q", hint)
	}
	join = wizardKeys(t, join, "enter")
	if got := strings.Join(repositoryNames(join.repositories), ","); got != "api,docs" {
		t.Fatalf("joined repositories = %q", got)
	}
	joined := finishWizard(t, join)
	if len(joined.Config.App.Profiles) != 1 || len(joined.Config.App.Profiles[0].Repositories) != 2 || joined.Config.App.Profiles[0].Jira == nil {
		t.Fatalf("joined config = %#v", joined.Config.App.Profiles)
	}

	rename := newSetupModel(w.config(app, api), false, w.svc)
	rename.projectInput.SetValue("store")
	if hint := rename.projectHint(); hint != "Renames project shop to store" {
		t.Fatalf("hint = %q", hint)
	}
	renamed := finishWizard(t, wizardKeys(t, rename, "enter"))
	if len(renamed.Config.App.Profiles) != 1 || renamed.Config.App.Profiles[0].Name != "store" || renamed.Config.App.DefaultProfile != "store" {
		t.Fatalf("renamed config = %#v", renamed.Config.App.Profiles)
	}
}

// Assures that esc steps back through the wizard, closes the repository editor first, and only cancels from the first step outside startup.
func TestSetupWizardEscStepsBack(t *testing.T) {
	w := newWizardHome(t)
	api := w.repo(t, "api")
	setup := newSetupModel(w.config(w.svc.DefaultAppConfig(), api), false, w.svc)
	setup = wizardKeys(t, setup, "enter", "a")
	if setup.editor == nil {
		t.Fatal("a did not open the repository editor")
	}
	setup = wizardKeys(t, setup, "esc")
	if setup.editor != nil || setup.step != setupStepRepositories {
		t.Fatal("esc in the editor did not return to the list")
	}
	setup = wizardKeys(t, setup, "tab", "esc")
	if setup.step != setupStepRepositories {
		t.Fatalf("esc from agents went to %v", setup.step)
	}
	setup = wizardKeys(t, setup, "esc")
	if setup.step != setupStepProject {
		t.Fatalf("esc from repositories went to %v", setup.step)
	}
	if _, cmd := setup.Update(teaKeyType("esc")); cmd == nil {
		t.Fatal("esc on the first step did not cancel")
	} else if result := cmd().(setupResult); !result.Cancelled {
		t.Fatalf("result = %#v", result)
	}

	startup := newSetupModel(w.config(w.svc.DefaultAppConfig(), api), true, w.svc)
	startup, cmd := startup.Update(teaKeyType("esc"))
	if cmd != nil || !startup.statusErr || !strings.Contains(ansi.Strip(startup.View(100)), "ctrl+c quit") {
		t.Fatalf("startup esc: cmd=%v status=%q", cmd, startup.status)
	}
}

// Assures that a project needs a repository: without one the repository editor opens by itself and continuing is refused.
func TestSetupWizardRequiresRepository(t *testing.T) {
	w := newWizardHome(t)
	setup := newSetupModel(w.config(w.svc.DefaultAppConfig(), ""), true, w.svc)
	setup.setupRepo = ""
	setup.seedRepositories("")
	setup.projectInput.SetValue("empty")
	setup = wizardKeys(t, setup, "enter")
	if setup.editor == nil || !setup.editor.choosingFolder {
		t.Fatal("an empty project did not open the repository editor")
	}
	setup = wizardKeys(t, setup, "esc", "tab")
	if setup.step != setupStepRepositories || !setup.statusErr || !strings.Contains(setup.status, "at least one repository") {
		t.Fatalf("continuing without repositories: step=%v status=%q", setup.step, setup.status)
	}
}

// Assures that the wizard shows its progress: finished steps are ticked and later ones numbered.
func TestSetupWizardShowsProgress(t *testing.T) {
	w := newWizardHome(t)
	api := w.repo(t, "api")
	setup := wizardKeys(t, newSetupModel(w.config(w.svc.DefaultAppConfig(), api), true, w.svc), "enter", "tab")
	line := ansi.Strip(setup.stagesLine())
	if !strings.Contains(line, icons.OK+" Project") || !strings.Contains(line, icons.OK+" Repositories") || !strings.Contains(line, "3 Agents") || !strings.Contains(line, "5 Review") {
		t.Fatalf("progress = %q", line)
	}
}

// Assures that I edits the active project unless the app runs in a repository no project has, and that the palette can start a new project.
func TestSetupEntryPoints(t *testing.T) {
	spy := newMultiRepoSpy()
	m := multiRepoTestModel(spy)
	if setup := m.projectSetup(); setup.editing != "shop" || len(setup.repositories) != 2 {
		t.Fatalf("I set up %q with %d repositories, want the active project", setup.editing, len(setup.repositories))
	}

	w := newWizardHome(t)
	spy.cwdRepo = w.repo(t, "docs")
	setup := m.projectSetup()
	if setup.editing != "" || setup.setupRepo != spy.cwdRepo || !strings.Contains(setup.cfg.SetupReason, "not part of a project") {
		t.Fatalf("I in an unconfigured repository = editing %q, repo %q, reason %q", setup.editing, setup.setupRepo, setup.cfg.SetupReason)
	}

	next, _ := choosePaletteEntry(t, m, "project-new")
	if next.setup == nil || next.setup.editing != "" || next.setup.step != setupStepProject {
		t.Fatalf("new project setup = %#v", next.setup)
	}
	if view := ansi.Strip(next.View()); !strings.Contains(view, "Set up wt-manager") {
		t.Fatalf("view = %q", view)
	}
}

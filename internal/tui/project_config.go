package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// The config file can be changed from the app (palette helpers) or edited by
// hand. Hand edits are picked up while the app runs: the file is checked every
// configPollInterval and a changed, valid config is applied; a file with
// errors is reported and protected from in-app saves until it is fixed.

const configPollInterval = 2 * time.Second

type configPollTick struct{}

type configEditorClosed struct {
	Err error
}

func configPollCmd() tea.Cmd {
	return tea.Tick(configPollInterval, func(time.Time) tea.Msg { return configPollTick{} })
}

func (m model) checkConfigFileCmd() tea.Cmd {
	state, services, path := m.configSaveState, m.services, m.cfg.ConfigPath
	if state == nil || path == "" {
		return nil
	}
	return func() tea.Msg {
		state.mu.Lock()
		defer state.mu.Unlock()
		if result := state.checkDisk(services, path); result != nil {
			return *result
		}
		return nil
	}
}

func (m model) handleConfigPollTick() (tea.Model, tea.Cmd) {
	// Setup writes the file itself and reports the new config when it is done.
	if m.setup != nil {
		return m, configPollCmd()
	}
	return m, tea.Batch(configPollCmd(), m.checkConfigFileCmd())
}

func (m model) handleConfigFile(msg configFileResult) (tea.Model, tea.Cmd) {
	switch {
	case msg.Err != nil:
		m.statusMessage = fmt.Sprintf("Config file has errors, so in-app changes are not saved until it is fixed: %v", msg.Err)
		m.statusError = true
		return m, nil
	case msg.Recovered:
		return m, m.setTransientStatus("Config file is valid again")
	case !msg.Changed:
		return m, nil
	}
	message := "Reloaded config from " + m.services.CompactPath(m.cfg.ConfigPath, m.cfg.HomeDir)
	if msg.Rejected != "" {
		message = fmt.Sprintf("Config changed on disk and was reloaded; %q was not saved, so repeat it", msg.Rejected)
	}
	return m.applyConfig(msg.App, message)
}

// applyConfig adopts a config read from disk. The active project is reloaded
// only when its own settings changed.
func (m model) applyConfig(app AppConfig, message string) (tea.Model, tea.Cmd) {
	var previous *RepositoryProfile
	if idx := m.activeProfileIndex(); idx >= 0 {
		profile := m.cfg.App.Profiles[idx]
		previous = &profile
	}
	m.cfg.App = app
	profile := m.services.FindProfile(app.Profiles, app.DefaultProfile)
	if profile == nil {
		m.statusMessage = "The config has no projects; press I to set one up"
		m.statusError = true
		return m, nil
	}
	if previous != nil && profile.Name == m.cfg.ActiveProfile.Name && reflect.DeepEqual(*previous, *profile) {
		return m, m.setTransientStatus(message)
	}
	load := m.activateProject(*profile)
	return m, tea.Batch(load, m.startLoading(loadingWorktrees, message+". Loading worktrees..."))
}

func (m model) activeProfileIndex() int {
	for idx, profile := range m.cfg.App.Profiles {
		if profile.Name == m.cfg.ActiveProfile.Name {
			return idx
		}
	}
	return -1
}

// --- editing the file -------------------------------------------------------

// terminalEditor is the user's terminal editor, if they configured one.
func terminalEditor(getenv func(string) string) string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func (m model) editConfigInEditor() (tea.Model, tea.Cmd) {
	path, editor, services := m.cfg.ConfigPath, m.configuredEditor(), m.services
	shown := services.CompactPath(path, m.cfg.HomeDir)
	m.statusMessage = fmt.Sprintf("Opening %s in %s...", shown, editor.Name)
	m.statusError = false
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := services.OpenEditorFile(ctx, path, editor); err != nil {
			return actionResult{Err: err}
		}
		return actionResult{Message: fmt.Sprintf("Opened %s in %s; saved changes apply automatically", shown, editor.Name)}
	}
}

// editConfigInTerminal suspends the UI while the terminal editor runs, then
// applies the saved file at once.
func (m model) editConfigInTerminal() (tea.Model, tea.Cmd) {
	editor := terminalEditor(os.Getenv)
	if editor == "" {
		m.statusMessage = "Set $EDITOR or $VISUAL to edit the config in the terminal"
		m.statusError = true
		return m, nil
	}
	cmd := exec.Command("/bin/sh", "-c", editor+" "+quoteShellArg(m.cfg.ConfigPath))
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return configEditorClosed{Err: err} })
}

func (m model) handleConfigEditorClosed(msg configEditorClosed) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.statusMessage = fmt.Sprintf("Editor exited with an error: %v", msg.Err)
		m.statusError = true
	} else {
		m.statusMessage = "Config editor closed"
		m.statusError = false
	}
	return m, m.checkConfigFileCmd()
}

func quoteShellArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// reloadConfig reads the file again even if it looks unchanged.
func (m model) reloadConfig() (tea.Model, tea.Cmd) {
	app, err := m.services.ReadAppConfig(m.cfg.ConfigPath)
	if err != nil {
		m.statusMessage = fmt.Sprintf("Reload config failed: %v", err)
		m.statusError = true
		return m, nil
	}
	m.configSaveState.markSynced(m.services, app)
	return m.applyConfig(app, "Reloaded config from "+m.services.CompactPath(m.cfg.ConfigPath, m.cfg.HomeDir))
}

// --- repositories of the active project --------------------------------------

// openAddRepository asks for a repository to add to the active project, with
// the repository the app was started in offered first when it is not part of
// the project yet.
func (m model) openAddRepository() (tea.Model, tea.Cmd) {
	if m.activeProfileIndex() < 0 {
		m.statusMessage = "Set up a project first (press I)"
		m.statusError = true
		return m, nil
	}
	repositories := m.cfg.Repositories()
	initial := m.cfg.HomeDir
	if len(repositories) > 0 {
		initial = filepath.Dir(repositories[0].RepositoryPath)
	}
	if current, ok := m.services.InferRepoFromCWD(); ok && !m.projectHasRepository(current) {
		initial = current
	}
	editor := newRepositoryEditor(m.services, m.cfg.HomeDir, repositories, initial)
	m.addingRepository = &editor
	m.clearDeleteArm()
	m.statusMessage = "Choose the repository to add to " + m.cfg.ActiveProfile.Name
	m.statusError = false
	return m, nil
}

// projectSetup opens setup for the active project, unless the app was started
// in a repository no project has yet, which is then set up instead.
func (m model) projectSetup() setupModel {
	setupCfg := m.cfg
	setupCfg.SetupRepo = m.cfg.ActiveProfile.RepositoryPath
	setupCfg.SetupReason = ""
	if current, ok := m.services.InferRepoFromCWD(); ok {
		if _, _, owned := findRepositoryOwner(m.services, m.cfg.App.Profiles, m.cfg.HomeDir, current); !owned {
			setupCfg.SetupRepo = current
			setupCfg.SetupReason = m.services.CompactPath(current, m.cfg.HomeDir) + " is not part of a project yet"
		}
	}
	return newSetupModel(setupCfg, false, m.services)
}

func (m model) openSetup(setup setupModel) (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.setup = &setup
	m.loading = false
	m.statusMessage = "Setup"
	m.statusError = false
	return m, nil
}

func (m model) projectHasRepository(path string) bool {
	want := m.services.CanonicalPath(path)
	for _, repository := range m.cfg.Repositories() {
		if m.services.CanonicalPath(repository.RepositoryPath) == want {
			return true
		}
	}
	return false
}

func (m model) updateAddRepository(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	return m.updateAddRepositoryMessage(msg)
}

// updateAddRepositoryMessage feeds keys and cursor blinks to the editor.
func (m model) updateAddRepositoryMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	editor, cmd := m.addingRepository.Update(msg)
	switch {
	case editor.cancelled:
		m.addingRepository = nil
		return m, m.setTransientStatus("Add repository cancelled")
	case editor.done:
		return m.saveAddedRepository(editor)
	}
	m.addingRepository = &editor
	return m, cmd
}

func (m model) saveAddedRepository(editor repositoryEditor) (tea.Model, tea.Cmd) {
	idx := m.activeProfileIndex()
	if idx < 0 {
		m.addingRepository = nil
		m.statusMessage = "The active project is not in the config anymore"
		m.statusError = true
		return m, nil
	}
	added := editor.result
	added.RepositoryPath = m.services.CompactPath(added.RepositoryPath, m.cfg.HomeDir)
	added.WorktreesDir = m.services.CompactPath(added.WorktreesDir, m.cfg.HomeDir)
	profile := m.cfg.App.Profiles[idx]
	app, err := m.withProjectRepositories(idx, append(slices.Clone(profile.RepositoryList()), added))
	if err != nil {
		editor.done = false
		editor.err = err.Error()
		m.addingRepository = &editor
		return m, nil
	}
	if err := os.MkdirAll(m.services.ExpandPath(added.WorktreesDir, m.cfg.HomeDir), 0o755); err != nil {
		editor.done = false
		editor.err = fmt.Sprintf("Create worktrees folder failed: %v", err)
		m.addingRepository = &editor
		return m, nil
	}
	m.addingRepository = nil
	m.cfg.App = app
	load := m.activateProject(app.Profiles[idx])
	message := fmt.Sprintf("Added %s to %s", added.Name, profile.Name)
	return m, tea.Batch(m.persistConfig(message), load, m.startLoading(loadingWorktrees, message+". Loading worktrees..."))
}

// removeRepository drops a repository from the active project's config; its
// folder and worktrees stay on disk.
func (m model) removeRepository(name string) (tea.Model, tea.Cmd) {
	idx := m.activeProfileIndex()
	if idx < 0 {
		return m, nil
	}
	profile := m.cfg.App.Profiles[idx]
	var remaining []core.Repository
	for _, repository := range profile.RepositoryList() {
		if repository.Name != name {
			remaining = append(remaining, repository)
		}
	}
	if len(remaining) == 0 {
		m.statusMessage = "A project needs at least one repository"
		m.statusError = true
		return m, nil
	}
	app, err := m.withProjectRepositories(idx, remaining)
	if err != nil {
		m.statusMessage = err.Error()
		m.statusError = true
		return m, nil
	}
	m.cfg.App = app
	load := m.activateProject(app.Profiles[idx])
	message := fmt.Sprintf("Removed %s from %s; its files and worktrees stay on disk", name, profile.Name)
	return m, tea.Batch(m.persistConfig(message), load, m.startLoading(loadingWorktrees, "Reloading "+profile.Name+"..."))
}

// withProjectRepositories returns the app config with the profile at idx
// listing repositories, normalized and validated.
func (m model) withProjectRepositories(idx int, repositories []core.Repository) (AppConfig, error) {
	app := cloneAppConfig(m.cfg.App)
	app.Profiles[idx] = app.Profiles[idx].WithRepositories(repositories)
	normalized, _, err := m.services.NormalizeAppConfig(app)
	return normalized, err
}

func renderAddRepositoryModal(m model) string {
	editor := m.addingRepository
	title := "Add Repository to " + m.cfg.ActiveProfile.Name
	width := max(48, min(84, m.width-8))
	return renderWideModal(title, editor.View(width-6), editor.hints(), width)
}

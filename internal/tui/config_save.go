package tui

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
)

type configSaveState struct {
	mu       sync.Mutex
	revision atomic.Uint64
}

func (m model) persistConfig(message string) tea.Cmd {
	state := m.configSaveState
	if state == nil {
		state = &configSaveState{}
	}
	revision := state.revision.Add(1)
	app := cloneAppConfig(m.cfg.App)
	path, services := m.cfg.ConfigPath, m.services
	return func() tea.Msg {
		state.mu.Lock()
		defer state.mu.Unlock()
		if revision < state.revision.Load() {
			return nil
		}
		err := services.WriteAppConfig(path, app)
		// A newer choice may arrive during a disk write. Its queued write will
		// replace this one, so avoid showing an outdated success or error.
		if revision < state.revision.Load() {
			return nil
		}
		if err != nil {
			return actionResult{Err: fmt.Errorf("save config failed: %w", err)}
		}
		return actionResult{Message: message}
	}
}

func cloneAppConfig(app AppConfig) AppConfig {
	app.Agents = slices.Clone(app.Agents)
	app.Profiles = slices.Clone(app.Profiles)
	for idx := range app.Profiles {
		if original := app.Profiles[idx].Jira; original != nil {
			jira := *original
			jira.Boards = slices.Clone(jira.Boards)
			for boardIdx := range jira.Boards {
				jira.Boards[boardIdx].Projects = slices.Clone(jira.Boards[boardIdx].Projects)
			}
			app.Profiles[idx].Jira = &jira
		}
	}
	return app
}

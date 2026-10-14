package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// configSaveState serializes config writes and remembers what the file held
// when the app last read or wrote it. The file may also be edited outside the
// app (see the palette's "Edit configuration file"), so a write first checks
// that it would not overwrite such edits.
type configSaveState struct {
	mu       sync.Mutex
	revision atomic.Uint64
	// synced fingerprints the config last read from or written to disk; empty
	// until known, which disables change detection.
	synced  string
	modTime time.Time
	size    int64
	// invalid is why the file on disk cannot be used. Saving is refused until
	// it is fixed, so the app never overwrites a half-finished edit.
	invalid error
}

// configFileResult reports the config file changing on disk.
type configFileResult struct {
	App AppConfig
	// Changed is set when the file holds a different, valid config.
	Changed bool
	// Recovered is set when a file that had errors is valid again.
	Recovered bool
	Err       error
	// Rejected names the in-app change that was not saved because the file
	// had changed on disk first.
	Rejected string
}

func newConfigSaveState(services core.ConfigService, app AppConfig) *configSaveState {
	state := &configSaveState{}
	state.markSynced(services, app)
	return state
}

// markSynced records app as what the config file holds.
func (s *configSaveState) markSynced(services core.ConfigService, app AppConfig) {
	if s == nil || services == nil {
		return
	}
	fingerprint := configFingerprint(services, app)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.synced = fingerprint
	s.invalid = nil
	s.modTime, s.size = time.Time{}, -1
}

// configFingerprint identifies a config by its normalized content, so a file
// that differs only in formatting or defaults counts as unchanged.
func configFingerprint(services core.ConfigService, app AppConfig) string {
	app = cloneAppConfig(app)
	if normalized, _, err := services.NormalizeAppConfig(app); err == nil {
		app = normalized
	}
	data, _ := json.Marshal(app)
	return string(data)
}

// checkDisk compares the config file with the synced config. It must be
// called with mu held and returns nil when nothing changed.
func (s *configSaveState) checkDisk(services core.ConfigService, path string) *configFileResult {
	if s.synced == "" || path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || (info.ModTime().Equal(s.modTime) && info.Size() == s.size) {
		return nil
	}
	s.modTime, s.size = info.ModTime(), info.Size()
	app, err := services.ReadAppConfig(path)
	if err != nil {
		s.invalid = err
		return &configFileResult{Err: err}
	}
	wasInvalid := s.invalid != nil
	s.invalid = nil
	if fingerprint := configFingerprint(services, app); fingerprint != s.synced {
		s.synced = fingerprint
		return &configFileResult{App: app, Changed: true}
	}
	if wasInvalid {
		return &configFileResult{Recovered: true}
	}
	return nil
}

func (s *configSaveState) recordWrite(services core.ConfigService, path string, app AppConfig) {
	if s.synced == "" {
		return
	}
	s.synced = configFingerprint(services, app)
	if info, err := os.Stat(path); err == nil {
		s.modTime, s.size = info.ModTime(), info.Size()
	}
}

func (m model) persistConfig(message string) tea.Cmd {
	return m.persistConfigWith(message, false)
}

// persistConfigQuietly saves a preference without announcing it.
func (m model) persistConfigQuietly() tea.Cmd {
	return m.persistConfigWith("", true)
}

func (m model) persistConfigWith(message string, quiet bool) tea.Cmd {
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
		// Edits saved to the file outside the app win over the in-app change.
		if changed := state.checkDisk(services, path); changed != nil && changed.Changed {
			changed.Rejected = message
			if changed.Rejected == "" {
				changed.Rejected = "the last setting"
			}
			return *changed
		}
		if state.invalid != nil {
			return actionResult{Err: fmt.Errorf("config not saved: fix %s first (%v)", path, state.invalid)}
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
		state.recordWrite(services, path, app)
		if quiet {
			return nil
		}
		return actionResult{Message: message}
	}
}

func cloneAppConfig(app AppConfig) AppConfig {
	app.Agents = slices.Clone(app.Agents)
	app.Profiles = slices.Clone(app.Profiles)
	for idx := range app.Profiles {
		app.Profiles[idx].Repositories = slices.Clone(app.Profiles[idx].Repositories)
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

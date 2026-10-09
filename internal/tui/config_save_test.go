package tui

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

type configSaveTestServices struct {
	core.Services
	write func(string, AppConfig) error
}

func (s configSaveTestServices) WriteAppConfig(path string, app AppConfig) error {
	return s.write(path, app)
}

// Reversed save-command execution persists only the latest user settings and suppresses stale confirmations.
func TestPersistConfigLatestChoiceWins(t *testing.T) {
	var saved []AppConfig
	m := model{
		configSaveState: &configSaveState{},
		cfg:             Config{ConfigPath: "/tmp/config", App: AppConfig{DefaultAgent: "old"}},
		services: configSaveTestServices{write: func(path string, app AppConfig) error {
			if path != "/tmp/config" {
				t.Fatalf("config path = %q, want /tmp/config", path)
			}
			saved = append(saved, app)
			return nil
		}},
	}
	older := m.persistConfig("Old settings saved")
	m.cfg.App.DefaultAgent = "new"
	newer := m.persistConfig("New settings saved")
	if result, ok := newer().(actionResult); !ok || result.Err != nil || result.Message != "New settings saved" {
		t.Fatalf("latest save result = %#v", result)
	}
	if result := older(); result != nil {
		t.Fatalf("stale save result = %#v, want nil", result)
	}
	if len(saved) != 1 || saved[0].DefaultAgent != "new" {
		t.Fatalf("saved settings = %#v, want one latest snapshot", saved)
	}
}

// A newer save waits for an already-running write, then leaves the latest settings on disk.
func TestPersistConfigSerializesRunningWrites(t *testing.T) {
	oldEntered := make(chan struct{})
	newEntered := make(chan struct{})
	releaseOld := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseOld) }) }
	t.Cleanup(release)
	var saved []string
	m := model{
		configSaveState: &configSaveState{},
		cfg:             Config{App: AppConfig{DefaultAgent: "old"}},
		services: configSaveTestServices{write: func(_ string, app AppConfig) error {
			if app.DefaultAgent == "old" {
				close(oldEntered)
				<-releaseOld
			} else {
				close(newEntered)
			}
			saved = append(saved, app.DefaultAgent)
			return nil
		}},
	}
	older := m.persistConfig("Old settings saved")
	oldResult := make(chan tea.Msg, 1)
	go func() { oldResult <- older() }()
	select {
	case <-oldEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("older save never entered the writer")
	}
	m.cfg.App.DefaultAgent = "new"
	newer := m.persistConfig("New settings saved")
	newResult := make(chan tea.Msg, 1)
	go func() { newResult <- newer() }()
	select {
	case <-newEntered:
		t.Fatal("newer save overlapped the older disk write")
	case <-time.After(25 * time.Millisecond):
	}
	release()
	select {
	case result := <-oldResult:
		if result != nil {
			t.Fatalf("superseded running save result = %#v, want nil", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("older save did not finish")
	}
	select {
	case result := <-newResult:
		if got, ok := result.(actionResult); !ok || got.Err != nil || got.Message != "New settings saved" {
			t.Fatalf("newer save result = %#v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("newer save did not finish")
	}
	if !reflect.DeepEqual(saved, []string{"old", "new"}) {
		t.Fatalf("write order = %#v, want old then new", saved)
	}
}

// Queued saves retain their original agent, profile, and nested JIRA settings when the model changes afterward.
func TestPersistConfigCapturesIndependentSnapshot(t *testing.T) {
	app := AppConfig{
		Agents: []AgentTool{{Name: "codex", Command: "codex"}},
		Profiles: []RepositoryProfile{{
			Name: "original",
			Jira: &core.JiraConfig{Site: "original-site", Boards: []core.JiraBoard{{Name: "original-board", Projects: []string{"APP"}}}},
		}},
	}
	var saved AppConfig
	m := model{
		configSaveState: &configSaveState{},
		cfg:             Config{App: app},
		services: configSaveTestServices{write: func(_ string, snapshot AppConfig) error {
			saved = snapshot
			return nil
		}},
	}
	cmd := m.persistConfig("Saved")
	m.cfg.App.Agents[0].Command = "changed"
	m.cfg.App.Profiles[0].Name = "changed"
	m.cfg.App.Profiles[0].Jira.Site = "changed"
	m.cfg.App.Profiles[0].Jira.Boards[0].Name = "changed"
	m.cfg.App.Profiles[0].Jira.Boards[0].Projects[0] = "CHANGED"
	if result, ok := cmd().(actionResult); !ok || result.Err != nil {
		t.Fatalf("save result = %#v", result)
	}
	if saved.Agents[0].Command != "codex" || saved.Profiles[0].Name != "original" || saved.Profiles[0].Jira.Site != "original-site" || saved.Profiles[0].Jira.Boards[0].Name != "original-board" || saved.Profiles[0].Jira.Boards[0].Projects[0] != "APP" {
		t.Fatalf("snapshot changed with model: %#v / %#v", saved, saved.Profiles[0].Jira)
	}
}

// Failures from the latest configuration write reach the application's action error handler.
func TestPersistConfigReportsWriteFailure(t *testing.T) {
	m := model{
		configSaveState: &configSaveState{},
		services: configSaveTestServices{write: func(string, AppConfig) error {
			return errors.New("disk is read-only")
		}},
	}
	result, ok := m.persistConfig("Saved")().(actionResult)
	if !ok || result.Err == nil || !strings.Contains(result.Err.Error(), "save config failed: disk is read-only") || result.Message != "" {
		t.Fatalf("failed save result = %#v", result)
	}
}

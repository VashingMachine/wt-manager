package tui

import (
	"context"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
)

type enterActionSpyServices struct {
	core.Services
	opened    string
	saved     AppConfig
	saveCount int
}

func newEnterActionSpyServices() *enterActionSpyServices {
	return &enterActionSpyServices{Services: services.NewService()}
}

func (s *enterActionSpyServices) OpenVSCodeWorktree(ctx context.Context, wt Worktree) error {
	s.opened = core.EnterActionVSCode
	return nil
}

func (s *enterActionSpyServices) OpenAgent(ctx context.Context, wt Worktree, agent AgentTool) error {
	s.opened = core.EnterActionAgent
	return nil
}

func (s *enterActionSpyServices) OpenTerminal(ctx context.Context, wt Worktree) error {
	s.opened = core.EnterActionTerminal
	return nil
}

func (s *enterActionSpyServices) WriteAppConfig(configPath string, appConfig AppConfig) error {
	s.saved = appConfig
	s.saveCount++
	return nil
}

func TestEnterActionSelectorOpensAndEscCancels(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.DefaultEnterAction = core.EnterActionVSCode

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	got := next.(model)
	if !got.selectingEnterAction {
		t.Fatal("e did not open Enter action selector")
	}
	if got.enterActionCursor != 0 {
		t.Fatalf("enterActionCursor = %d, want 0", got.enterActionCursor)
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(model)
	if got.selectingEnterAction {
		t.Fatal("esc did not close Enter action selector")
	}
	if got.cfg.App.DefaultEnterAction != core.EnterActionVSCode {
		t.Fatalf("DefaultEnterAction = %q, want vscode", got.cfg.App.DefaultEnterAction)
	}
}

func TestEnterActionSelectorSavesHighlightedAction(t *testing.T) {
	m, spy := enterActionTestModel()
	m.cfg.App.DefaultEnterAction = core.EnterActionVSCode

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	got := next.(model)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	got = next.(model)
	next, cmd := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(model)
	if got.selectingEnterAction {
		t.Fatal("enter did not close Enter action selector")
	}
	if got.cfg.App.DefaultEnterAction != core.EnterActionAgent {
		t.Fatalf("DefaultEnterAction = %q, want agent", got.cfg.App.DefaultEnterAction)
	}
	if cmd == nil {
		t.Fatal("enter did not return save command")
	}
	if result, ok := cmd().(actionResult); !ok || result.Err != nil {
		t.Fatalf("save command result = %#v, want successful actionResult", result)
	}
	if spy.saveCount != 1 {
		t.Fatalf("saveCount = %d, want 1", spy.saveCount)
	}
	if spy.saved.DefaultEnterAction != core.EnterActionAgent {
		t.Fatalf("saved DefaultEnterAction = %q, want agent", spy.saved.DefaultEnterAction)
	}
}

func TestEnterDispatchesConfiguredWorktreeAction(t *testing.T) {
	for _, action := range []string{core.EnterActionVSCode, core.EnterActionAgent, core.EnterActionTerminal} {
		t.Run(action, func(t *testing.T) {
			m, spy := enterActionTestModel()
			m.cfg.App.DefaultEnterAction = action

			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("enter returned nil command")
			}
			if result, ok := cmd().(actionResult); !ok || result.Err != nil {
				t.Fatalf("open command result = %#v, want successful actionResult", result)
			}
			if spy.opened != action {
				t.Fatalf("opened action = %q, want %q", spy.opened, action)
			}
		})
	}
}

func TestEnterActionBlocksMissingAndPrunableWorktrees(t *testing.T) {
	for name, wt := range map[string]Worktree{
		"missing":  {Name: "missing", Path: "/tmp/missing", Missing: true},
		"prunable": {Name: "prunable", Path: "/tmp/prunable", Prunable: true},
	} {
		t.Run(name, func(t *testing.T) {
			m, spy := enterActionTestModel()
			m.cfg.App.DefaultEnterAction = core.EnterActionTerminal
			m.visibleWorktrees = []Worktree{wt}
			m.rebuildRows()
			m.table.SetCursor(0)

			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			got := next.(model)
			if cmd != nil {
				t.Fatal("enter returned command for unavailable worktree")
			}
			if !got.statusError {
				t.Fatal("statusError = false, want true")
			}
			if spy.opened != "" {
				t.Fatalf("opened action = %q, want none", spy.opened)
			}
		})
	}
}

func TestEnterActionAgentRequiresConfiguredAgent(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.DefaultEnterAction = core.EnterActionAgent
	m.cfg.App.Agents = nil
	m.cfg.App.DefaultAgent = ""

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if cmd != nil {
		t.Fatal("enter returned command without a configured agent")
	}
	if !got.statusError {
		t.Fatal("statusError = false, want true")
	}
	if got.statusMessage != "No AI agent configured" {
		t.Fatalf("statusMessage = %q, want No AI agent configured", got.statusMessage)
	}
}

func enterActionTestModel() (model, *enterActionSpyServices) {
	spy := newEnterActionSpyServices()
	m := NewModel(Config{
		ConfigPath: "/tmp/wt-manager.json",
		ActiveProfile: RepositoryProfile{
			Name:           "app",
			RepositoryPath: "/Users/test/projects/app",
			WorktreesDir:   "/Users/test/projects",
			RemoteName:     "origin",
		},
		App: AppConfig{
			DefaultAgent:       "opencode",
			DefaultEnterAction: core.EnterActionVSCode,
			Agents:             []AgentTool{{Name: "opencode", Command: "opencode"}},
		},
	}, spy)
	m.ready = true
	m.loading = false
	m.width = 120
	m.height = 30
	wt := layoutTestWorktree()
	m.allWorktrees = []Worktree{wt}
	m.visibleWorktrees = m.allWorktrees
	m.resize()
	m.rebuildRows()
	m.table.SetCursor(0)
	return m, spy
}

var _ core.Services = (*enterActionSpyServices)(nil)

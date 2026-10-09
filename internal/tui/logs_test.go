package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type logSpyServices struct {
	*enterActionSpyServices
	content string
	err     error
}

// Full action errors remain in the session log after newer status messages replace them or expire.
func TestSessionLogRetainsReplacedStatusError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	m, _ := enterActionTestModel()
	s := services.NewService()
	m.services = s
	fullError := "open IntelliJ IDEA: " + strings.Repeat("long execution detail ", 80) + "final error detail"
	next, _ := m.Update(actionResult{Err: errors.New(fullError)})
	m = next.(model)
	next, _ = m.Update(actionResult{Message: "another action completed"})
	m = next.(model)
	next, _ = m.Update(statusExpired{Seq: m.statusSeq})
	m = next.(model)
	if m.statusMessage != "" {
		t.Fatal("status did not expire")
	}
	_, content, err := s.ReadSessionLog()
	if err != nil || !strings.Contains(content, fullError) || !strings.Contains(content, "another action completed") {
		t.Fatalf("session log lost an earlier action result: %v\n%s", err, content)
	}
}

func (s *logSpyServices) ReadSessionLog() (string, string, error) {
	return "/tmp/wt-manager-session.log", s.content, s.err
}

func logTestModel() (model, *logSpyServices) {
	m, base := enterActionTestModel()
	spy := &logSpyServices{enterActionSpyServices: base}
	for i := range 100 {
		spy.content += fmt.Sprintf("diagnostic line %03d %s\n", i, strings.Repeat("output ", 15))
	}
	m.services = spy
	m.width, m.height, m.ready = 72, 18, true
	return m, spy
}

// The log command is searchable in every view and opens the current session's latest output.
func TestPaletteSessionLogCommandAvailableInAllViews(t *testing.T) {
	for _, mode := range []viewMode{modeWorktrees, modePRs, modeTickets} {
		m, _ := logTestModel()
		m.mode = mode
		m = openTestPalette(m)
		m.paletteInput.SetValue("logs")
		matches := m.paletteMatches()
		if len(matches) == 0 || matches[0].ID != "logs" {
			t.Fatalf("mode %d: logs command missing or not first: %#v", mode, matches)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(model)
		if !m.logsOpen || m.paletteOpen || cmd == nil {
			t.Fatalf("mode %d: command did not open log viewer", mode)
		}
		next, _ = m.Update(cmd())
		m = next.(model)
		view := ansi.Strip(m.View())
		for _, want := range []string{"Session logs", "/tmp/wt-manager-session.log", "diagnostic line 099", "esc close", "r refresh"} {
			if !strings.Contains(view, want) {
				t.Fatalf("mode %d: missing %q in log viewer:\n%s", mode, want, view)
			}
		}
	}
}

// Logs wrap long lines, scroll to early output, survive resizing, and close without changing the underlying reader or selection.
func TestSessionLogViewerScrollResizeAndClose(t *testing.T) {
	m, _ := logTestModel()
	m.mode, m.prDiffMode = modePRs, true
	m.detail.SetContent("original diff content")
	path, index := m.selectedPath, m.selectedIndex
	m, cmd := choosePaletteEntry(t, m, "logs")
	next, _ := m.Update(cmd())
	m = next.(model)
	for _, size := range []tea.WindowSizeMsg{{Width: 24, Height: 8}, {Width: 90, Height: 26}, {Width: 4, Height: 2}} {
		next, _ = m.Update(size)
		m = next.(model)
		view := m.View()
		if lipgloss.Height(view) > size.Height {
			t.Fatalf("logs overflow height %d", size.Height)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size.Width {
				t.Fatalf("logs overflow width %d: %q", size.Width, line)
			}
		}
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 72, Height: 18})
	m = next.(model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyHome})
	m = next.(model)
	if !strings.Contains(ansi.Strip(m.View()), "diagnostic line 000") {
		t.Fatal("could not scroll to the beginning of the logs")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(model)
	if m.logsViewport.YOffset == 0 {
		t.Fatal("page down did not scroll logs")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	if m.logsOpen || !m.prDiffMode || m.mode != modePRs || m.selectedPath != path || m.selectedIndex != index {
		t.Fatal("closing logs changed the underlying view")
	}
}

// Refresh reads newly appended diagnostics while delayed results cannot reopen or overwrite a newer viewer.
func TestSessionLogViewerRefreshAndStaleResults(t *testing.T) {
	m, spy := logTestModel()
	m, firstCmd := choosePaletteEntry(t, m, "logs")
	oldResult := firstCmd()
	spy.content += "new failure at end\n"
	next, cmd := m.Update(teaKey("r"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if !strings.Contains(ansi.Strip(m.View()), "new failure at end") {
		t.Fatal("refresh did not show appended logs")
	}
	next, _ = m.Update(oldResult)
	m = next.(model)
	if !strings.Contains(m.logsContent, "new failure at end") {
		t.Fatal("stale read overwrote refreshed logs")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)
	next, _ = m.Update(oldResult)
	if next.(model).logsOpen {
		t.Fatal("delayed logs reopened the viewer")
	}
}

// Read failures remain fully inspectable, allow retry, and never prevent closing the viewer.
func TestSessionLogViewerReadError(t *testing.T) {
	m, spy := logTestModel()
	spy.err = errors.New("cannot read log file: " + strings.Repeat("diagnostic ", 40) + "final detail")
	m, cmd := choosePaletteEntry(t, m, "logs")
	next, _ := m.Update(cmd())
	m = next.(model)
	if !strings.Contains(ansi.Strip(m.View()), "final detail") {
		t.Fatal("log read error was truncated")
	}
	spy.err, spy.content = nil, "logs available again"
	next, cmd = m.Update(teaKey("r"))
	m = next.(model)
	next, _ = m.Update(cmd())
	m = next.(model)
	if !strings.Contains(ansi.Strip(m.View()), "logs available again") {
		t.Fatal("could not retry failed log read")
	}
}

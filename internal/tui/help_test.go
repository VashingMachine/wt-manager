package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// helpPageLists reports whether a line of the help page shows a key followed
// by what it does.
func helpPageLists(content, key, text string) bool {
	return regexp.MustCompile(`(?m)(^|\s)` + regexp.QuoteMeta(key) + `\s+` + regexp.QuoteMeta(text) + `(\s|$)`).MatchString(content)
}

// helpPageText is everything the help page shows across its whole scroll
// range.
func helpPageText(m model) string {
	var text strings.Builder
	for offset := 0; offset <= m.helpScrollLimit(); offset++ {
		m.helpScroll = offset
		text.WriteString(ansi.Strip(m.View()))
		text.WriteString("\n")
	}
	return text.String()
}

// Assures that the help page opens on the keys of the view it was opened
// from, marks that view, and that tab, shift+tab and the arrows walk through
// the other views and wrap around.
func TestHelpPageStartsOnTheCurrentViewAndSwitchesViews(t *testing.T) {
	m, _ := keybindingModeModel(modePRs)
	m, _ = tapKeys(m, "h")
	if !m.showingHelp || m.helpTab != modePRs {
		t.Fatalf("help opened on view %d, showing=%v", m.helpTab, m.showingHelp)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "● PR Radar") || !helpPageLists(view, "A", "approve the pull request") || !helpPageLists(view, "/", "filter the rows") {
		t.Fatalf("PR Radar help:\n%s", view)
	}
	if strings.Contains(view, "sweep stale worktrees") || strings.Contains(view, "change status") {
		t.Fatalf("PR Radar help lists keys of other views:\n%s", view)
	}

	steps := []struct {
		key  string
		view viewMode
		text string
	}{
		{"tab", modeTickets, "change status"},
		{"tab", modeWorktrees, "sweep stale worktrees"},
		{"tab", modePRs, "approve the pull request"},
		{"shift+tab", modeWorktrees, "sweep stale worktrees"},
		{"left", modeTickets, "change status"},
		{"right", modeWorktrees, "sweep stale worktrees"},
	}
	for _, step := range steps {
		m, _ = tapKeys(m, step.key)
		if view := ansi.Strip(m.View()); m.helpTab != step.view || !strings.Contains(view, step.text) || !strings.Contains(view, "● PR Radar") || !helpPageLists(view, "/", "filter the rows") {
			t.Fatalf("after %s: view %d, want %d with %q:\n%s", step.key, m.helpTab, step.view, step.text, view)
		}
		if !m.isPRMode() {
			t.Fatal("browsing the help changed the view of the app")
		}
	}

	m, _ = tapKeys(m, "esc")
	tickets, _ := keybindingModeModel(modeTickets)
	tickets.helpTab = modePRs
	if tickets, _ = tapKeys(tickets, "h"); tickets.helpTab != modeTickets {
		t.Fatalf("help reopened on view %d, want the current one", tickets.helpTab)
	}
}

// Assures that the help page never outgrows the terminal, that an 80x24
// terminal shows every key of a view without scrolling, and that smaller
// terminals scroll to the rest and say how much is left.
func TestHelpPageFitsTerminalAndScrolls(t *testing.T) {
	for _, size := range [][2]int{{200, 60}, {120, 32}, {100, 30}, {80, 24}, {69, 24}, {60, 18}, {40, 12}, {30, 8}} {
		for _, mode := range helpViews {
			m, _ := keybindingModeModel(mode)
			next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m, _ = tapKeys(next.(model), "h")
			for _, key := range []string{"", "G", "pgup", "j"} {
				if key != "" {
					m, _ = tapKeys(m, key)
				}
				if view := m.View(); lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
					t.Fatalf("help for view %d overflowed %v after %q: %dx%d\n%s", mode, size, key, lipgloss.Width(view), lipgloss.Height(view), ansi.Strip(view))
				}
			}
			if size[0] >= 80 && size[1] >= 24 && m.helpScrollLimit() != 0 {
				t.Fatalf("help for view %d needs scrolling at %v", mode, size)
			}
			// Whatever the size, every key can be reached.
			text := helpPageText(m)
			for _, want := range []string{"move selection", "quit (ctrl+c too)", "command palette", "create a worktree for it", "new worktree"} {
				if (want == "new worktree") != (mode == modeWorktrees) && (want == "new worktree" || want == "create a worktree for it") {
					continue
				}
				if size[0] >= 40 && !strings.Contains(text, want) {
					t.Fatalf("help for view %d at %v never shows %q:\n%s", mode, size, want, text)
				}
			}
		}
	}

	m, _ := enterActionTestModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	m, _ = tapKeys(next.(model), "h")
	limit := m.helpScrollLimit()
	top := ansi.Strip(m.View())
	if limit == 0 || !strings.Contains(top, "more") || !strings.Contains(top, "Open") || strings.Contains(top, "quit (ctrl+c too)") {
		t.Fatalf("a small terminal does not show the start of a scrolling help: limit=%d\n%s", limit, top)
	}
	if down, _ := tapKeys(m, "j", "down"); down.helpScroll != 2 {
		t.Fatalf("j and down scrolled to %d", down.helpScroll)
	}
	if up, _ := tapKeys(m, "j", "j", "k", "up", "up"); up.helpScroll != 0 {
		t.Fatalf("scrolling up went to %d", up.helpScroll)
	}
	end, _ := tapKeys(m, "G")
	bottom := ansi.Strip(end.View())
	if end.helpScroll != limit || !strings.Contains(bottom, "quit (ctrl+c too)") || !strings.Contains(bottom, "end") {
		t.Fatalf("G did not reach the end: scroll=%d limit=%d\n%s", end.helpScroll, limit, bottom)
	}
	if past, _ := tapKeys(end, "j", "pgdown", "space"); past.helpScroll != limit {
		t.Fatalf("scrolled past the end to %d", past.helpScroll)
	}
	if home, _ := tapKeys(end, "g"); home.helpScroll != 0 {
		t.Fatal("g did not return to the top")
	}
	if other, _ := tapKeys(end, "tab"); other.helpScroll != 0 || other.helpTab != modePRs {
		t.Fatalf("switching views kept the scroll position %d", other.helpScroll)
	}
}

// Assures that the help page closes with esc, enter and its own key, runs no
// command of the view behind it, and leads to the keybindings screen and the
// palette.
func TestHelpPageClosesAndLeadsToCustomizing(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{cmdHelp: {"?"}}
	help, _ := tapKeys(m, "?")
	if !help.showingHelp {
		t.Fatal("the help key did not open the help page")
	}
	if view := ansi.Strip(help.View()); !strings.Contains(view, "?/esc close") || !strings.Contains(view, ", customize keys") || !helpPageLists(view, "?", "this help") {
		t.Fatalf("help page hints:\n%s", view)
	}
	for _, key := range []string{"esc", "enter", "?"} {
		if closed, _ := tapKeys(help, key); closed.showingHelp {
			t.Fatalf("%s did not close the help page", key)
		}
	}
	for _, key := range []string{"r", "d", "n", "q", "h"} {
		if got, cmd := tapKeys(help, key); !got.showingHelp || got.loading || got.creatingWorktree || got.deleteArmedPath != "" || cmd != nil {
			t.Fatalf("%s ran a command behind the help page", key)
		}
	}
	if settings, _ := tapKeys(help, ","); !settings.keybindings.open || settings.showingHelp {
		t.Fatal("the keybindings key did not lead from the help to the keybindings screen")
	}
	if palette, _ := tapKeys(help, "ctrl+p"); !palette.paletteOpen || palette.showingHelp {
		t.Fatal("the palette did not open over the help page")
	}
	if _, cmd := tapKeys(help, "ctrl+c"); cmd == nil {
		t.Fatal("ctrl+c did not quit from the help page")
	}

	// A confirmation that arrived while the page was open waits behind it.
	help.confirmingPRApproval = true
	if closed, _ := tapKeys(help, "?"); closed.showingHelp || !closed.confirmingPRApproval {
		t.Fatal("the help key did not close the page in front of a waiting confirmation")
	}
}

// Assures that the help page names the configured editor, agent and Enter
// action, shows a command's extra keys as a count, and marks a command
// without a key.
func TestHelpPageDescribesTheCurrentSetup(t *testing.T) {
	m, _ := enterActionTestModel()
	m.showingHelp = true
	m.cfg.App.Editor = core.EditorTool{Name: "IntelliJ IDEA", Command: "idea"}
	for action, want := range map[string]string{
		core.EnterActionVSCode:   "open in IntelliJ IDEA",
		core.EnterActionAgent:    "open with opencode",
		core.EnterActionTerminal: "open in a terminal",
	} {
		m.cfg.App.DefaultEnterAction = action
		if view := ansi.Strip(m.View()); !helpPageLists(view, "enter", want) || !helpPageLists(view, "v", "open in IntelliJ IDEA") || !helpPageLists(view, "i", "open with opencode") {
			t.Fatalf("Enter action %q is not described as %q:\n%s", action, want, view)
		}
	}

	m.cfg.App.Keybindings = map[string][]string{"refresh": {"r", "f5", "ctrl+r"}, "sweep": {}, "worktrees": {"W"}, "new": {"c", "n"}}
	view := ansi.Strip(m.View())
	for key, text := range map[string]string{"r +2": "refresh this view", "—": "sweep stale worktrees", "W": "worktrees", "c/n": "new worktree", "↑/k": "↓/j"} {
		if !helpPageLists(view, key, text) {
			t.Fatalf("help page does not list %q as %q:\n%s", key, text, view)
		}
	}
	m.cfg.App.Keybindings = nil
	if view = ansi.Strip(m.View()); helpPageLists(view, "—", "worktrees") || strings.Contains(view, "+2") {
		t.Fatalf("default help lists a command that has no key by default:\n%s", view)
	}
}

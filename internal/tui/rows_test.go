package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// uncoveredSegments returns the visible parts of a rendered row that are not
// painted with the background whose SGR parameter is bg.
func uncoveredSegments(line string, bg string) []string {
	var uncovered []string
	for _, segment := range strings.Split(line, "\x1b[0m") {
		if ansi.Strip(segment) == "" {
			continue
		}
		if !strings.Contains(segment, bg) {
			uncovered = append(uncovered, ansi.Strip(segment))
		}
	}
	return uncovered
}

// Assures that a selected, armed, or blurred-selected row is highlighted across every cell in all tables, colored cells and the repository column included.
func TestRowHighlightSpansEveryCell(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	wt := &Worktree{Name: "backend-ACT", Branch: "ACT-3986", Repository: "backend", Status: RepoStatus{Dirty: true, Unstaged: 1}, PR: &PullRequest{Number: 270, State: "OPEN"}, Commit: CommitInfo{Relative: "1 day ago", Subject: "fix"}}
	pr := remotePRFixture(270, "OPEN", "me", "Fix checkout", "ACT-3986")
	ticket := Ticket{Key: "ACT-3986", Type: "Story", Status: "In Progress", StatusCategory: "indeterminate", Summary: "Fix checkout", PR: wt.PR}
	rows := map[string]func(selected, focused, armed bool) string{
		"worktrees": func(selected, focused, armed bool) string {
			return renderTableRow(buildColumns(120), worktreeRowCells(*wt, false), wt, selected, focused, armed)
		},
		"repository worktrees": func(selected, focused, armed bool) string {
			return renderTableRow(buildRepositoryColumns(120), append([]string{"backend"}, worktreeRowCells(*wt, false)...), wt, selected, focused, armed)
		},
		"pull requests": func(selected, focused, _ bool) string {
			return renderPRTableRow(buildPRColumns(120), model{}.prRowCells(pr, false), &pr, selected, focused)
		},
		"tickets": func(selected, focused, _ bool) string {
			return renderTicketTableRow(buildTicketColumns(120), model{}.ticketRowCells(ticket, false), &ticket, selected, focused)
		},
	}
	for name, render := range rows {
		for _, state := range []struct {
			label                    string
			selected, focused, armed bool
			bg                       string
		}{
			{"selected", true, true, false, "48;5;62"},
			{"blurred", true, false, false, "48;5;237"},
			{"armed", true, true, true, "48;5;52"},
		} {
			if state.armed && (name == "pull requests" || name == "tickets") {
				continue
			}
			line := render(state.selected, state.focused, state.armed)
			if uncovered := uncoveredSegments(line, state.bg); len(uncovered) > 0 {
				t.Fatalf("%s %s row leaves %q unhighlighted", state.label, name, uncovered)
			}
		}
		if plain := render(false, true, false); strings.Contains(plain, "48;5;62") {
			t.Fatalf("unselected %s row is highlighted", name)
		}
	}
}

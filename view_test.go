package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

func TestBuildColumnsFitRenderedTableWidth(t *testing.T) {
	for _, width := range []int{40, 60, 70, 71, 80, 96, 160} {
		columns := buildColumns(width)
		renderedWidth := lipgloss.Width(renderTableHeader(columns))
		if renderedWidth > width {
			t.Fatalf("rendered header width = %d, want <= %d for columns %#v", renderedWidth, width, columns)
		}
	}
}

func TestTableViewLinesFitTableWidth(t *testing.T) {
	m := layoutTestModel(100, 30)
	m.visibleWorktrees = []Worktree{layoutTestWorktree()}
	m.rebuildRows()

	for _, line := range strings.Split(m.tableView(), "\n") {
		if width := lipgloss.Width(line); width > m.table.Width() {
			t.Fatalf("table line width = %d, want <= %d: %q", width, m.table.Width(), ansi.Strip(line))
		}
	}
}

func TestWideViewFitsTerminalWidth(t *testing.T) {
	terminalWidth := 164
	m := layoutTestModel(terminalWidth, 40)
	m.allWorktrees = []Worktree{layoutTestWorktree()}
	m.visibleWorktrees = m.allWorktrees
	m.rebuildRows()
	m.updateDetailContent()

	for _, line := range strings.Split(m.View(), "\n") {
		if width := lipgloss.Width(line); width > terminalWidth {
			t.Fatalf("view line width = %d, want <= %d: %q", width, terminalWidth, ansi.Strip(line))
		}
	}
}

func TestDetailViewContentWrapsToWidth(t *testing.T) {
	wt := layoutTestWorktree()
	wt.PR.Body = "AI Prompt\n" + strings.Repeat("long-token ", 20) + strings.Repeat("x", 80)
	width := 48

	content := detailViewContent(&wt, true, width)
	for _, line := range strings.Split(content, "\n") {
		if lineWidth := runewidth.StringWidth(line); lineWidth > width {
			t.Fatalf("detail line width = %d, want <= %d: %q", lineWidth, width, line)
		}
	}
}

func layoutTestModel(width, height int) model {
	m := newModel(Config{
		ActiveProfile: RepositoryProfile{
			Name:           "bluesteel",
			RepositoryPath: "/Users/test/projects/bluesteel",
			WorktreesDir:   "/Users/test/projects",
			RemoteName:     "origin",
		},
		App: AppConfig{
			DefaultAgent: "opencode",
			Agents:       []AgentTool{{Name: "opencode", Command: "opencode"}},
		},
	})
	m.ready = true
	m.loading = false
	m.width = width
	m.height = height
	m.resize()
	return m
}

func layoutTestWorktree() Worktree {
	pr := &PullRequest{
		HeadRefName: "STEEL-3631",
		Number:      2136,
		Title:       "STEEL-3631: Remove needless TE logs",
		URL:         "https://github.com/CenturyLink/bluesteel/pull/2136",
		State:       "OPEN",
		Body:        "## Issue\nhttps://lumen.atlassian.net/browse/STEEL-3631\n\n## Changes\n" + strings.Repeat("Remove unnecessary task executor log output. ", 12),
	}
	pr.Author.Login = "VashingMachine"
	pr.Author.Name = "Dariusz Kwiatkowski"

	return Worktree{
		Name:   "STEEL-3631",
		Path:   "/Users/test/projects/STEEL-3631",
		Branch: "STEEL-3631",
		Status: RepoStatus{
			Dirty:    true,
			Unstaged: 1,
			Files:    []string{".vscode/settings.json"},
		},
		Commit: CommitInfo{
			Hash:     "dda1e2b06",
			Relative: "22 hours ago",
			Subject:  strings.Repeat("Remove more logs ", 8),
		},
		PR: pr,
	}
}

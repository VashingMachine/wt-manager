package tui

import (
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
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
	for _, line := range strings.Split(ansi.Strip(content), "\n") {
		if lineWidth := runewidth.StringWidth(line); lineWidth > width {
			t.Fatalf("detail line width = %d, want <= %d: %q", lineWidth, width, line)
		}
	}
}

func TestPRRadarTableViewLinesFitTableWidth(t *testing.T) {
	m := layoutTestModel(110, 30)
	m.prMode = true
	m.remotePRs = []RemotePullRequest{layoutTestRemotePR()}
	m.visiblePRs = m.remotePRs
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.updateDetailContent()

	for _, line := range strings.Split(m.tableView(), "\n") {
		if width := lipgloss.Width(line); width > m.table.Width() {
			t.Fatalf("PR table line width = %d, want <= %d: %q", width, m.table.Width(), ansi.Strip(line))
		}
	}
	view := ansi.Strip(m.tableView())
	for _, want := range []string{"Checks", "Ready", "1 pass", "ready"} {
		if !strings.Contains(view, want) {
			t.Fatalf("PR table view missing %q in %q", want, view)
		}
	}
}

func TestPRRadarBranchViewShowsBranchColumn(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.prMode = true
	m.prShowBranch = true
	m.remotePRs = []RemotePullRequest{layoutTestRemotePR()}
	m.visiblePRs = m.remotePRs
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()

	view := ansi.Strip(m.tableView())
	if !strings.Contains(view, "STEEL-431-auth-refresh") {
		t.Fatalf("branch view missing branch name in %q", view)
	}
	if strings.Contains(view, "Fix auth refresh race Fix") {
		t.Fatalf("branch view should not show title in final column: %q", view)
	}
}

func TestPRRadarTableSanitizesMultilineCells(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.prMode = true
	pr := layoutTestRemotePR()
	pr.Title = "first line\nsecond line\tthird line"
	m.remotePRs = []RemotePullRequest{pr}
	m.visiblePRs = m.remotePRs
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()

	view := ansi.Strip(m.tableView())
	if strings.Contains(view, "second line\n") || strings.Contains(view, "\t") {
		t.Fatalf("PR table contains unsanitized multiline cell: %q", view)
	}
	if !strings.Contains(view, "first line second line third line") {
		t.Fatalf("PR table missing sanitized title in %q", view)
	}
}

func TestTogglePRRadarDoesNotPanicWithExistingRows(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.prMode = true
	m.remotePRs = []RemotePullRequest{layoutTestRemotePR()}
	m.visiblePRs = m.remotePRs
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
}

func TestToggleFromPRRadarRefreshesWorktrees(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.prMode = true
	m.remotePRs = []RemotePullRequest{layoutTestRemotePR()}
	m.visiblePRs = m.remotePRs
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("P")})
	got := next.(model)
	if got.prMode {
		t.Fatal("PR Radar mode stayed enabled")
	}
	if !got.loading {
		t.Fatal("switching to worktree mode did not start refresh")
	}
	if cmd == nil {
		t.Fatal("switching to worktree mode returned no refresh command")
	}
}

func TestStaleRemotePRDetailResultIsIgnored(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.prMode = true
	first := layoutTestRemotePR()
	second := layoutTestRemotePR()
	second.Number = 432
	second.Title = "Second PR"
	m.remotePRs = []RemotePullRequest{first, second}
	m.visiblePRs = m.remotePRs
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.selectedPRNumber = first.Number
	m.prDetailRequest = 2
	m.prDetailLoading = true

	next, _ := m.Update(remotePRDetailResult{Number: first.Number, RequestID: 1, PullRequest: first})
	got := next.(model)
	if got.prDetail != nil {
		t.Fatalf("stale detail result set prDetail = %#v", got.prDetail)
	}
	if !got.prDetailLoading {
		t.Fatal("stale detail result cleared loading state")
	}
}

func TestRemotePRDetailResultRefreshesSelectedListRow(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.prMode = true
	pr := layoutTestRemotePR()
	pr.StatusCheckRollup = []StatusCheck{{Name: "tests", Status: "queued"}}
	m.remotePRs = []RemotePullRequest{pr}
	m.visiblePRs = m.remotePRs
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.selectedPRNumber = pr.Number
	m.prDetailRequest = 1
	m.prDetailLoading = true

	detail := pr
	detail.StatusCheckRollup = []StatusCheck{{Name: "tests", Status: "completed", Conclusion: "failure"}}
	next, _ := m.Update(remotePRDetailResult{Number: pr.Number, RequestID: 1, PullRequest: detail})
	got := next.(model)
	if got.prDetail == nil || checksSummary(got.prDetail.StatusCheckRollup) != "1 fail" {
		t.Fatalf("detail was not loaded: %#v", got.prDetail)
	}
	if checksSummary(got.visiblePRs[0].StatusCheckRollup) != "1 fail" {
		t.Fatalf("visible PR row was not refreshed: %#v", got.visiblePRs[0].StatusCheckRollup)
	}
	view := ansi.Strip(got.tableView())
	if !strings.Contains(view, "1 fail") || !strings.Contains(view, "blocked") {
		t.Fatalf("PR table did not render refreshed detail fields: %q", view)
	}
}

func TestRemotePRDetailViewContentWrapsToWidth(t *testing.T) {
	pr := layoutTestRemotePR()
	pr.Body = strings.Repeat("Risky auth change ", 30)
	pr.Comments = []PullComment{{Author: GitHubActor{Login: "reviewer"}, Body: strings.Repeat("please inspect ", 20)}}
	width := 52

	content := remotePRDetailViewContent(&pr, false, false, "what is risky?", "Check auth/session.go", width)
	for _, line := range strings.Split(ansi.Strip(content), "\n") {
		if lineWidth := runewidth.StringWidth(line); lineWidth > width {
			t.Fatalf("PR detail line width = %d, want <= %d: %q", lineWidth, width, line)
		}
	}
}

func TestRemotePRDetailKeepsWrappedLinesSeparate(t *testing.T) {
	pr := layoutTestRemotePR()
	pr.Title = "A title that is intentionally long enough to wrap without corrupting neighbouring content"
	pr.Body = "## Changes\n" + strings.Repeat("This paragraph should wrap cleanly without merging with the table pane. ", 6)
	pr.StatusCheckRollup = []StatusCheck{{Name: "very long failing workflow name that wraps", Status: "completed", Conclusion: "failure", URL: "https://github.com/example/really/long/check/url", Summary: strings.Repeat("summary wraps cleanly ", 8)}}
	width := 42

	content := ansi.Strip(remotePRDetailViewContent(&pr, false, true, "", "", width))
	for _, line := range strings.Split(content, "\n") {
		if lineWidth := runewidth.StringWidth(line); lineWidth > width {
			t.Fatalf("line width = %d, want <= %d: %q\ncontent:\n%s", lineWidth, width, line, content)
		}
	}
	for _, want := range []string{"Description:", "## Changes", "Failed GitHub Actions:", "very long failing workflow"} {
		if !strings.Contains(content, want) {
			t.Fatalf("detail content missing %q in:\n%s", want, content)
		}
	}
}

func TestRemotePRDetailShowsFailedGHAChecks(t *testing.T) {
	pr := layoutTestRemotePR()
	pr.StatusCheckRollup = []StatusCheck{
		{Name: "unit tests", Status: "completed", Conclusion: "failure", URL: "https://github.com/checks/1", Summary: "panic in TestAuthRefresh"},
		{Name: "lint", Status: "completed", Conclusion: "success"},
	}

	content := ansi.Strip(remotePRDetailViewContent(&pr, false, true, "", "", 80))
	for _, want := range []string{"Failed GitHub Actions:", "unit tests", "failure", "https://github.com/checks/1", "panic in TestAuthRefresh"} {
		if !strings.Contains(content, want) {
			t.Fatalf("failed GHA detail missing %q in %q", want, content)
		}
	}
	if strings.Contains(content, "lint") {
		t.Fatalf("failed GHA detail should not include successful lint check: %q", content)
	}
}

func TestHelpModalListsKeybindings(t *testing.T) {
	m := layoutTestModel(120, 30)
	content := ansi.Strip(renderHelpModal(m))
	for _, want := range []string{"Keybindings", "Global", "Worktrees", "PR Radar", "f        show failed GitHub Actions", "h        show or hide this help"} {
		if !strings.Contains(content, want) {
			t.Fatalf("help modal missing %q in %q", want, content)
		}
	}
}

func layoutTestModel(width, height int) model {
	m := NewModel(Config{
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
	}, services.NewService())
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

func layoutTestRemotePR() RemotePullRequest {
	return RemotePullRequest{
		Number:         431,
		Title:          strings.Repeat("Fix auth refresh race ", 4),
		URL:            "https://github.com/CenturyLink/bluesteel/pull/431",
		State:          "OPEN",
		HeadRefName:    "STEEL-431-auth-refresh",
		HeadSHA:        "abc123",
		BaseRefName:    "main",
		ReviewDecision: "REVIEW_REQUIRED",
		ChangedFiles:   2,
		Additions:      120,
		Deletions:      40,
		UpdatedAt:      "2026-05-05T12:00:00Z",
		Author:         GitHubActor{Login: "alice"},
		Labels:         []PullLabel{{Name: "backend"}},
		Files:          []PullFile{{Path: "auth/session.go", Additions: 100, Deletions: 20}, {Path: "auth/session_test.go", Additions: 20, Deletions: 20}},
		StatusCheckRollup: []StatusCheck{{
			Name:       "tests",
			Status:     "COMPLETED",
			Conclusion: "SUCCESS",
		}},
	}
}

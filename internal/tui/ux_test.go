package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSpinnerTicksWhilePRDetailLoading(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.loading = false
	m.prDetailLoading = true

	_, cmd := m.Update(m.spinner.Tick())
	if cmd == nil {
		t.Fatal("spinner tick was dropped while PR detail was loading")
	}

	m.prDetailLoading = false
	_, cmd = m.Update(m.spinner.Tick())
	if cmd != nil {
		t.Fatal("spinner tick should stop when nothing is loading")
	}
}

func TestPRRadarEmptyStateMessage(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.prMode = true
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()

	view := ansi.Strip(m.tableView())
	if strings.Contains(view, "No worktrees") {
		t.Fatalf("PR Radar empty state mentions worktrees: %q", view)
	}
	if !strings.Contains(view, "No open PRs") {
		t.Fatalf("PR Radar empty state missing PR message: %q", view)
	}

	m.prUserFilter = "alice"
	view = ansi.Strip(m.tableView())
	if !strings.Contains(view, "No PRs match") {
		t.Fatalf("filtered PR Radar empty state missing hint: %q", view)
	}
}

func TestWorktreeEmptyStateMentionsFilter(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.filterQuery = "zzz"

	view := ansi.Strip(m.tableView())
	if !strings.Contains(view, "esc clears filter") {
		t.Fatalf("filtered empty state missing hint: %q", view)
	}
}

func TestEscCancelsFilterRestoresPreviousQuery(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.allWorktrees = []Worktree{layoutTestWorktree()}
	m.visibleWorktrees = m.allWorktrees
	m.filterQuery = "STEEL"

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	got := next.(model)
	if !got.filtering {
		t.Fatal("/ did not enter filter mode")
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	got = next.(model)
	if got.filterQuery == "STEEL" {
		t.Fatal("typing did not change the live filter query")
	}

	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got = next.(model)
	if got.filtering {
		t.Fatal("esc did not leave filter mode")
	}
	if got.filterQuery != "STEEL" {
		t.Fatalf("esc did not restore previous query, got %q", got.filterQuery)
	}
}

func TestEnterCommitsFilterQuery(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.allWorktrees = []Worktree{layoutTestWorktree()}
	m.visibleWorktrees = m.allWorktrees

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	got := next.(model)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	got = next.(model)
	next, _ = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(model)
	if got.filtering {
		t.Fatal("enter did not leave filter mode")
	}
	if got.filterQuery != "S" {
		t.Fatalf("enter did not keep typed query, got %q", got.filterQuery)
	}
}

func TestTransientStatusAutoClears(t *testing.T) {
	m := layoutTestModel(120, 30)
	cmd := (&m).setTransientStatus("Help closed")
	if cmd == nil {
		t.Fatal("setTransientStatus returned no expiry command")
	}
	if m.statusMessage != "Help closed" {
		t.Fatalf("statusMessage = %q", m.statusMessage)
	}

	next, _ := m.Update(statusExpired{Seq: m.statusSeq})
	got := next.(model)
	if got.statusMessage != "" {
		t.Fatalf("status did not clear, got %q", got.statusMessage)
	}
}

func TestStaleStatusExpiryIsIgnored(t *testing.T) {
	m := layoutTestModel(120, 30)
	_ = (&m).setTransientStatus("old message")
	staleSeq := m.statusSeq
	_ = (&m).setTransientStatus("newer message")

	next, _ := m.Update(statusExpired{Seq: staleSeq})
	got := next.(model)
	if got.statusMessage != "newer message" {
		t.Fatalf("stale expiry cleared newer status, got %q", got.statusMessage)
	}
}

func TestPRDetailShowsSkeletonWhileLoading(t *testing.T) {
	pr := layoutTestRemotePR()
	pr.Body = ""
	pr.Files = nil

	content := ansi.Strip(remotePRDetailViewContent(&pr, true, false, false, "", "", 60))
	if !strings.Contains(content, icons.SkeletonBlock) {
		t.Fatalf("loading detail missing skeleton blocks:\n%s", content)
	}
	if !strings.Contains(content, "Loading PR details...") {
		t.Fatalf("loading detail missing loading note:\n%s", content)
	}

	loaded := layoutTestRemotePR()
	content = ansi.Strip(remotePRDetailViewContent(&loaded, true, false, false, "", "", 60))
	if strings.Contains(content, icons.SkeletonBlock) {
		t.Fatalf("refresh of already-loaded PR should not show skeleton:\n%s", content)
	}
}

func TestTableShowsInlineLoadingRow(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.loading = true

	view := ansi.Strip(m.tableView())
	if !strings.Contains(view, "Loading worktrees...") {
		t.Fatalf("loading table missing inline loading row: %q", view)
	}

	m.prMode = true
	m.table.SetColumns(m.activeColumns())
	view = ansi.Strip(m.tableView())
	if !strings.Contains(view, "Loading remote PRs...") {
		t.Fatalf("loading PR table missing inline loading row: %q", view)
	}
}

func TestDeleteArmTickKeepsCountingThenExpires(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.deleteArmedPath = "/tmp/wt"
	m.deleteArmedName = "wt"
	m.deleteArmedAt = time.Now()

	next, cmd := m.Update(deleteArmTick{Path: "/tmp/wt", ArmedAt: m.deleteArmedAt})
	got := next.(model)
	if got.deleteArmedPath == "" {
		t.Fatal("arm expired while window was still open")
	}
	if cmd == nil {
		t.Fatal("arm tick did not reschedule itself")
	}

	got.deleteArmedAt = time.Now().Add(-2 * deleteConfirmWindow)
	next, _ = got.Update(deleteArmTick{Path: "/tmp/wt", ArmedAt: got.deleteArmedAt})
	got = next.(model)
	if got.deleteArmedPath != "" {
		t.Fatal("arm did not expire after the confirmation window")
	}
	if got.statusMessage != "Delete confirmation expired" {
		t.Fatalf("statusMessage = %q", got.statusMessage)
	}
}

func TestStatusBarShowsDeleteCountdown(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.statusMessage = "Press d again to delete wt"
	m.deleteArmedPath = "/tmp/wt"
	m.deleteArmedName = "wt"
	m.deleteArmedAt = time.Now()

	status := ansi.Strip(m.statusBarView())
	if !strings.Contains(status, "Press d again to delete wt") || !strings.Contains(status, "s)") {
		t.Fatalf("status bar missing countdown: %q", status)
	}
}

func TestStartLoadingPicksContextualSpinner(t *testing.T) {
	m := layoutTestModel(120, 30)
	_ = (&m).startLoading(loadingAgent, "Asking agent...")
	if !m.loading || m.loadingKind != loadingAgent {
		t.Fatalf("startLoading state = loading:%v kind:%q", m.loading, m.loadingKind)
	}
	agentSpinner := m.spinner.Spinner.Frames[0]

	_ = (&m).startLoading(loadingWorktrees, "Refreshing...")
	if m.spinner.Spinner.Frames[0] == agentSpinner {
		t.Fatal("worktree load reused the agent spinner variant")
	}
}

func TestTruncateTextHandlesUTF8(t *testing.T) {
	got := truncateText("żółć-gałąź", 5)
	if got != "żółć…" {
		t.Fatalf("truncateText = %q, want %q", got, "żółć…")
	}
	if !strings.HasSuffix(truncateText("ascii-branch-name", 10), "…") {
		t.Fatal("truncateText did not append ellipsis")
	}
	if truncateText("short", 10) != "short" {
		t.Fatal("truncateText modified text shorter than max")
	}
}

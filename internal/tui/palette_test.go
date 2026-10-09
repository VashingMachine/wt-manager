package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type paletteSpyServices struct {
	*enterActionSpyServices
	editor       core.EditorTool
	worktree     Worktree
	collectedAll bool
	worktrees    []Worktree
	ensureCount  int
}

func (s *paletteSpyServices) OpenEditorWorktree(_ context.Context, wt Worktree, editor core.EditorTool) error {
	s.editor, s.worktree = editor, wt
	return nil
}

func (s *paletteSpyServices) CollectWorktrees(_ context.Context, _ Config, all bool) ([]Worktree, string, error) {
	s.collectedAll = all
	return s.worktrees, "", nil
}

func (s *paletteSpyServices) EnsurePullRequestWorktree(_ context.Context, _ Config, pr RemotePullRequest) (Worktree, bool, error) {
	s.ensureCount++
	return Worktree{Name: pr.HeadRefName, Path: "/tmp/pr-worktree", Branch: pr.HeadRefName}, false, nil
}

func openTestPalette(m model) model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	return next.(model)
}

func choosePaletteEntry(t *testing.T, m model, id string) (model, tea.Cmd) {
	t.Helper()
	m.paletteOpen = true
	m.paletteInput.SetValue("")
	for idx, entry := range m.paletteMatches() {
		if entry.ID == id {
			m.paletteCursor = idx
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			return next.(model), cmd
		}
	}
	t.Fatalf("palette entry %q not found", id)
	return m, nil
}

// Assures Ctrl+P opens a focused palette and cancellation preserves the underlying filter and selection.
func TestPaletteOpensAndCancelsWithoutChangingSelection(t *testing.T) {
	m, _ := enterActionTestModel()
	m.filterQuery = m.visibleWorktrees[0].Name
	path := m.selectedWorktreePath()
	m.deleteArmedPath = path
	got := openTestPalette(m)
	if !got.paletteOpen || !got.paletteInput.Focused() || got.deleteArmedPath != "" {
		t.Fatalf("palette did not focus and clear delete confirmation: %#v", got)
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlP}} {
		next, _ := got.Update(key)
		closed := next.(model)
		if closed.paletteOpen || closed.paletteInput.Focused() || closed.filterQuery != m.filterQuery || closed.selectedWorktreePath() != path {
			t.Fatalf("closing palette changed underlying state: query=%q path=%q", closed.filterQuery, closed.selectedWorktreePath())
		}
	}
}

// Assures typed shortcut letters become search text and empty results cannot invoke a command.
func TestPaletteSearchOwnsInputAndEmptyEnterDoesNothing(t *testing.T) {
	m, spy := enterActionTestModel()
	m = openTestPalette(m)
	next, _ := m.Update(teaKey("qjkv/n"))
	m = next.(model)
	if m.paletteInput.Value() != "qjkv/n" || m.creatingWorktree || m.filtering || spy.opened != "" {
		t.Fatal("search text was dispatched as a shortcut")
	}
	m.paletteInput.SetValue("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	next, cmd := m.Update(teaKey("enter"))
	if !next.(model).paletteOpen || cmd != nil {
		t.Fatal("Enter on empty results ran a command or closed the palette")
	}
}

// Assures palette search matches fuzzy command names and worktree names, branches, paths, and multiple terms.
func TestPaletteSearchMatchesCommandsSettingsAndWorktrees(t *testing.T) {
	m, _ := enterActionTestModel()
	m.allWorktrees = []Worktree{{Name: "Billing fix", Branch: "feature/APP-123", Path: "/projects/billing", Managed: true}}
	for _, tc := range []struct{ query, id string }{
		{"rfrsh", "refresh"}, {"CONFIGURE EDITOR", "editor"}, {"billing", "worktree:/projects/billing"},
		{"APP-123", "worktree:/projects/billing"}, {"/projects/billing", "worktree:/projects/billing"},
		{"billing APP-123", "worktree:/projects/billing"}, {"IntelliJ", "editor:1"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			m.paletteInput.SetValue(tc.query)
			found := false
			for _, entry := range m.paletteMatches() {
				found = found || entry.ID == tc.id
			}
			if !found {
				t.Fatalf("query %q did not match %q", tc.query, tc.id)
			}
		})
	}
	m.paletteInput.SetValue("Billing fix")
	if first := m.paletteMatches()[0]; first.ID != "worktree:/projects/billing" {
		t.Fatalf("exact title was not ranked first: %#v", first)
	}
}

// Assures arrows stay within results, typing resets selection, and Enter persists the selected setting.
func TestPaletteNavigationAndDirectEnterConfiguration(t *testing.T) {
	m, spy := enterActionTestModel()
	m = openTestPalette(m)
	m.paletteInput.SetValue("Set Enter action")
	next, _ := m.Update(teaKey("up"))
	m = next.(model)
	if m.paletteCursor != 0 {
		t.Fatal("Up moved above first result")
	}
	next, _ = m.Update(teaKey("down"))
	m = next.(model)
	next, cmd := m.Update(teaKey("enter"))
	m = next.(model)
	if m.paletteOpen || m.cfg.App.DefaultEnterAction != core.EnterActionAgent || cmd == nil {
		t.Fatal("Enter did not choose highlighted setting")
	}
	cmd()
	if spy.saved.DefaultEnterAction != core.EnterActionAgent || spy.saveCount != 1 {
		t.Fatal("Enter setting was not persisted")
	}
	m = openTestPalette(m)
	m.paletteCursor = len(m.paletteMatches()) - 1
	next, _ = m.Update(teaKey("down"))
	if next.(model).paletteCursor != m.paletteCursor {
		t.Fatal("Down exceeded last result")
	}
	next, _ = m.Update(teaKey("x"))
	if next.(model).paletteCursor != 0 {
		t.Fatal("typing did not reset result selection")
	}
}

// Assures direct agent and editor choices from the palette persist without an extra selector.
func TestPalettePersistsAgentAndEditorChoices(t *testing.T) {
	m, spy := enterActionTestModel()
	m.cfg.App.Agents = append(m.cfg.App.Agents, AgentTool{Name: "codex", Command: "codex"})
	m, cmd := choosePaletteEntry(t, m, "agent:1")
	cmd()
	if spy.saved.DefaultAgent != "codex" || m.paletteOpen {
		t.Fatal("agent choice was not saved")
	}
	m, cmd = choosePaletteEntry(t, m, "editor:1")
	cmd()
	if spy.saved.Editor.Command != "idea" || m.cfg.App.Editor.Name != "IntelliJ IDEA" || m.paletteOpen {
		t.Fatal("editor choice was not saved")
	}
	m, _ = choosePaletteEntry(t, m, "editor")
	if !m.selectingEditor {
		t.Fatal("Configure editor did not open the editor selector")
	}
}

// Assures selecting a searched worktree clears a hiding filter and navigates from PR Radar to the worktree.
func TestPaletteNavigatesToFilteredWorktreeFromOtherModes(t *testing.T) {
	for _, mode := range []viewMode{modeWorktrees, modePRs, modeTickets} {
		m, _ := enterActionTestModel()
		wt := Worktree{Name: "hidden", Path: "/tmp/hidden", Branch: "fix/hidden", Managed: true}
		m.allWorktrees = append(m.allWorktrees, wt)
		m.mode = mode
		m.table.SetRows(nil)
		m.table.SetColumns(m.activeColumns())
		m.filterQuery = "does-not-match"
		m.applyFilter()
		m, _ = choosePaletteEntry(t, m, "worktree:"+wt.Path)
		if !m.isWorktreeMode() || m.filterQuery != "" || m.selectedWorktreePath() != wt.Path || !m.focusTable {
			t.Fatalf("navigation failed in mode %d: mode=%d query=%q selected=%q", mode, m.mode, m.filterQuery, m.selectedWorktreePath())
		}
	}
}

// Assures palette opening loads all repository worktrees and ignores responses for a closed or older palette.
func TestPaletteLoadsAllWorktreesAndRejectsStaleResults(t *testing.T) {
	m, base := enterActionTestModel()
	wt := Worktree{Name: "main", Path: "/tmp/main", IsMain: true}
	spy := &paletteSpyServices{enterActionSpyServices: base, worktrees: []Worktree{wt}}
	m.services = spy
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(model)
	batch := cmd().(tea.BatchMsg)
	var result paletteWorktreesResult
	for _, command := range batch {
		if command != nil {
			if msg, ok := command().(paletteWorktreesResult); ok {
				result = msg
			}
		}
	}
	if !spy.collectedAll {
		t.Fatal("palette did not request all repository worktrees")
	}
	next, _ = m.Update(result)
	m = next.(model)
	m, _ = choosePaletteEntry(t, m, "worktree:"+wt.Path)
	if !m.showAll || m.selectedWorktreePath() != wt.Path {
		t.Fatal("unmanaged worktree was not navigable")
	}
	next, _ = m.Update(result)
	if next.(model).paletteOpen {
		t.Fatal("late result reopened palette")
	}
	m = openTestPalette(m)
	next, _ = m.Update(result)
	if len(next.(model).paletteWorktrees) != 0 {
		t.Fatal("stale result was accepted")
	}
	result.Request = m.paletteRequest
	result.Err = errors.New("offline")
	next, _ = m.Update(result)
	if len(next.(model).allWorktrees) != len(m.allWorktrees) {
		t.Fatal("failed lookup erased cached worktrees")
	}
}

// Assures the palette exposes mode-specific commands and deletion retains the existing second-press confirmation.
func TestPaletteContextCommandsAndDeletionConfirmation(t *testing.T) {
	m, _ := enterActionTestModel()
	m, cmd := choosePaletteEntry(t, m, "delete")
	if cmd == nil || m.deleteArmedPath == "" || len(m.pendingDeletes) != 0 {
		t.Fatal("palette deletion bypassed confirmation")
	}
	m.mode = modePRs
	ids := map[string]bool{}
	for _, entry := range m.paletteEntries() {
		ids[entry.ID] = true
	}
	if ids["delete"] || !ids["approve"] || !ids["diff"] {
		t.Fatal("PR catalogue contained worktree-only actions or omitted PR actions")
	}
	m.mode = modeTickets
	ids = map[string]bool{}
	for _, entry := range m.paletteEntries() {
		ids[entry.ID] = true
	}
	if ids["approve"] || !ids["ticket-status"] || !ids["ticket-filters"] {
		t.Fatal("ticket catalogue contained PR-only actions or omitted ticket actions")
	}
}

// Assures palette access does not interrupt draft text or an approval confirmation, and works over full-screen readers.
func TestPalettePreservesDraftsAndFullscreenOnCancel(t *testing.T) {
	m, _ := enterActionTestModel()
	for _, modal := range []string{"filter", "new", "approval", "comment"} {
		copy := m
		switch modal {
		case "filter":
			copy.filtering = true
		case "new":
			copy.creatingWorktree = true
		case "approval":
			copy.confirmingPRApproval = true
		case "comment":
			copy.commentingTicket = true
		}
		next, _ := copy.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
		if next.(model).paletteOpen {
			t.Fatalf("palette interrupted %s", modal)
		}
	}
	m.mode, m.prDiffMode = modePRs, true
	m = openTestPalette(m)
	if !m.paletteOpen || !strings.Contains(ansi.Strip(m.View()), "Command Palette") {
		t.Fatal("palette did not appear above full-screen diff")
	}
	next, _ := m.Update(teaKey("esc"))
	if !next.(model).prDiffMode {
		t.Fatal("canceling palette closed full-screen diff")
	}
}

// Assures selecting a profile from palette applies and persists it without another selection step.
func TestPaletteDirectProfileSwitch(t *testing.T) {
	m, spy := enterActionTestModel()
	m.cfg.App.Profiles = []RepositoryProfile{{Name: "another", RepositoryPath: "/tmp/another", WorktreesDir: "/tmp/trees", GitHubRepo: "owner/repo"}}
	m, cmd := choosePaletteEntry(t, m, "profile:0")
	if m.cfg.ActiveProfile.Name != "another" || m.paletteOpen || m.selectingProfile || cmd == nil {
		t.Fatal("palette did not switch profile")
	}
	runBatchCommands(t, cmd)
	if spy.saved.DefaultProfile != "another" {
		t.Fatal("profile was not saved")
	}
	m = openTestPalette(m)
	for _, entry := range m.paletteEntries() {
		if entry.Category == "Worktrees" {
			t.Fatal("profile switch exposed a previous repository's worktree")
		}
	}
}

// Assures palette selection does not override later manual navigation when worktrees refresh.
func TestPaletteSelectionDoesNotPinFutureRefreshes(t *testing.T) {
	m, _ := enterActionTestModel()
	wt := Worktree{Name: "second", Path: "/tmp/second", Managed: true}
	m.allWorktrees = append(m.allWorktrees, wt)
	m.applyFilter()
	m, _ = choosePaletteEntry(t, m, "worktree:"+wt.Path)
	if m.pendingWorktreePath != "" {
		t.Fatal("palette pinned selection without a pending load")
	}
	m.moveSelection(-1)
	path := m.selectedWorktreePath()
	next, _ := m.Update(loadResult{Worktrees: m.allWorktrees})
	got := next.(model)
	if got.selectedWorktreePath() != path {
		t.Fatal("refresh overrode subsequent manual navigation")
	}
}

// Assures old managed-only or previous-profile loads cannot erase a worktree selected by the palette.
func TestPaletteWorktreeSelectionRejectsLoadsFromOldScope(t *testing.T) {
	m, _ := enterActionTestModel()
	wt := Worktree{Name: "external", Path: "/tmp/external"}
	m.paletteWorktrees = []Worktree{wt}
	m, cmd := choosePaletteEntry(t, m, "worktree:"+wt.Path)
	if cmd == nil || !m.showAll || m.pendingWorktreePath != wt.Path {
		t.Fatal("scope expansion did not schedule a reload")
	}
	for _, stale := range []loadResult{
		{RepositoryPath: m.cfg.ActiveProfile.RepositoryPath, ShowAll: false},
		{RepositoryPath: "/old/repository", ShowAll: true},
	} {
		next, _ := m.Update(stale)
		got := next.(model)
		if got.selectedWorktreePath() != wt.Path {
			t.Fatal("stale load erased palette selection")
		}
	}
	m.moveSelection(0)
	if m.pendingWorktreePath != "" {
		t.Fatal("manual selection did not release pending palette selection")
	}
}

// Assures palette presentation occupies one bounded terminal surface and advertises its shortcut.
func TestPaletteViewFitsTerminalAndShowsShortcut(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {80, 24}, {40, 12}, {30, 10}} {
		m, _ := enterActionTestModel()
		m.width, m.height = size[0], size[1]
		m = openTestPalette(m)
		view := m.View()
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("palette overflowed %v: %dx%d", size, lipgloss.Width(view), lipgloss.Height(view))
		}
	}
	m, _ := enterActionTestModel()
	if !strings.Contains(ansi.Strip(renderHelpModal(m)), "ctrl+p") {
		t.Fatal("help omitted palette shortcut")
	}
}

// Assures resizing preserves the selected worktree, PR, or ticket so actions still target that row.
func TestTerminalResizePreservesSelectedRow(t *testing.T) {
	for _, mode := range []viewMode{modeWorktrees, modePRs, modeTickets} {
		m, _ := enterActionTestModel()
		m.mode = mode
		switch mode {
		case modeWorktrees:
			m.selectedPath = m.allWorktrees[0].Path
		case modePRs:
			m.visiblePRs = []RemotePullRequest{remotePRFixture(42, "OPEN", "dev", "Selected PR", "fix/resize")}
			m.selectedPRNumber = 42
		case modeTickets:
			m.visibleTickets = []Ticket{{Key: "APP-42", Summary: "Selected ticket"}}
			m.selectedTicketKey = "APP-42"
		}
		for _, width := range []int{54, 160, 40} {
			next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 22})
			m = next.(model)
			if m.table.Cursor() != 0 {
				t.Fatalf("mode %d selection disappeared at width %d: cursor=%d", mode, width, m.table.Cursor())
			}
			switch mode {
			case modeWorktrees:
				if m.selectedWorktree() == nil {
					t.Fatal("worktree action lost its selected row")
				}
			case modePRs:
				if pr := m.selectedRemotePR(); pr == nil || pr.Number != 42 {
					t.Fatal("PR action lost its selected row")
				}
			case modeTickets:
				if ticket := m.selectedTicket(); ticket == nil || ticket.Key != "APP-42" {
					t.Fatal("ticket action lost its selected row")
				}
			}
		}
	}
}

// Assures v and editor Enter behavior use the configured IDE for worktrees, tickets, and PR creation.
func TestConfiguredEditorIsUsedByEveryWorktreeEntryPoint(t *testing.T) {
	for _, action := range []string{"v", "enter", "ticket", "pr"} {
		t.Run(action, func(t *testing.T) {
			m, base := enterActionTestModel()
			spy := &paletteSpyServices{enterActionSpyServices: base}
			m.services = spy
			m.cfg.App.Editor = core.EditorTool{Name: "IntelliJ IDEA", Command: "idea"}
			key := "v"
			switch action {
			case "enter":
				key = "enter"
			case "ticket":
				wt := m.allWorktrees[0]
				m.mode = modeTickets
				m.tickets = []Ticket{{Key: "APP-1", Worktree: &wt}}
				m.visibleTickets = m.tickets
				m.table.SetRows(nil)
				m.table.SetColumns(m.activeColumns())
				m.rebuildRows()
			case "pr":
				m.mode = modePRs
				m.visiblePRs = []RemotePullRequest{remotePRFixture(1, "OPEN", "dev", "Fix", "fix/editor")}
				m.prDetail = &m.visiblePRs[0]
				m.table.SetRows(nil)
				m.table.SetColumns(m.activeColumns())
				m.rebuildRows()
			}
			m.table.SetCursor(0)
			_, cmd := m.Update(teaKey(key))
			if cmd == nil {
				t.Fatal("editor action did not dispatch")
			}
			if action == "pr" {
				runBatchCommands(t, cmd)
			} else {
				cmd()
			}
			if spy.editor != m.cfg.App.Editor || spy.worktree.Path == "" {
				t.Fatalf("configured editor not used: %#v", spy.editor)
			}
			if action == "pr" && spy.ensureCount != 1 {
				t.Fatal("PR editor action skipped worktree preparation")
			}
		})
	}
}

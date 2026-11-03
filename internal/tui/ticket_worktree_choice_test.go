package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ticketWithWorktreeModel is a single-repository tickets view whose only
// ticket already has a worktree.
func ticketWithWorktreeModel() (model, Worktree) {
	m := layoutTestModel(120, 30)
	m.mode = modeTickets
	existing := Worktree{Name: "APP-1-create-worktree", Branch: "APP-1-create-worktree", Path: "/Users/test/projects/APP-1-create-worktree", Managed: true}
	m.allWorktrees = []Worktree{existing}
	m.tickets = []Ticket{{Key: "APP-1", Summary: "Create worktree", Worktree: &existing}}
	m.visibleTickets = m.tickets
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)
	return m, existing
}

// Assures that n on a ticket with a worktree opens a dialog saying the worktree exists instead of reusing it silently.
func TestNewOnTicketWithWorktreeShowsExistsDialog(t *testing.T) {
	m, existing := ticketWithWorktreeModel()

	next, cmd := m.Update(teaKey("n"))
	got := next.(model)
	if cmd != nil || !got.choosingTicketWorktree || got.ticketWorktreeChoice.Existing.Path != existing.Path {
		t.Fatalf("dialog = open:%v choice:%#v cmd:%v", got.choosingTicketWorktree, got.ticketWorktreeChoice, cmd != nil)
	}
	view := ansi.Strip(got.View())
	for _, want := range []string{"Worktree Already Exists", "APP-1 already has a worktree", existing.Name, "Use the existing worktree", "Create a new worktree"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dialog is missing %q:\n%s", want, view)
		}
	}
}

// Assures that choosing "use existing" in the dialog selects the ticket's worktree without creating one.
func TestTicketWorktreeDialogUsesExistingWorktree(t *testing.T) {
	m, existing := ticketWithWorktreeModel()
	next, _ := m.Update(teaKey("n"))
	next, cmd := next.(model).Update(teaKey("enter"))
	got := next.(model)
	if got.choosingTicketWorktree || got.creatingWorktree || cmd == nil {
		t.Fatalf("dialog after enter = choosing:%v creating:%v", got.choosingTicketWorktree, got.creatingWorktree)
	}
	if got.pendingWorktreePath != existing.Path || len(got.pendingCreations) != 0 {
		t.Fatalf("existing worktree not selected: pending=%q creations=%v", got.pendingWorktreePath, got.pendingCreations)
	}
}

// Assures that esc closes the dialog without selecting or creating anything.
func TestTicketWorktreeDialogCancels(t *testing.T) {
	m, _ := ticketWithWorktreeModel()
	next, _ := m.Update(teaKey("n"))
	next, _ = next.(model).Update(teaKey("esc"))
	got := next.(model)
	if got.choosingTicketWorktree || got.creatingWorktree || len(got.pendingCreations) != 0 {
		t.Fatalf("esc left state behind: choosing:%v creating:%v creations:%v", got.choosingTicketWorktree, got.creatingWorktree, got.pendingCreations)
	}
}

// Assures that choosing "create new" asks for a branch prefilled with a free ticket branch name and creates a second worktree linked to the ticket.
func TestTicketWorktreeDialogCreatesAdditionalWorktree(t *testing.T) {
	m, existing := ticketWithWorktreeModel()
	next, _ := m.Update(teaKey("n"))
	next, _ = next.(model).Update(teaKey("n"))
	asking := next.(model)
	if !asking.creatingWorktree || asking.newWorktreeTicketKey != "APP-1" {
		t.Fatalf("create new did not open the branch input for APP-1: creating:%v key:%q", asking.creatingWorktree, asking.newWorktreeTicketKey)
	}
	if got := asking.newInput.Value(); got != "APP-1-create-worktree-2" {
		t.Fatalf("suggested branch = %q, want APP-1-create-worktree-2", got)
	}

	next, cmd := asking.Update(teaKey("enter"))
	created := next.(model)
	if cmd == nil || created.creatingWorktree || created.newWorktreeTicketKey != "" {
		t.Fatalf("enter did not start creation: creating:%v key:%q", created.creatingWorktree, created.newWorktreeTicketKey)
	}
	selected := created.selectedTicket()
	if selected == nil || selected.Worktree == nil || selected.Worktree.Branch != "APP-1-create-worktree-2" {
		t.Fatalf("ticket does not show the new worktree: %#v", selected)
	}
	if !worktreeExists(created.allWorktrees, existing.Path) {
		t.Fatal("the existing worktree disappeared")
	}
	newPath := selected.Worktree.Path
	creation, ok := created.pendingCreations[newPath]
	if !ok || creation.TicketKey != "APP-1" || newPath == existing.Path {
		t.Fatalf("pending creation = %#v (ok=%v)", creation, ok)
	}
}

// Assures that the suggested branch skips every suffix already used by a worktree.
func TestFreeTicketBranchNameSkipsTakenSuffixes(t *testing.T) {
	m, existing := ticketWithWorktreeModel()
	second := existing
	second.Name, second.Branch, second.Path = "APP-1-create-worktree-2", "APP-1-create-worktree-2", "/Users/test/projects/APP-1-create-worktree-2"
	m.allWorktrees = append(m.allWorktrees, second)
	if got := m.freeTicketBranchName(m.tickets[0], m.cfg); got != "APP-1-create-worktree-3" {
		t.Fatalf("free branch = %q, want APP-1-create-worktree-3", got)
	}
}

// Assures that in a multi-repository project, picking a repository where the ticket has a worktree offers to create another one there.
func TestTicketWorktreeDialogCreatesAdditionalWorktreeInRepository(t *testing.T) {
	spy := newMultiRepoSpy()
	m := ticketModeModel(t, spy)
	existing := Worktree{Name: "APP-1-fix-checkout", Path: "/wt/web/APP-1-fix-checkout", Branch: "APP-1-fix-checkout", Managed: true, Repository: "web"}
	m.allWorktrees = append(m.allWorktrees, existing)

	m, _ = pressKeys(t, m, "n", "2")
	if !m.choosingTicketWorktree {
		t.Fatal("picking web did not show the worktree-exists dialog")
	}
	if view := ansi.Strip(renderTicketWorktreeChoiceModal(m)); !strings.Contains(view, "web") {
		t.Fatalf("dialog does not name the repository: %q", view)
	}
	m, _ = pressKeys(t, m, "n")
	if m.newInput.Value() != "APP-1-fix-checkout-2" || m.newWorktreeRepository != "web" {
		t.Fatalf("input = %q in %q", m.newInput.Value(), m.newWorktreeRepository)
	}
	m, msgs := pressKeys(t, m, "enter")
	if len(spy.created) != 1 || spy.created[0] != "web:/wt/web/APP-1-fix-checkout-2" {
		t.Fatalf("created = %v, want a second web worktree", spy.created)
	}
	if results := messagesOf[ticketWorktreeResult](msgs); len(results) != 1 || results[0].Key != "APP-1" {
		t.Fatalf("creation result = %#v", results)
	}
}

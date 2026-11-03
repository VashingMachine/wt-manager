package tui

import (
	"context"
	"fmt"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Pressing n on a ticket that already has a worktree does not silently reuse
// it: a dialog says the worktree exists and offers to either use it or create
// another worktree for the same ticket under a new branch name.

// ticketWorktreeChoice is what the "worktree already exists" dialog decides on.
type ticketWorktreeChoice struct {
	Ticket     Ticket
	Existing   Worktree
	Repository string // empty for a single-repository project
	Cursor     int
}

const (
	ticketWorktreeUseExisting = iota
	ticketWorktreeCreateNew
)

func (m model) openTicketWorktreeChoice(ticket Ticket, existing Worktree, repository string) (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.choosingTicketWorktree = true
	m.ticketWorktreeChoice = ticketWorktreeChoice{Ticket: ticket, Existing: existing, Repository: repository}
	m.statusMessage = fmt.Sprintf("%s already has worktree %s", ticket.Key, existing.Name)
	m.statusError = false
	return m, nil
}

func (m model) updateTicketWorktreeChoice(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.choosingTicketWorktree = false
		return m, m.setTransientStatus("Ticket worktree cancelled")
	case "up", "k":
		m.ticketWorktreeChoice.Cursor = ticketWorktreeUseExisting
		return m, nil
	case "down", "j":
		m.ticketWorktreeChoice.Cursor = ticketWorktreeCreateNew
		return m, nil
	case "u":
		return m.chooseTicketWorktree(ticketWorktreeUseExisting)
	case "n":
		return m.chooseTicketWorktree(ticketWorktreeCreateNew)
	case "enter":
		return m.chooseTicketWorktree(m.ticketWorktreeChoice.Cursor)
	}
	return m, nil
}

func (m model) chooseTicketWorktree(option int) (tea.Model, tea.Cmd) {
	choice := m.ticketWorktreeChoice
	m.choosingTicketWorktree = false
	if option == ticketWorktreeCreateNew {
		return m.openTicketWorktreeInput(choice.Ticket, choice.Repository)
	}
	return m.selectTicketWorktree(choice.Ticket.Key, choice.Existing)
}

// openTicketWorktreeInput asks for the branch of an additional worktree for
// ticket, prefilled with a ticket branch name that is still free.
func (m model) openTicketWorktreeInput(ticket Ticket, repository string) (tea.Model, tea.Cmd) {
	next, cmd := m.openNewWorktreeInput(repository)
	m = next.(model)
	m.newWorktreeTicketKey = ticket.Key
	m.newInput.SetValue(m.freeTicketBranchName(ticket, m.cfg.ForRepository(repository)))
	m.newInput.CursorEnd()
	m.statusMessage = fmt.Sprintf("Enter branch name for a new %s worktree", ticket.Key)
	return m, cmd
}

// freeTicketBranchName is the ticket's usual branch name, suffixed with -2,
// -3, ... until neither its branch nor its path is used by a worktree of the
// repository cfg is scoped to.
func (m model) freeTicketBranchName(ticket Ticket, cfg Config) string {
	base := m.services.TicketBranchName(ticket)
	repository := cfg.RepositoryName()
	taken := func(name string) bool {
		path := m.services.CreateWorktreePath(cfg, name)
		for _, wt := range m.allWorktrees {
			if wt.Path == path || (wt.Branch == name && m.worktreeRepository(wt) == repository) {
				return true
			}
		}
		return false
	}
	name := base
	for n := 2; taken(name); n++ {
		name = fmt.Sprintf("%s-%d", base, n)
	}
	return name
}

// createNamedTicketWorktree creates a worktree on branch for the ticket key in
// the repository cfg is scoped to, showing it on the ticket right away.
func (m model) createNamedTicketWorktree(key string, cfg Config, branch string) (tea.Model, tea.Cmd) {
	worktree := Worktree{
		Name:       branch,
		Branch:     branch,
		Path:       m.services.CreateWorktreePath(cfg, branch),
		Managed:    true,
		Repository: cfg.RepositoryName(),
	}
	m.beginWorktreeCreation(worktree, key)
	creating := fmt.Sprintf("Creating %s for %s...", branch, key)
	if m.cfg.MultiRepository() {
		creating = fmt.Sprintf("Creating %s in %s for %s...", branch, worktree.Repository, key)
	}
	return m, tea.Batch(
		m.ops.enqueue(gitQueueKey(cfg), createTicketWorktreeCmd(m.services, cfg, key, worktree)),
		m.startLoading(loadingMutation, creating),
	)
}

func createTicketWorktreeCmd(services core.WorktreeService, cfg Config, key string, worktree Worktree) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		existing, err := services.CreateWorktree(ctx, cfg, worktree.Branch, worktree.Path)
		return ticketWorktreeResult{Key: key, Worktree: worktree, Existing: existing, Err: err}
	}
}

func renderTicketWorktreeChoiceModal(m model) string {
	choice := m.ticketWorktreeChoice
	location := m.services.CompactPath(choice.Existing.Path, m.cfg.HomeDir)
	if m.cfg.MultiRepository() {
		location = icons.Repo + " " + m.worktreeRepository(choice.Existing) + "  " + location
	}
	body := []string{
		fmt.Sprintf("%s already has a worktree:", choice.Ticket.Key),
		"",
		"  " + infoStyle.Render(choice.Existing.Name),
		"  " + mutedStyle.Render(location),
		"",
		selectorLine(choice.Cursor == ticketWorktreeUseExisting, "Use the existing worktree", false),
		selectorLine(choice.Cursor == ticketWorktreeCreateNew, "Create a new worktree for this ticket", false),
	}
	return renderModal("Worktree Already Exists", body, []keyHint{{"j/k", "move"}, {"enter", "select"}, {"u", "use existing"}, {"n", "new"}, {"esc", "cancel"}})
}

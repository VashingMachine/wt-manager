package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Mutations show their result immediately and are confirmed in the background
// through opQueue. A failure reverts only what the failed mutation changed and
// only if nothing newer has replaced it since.

type ticketMutation struct {
	Patch  core.TicketPatch
	Before Ticket
	Action string
}

func ticketQueueKey(key string) string {
	return "jira:" + strings.ToUpper(key)
}

func prQueueKey(cfg Config, number int) string {
	return fmt.Sprintf("github:%s#%d", cfg.RepoSlug, number)
}

// startTicketMutation applies patch to every copy of the ticket, then queues
// the JIRA call behind earlier mutations of the same ticket.
func (m model) startTicketMutation(action string, patch core.TicketPatch, pending string, done string, run func(context.Context) error) (tea.Model, tea.Cmd) {
	before, ok := m.ticketByKey(patch.Key)
	if !ok {
		m.statusMessage = "Select a ticket first"
		m.statusError = true
		return m, nil
	}
	if m.ticketMutations == nil {
		m.ticketMutations = map[int]ticketMutation{}
	}
	m.nextMutationID++
	id := m.nextMutationID
	m.ticketMutations[id] = ticketMutation{Patch: patch, Before: before, Action: action}
	m.applyTicketPatch(patch)
	m.statusMessage = pending
	m.statusError = false

	services, cfg, board := m.services, m.cfg, m.ticketBoard
	cmd := m.ops.enqueue(ticketQueueKey(patch.Key), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		if err := run(ctx); err != nil {
			return ticketMutationResult{ID: id, Key: patch.Key, Action: action, Err: err}
		}
		services.ApplyTicketPatch(cfg, board, patch)
		return ticketMutationResult{ID: id, Key: patch.Key, Action: action, Message: done}
	})
	return m, tea.Batch(cmd, m.spinner.Tick)
}

func (m model) handleTicketMutationResult(msg ticketMutationResult) (tea.Model, tea.Cmd) {
	mutation, tracked := m.ticketMutations[msg.ID]
	delete(m.ticketMutations, msg.ID)
	if msg.Err != nil {
		if tracked {
			m.revertTicketPatch(mutation)
		}
		m.statusMessage = fmt.Sprintf("JIRA %s failed for %s: %v", msg.Action, msg.Key, msg.Err)
		m.statusError = true
		return m, nil
	}
	m.statusMessage = msg.Message
	m.statusError = false

	var cmds []tea.Cmd
	// Reconcile with JIRA (status categories, scope membership) once the burst
	// of edits is over, without blocking the list.
	if m.ticketMutationsInFlight() == 0 && m.ticketBoard.ID != 0 {
		m.ticketsLoading = true
		cmds = append(cmds, m.backgroundTicketsCmd(true), m.spinner.Tick)
	}
	if m.isTicketMode() && msg.Key == m.selectedTicketKey && !m.ticketReadMode {
		next, cmd := m.startTicketDetailLoad(msg.Key)
		m = next.(model)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// withPendingPatches re-applies edits still awaiting JIRA to data fetched
// before they were made, so a slow response cannot undo them on screen.
func (m model) withPendingPatches(ticket Ticket) Ticket {
	for id := 1; id <= m.nextMutationID; id++ {
		if mutation, ok := m.ticketMutations[id]; ok {
			mutation.Patch.Apply(&ticket)
		}
	}
	return ticket
}

// reapplyPendingApprovals keeps in-flight approvals visible across list
// refreshes, which do not carry review decisions.
func (m *model) reapplyPendingApprovals() {
	for number, before := range m.prApprovals {
		m.setPRReviewDecision(number, optimisticReviewDecision(before), "", false)
	}
}

func (m *model) ticketByKey(key string) (Ticket, bool) {
	if m.ticketDetail != nil && m.ticketDetail.Key == key {
		return *m.ticketDetail, true
	}
	for _, ticket := range m.tickets {
		if ticket.Key == key {
			return ticket, true
		}
	}
	return Ticket{}, false
}

func (m *model) applyTicketPatch(patch core.TicketPatch) {
	for i := range m.tickets {
		patch.Apply(&m.tickets[i])
	}
	if m.ticketDetail != nil {
		patch.Apply(m.ticketDetail)
	}
	m.rememberTicketSelectionIfVisible()
	m.applyTicketFilter()
}

func (m *model) revertTicketPatch(mutation ticketMutation) {
	patch, before := mutation.Patch, mutation.Before
	revert := func(ticket *Ticket) {
		if ticket == nil || ticket.Key != patch.Key {
			return
		}
		if patch.Status != "" && ticket.Status == patch.Status {
			ticket.Status = before.Status
			ticket.StatusCategory = before.StatusCategory
		}
		if patch.Assignee != nil && ticket.Assignee == *patch.Assignee {
			ticket.Assignee = before.Assignee
		}
		if patch.Comment != nil {
			for i := len(ticket.Comments) - 1; i >= 0; i-- {
				if ticket.Comments[i] == *patch.Comment {
					ticket.Comments = append(ticket.Comments[:i:i], ticket.Comments[i+1:]...)
					break
				}
			}
		}
	}
	for i := range m.tickets {
		revert(&m.tickets[i])
	}
	revert(m.ticketDetail)
	m.rememberTicketSelectionIfVisible()
	m.applyTicketFilter()
}

// rememberTicketSelectionIfVisible records the cursor only while the ticket
// table is on screen; in other modes the table holds other rows.
func (m *model) rememberTicketSelectionIfVisible() {
	if m.isTicketMode() {
		m.rememberTicketSelection()
	}
}

// statusCategoryFor guesses the category of a target status from tickets that
// already have it, so an optimistic transition is coloured correctly.
func (m model) statusCategoryFor(status string) string {
	for _, ticket := range m.tickets {
		if strings.EqualFold(ticket.Status, status) && ticket.StatusCategory != "" {
			return ticket.StatusCategory
		}
	}
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "done"), strings.Contains(lower, "closed"), strings.Contains(lower, "resolved"):
		return core.StatusCategoryDone
	case strings.Contains(lower, "progress"), strings.Contains(lower, "review"):
		return core.StatusCategoryInProgress
	}
	return ""
}

func assigneeForPatch(assignee string, options []JiraUser) JiraUser {
	if assignee == core.TicketAssigneeSelf {
		return JiraUser{AccountID: core.TicketAssigneeSelf, DisplayName: "@me"}
	}
	for _, option := range options {
		if option.AccountID == assignee || (option.Email != "" && option.Email == assignee) {
			return option
		}
	}
	if strings.Contains(assignee, "@") {
		return JiraUser{Email: assignee}
	}
	return JiraUser{AccountID: assignee}
}

// --- tickets data source ----------------------------------------------------

func (m model) ticketSource() string {
	return fmt.Sprintf("%s|%d|%t|%t", m.cfg.ActiveProfile.RepositoryPath, m.ticketBoard.ID, m.ticketMineOnly, m.ticketSprintOnly)
}

// paintCachedTickets shows the cached rows for the current board and scope,
// joined to the worktrees and PRs already known.
func (m *model) paintCachedTickets() bool {
	if m.ticketBoard.ID == 0 {
		return false
	}
	cached, fetchedAt, ok := m.services.PeekTicketCache(m.cfg, m.ticketBoard, m.ticketMineOnly, m.ticketSprintOnly)
	if !ok {
		return false
	}
	m.tickets = m.services.AttachTicketContext(cached, m.allWorktrees, m.prIndex)
	m.ticketsSource = m.ticketSource()
	m.ticketsFetchedAt = fetchedAt
	m.ticketCacheLabel = "cached"
	if !fetchedAt.IsZero() {
		m.ticketCacheLabel = "cached " + relativeAge(fetchedAt)
	}
	return true
}

// backgroundTicketsCmd revalidates the current board without taking over the
// view; its result only repaints rows while tickets are on screen.
func (m model) backgroundTicketsCmd(refresh bool) tea.Cmd {
	load := m.loadTicketsCmd(refresh)
	return func() tea.Msg {
		msg := load()
		if result, ok := msg.(ticketListResult); ok {
			result.Background = true
			return result
		}
		return msg
	}
}

// --- PR approval -------------------------------------------------------------

func optimisticReviewDecision(before string) string {
	// One approval cannot clear another reviewer's requested changes.
	if strings.EqualFold(before, "CHANGES_REQUESTED") {
		return before
	}
	return "APPROVED"
}

func (m *model) setPRReviewDecision(number int, decision string, onlyIf string, conditional bool) {
	apply := func(pr *RemotePullRequest) {
		if pr == nil || pr.Number != number {
			return
		}
		if conditional && pr.ReviewDecision != onlyIf {
			return
		}
		pr.ReviewDecision = decision
	}
	for i := range m.remotePRs {
		apply(&m.remotePRs[i])
	}
	for i := range m.visiblePRs {
		apply(&m.visiblePRs[i])
	}
	apply(m.prDetail)
	if m.isPRMode() {
		m.rebuildRows()
		m.updateDetailContent()
	}
}

func (m model) startPRApproval(number int) (tea.Model, tea.Cmd) {
	before := ""
	if selected := m.selectedRemotePRForDetail(); selected != nil && selected.Number == number {
		before = selected.ReviewDecision
	}
	if m.prApprovals == nil {
		m.prApprovals = map[int]string{}
	}
	m.prApprovals[number] = before
	m.setPRReviewDecision(number, optimisticReviewDecision(before), "", false)
	m.statusMessage = fmt.Sprintf("Approving PR #%d...", number)
	m.statusError = false
	cmd := m.ops.enqueue(prQueueKey(m.cfg, number), approveRemotePullRequestCmd(m.services, m.cfg, number))
	return m, tea.Batch(cmd, m.spinner.Tick)
}

func (m model) handleApprovePRResult(msg approvePRResult) (tea.Model, tea.Cmd) {
	before, tracked := m.prApprovals[msg.Number]
	delete(m.prApprovals, msg.Number)
	if msg.Err != nil {
		if tracked {
			m.setPRReviewDecision(msg.Number, before, optimisticReviewDecision(before), true)
		}
		m.statusMessage = fmt.Sprintf("Approve failed for PR #%d: %v", msg.Number, msg.Err)
		m.statusError = true
		return m, nil
	}
	m.statusMessage = fmt.Sprintf("Approved PR #%d. Refreshing details...", msg.Number)
	m.statusError = false
	notify := notifyCmd(m.services, "PR approved", fmt.Sprintf("Approved PR #%d", msg.Number))
	if m.isPRMode() && msg.Number == m.selectedPRNumber {
		next, cmd := m.startPRDetailLoad(msg.Number)
		return next, tea.Batch(cmd, notify)
	}
	return m, notify
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}

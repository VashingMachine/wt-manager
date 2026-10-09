package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	customTicketStatusLabel   = "Type another status..."
	customTicketAssigneeLabel = "Enter email/account ID..."
)

// Tickets mode: JIRA work items joined to the local worktrees and open PRs that
// share their issue key. Structure deliberately mirrors PR Radar — same
// remember/restore selection dance, same debounced cancellable detail load, same
// merge-detail-back-into-the-list caching — so the three modes behave alike.

// toggleMode returns target unless we are already there, in which case it falls
// back to worktrees. That keeps every mode key a toggle rather than a one-way trip.
func (m model) toggleMode(target viewMode) viewMode {
	if m.mode == target {
		return modeWorktrees
	}
	return target
}

// switchMode is the single entry point for changing surface. It parks the current
// selection, tears down every transient overlay and in-flight detail load, then
// dispatches the loader for the mode being entered.
func (m model) switchMode(target viewMode) (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.cancelPRDetailLoad()
	m.cancelTicketDetailLoad()

	var selectedTicketWorktree *Worktree
	switch m.mode {
	case modePRs:
		m.rememberPRSelection()
	case modeTickets:
		m.rememberTicketSelection()
		if target == modeWorktrees {
			if selected := m.selectedTicket(); selected != nil && selected.Worktree != nil && !selected.Worktree.Missing && !selected.Worktree.Prunable {
				worktree := *selected.Worktree
				selectedTicketWorktree = &worktree
			}
		}
	default:
		m.rememberSelection()
	}

	m.mode = target
	m.filtering = false
	m.creatingWorktree = false
	m.selectingEnterAction = false
	m.askingPR = false
	m.selectingPRUser = false
	m.confirmingPRApproval = false
	m.prApprovalPendingNumber = 0
	m.prApprovalAwaitingDetailNumber = 0
	m.prApprovalWarnings = nil
	m.prDiffMode = false
	m.selectingTicketStatus = false
	m.enteringTicketStatus = false
	m.commentingTicket = false
	m.selectingTicketAssignee = false
	m.enteringTicketAssignee = false
	m.selectingBoard = false
	m.selectingTicketFilters = false
	m.ticketReadMode = false
	m.table.SetCursor(0)
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	if selectedTicketWorktree != nil {
		m.pendingWorktreePath = selectedTicketWorktree.Path
		m.selectedPath = selectedTicketWorktree.Path
		m.selectedIndex = 0
	}
	if target == modeWorktrees {
		if !m.showAll {
			worktrees := make([]Worktree, 0, len(m.allWorktrees))
			for _, worktree := range m.allWorktrees {
				if worktree.Managed || (selectedTicketWorktree != nil && worktree.Path == selectedTicketWorktree.Path) {
					worktrees = append(worktrees, worktree)
				}
			}
			m.allWorktrees = worktrees
		}
		m.applyFilter()
		if selectedTicketWorktree != nil && !worktreeExists(m.visibleWorktrees, selectedTicketWorktree.Path) {
			m.filterQuery = ""
			m.filterInput.SetValue("")
			m.applyFilter()
		}
	} else {
		m.rebuildRows()
	}
	m.updateDetailContent()
	m.statusError = false

	switch target {
	case modePRs:
		return m, tea.Batch(loadRemotePullRequestsCmd(m.services, m.cfg), m.startLoading(loadingPRs, "Loading remote PRs..."))
	case modeTickets:
		return m.enterTicketMode()
	default:
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, "Refreshing worktrees..."))
	}
}

func (m model) startTicketWorktreeCreation(ticket Ticket, action string) (tea.Model, tea.Cmd) {
	if ticket.Worktree != nil && !ticket.Worktree.Missing && !ticket.Worktree.Prunable {
		if action != "" {
			return m.openWorktreeWithAction(*ticket.Worktree, action)
		}
		m.selectedTicketKey = ticket.Key
		m.pendingWorktreePath = ticket.Worktree.Path
		m.statusMessage = fmt.Sprintf("Selected existing worktree %s. Refreshing tickets...", ticket.Worktree.Name)
		return m, tea.Batch(m.loadTicketsCmd(false), m.startLoading(loadingTickets, m.statusMessage))
	}
	branch := m.services.TicketBranchName(ticket)
	worktree := Worktree{
		Name:    branch,
		Branch:  branch,
		Path:    m.services.CreateWorktreePath(m.cfg, branch),
		Managed: true,
	}
	m.beginWorktreeCreation(worktree, ticket.Key)
	if action != "" {
		m.deferPendingWorktreeAction(worktree.Path, action)
	}
	return m, tea.Batch(
		ensureTicketWorktreeCmd(m.services, m.cfg, ticket),
		m.startLoading(loadingMutation, fmt.Sprintf("Preparing worktree for %s...", ticket.Key)),
	)
}

// enterTicketMode paints whatever is on disk before touching the network, so
// switching into tickets is instant even though an acli listing takes seconds.
func (m model) enterTicketMode() (tea.Model, tea.Cmd) {
	jira := m.cfg.ActiveProfile.Jira
	if jira == nil || len(jira.Boards) == 0 {
		m.tickets = nil
		m.visibleTickets = nil
		m.ticketCacheLabel = ""
		m.rebuildRows()
		m.updateDetailContent()
		m.statusMessage = "No JIRA board configured for this profile — press I to run setup"
		m.statusError = true
		return m, nil
	}
	if m.ticketBoard.ID == 0 {
		if board := jira.ActiveBoard(); board != nil {
			m.ticketBoard = *board
		}
	}

	if cached, fetchedAt, ok := m.services.PeekTicketCache(m.cfg, m.ticketBoard, m.ticketMineOnly, m.ticketSprintOnly); ok {
		m.tickets = cached
		m.ticketCacheLabel = "cached " + relativeAge(fetchedAt)
		m.applyTicketFilter()
	}
	return m, tea.Batch(m.loadTicketsCmd(false), m.startLoading(loadingTickets, m.ticketLoadingMessage()))
}

func (m model) ticketLoadingMessage() string {
	if len(m.tickets) > 0 {
		return fmt.Sprintf("Refreshing %s...", m.ticketBoard.Name)
	}
	return fmt.Sprintf("Loading tickets from %s...", m.ticketBoard.Name)
}

func (m model) loadTicketsCmd(refresh bool) tea.Cmd {
	return loadTicketsCmd(m.services, m.cfg, m.ticketBoard, m.ticketMineOnly, m.ticketSprintOnly, refresh)
}

// updateTicketMode handles the keys that only exist in tickets mode. It returns
// handled=false so the caller can fall through to the shared bindings.
func (m model) updateTicketMode(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.TicketFilters):
		m.selectingTicketFilters = true
		m.ticketFilterCursor = 0
		m.ticketFilterMineOnly = m.ticketMineOnly
		m.ticketFilterSprintOnly = m.ticketSprintOnly
		m.ticketFilterOpenOnly = m.ticketOpenOnly
		m.statusMessage = "Configure JIRA ticket filters"
		m.statusError = false
		return m, nil, true

	case key.Matches(msg, m.keys.MineFilter):
		m.ticketMineOnly = !m.ticketMineOnly
		return m.reloadTicketsAfterScopeChange("assignee")

	case key.Matches(msg, m.keys.SprintOnly):
		m.ticketSprintOnly = !m.ticketSprintOnly
		return m.reloadTicketsAfterScopeChange("sprint")

	case key.Matches(msg, m.keys.BoardMenu):
		if m.cfg.ActiveProfile.Jira == nil || len(m.cfg.ActiveProfile.Jira.Boards) == 0 {
			m.statusMessage = "No boards configured — press I to run setup"
			m.statusError = true
			return m, nil, true
		}
		m.selectingBoard = true
		m.boardCursor = m.currentBoardIndex()
		m.statusMessage = "Select a JIRA board"
		m.statusError = false
		return m, nil, true

	case key.Matches(msg, m.keys.SetStatus):
		selected := m.selectedTicket()
		if selected == nil {
			m.statusMessage = "Select a ticket first"
			m.statusError = true
			return m, nil, true
		}
		m.ticketStatusOptions = m.services.TicketStatuses(m.tickets)
		m.ticketStatusOptions = append(m.ticketStatusOptions, customTicketStatusLabel)
		m.ticketStatusCursor = m.currentTicketStatusIndex()
		m.selectingTicketStatus = true
		m.statusMessage = fmt.Sprintf("Change status for %s", selected.Key)
		m.statusError = false
		return m, nil, true

	case key.Matches(msg, m.keys.WriteNote):
		selected := m.selectedTicket()
		if selected == nil {
			m.statusMessage = "Select a ticket first"
			m.statusError = true
			return m, nil, true
		}
		m.commentingTicket = true
		m.ticketCommentInput.SetValue("")
		m.ticketCommentInput.Focus()
		m.statusMessage = fmt.Sprintf("Comment on %s", selected.Key)
		m.statusError = false
		return m, nil, true

	case key.Matches(msg, m.keys.AssignSelf):
		selected := m.selectedTicket()
		if selected == nil {
			m.statusMessage = "Select a ticket first"
			m.statusError = true
			return m, nil, true
		}
		m.ticketAssigneeOptions = []JiraUser{{AccountID: core.TicketAssigneeSelf, DisplayName: "@me (yourself)"}}
		m.ticketAssigneeOptions = append(m.ticketAssigneeOptions, m.services.TicketAssignees(m.tickets)...)
		m.ticketAssigneeOptions = append(m.ticketAssigneeOptions, JiraUser{AccountID: customTicketAssigneeLabel, DisplayName: customTicketAssigneeLabel})
		m.ticketAssigneeCursor = 0
		m.selectingTicketAssignee = true
		m.statusMessage = fmt.Sprintf("Assign %s", selected.Key)
		m.statusError = false
		return m, nil, true
	}
	return m, nil, false
}

func (m model) updateTicketFilters(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingTicketFilters = false
		return m, m.setTransientStatus("Ticket filters cancelled")
	case "up", "k":
		if m.ticketFilterCursor > 0 {
			m.ticketFilterCursor--
		}
		return m, nil
	case "down", "j":
		if m.ticketFilterCursor < 2 {
			m.ticketFilterCursor++
		}
		return m, nil
	case " ":
		switch m.ticketFilterCursor {
		case 0:
			m.ticketFilterMineOnly = !m.ticketFilterMineOnly
		case 1:
			m.ticketFilterSprintOnly = !m.ticketFilterSprintOnly
		case 2:
			m.ticketFilterOpenOnly = !m.ticketFilterOpenOnly
		}
		return m, nil
	case "enter":
		scopeChanged := m.ticketMineOnly != m.ticketFilterMineOnly ||
			m.ticketSprintOnly != m.ticketFilterSprintOnly
		m.ticketMineOnly = m.ticketFilterMineOnly
		m.ticketSprintOnly = m.ticketFilterSprintOnly
		m.ticketOpenOnly = m.ticketFilterOpenOnly
		m.selectingTicketFilters = false
		m.persistTicketScope()
		if scopeChanged {
			next, cmd, _ := m.reloadTicketsAfterScopeChange("filters")
			return next, cmd
		}
		m.applyTicketFilter()
		m.statusMessage = m.ticketFilterSummary()
		m.statusError = false
		return m, saveAppConfigCmd(m.services, m.cfg)
	}
	return m, nil
}

func (m model) updateTicketStatusSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingTicketStatus = false
		return m, m.setTransientStatus("Ticket status change cancelled")
	case "up", "k":
		if m.ticketStatusCursor > 0 {
			m.ticketStatusCursor--
		}
		return m, nil
	case "down", "j":
		if m.ticketStatusCursor < len(m.ticketStatusOptions)-1 {
			m.ticketStatusCursor++
		}
		return m, nil
	case "e":
		return m.openTicketStatusInput()
	case "enter":
		if len(m.ticketStatusOptions) == 0 {
			return m, nil
		}
		status := m.ticketStatusOptions[min(max(m.ticketStatusCursor, 0), len(m.ticketStatusOptions)-1)]
		if status == customTicketStatusLabel {
			return m.openTicketStatusInput()
		}
		return m.submitTicketStatus(status)
	}
	return m, nil
}

func (m model) openTicketStatusInput() (tea.Model, tea.Cmd) {
	m.selectingTicketStatus = false
	m.enteringTicketStatus = true
	m.ticketStatusInput.SetValue("")
	m.ticketStatusInput.Focus()
	m.statusMessage = "Enter target JIRA status"
	return m, nil
}

func (m model) updateTicketStatusInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.enteringTicketStatus = false
		m.ticketStatusInput.Blur()
		return m, m.setTransientStatus("Ticket status change cancelled")
	case "enter":
		return m.submitTicketStatus(strings.TrimSpace(m.ticketStatusInput.Value()))
	}
	var cmd tea.Cmd
	m.ticketStatusInput, cmd = m.ticketStatusInput.Update(msg)
	return m, cmd
}

func (m model) submitTicketStatus(status string) (tea.Model, tea.Cmd) {
	selected := m.selectedTicket()
	if selected == nil {
		m.statusMessage = "Select a ticket first"
		m.statusError = true
		return m, nil
	}
	if strings.TrimSpace(status) == "" {
		m.statusMessage = "Status is required"
		m.statusError = true
		return m, nil
	}
	m.selectingTicketStatus = false
	m.enteringTicketStatus = false
	m.ticketStatusInput.Blur()
	return m, tea.Batch(
		transitionTicketCmd(m.services, m.cfg, m.ticketBoard, selected.Key, status),
		m.startLoading(loadingMutation, fmt.Sprintf("Changing %s to %s...", selected.Key, status)),
	)
}

func (m model) updateTicketComment(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.commentingTicket = false
		m.ticketCommentInput.Blur()
		return m, m.setTransientStatus("Ticket comment cancelled")
	case "enter":
		selected := m.selectedTicket()
		body := strings.TrimSpace(m.ticketCommentInput.Value())
		if selected == nil {
			m.statusMessage = "Select a ticket first"
			m.statusError = true
			return m, nil
		}
		if body == "" {
			m.statusMessage = "Comment is required"
			m.statusError = true
			return m, nil
		}
		m.commentingTicket = false
		m.ticketCommentInput.Blur()
		return m, tea.Batch(
			commentTicketCmd(m.services, m.cfg, m.ticketBoard, selected.Key, body),
			m.startLoading(loadingMutation, fmt.Sprintf("Commenting on %s...", selected.Key)),
		)
	}
	var cmd tea.Cmd
	m.ticketCommentInput, cmd = m.ticketCommentInput.Update(msg)
	return m, cmd
}

func (m model) updateTicketAssigneeSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingTicketAssignee = false
		return m, m.setTransientStatus("Ticket assignment cancelled")
	case "up", "k":
		if m.ticketAssigneeCursor > 0 {
			m.ticketAssigneeCursor--
		}
		return m, nil
	case "down", "j":
		if m.ticketAssigneeCursor < len(m.ticketAssigneeOptions)-1 {
			m.ticketAssigneeCursor++
		}
		return m, nil
	case "enter":
		if len(m.ticketAssigneeOptions) == 0 {
			return m, nil
		}
		user := m.ticketAssigneeOptions[min(max(m.ticketAssigneeCursor, 0), len(m.ticketAssigneeOptions)-1)]
		if user.AccountID == customTicketAssigneeLabel {
			m.selectingTicketAssignee = false
			m.enteringTicketAssignee = true
			m.ticketAssigneeInput.SetValue("")
			m.ticketAssigneeInput.Focus()
			m.statusMessage = "Enter assignee email or account ID"
			return m, nil
		}
		assignee := user.AccountID
		if assignee == "" {
			assignee = user.Email
		}
		return m.submitTicketAssignee(assignee)
	}
	return m, nil
}

func (m model) updateTicketAssigneeInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.enteringTicketAssignee = false
		m.ticketAssigneeInput.Blur()
		return m, m.setTransientStatus("Ticket assignment cancelled")
	case "enter":
		return m.submitTicketAssignee(strings.TrimSpace(m.ticketAssigneeInput.Value()))
	}
	var cmd tea.Cmd
	m.ticketAssigneeInput, cmd = m.ticketAssigneeInput.Update(msg)
	return m, cmd
}

func (m model) submitTicketAssignee(assignee string) (tea.Model, tea.Cmd) {
	selected := m.selectedTicket()
	if selected == nil {
		m.statusMessage = "Select a ticket first"
		m.statusError = true
		return m, nil
	}
	if strings.TrimSpace(assignee) == "" {
		assignee = core.TicketAssigneeSelf
	}
	m.selectingTicketAssignee = false
	m.enteringTicketAssignee = false
	m.ticketAssigneeInput.Blur()
	return m, tea.Batch(
		assignTicketCmd(m.services, m.cfg, m.ticketBoard, selected.Key, assignee),
		m.startLoading(loadingMutation, fmt.Sprintf("Assigning %s...", selected.Key)),
	)
}

func (m model) updateBoardSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	jira := m.cfg.ActiveProfile.Jira
	if jira == nil || len(jira.Boards) == 0 {
		m.selectingBoard = false
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingBoard = false
		return m, m.setTransientStatus("Board selection cancelled")
	case "up", "k":
		if m.boardCursor > 0 {
			m.boardCursor--
		}
		return m, nil
	case "down", "j":
		if m.boardCursor < len(jira.Boards)-1 {
			m.boardCursor++
		}
		return m, nil
	case "enter":
		m.boardCursor = min(max(m.boardCursor, 0), len(jira.Boards)-1)
		m.ticketBoard = jira.Boards[m.boardCursor]
		m.selectingBoard = false
		m.persistTicketScope()
		returnModel, cmd, _ := m.reloadTicketsAfterScopeChange("board")
		return returnModel, tea.Batch(cmd, saveAppConfigCmd(m.services, m.cfg))
	}
	return m, nil
}

func (m model) updateTicketReadMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.OpenPR):
		selected := m.selectedTicketForDetail()
		if selected == nil || selected.URL == "" {
			return m, nil
		}
		return m, openBrowserMessageCmd(m.services, selected.URL, "Opened JIRA ticket in browser")
	}
	switch msg.String() {
	case "esc", "enter":
		m.ticketReadMode = false
		m.resize()
		return m, m.setTransientStatus("Closed ticket reader")
	}
	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

func (m model) reloadTicketsAfterScopeChange(what string) (tea.Model, tea.Cmd, bool) {
	m.rememberTicketSelection()
	m.cancelTicketDetailLoad()
	m.persistTicketScope()

	// Each scope has its own cache file, so swapping scope can also paint instantly.
	m.tickets = nil
	m.ticketCacheLabel = ""
	if cached, fetchedAt, ok := m.services.PeekTicketCache(m.cfg, m.ticketBoard, m.ticketMineOnly, m.ticketSprintOnly); ok {
		m.tickets = cached
		m.ticketCacheLabel = "cached " + relativeAge(fetchedAt)
	}
	m.applyTicketFilter()
	_ = what
	return m, tea.Batch(m.loadTicketsCmd(false), saveAppConfigCmd(m.services, m.cfg), m.startLoading(loadingTickets, m.ticketLoadingMessage())), true
}

// persistTicketScope keeps the mine/sprint toggles across restarts by writing
// them back into the profile's jira block.
func (m *model) persistTicketScope() {
	jira := m.cfg.ActiveProfile.Jira
	if jira == nil {
		return
	}
	jira.MineOnly = m.ticketMineOnly
	jira.SprintOnly = m.ticketSprintOnly
	jira.OpenOnly = m.ticketOpenOnly
	jira.FiltersConfigured = true
	jira.DefaultBoard = m.ticketBoard.Name
	for i := range m.cfg.App.Profiles {
		if m.cfg.App.Profiles[i].Name != m.cfg.ActiveProfile.Name {
			continue
		}
		if m.cfg.App.Profiles[i].Jira == nil {
			continue
		}
		m.cfg.App.Profiles[i].Jira.MineOnly = m.ticketMineOnly
		m.cfg.App.Profiles[i].Jira.SprintOnly = m.ticketSprintOnly
		m.cfg.App.Profiles[i].Jira.OpenOnly = m.ticketOpenOnly
		m.cfg.App.Profiles[i].Jira.FiltersConfigured = true
		m.cfg.App.Profiles[i].Jira.DefaultBoard = m.ticketBoard.Name
	}
}

func (m model) currentBoardIndex() int {
	jira := m.cfg.ActiveProfile.Jira
	if jira == nil {
		return 0
	}
	for idx, board := range jira.Boards {
		if board.Name == m.ticketBoard.Name {
			return idx
		}
	}
	return 0
}

func (m model) currentTicketStatusIndex() int {
	selected := m.selectedTicket()
	if selected == nil {
		return 0
	}
	for idx, status := range m.ticketStatusOptions {
		if strings.EqualFold(status, selected.Status) {
			return idx
		}
	}
	return 0
}

// --- selection ---------------------------------------------------------------

func (m *model) selectedTicket() *Ticket {
	if len(m.visibleTickets) == 0 {
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.visibleTickets) {
		return nil
	}
	return &m.visibleTickets[idx]
}

// selectedTicketForDetail prefers the enriched copy so the detail pane keeps
// showing the description while the cursor stays put.
func (m *model) selectedTicketForDetail() *Ticket {
	if m.ticketDetail != nil && m.ticketDetail.Key == m.selectedTicketKey {
		return m.ticketDetail
	}
	return m.selectedTicket()
}

func (m *model) rememberTicketSelection() {
	m.selectedTicketIndex = m.table.Cursor()
	if selected := m.selectedTicket(); selected != nil {
		m.selectedTicketKey = selected.Key
	}
}

func (m *model) selectTicketByKey() {
	if len(m.visibleTickets) == 0 {
		m.selectedTicketKey = ""
		m.table.SetCursor(0)
		return
	}
	if m.selectedTicketKey == "" {
		idx := min(max(m.selectedTicketIndex, 0), len(m.visibleTickets)-1)
		m.table.SetCursor(idx)
		m.selectedTicketIndex = idx
		m.selectedTicketKey = m.visibleTickets[idx].Key
		return
	}
	for idx, ticket := range m.visibleTickets {
		if ticket.Key == m.selectedTicketKey {
			m.table.SetCursor(idx)
			m.selectedTicketIndex = idx
			return
		}
	}
	fallback := min(max(m.selectedTicketIndex-1, 0), len(m.visibleTickets)-1)
	m.table.SetCursor(fallback)
	m.selectedTicketIndex = fallback
	m.selectedTicketKey = m.visibleTickets[fallback].Key
}

func (m *model) moveTicketSelection(delta int) {
	if len(m.visibleTickets) == 0 {
		return
	}
	next := m.table.Cursor() + delta
	if next < 0 {
		next = 0
	}
	if maxIndex := len(m.visibleTickets) - 1; next > maxIndex {
		next = maxIndex
	}
	m.table.SetCursor(next)
	m.selectedTicketIndex = next
	m.selectedTicketKey = m.visibleTickets[next].Key
	m.updateDetailContent()
}

func (m model) moveTicketSelectionAndLoad(delta int) (tea.Model, tea.Cmd) {
	m.moveTicketSelection(delta)
	selected := m.selectedTicket()
	if selected == nil {
		return m, nil
	}
	if selected.Detail {
		// Already enriched from an earlier visit; no need to hit acli again.
		m.ticketDetail = selected
		m.updateDetailContent()
		return m, nil
	}
	return m.startTicketDetailLoad(selected.Key)
}

// --- detail loading ----------------------------------------------------------

func (m model) startTicketDetailLoad(key string) (tea.Model, tea.Cmd) {
	m.cancelTicketDetailLoad()
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	m.ticketDetailRequest++
	m.ticketDetailCancel = cancel
	m.ticketDetailLoading = true
	m.ticketDetail = nil
	m.statusMessage = fmt.Sprintf("Loading %s...", key)
	m.statusError = false
	if !m.loading {
		m.applySpinnerForKind(loadingDetail)
	}
	m.updateDetailContent()
	return m, tea.Batch(loadTicketDetailCmd(m.services, ctx, m.cfg, key, m.ticketDetailRequest), m.spinner.Tick)
}

func (m *model) cancelTicketDetailLoad() {
	if m.ticketDetailCancel != nil {
		m.ticketDetailCancel()
		m.ticketDetailCancel = nil
	}
	m.ticketDetailRequest++
	m.ticketDetailLoading = false
	m.ticketDetail = nil
}

func (m model) isCurrentTicketDetailResult(result ticketDetailResult) bool {
	return result.RequestID == m.ticketDetailRequest && result.Key == m.selectedTicketKey
}

// mergeTicketDetail writes the enriched ticket back into both slices so the
// description survives cursor movement and re-filtering without another fetch.
func (m *model) mergeTicketDetail(detail Ticket) {
	for idx := range m.tickets {
		if m.tickets[idx].Key == detail.Key {
			detail.Worktree = m.tickets[idx].Worktree
			detail.PR = m.tickets[idx].PR
			m.tickets[idx] = detail
			break
		}
	}
	for idx := range m.visibleTickets {
		if m.visibleTickets[idx].Key == detail.Key {
			m.visibleTickets[idx] = detail
			break
		}
	}
	m.rebuildRows()
	m.selectTicketByKey()
}

func (m *model) setTicketWorktree(key string, worktree *Worktree) {
	for index := range m.tickets {
		if m.tickets[index].Key == key {
			m.tickets[index].Worktree = worktree
		}
	}
	for index := range m.visibleTickets {
		if m.visibleTickets[index].Key == key {
			m.visibleTickets[index].Worktree = worktree
		}
	}
	if m.ticketDetail != nil && m.ticketDetail.Key == key {
		m.ticketDetail.Worktree = worktree
	}
}

func (m *model) clearTicketWorktree(key string, path string) {
	clear := func(ticket *Ticket) {
		if ticket.Key == key && ticket.Worktree != nil && ticket.Worktree.Path == path {
			ticket.Worktree = nil
		}
	}
	for index := range m.tickets {
		clear(&m.tickets[index])
	}
	for index := range m.visibleTickets {
		clear(&m.visibleTickets[index])
	}
	if m.ticketDetail != nil {
		clear(m.ticketDetail)
	}
}

// --- filtering and rows ------------------------------------------------------

func (m *model) applyTicketFilter() {
	m.visibleTickets = m.services.FilterTickets(m.tickets, m.filterQuery, m.ticketStatusFilter, m.ticketOpenOnly)
	m.rebuildRows()
	m.selectTicketByKey()
	m.updateDetailContent()
}

func (m *model) rebuildTicketRows() {
	rows := make([]table.Row, 0, len(m.visibleTickets))
	compact := m.table.Width() <= 70
	for _, ticket := range m.visibleTickets {
		rows = append(rows, table.Row(m.ticketRowCells(ticket, compact)))
	}
	if len(rows) == 0 {
		columnCount := 6
		if compact {
			columnCount = 4
		}
		rows = append(rows, table.Row(emptyTableRow(columnCount, m.emptyTicketsMessage())))
	}
	m.table.SetRows(rows)
}

func (m model) emptyTicketsMessage() string {
	if m.cfg.ActiveProfile.Jira == nil {
		return "JIRA is not configured for this profile — press I"
	}
	if m.filterQuery != "" {
		return fmt.Sprintf("No tickets match %q", m.filterQuery)
	}
	if m.ticketOpenOnly {
		return "No open tickets match the active filters — press f to adjust"
	}
	if m.ticketSprintOnly {
		return "No tickets in the active sprint"
	}
	if m.ticketMineOnly {
		return "No tickets assigned to you — press Y to show the whole board"
	}
	return "No tickets on this board"
}

func (m model) ticketRowCells(ticket Ticket, compact bool) []string {
	worktreeCell := "-"
	if ticket.Worktree != nil {
		worktreeCell = icons.Link + " " + ticket.Worktree.Name
	}
	prCell := "-"
	if ticket.PR != nil {
		prCell = fmt.Sprintf("%s #%d", prStateIcon(ticket.PR.State, false), ticket.PR.Number)
	}
	statusCell := ticketStatusIcon(ticket) + " " + emptyDash(ticket.Status)

	if compact {
		return []string{ticket.Key, statusCell, emptyDash(ticket.Summary), worktreeCell}
	}
	return []string{ticket.Key, emptyDash(ticket.Type), statusCell, emptyDash(ticket.Summary), worktreeCell, prCell}
}

func ticketDetailViewContent(ticket *Ticket, loading bool, width int) string {
	if ticket == nil {
		return "No JIRA ticket selected"
	}
	contentWidth := max(20, width-2)
	lines := []string{
		titleStyle.Render(ticket.Key + "  " + ticket.Summary),
		fmt.Sprintf("%s %s", ticketStatusIcon(*ticket), emptyDash(ticket.Status)),
		"",
		detailSection("Ticket"),
		styledWrappedLine("Type: "+emptyDash(ticket.Type), subtleStyle, contentWidth),
		styledWrappedLine("Priority: "+emptyDash(ticket.Priority), subtleStyle, contentWidth),
		styledWrappedLine("Assignee: "+emptyDash(ticket.Assignee.Name()), subtleStyle, contentWidth),
		styledWrappedLine("Reporter: "+emptyDash(ticket.Reporter.Name()), subtleStyle, contentWidth),
	}
	if len(ticket.Labels) > 0 {
		lines = append(lines, styledWrappedLine("Labels: "+strings.Join(ticket.Labels, ", "), subtleStyle, contentWidth))
	}
	if ticket.Worktree != nil {
		lines = append(lines, styledWrappedLine("Worktree: "+ticket.Worktree.Name+"  "+ticket.Worktree.Path, infoStyle, contentWidth))
	}
	if ticket.PR != nil {
		lines = append(lines, styledWrappedLine(fmt.Sprintf("PR: #%d %s", ticket.PR.Number, ticket.PR.Title), magentaStyle, contentWidth))
	}
	if loading && !ticket.Detail {
		lines = append(lines, "", subtleStyle.Render("Loading description and comments..."))
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "", detailSection("Description"))
	if strings.TrimSpace(ticket.Description) == "" {
		lines = append(lines, mutedStyle.Render("No description"))
	} else {
		lines = append(lines, styledWrappedLine(ticket.Description, lipgloss.NewStyle(), contentWidth))
	}
	lines = append(lines, "", detailSection(fmt.Sprintf("Comments (%d)", len(ticket.Comments))))
	if len(ticket.Comments) == 0 {
		lines = append(lines, mutedStyle.Render("No comments"))
	} else {
		for _, comment := range ticket.Comments {
			meta := emptyDash(comment.Author)
			if created := formatTicketTime(comment.Created); created != "" {
				meta += " · " + created
			}
			lines = append(lines, subtleStyle.Render(meta), styledWrappedLine(comment.Body, lipgloss.NewStyle(), contentWidth), "")
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func formatTicketTime(value string) string {
	if value == "" {
		return ""
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Local().Format("2006-01-02 15:04")
		}
	}
	return value
}

func buildTicketColumns(totalWidth int) []table.Column {
	if totalWidth <= 70 {
		widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*4, []int{9, 8, 12, 8}, []int{13, 14, 30, 16}, []int{3, 2, 0, 1})
		return []table.Column{{Title: "Key", Width: widths[0]}, {Title: "Status", Width: widths[1]}, {Title: "Summary", Width: widths[2]}, {Title: "WT", Width: widths[3]}}
	}
	widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*6, []int{9, 6, 10, 14, 10, 6}, []int{13, 10, 16, 34, 20, 9}, []int{5, 4, 2, 0, 1, 3})
	return []table.Column{{Title: "Key", Width: widths[0]}, {Title: "Type", Width: widths[1]}, {Title: "Status", Width: widths[2]}, {Title: "Summary", Width: widths[3]}, {Title: "Worktree", Width: widths[4]}, {Title: "PR", Width: widths[5]}}
}

func (m model) ticketFilterSummary() string {
	parts := []string{fmt.Sprintf("Showing %d/%d tickets", len(m.visibleTickets), len(m.tickets))}
	if m.ticketBoard.Name != "" {
		parts = append(parts, "board="+m.ticketBoard.Name)
	}
	if m.ticketMineOnly {
		parts = append(parts, "assignee=me")
	}
	if m.ticketSprintOnly {
		parts = append(parts, "sprint=active")
	}
	if m.ticketOpenOnly {
		parts = append(parts, "status=open")
	}
	if m.filterQuery != "" {
		parts = append(parts, fmt.Sprintf("query=%q", m.filterQuery))
	}
	if m.ticketCacheLabel != "" {
		parts = append(parts, m.ticketCacheLabel)
	}
	return strings.Join(parts, " ")
}

func (m model) ticketLoadSummary() string {
	summary := fmt.Sprintf("Loaded %d tickets from %s", len(m.tickets), emptyDash(m.ticketBoard.Name))
	if m.filterQuery != "" {
		summary += fmt.Sprintf(", %d match %q", len(m.visibleTickets), m.filterQuery)
	}
	return summary
}

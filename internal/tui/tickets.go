package tui

import (
	"context"
	"fmt"
	"slices"
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
		m.attachPRIndex()
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
		return m.enterPRMode()
	case modeTickets:
		return m.enterTicketMode()
	default:
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, "Refreshing worktrees..."))
	}
}

func (m model) startTicketWorktreeCreation(ticket Ticket, action string) (tea.Model, tea.Cmd) {
	present := ticket.Worktree != nil && !ticket.Worktree.Missing && !ticket.Worktree.Prunable
	if m.cfg.MultiRepository() {
		// Opening the ticket's worktree needs no repository; making one does,
		// even when the ticket already has a worktree in another repository.
		if present && action != "" {
			return m.openWorktreeWithAction(*ticket.Worktree, action)
		}
		return m.openRepositoryPicker(pickRepoForTicket, ticket, action)
	}
	if present {
		if action != "" {
			return m.openWorktreeWithAction(*ticket.Worktree, action)
		}
		return m.selectTicketWorktree(ticket.Key, *ticket.Worktree)
	}
	return m.createTicketWorktree(ticket, m.cfg, action)
}

// startTicketWorktreeInRepository reuses the ticket's worktree in repository
// or creates one there, then runs action on it.
func (m model) startTicketWorktreeInRepository(ticket Ticket, repository string, action string) (tea.Model, tea.Cmd) {
	remember := m.rememberRepository(repository)
	if existing := m.ticketWorktreeIn(ticket.Key, repository); existing != nil {
		var next tea.Model
		var cmd tea.Cmd
		if action != "" {
			next, cmd = m.openWorktreeWithAction(*existing, action)
		} else {
			next, cmd = m.selectTicketWorktree(ticket.Key, *existing)
		}
		return next, tea.Batch(cmd, remember)
	}
	ticket.Worktree = nil
	next, cmd := m.createTicketWorktree(ticket, m.cfg.ForRepository(repository), action)
	return next, tea.Batch(cmd, remember)
}

func (m model) selectTicketWorktree(key string, worktree Worktree) (tea.Model, tea.Cmd) {
	m.selectedTicketKey = key
	m.pendingWorktreePath = worktree.Path
	m.statusMessage = fmt.Sprintf("Selected existing worktree %s. Refreshing tickets...", worktree.Name)
	return m, tea.Batch(m.loadTicketsCmd(false), m.startLoading(loadingTickets, m.statusMessage))
}

// createTicketWorktree creates the ticket's worktree in the repository cfg is
// scoped to.
func (m model) createTicketWorktree(ticket Ticket, cfg Config, action string) (tea.Model, tea.Cmd) {
	branch := m.services.TicketBranchName(ticket)
	worktree := Worktree{
		Name:       branch,
		Branch:     branch,
		Path:       m.services.CreateWorktreePath(cfg, branch),
		Managed:    true,
		Repository: cfg.RepositoryName(),
	}
	m.beginWorktreeCreation(worktree, ticket.Key)
	if action != "" {
		m.deferPendingWorktreeAction(worktree.Path, action)
	}
	preparing := fmt.Sprintf("Preparing worktree for %s...", ticket.Key)
	if m.cfg.MultiRepository() {
		preparing = fmt.Sprintf("Preparing %s worktree for %s...", worktree.Repository, ticket.Key)
	}
	return m, tea.Batch(
		m.ops.enqueue(gitQueueKey(cfg), ensureTicketWorktreeCmd(m.services, cfg, ticket)),
		m.startLoading(loadingMutation, preparing),
	)
}

// enterPRMode reuses prefetched or cached PRs when there are any, so the list
// paints at once and is only revalidated in the background once it ages out.
func (m model) enterPRMode() (tea.Model, tea.Cmd) {
	prCfg := m.prCfg()
	if m.prListSlug != prCfg.RepoSlug {
		m.remotePRs = nil
		m.prListFetchedAt = time.Time{}
	}
	if len(m.remotePRs) == 0 {
		if cached, fetchedAt, ok := m.services.PeekRemotePullRequests(prCfg); ok {
			m.remotePRs = cached
			m.prListFetchedAt = fetchedAt
			m.prListSlug = prCfg.RepoSlug
		}
	}
	if len(m.remotePRs) == 0 {
		m.prListLoading = true
		loading := "Loading remote PRs..."
		if m.cfg.MultiRepository() {
			loading = fmt.Sprintf("Loading %s PRs...", prCfg.RepositoryName())
		}
		return m, tea.Batch(loadRemotePullRequestsCmd(m.services, prCfg, false), m.startLoading(loadingPRs, loading))
	}

	m.applyPRFilter()
	m.statusMessage = m.prFilterSummary()
	cmds := []tea.Cmd{m.loadMissingChecks()}
	if !m.prListLoading && !isRecent(m.prListFetchedAt, prListTTL) {
		m.prListLoading = true
		cmds = append(cmds, loadRemotePullRequestsCmd(m.services, prCfg, true), m.spinner.Tick)
	}
	if selected := m.selectedRemotePR(); selected != nil {
		next, cmd := m.startPRDetailLoad(selected.Number)
		m = next.(model)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
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
		m.statusMessage = "No JIRA board or space configured for this profile — press I to run setup"
		m.statusError = true
		return m, nil
	}
	if !m.ticketBoard.Configured() {
		if board := jira.ActiveBoard(); board != nil {
			m.ticketBoard = *board
		}
	}

	if m.ticketsSource != m.ticketSource() {
		m.tickets = nil
	}
	if len(m.tickets) == 0 {
		m.paintCachedTickets()
	}
	if len(m.tickets) > 0 {
		// Prefetched or cached rows: show them now and revalidate quietly.
		m.applyTicketFilter()
		m.statusMessage = m.ticketFilterSummary()
		cmds := []tea.Cmd{}
		if !m.ticketsLoading {
			m.ticketsLoading = true
			cmds = append(cmds, m.backgroundTicketsCmd(false), m.spinner.Tick)
		}
		if selected := m.selectedTicket(); selected != nil {
			next, cmd := m.startTicketDetailLoad(selected.Key)
			m = next.(model)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	m.ticketsLoading = true
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
		if !m.ticketBoard.SprintScoped(true) {
			m.statusMessage = fmt.Sprintf("%s is a whole space, which has no sprint of its own — press b to pick a board for sprint scope", m.ticketBoard.Name)
			if m.ticketBoard.IsCombined() {
				m.statusMessage = "Only spaces are configured, and spaces have no sprint of their own — add a board in setup (I) for sprint scope"
			}
			m.statusError = false
			return m, nil, true
		}
		m.ticketSprintOnly = !m.ticketSprintOnly
		return m.reloadTicketsAfterScopeChange("sprint")

	case key.Matches(msg, m.keys.BoardMenu):
		if m.cfg.ActiveProfile.Jira == nil || len(m.cfg.ActiveProfile.Jira.Boards) == 0 {
			m.statusMessage = "No boards or spaces configured — press I to run setup"
			m.statusError = true
			return m, nil, true
		}
		m.selectingBoard = true
		m.boardCursor = m.currentBoardIndex()
		m.statusMessage = "Select a JIRA board or space"
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
		return m, m.persistConfig("Saved config " + m.cfg.ConfigPath)
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
	key, services := selected.Key, m.services
	patch := core.TicketPatch{Key: key, Status: status, StatusCategory: m.statusCategoryFor(status)}
	return m.startTicketMutation("transition", patch,
		fmt.Sprintf("Changing %s to %s...", key, status),
		fmt.Sprintf("Changed %s to %s", key, status),
		func(ctx context.Context) error { return services.TransitionTicket(ctx, key, status) })
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
		key, services := selected.Key, m.services
		patch := core.TicketPatch{Key: key, Comment: &TicketComment{Author: "You", Body: body, Created: nowRFC3339()}}
		return m.startTicketMutation("comment", patch,
			fmt.Sprintf("Commenting on %s...", key),
			fmt.Sprintf("Commented on %s", key),
			func(ctx context.Context) error { return services.CommentTicket(ctx, key, body) })
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
	key, services := selected.Key, m.services
	user := assigneeForPatch(assignee, m.ticketAssigneeOptions)
	patch := core.TicketPatch{Key: key, Assignee: &user}
	return m.startTicketMutation("assign", patch,
		fmt.Sprintf("Assigning %s...", key),
		fmt.Sprintf("Assigned %s to %s", key, assignee),
		func(ctx context.Context) error { return services.AssignTicket(ctx, key, assignee) })
}

func (m model) updateBoardSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	options := m.ticketSourceOptions()
	if len(options) == 0 {
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
		if m.boardCursor < len(options)-1 {
			m.boardCursor++
		}
		return m, nil
	case "enter":
		m.boardCursor = min(max(m.boardCursor, 0), len(options)-1)
		m.ticketBoard = options[m.boardCursor]
		m.selectingBoard = false
		m.persistTicketScope()
		returnModel, cmd, _ := m.reloadTicketsAfterScopeChange("board")
		return returnModel, tea.Batch(cmd, m.persistConfig("Saved config "+m.cfg.ConfigPath))
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

	// Each scope is cached separately, so swapping scope can also paint instantly.
	m.tickets = nil
	m.ticketsSource = ""
	m.ticketCacheLabel = ""
	m.paintCachedTickets()
	m.applyTicketFilter()
	_ = what
	m.ticketsLoading = true
	return m, tea.Batch(m.loadTicketsCmd(false), m.persistConfig("Saved config "+m.cfg.ConfigPath), m.startLoading(loadingTickets, m.ticketLoadingMessage())), true
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

// ticketSourceOptions lists what b can switch to: every board and space
// together first, when there is more than one, then each on its own.
func (m model) ticketSourceOptions() []JiraBoard {
	jira := m.cfg.ActiveProfile.Jira
	if jira == nil || len(jira.Boards) == 0 {
		return nil
	}
	options := make([]JiraBoard, 0, len(jira.Boards)+1)
	if len(jira.Boards) > 1 {
		options = append(options, jira.AllSources())
	}
	return append(options, jira.Boards...)
}

func (m model) currentBoardIndex() int {
	for idx, option := range m.ticketSourceOptions() {
		if option.SourceID() == m.ticketBoard.SourceID() {
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
	// Show the description and comments from an earlier visit while JIRA is
	// asked again.
	if cached, ok := m.services.PeekTicketDetail(m.cfg, key); ok {
		if listed, found := m.ticketByKey(key); found {
			cached.Worktree, cached.PR = listed.Worktree, listed.PR
			if listed.Status != "" {
				cached.Status, cached.StatusCategory = listed.Status, listed.StatusCategory
			}
		}
		m.ticketDetail = &cached
	}
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	m.ticketDetailRequest++
	m.ticketDetailCancel = cancel
	m.ticketDetailLoading = true
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
	if m.ticketSprintScoped() {
		return "No tickets in the active sprint"
	}
	if m.ticketBoard.IsSpace() {
		if m.ticketMineOnly {
			return "No tickets in this space are assigned to you — press Y to show the whole space"
		}
		return "No tickets in this space"
	}
	if m.ticketBoard.IsCombined() {
		if m.ticketMineOnly {
			return "No tickets in any board or space are assigned to you — press Y to show everyone's"
		}
		return "No tickets in any board or space"
	}
	if m.ticketMineOnly {
		return "No tickets assigned to you — press Y to show the whole board"
	}
	return "No tickets on this board"
}

// ticketSprintScoped reports whether the sprint filter applies to the current
// source; a space has no sprint of its own.
func (m model) ticketSprintScoped() bool {
	return m.ticketBoard.SprintScoped(m.ticketSprintOnly)
}

// ticketSourceLabel names the current source for status lines.
func (m model) ticketSourceLabel() string {
	if m.ticketBoard.IsCombined() {
		return "source=all boards and spaces"
	}
	if m.ticketBoard.IsSpace() {
		return "space=" + m.ticketBoard.Name
	}
	return "board=" + emptyDash(m.ticketBoard.Name)
}

// jiraSourceTag labels what a source lists: a board's spaces, a whole space,
// or every space the boards and spaces shown together cover.
func jiraSourceTag(board JiraBoard) string {
	if board.IsSpace() {
		return "[whole space]"
	}
	var keys []string
	for _, member := range board.Members() {
		for _, key := range member.Projects {
			if !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}
	if len(keys) == 0 {
		return ""
	}
	return "[" + strings.Join(keys, ", ") + "]"
}

func (m model) ticketRowCells(ticket Ticket, compact bool) []string {
	worktreeCell := "-"
	if ticket.Worktree != nil {
		worktreeCell = icons.Link + " " + ticket.Worktree.Name
		if m.cfg.MultiRepository() {
			worktreeCell = icons.Link + " " + m.worktreeRepository(*ticket.Worktree) + "/" + ticket.Worktree.Name
		}
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
	return ticketDetailViewContentWithWorktrees(ticket, nil, loading, width)
}

// ticketDetailViewContentWithWorktrees lists the ticket's worktrees in every
// repository of a multi-repository project instead of its single worktree.
func ticketDetailViewContentWithWorktrees(ticket *Ticket, worktrees []Worktree, loading bool, width int) string {
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
	if len(worktrees) > 0 {
		lines = append(lines, styledWrappedLine("Worktrees:", infoStyle, contentWidth))
		for _, wt := range worktrees {
			lines = append(lines, styledWrappedLine("  "+icons.Repo+" "+wt.Repository+"  "+wt.Name+"  "+wt.Path, infoStyle, contentWidth))
		}
	} else if ticket.Worktree != nil {
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
		parts = append(parts, m.ticketSourceLabel())
	}
	if m.ticketMineOnly {
		parts = append(parts, "assignee=me")
	}
	if m.ticketSprintScoped() {
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

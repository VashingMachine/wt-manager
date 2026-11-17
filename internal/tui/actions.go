package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Commands: what the keys of the main view and the entries of the command
// palette run. A command is addressed by its ID from the catalog in keymap.go;
// which key reaches it is decided there, never here.

// commandReachable reports whether a command's keys work right now. Every
// command is reachable in the main view; full-screen readers and overlays
// keep most keys for themselves.
func (m model) commandReachable(id string) bool {
	switch {
	case m.logsOpen:
		return id == cmdPalette
	case m.showingHelp:
		return id == cmdPalette || id == cmdHelp || id == cmdKeybindings
	case m.isPRMode() && m.prDiffMode:
		return slices.Contains([]string{cmdPalette, cmdQuit, "refresh", "approve", "open-browser"}, id)
	case m.isTicketMode() && m.ticketReadMode:
		return slices.Contains([]string{cmdPalette, cmdQuit, "open-browser"}, id)
	}
	return true
}

// inMainView reports that nothing covers the table and details panes.
func (m model) inMainView() bool {
	return !m.logsOpen && !m.showingHelp && !(m.isPRMode() && m.prDiffMode) && !(m.isTicketMode() && m.ticketReadMode)
}

// dispatchKey runs the command the pressed key is bound to in the current
// view. The keys of a longer keybinding arrive one at a time; until it is
// complete, the keys so far wait in pendingKeys.
func (m model) dispatchKey(pending keySeq, msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	name := keyName(msg)
	if name == "" {
		return m, nil, len(pending) > 0
	}
	if len(pending) > 0 && name == "esc" {
		return m, m.setTransientStatus("Cancelled " + pending.Label()), true
	}
	pressed := append(slices.Clone(pending), name)
	km := m.keymap()
	scope := scopeOf(m.mode)
	id, more := km.resolve(pressed, scope, m.commandReachable)
	switch {
	case id != "":
		next, cmd := m.runCommand(id)
		return next, cmd, true
	case more:
		m.pendingKeys = pressed
		return m, nil, true
	case len(pending) > 0:
		return m, m.setTransientStatus(pressed.Label() + " is not a keybinding"), true
	}
	if m.inMainView() {
		if id := km.explained(pressed, scope); id != "" {
			next, cmd := m.runCommand(id)
			return next, cmd, true
		}
	}
	return m, nil, false
}

// pendingKeysHint tells which keys of a longer keybinding were pressed so far.
func (m model) pendingKeysHint() string {
	if len(m.pendingKeys) == 0 {
		return ""
	}
	return m.pendingKeys.Label() + " › …  waiting for the next key (esc cancels)"
}

// runCommand runs a command the way its key does in the main view.
func (m model) runCommand(id string) (tea.Model, tea.Cmd) {
	next, cmd, _ := m.execCommand(id)
	return next, cmd
}

// execCommand runs a command and reports whether the ID names one.
func (m model) execCommand(id string) (tea.Model, tea.Cmd, bool) {
	var next tea.Model
	var cmd tea.Cmd
	switch id {
	case cmdPalette:
		next, cmd = m.openCommandPalette()
	case cmdHelp:
		next, cmd = m.toggleHelp()
	case cmdKeybindings:
		next, cmd = m.openKeybindings()
	case "refresh":
		next, cmd = m.refreshCurrentView()
	case "filter":
		next, cmd = m.startFilter()
	case cmdClearFilter:
		next, cmd = m.clearFilter()
	case "focus":
		next, cmd = m.toggleFocus()
	case "up":
		next, cmd = m.moveCursor(-1)
	case "down":
		next, cmd = m.moveCursor(1)
	case "worktrees":
		next, cmd = m.switchMode(modeWorktrees)
	case "prs":
		next, cmd = m.switchMode(m.toggleMode(modePRs))
	case "tickets":
		next, cmd = m.switchMode(m.toggleMode(modeTickets))
	case "new":
		next, cmd = m.createWorktreeForSelection()
	case "open-editor":
		next, cmd = m.openSelectionWith(core.EnterActionVSCode)
	case "open-agent":
		next, cmd = m.openSelectionWith(core.EnterActionAgent)
	case "open-browser":
		next, cmd = m.openSelectionInBrowser()
	case "logs":
		next, cmd = m.openSessionLogs()
	case cmdQuit:
		next, cmd = m, tea.Quit

	case "open":
		if !m.isWorktreeMode() {
			return m, nil, true
		}
		next, cmd = m.openSelectedWorktreeWithAction(m.cfg.App.DefaultEnterAction)
	case "terminal":
		if !m.isWorktreeMode() {
			return m, nil, true
		}
		next, cmd = m.openSelectedWorktreeWithAction(core.EnterActionTerminal)
	case "toggle-all":
		next, cmd = m.toggleAllWorktrees()
	case "enter-menu":
		next, cmd = m.openEnterActionMenu()
	case "stack-new":
		next, cmd = m.startStackedWorktree()
	case "stack-on":
		next, cmd = m.openStackOnSelected()
	case "restack":
		next, cmd = m.openRestackConfirmation()
	case "unstack":
		next, cmd = m.unstackSelected()
	case cmdDelete:
		next, cmd = m.deleteSelectedWorktree()
	case "sweep":
		next, cmd = m.startSweepScan()
	case "body":
		next, cmd = m.togglePRBody()

	case "diff", "state", "user", "mine", "branch", "checks", "approve", "ask":
		if !m.isPRMode() {
			return m, nil, true
		}
		next, cmd = m.runPRCommand(id)
	case "pr-repo":
		next, cmd = m.choosePRRepository()

	case "ticket-read", "ticket-filters", "ticket-mine", "ticket-sprint", "ticket-board", "ticket-status", "ticket-comment", "ticket-assign":
		if !m.isTicketMode() {
			return m, nil, true
		}
		next, cmd = m.runTicketCommand(id)

	case "agent-menu":
		next, cmd = m.openAgentMenu()
	case "editor":
		next, cmd = m.openEditorSelector()
	case "config-edit":
		next, cmd = m.editConfigInEditor()
	case "config-edit-terminal":
		next, cmd = m.editConfigInTerminal()
	case "config-reload":
		next, cmd = m.reloadConfig()

	case "profile-menu":
		next, cmd = m.openProfileMenu()
	case "setup":
		next, cmd = m.openSetup(m.projectSetup())
	case "project-new":
		next, cmd = m.openSetup(newProjectSetupModel(m.cfg, m.services))
	case "repo-add":
		next, cmd = m.openAddRepository()
	case "repo-remove":
		if !m.cfg.MultiRepository() {
			return m, m.setTransientStatus("This project has one repository, which cannot be removed"), true
		}
		next, cmd = m.openRepositoryPicker(pickRepoToRemove, Ticket{}, "")

	default:
		return m.execChoiceCommand(id)
	}
	return next, cmd, true
}

// execChoiceCommand runs the commands that pick one configured choice
// directly: an Enter action, an agent, an editor, or a project.
func (m model) execChoiceCommand(id string) (tea.Model, tea.Cmd, bool) {
	if action, ok := strings.CutPrefix(id, cmdEnterActionPrefix); ok {
		for _, option := range enterActionOptions {
			if enterActionCommandID(option) == cmdEnterActionPrefix+action {
				m.cfg.App.DefaultEnterAction = option.Action
				return m, m.persistConfig("Default Enter action set to " + m.configuredEnterActionLabel()), true
			}
		}
	}
	if name, ok := strings.CutPrefix(id, cmdAgentPrefix); ok {
		for _, agent := range m.cfg.App.Agents {
			if agent.Name == name {
				m.cfg.App.DefaultAgent = agent.Name
				return m, m.persistConfig("Default AI agent set to " + m.cfg.App.DefaultAgent), true
			}
		}
	}
	if command, ok := strings.CutPrefix(id, cmdEditorPrefix); ok {
		for _, editor := range editorPresets {
			if editor.Command == command {
				next, cmd := m.saveEditor(editor)
				return next, cmd, true
			}
		}
	}
	if name, ok := strings.CutPrefix(id, cmdProfilePrefix); ok {
		for idx, profile := range m.cfg.App.Profiles {
			if profile.Name == name {
				m.profileCursor = idx
				next, cmd := m.updateProfileSelector(tea.KeyMsg{Type: tea.KeyEnter})
				return next, cmd, true
			}
		}
	}
	return m, nil, false
}

func (m model) startFilter() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.filtering = true
	m.filterPrevQuery = m.filterQuery
	m.filterInput.SetValue(m.filterQuery)
	m.filterInput.CursorEnd()
	m.filterInput.Focus()
	m.statusMessage = map[viewMode]string{
		modeWorktrees: "Filter by worktree or branch",
		modePRs:       "Filter remote pull requests",
		modeTickets:   "Filter JIRA tickets",
	}[m.mode]
	m.statusError = false
	return m, nil
}

func (m model) clearFilter() (tea.Model, tea.Cmd) {
	m.filterQuery = ""
	m.filterInput.SetValue("")
	m.applyFilter()
	return m, m.setTransientStatus("Filter cleared")
}

func (m model) toggleFocus() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.focusTable = !m.focusTable
	m.table.Focus()
	if !m.focusTable {
		m.table.Blur()
	}
	return m, m.setTransientStatus(map[bool]string{true: "Table focused", false: "Details focused"}[m.focusTable])
}

// moveCursor moves the table selection, or scrolls the details pane when
// that has focus.
func (m model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if !m.focusTable {
		if delta < 0 {
			m.detail.ScrollUp(-delta)
		} else {
			m.detail.ScrollDown(delta)
		}
		return m, nil
	}
	m.clearDeleteArm()
	if m.isPRMode() {
		return m.movePRSelectionAndLoad(delta)
	}
	if m.isTicketMode() {
		return m.moveTicketSelectionAndLoad(delta)
	}
	m.moveSelection(delta)
	return m, nil
}

func (m model) refreshCurrentView() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	if m.isTicketMode() {
		m.rememberTicketSelection()
		m.cancelTicketDetailLoad()
		return m, tea.Batch(m.loadTicketsCmd(true), m.startLoading(loadingTickets, "Refreshing JIRA tickets..."))
	}
	if m.isPRMode() {
		m.rememberPRSelection()
		m.cancelPRDetailLoad()
		m.confirmingPRApproval = false
		m.prApprovalPendingNumber = 0
		m.prApprovalAwaitingDetailNumber = 0
		m.prApprovalWarnings = nil
		m.prListLoading = true
		return m, tea.Batch(loadRemotePullRequestsCmd(m.services, m.prCfg(), false), m.startLoading(loadingPRs, "Refreshing remote PRs..."))
	}
	m.rememberSelection()
	m.refreshPRIndex = true
	return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, "Refreshing worktrees..."))
}

func (m model) choosePRRepository() (tea.Model, tea.Cmd) {
	if !m.cfg.MultiRepository() {
		return m, m.setTransientStatus("This project has one repository; add another from the command palette")
	}
	return m.openRepositoryPicker(pickRepoForPRRadar, Ticket{}, "")
}

// runPRCommand runs the commands that only exist in PR Radar.
func (m model) runPRCommand(id string) (tea.Model, tea.Cmd) {
	switch id {
	case "state":
		m.cyclePRStateFilter()
		m.applyPRFilter()
		m.statusMessage = m.prFilterSummary()
		m.statusError = false
	case "user":
		m.prUserOptions = append([]string{"all"}, m.services.RemotePRUsers(m.remotePRs)...)
		m.prUserCursor = m.currentPRUserIndex()
		m.selectingPRUser = true
		m.statusMessage = "Filter PRs by user"
		m.statusError = false
	case "mine":
		if m.prAuthorFilter != "" {
			m.prAuthorFilter = ""
			m.applyPRFilter()
			m.statusMessage = m.prFilterSummary()
			m.statusError = false
			return m, nil
		}
		if m.prCurrentUser == "" {
			m.statusMessage = "Current GitHub user is unknown; refresh PR Radar first"
			m.statusError = true
			return m, nil
		}
		m.prAuthorFilter = m.prCurrentUser
		m.applyPRFilter()
		m.statusMessage = m.prFilterSummary()
		m.statusError = false
	case "branch":
		m.prShowBranch = !m.prShowBranch
		m.rebuildRows()
		m.statusMessage = map[bool]string{true: "Showing PR branches", false: "Showing PR titles"}[m.prShowBranch]
		m.statusError = false
	case "checks":
		m.prShowFailedGHA = !m.prShowFailedGHA
		m.updateDetailContent()
		m.statusMessage = map[bool]string{true: "Showing failed GitHub Actions in Details", false: "Hiding failed GitHub Actions"}[m.prShowFailedGHA]
		m.statusError = false
	case "ask":
		selected := m.selectedRemotePRForDetail()
		if selected == nil {
			m.statusMessage = "Select a remote PR first"
			m.statusError = true
			return m, nil
		}
		m.askingPR = true
		m.prQuestionInput.SetValue("")
		m.prQuestionInput.Focus()
		m.statusMessage = fmt.Sprintf("Ask %s about PR #%d", m.prAgentLabel(), selected.Number)
		m.statusError = false
	case "diff":
		selected := m.selectedRemotePR()
		if selected == nil {
			m.statusMessage = "Select a remote PR first"
			m.statusError = true
			return m, nil
		}
		m.prDiffMode = true
		m.resize()
		m.statusMessage = "Showing full-screen PR diff"
		m.statusError = false
		if (m.prDetail == nil || m.prDetail.Number != selected.Number) && !m.prDetailLoading {
			return m.startPRDetailLoad(selected.Number)
		}
		if m.prDetailLoading {
			m.statusMessage = fmt.Sprintf("Loading PR #%d diff...", selected.Number)
		}
	case "approve":
		return m.beginPRApproval()
	}
	return m, nil
}

func (m model) openEnterActionMenu() (tea.Model, tea.Cmd) {
	if !m.isWorktreeMode() {
		m.statusMessage = "Enter action is available for local worktrees"
		m.statusError = true
		return m, nil
	}
	m.clearDeleteArm()
	m.selectingEnterAction = true
	m.enterActionCursor = m.defaultEnterActionIndex()
	m.statusMessage = "Select default Enter action"
	m.statusError = false
	return m, nil
}

func (m model) toggleAllWorktrees() (tea.Model, tea.Cmd) {
	if !m.isWorktreeMode() {
		m.statusMessage = "Toggle all worktrees is unavailable in PR Radar"
		m.statusError = true
		return m, nil
	}
	m.clearDeleteArm()
	m.showAll = !m.showAll
	m.rememberSelection()
	message := map[bool]string{true: "Showing all repo worktrees", false: "Showing managed worktrees only"}[m.showAll]
	return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, message))
}

func (m model) togglePRBody() (tea.Model, tea.Cmd) {
	if m.isTicketMode() {
		m.statusMessage = "PR body toggle is unavailable in JIRA tickets"
		m.statusError = true
		return m, nil
	}
	m.showPRBody = !m.showPRBody
	m.updateDetailContent()
	return m, nil
}

func (m model) openSelectionInBrowser() (tea.Model, tea.Cmd) {
	if m.isTicketMode() {
		selected := m.selectedTicketForDetail()
		if selected == nil || selected.URL == "" {
			m.statusMessage = "Selected row has no JIRA ticket URL"
			m.statusError = true
			return m, nil
		}
		return m, openBrowserMessageCmd(m.services, selected.URL, "Opened JIRA ticket in browser")
	}
	if m.isPRMode() {
		selected := m.selectedRemotePRForDetail()
		if selected == nil {
			m.statusMessage = "Selected row has no PR"
			m.statusError = true
			return m, nil
		}
		return m, openBrowserCmd(m.services, selected.URL)
	}
	selected := m.selectedWorktree()
	if selected == nil || selected.PR == nil {
		m.statusMessage = "Selected worktree has no PR"
		m.statusError = true
		return m, nil
	}
	return m, openBrowserCmd(m.services, selected.PR.URL)
}

// openSelectionWith opens the worktree of the selected row in the editor or
// with the default agent, creating the worktree of a ticket or PR first.
func (m model) openSelectionWith(action string) (tea.Model, tea.Cmd) {
	if m.isTicketMode() {
		selected := m.selectedTicket()
		if selected == nil {
			return m, nil
		}
		if selected.Worktree == nil || selected.Worktree.Missing || selected.Worktree.Prunable {
			return m.startTicketWorktreeCreation(*selected, action)
		}
		return m.openWorktreeWithAction(*selected.Worktree, action)
	}
	if m.isPRMode() {
		selected := m.selectedRemotePRForDetail()
		if selected == nil {
			m.statusMessage = "Select a remote PR first"
			m.statusError = true
			return m, nil
		}
		prCfg := m.prCfg()
		if m.deferPendingWorktreeAction(m.services.CreateWorktreePath(prCfg, selected.HeadRefName), action) {
			return m, nil
		}
		if action == core.EnterActionAgent {
			agent := m.defaultAgent()
			if agent == nil {
				m.statusMessage = "No AI agent configured"
				m.statusError = true
				return m, nil
			}
			return m, tea.Batch(m.ops.enqueue(gitQueueKey(prCfg), openPRWorktreeAgentCmd(m.services, m.services, prCfg, *selected, *agent)), m.startLoading(loadingMutation, fmt.Sprintf("Preparing PR #%d worktree for %s...", selected.Number, agent.Name)))
		}
		return m, tea.Batch(m.ops.enqueue(gitQueueKey(prCfg), openPRWorktreeInEditorCmd(m.services, m.services, prCfg, *selected, m.configuredEditor())), m.startLoading(loadingMutation, fmt.Sprintf("Preparing PR #%d worktree for %s...", selected.Number, m.configuredEditor().Name)))
	}
	selected := m.selectedWorktree()
	if selected == nil {
		return m, nil
	}
	return m.openWorktreeWithAction(*selected, action)
}

func (m model) openAgentMenu() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.selectingAgent = true
	m.agentCursor = m.defaultAgentIndex()
	m.statusMessage = "Select default AI agent"
	m.statusError = false
	return m, nil
}

func (m model) openProfileMenu() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.selectingProfile = true
	m.profileCursor = m.defaultProfileIndex()
	m.statusMessage = "Select repository profile"
	m.statusError = false
	return m, nil
}

// createWorktreeForSelection creates a worktree: for the selected ticket or
// PR in those views, and from a branch name otherwise.
func (m model) createWorktreeForSelection() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	if m.isTicketMode() {
		selected := m.selectedTicket()
		if selected == nil {
			m.statusMessage = "Select a JIRA ticket first"
			m.statusError = true
			return m, nil
		}
		return m.startTicketWorktreeCreation(*selected, "")
	}
	if m.isPRMode() {
		selected := m.selectedRemotePRForDetail()
		if selected == nil || strings.TrimSpace(selected.HeadRefName) == "" {
			m.statusMessage = "Selected PR has no branch name"
			m.statusError = true
			return m, nil
		}
		branch := selected.HeadRefName
		prCfg := m.prCfg()
		m.beginWorktreeCreation(Worktree{
			Name:       branch,
			Branch:     branch,
			Path:       m.services.CreateWorktreePath(prCfg, branch),
			Managed:    true,
			Repository: prCfg.RepositoryName(),
		}, "")
		return m, tea.Batch(m.ops.enqueue(gitQueueKey(prCfg), createWorktreeCmd(m.services, prCfg, branch)), m.startLoading(loadingMutation, fmt.Sprintf("Creating worktree for PR #%d (%s)...", selected.Number, branch)))
	}
	if m.cfg.MultiRepository() {
		return m.openRepositoryPicker(pickRepoForWorktree, Ticket{}, "")
	}
	return m.openNewWorktreeInput("")
}

// deleteConfirmHint says how to confirm an armed deletion.
func (m model) deleteConfirmHint(name string) string {
	if label := m.keyLabel(cmdDelete); label != "" {
		return fmt.Sprintf("Press %s again to delete %s", label, name)
	}
	return fmt.Sprintf("Run delete again to delete %s", name)
}

// deleteSelectedWorktree arms deletion of the selected worktree, and deletes
// it when run again within deleteConfirmWindow.
func (m model) deleteSelectedWorktree() (tea.Model, tea.Cmd) {
	if !m.isWorktreeMode() {
		m.statusMessage = "Delete is available for local worktrees"
		m.statusError = true
		return m, nil
	}
	selected := m.selectedWorktree()
	if selected == nil {
		return m, nil
	}
	if selected.IsMain {
		m.statusMessage = "The main repository worktree cannot be deleted"
		m.statusError = true
		return m, nil
	}

	now := time.Now()
	if m.deleteArmedPath == selected.Path && now.Sub(m.deleteArmedAt) <= deleteConfirmWindow {
		wt := *selected
		m.rememberSelection()
		m.clearDeleteArm()
		if m.pendingDeletes == nil {
			m.pendingDeletes = make(map[string]pendingDelete)
		}
		m.pendingDeletes[wt.Path] = pendingDelete{Worktree: wt, Index: m.worktreeIndex(wt.Path)}
		m.optimisticRemove(wt.Path)
		wtCfg := m.worktreeCfg(wt)
		return m, tea.Batch(m.ops.enqueue(gitQueueKey(wtCfg), removeWorktreeCmd(m.services, wtCfg, wt)), m.startLoading(loadingMutation, fmt.Sprintf("Deleting %s...", wt.Name)))
	}

	m.deleteArmedPath = selected.Path
	m.deleteArmedName = selected.Name
	m.deleteArmedAt = now
	m.statusMessage = m.deleteConfirmHint(selected.Name)
	m.statusError = false
	return m, deleteArmTickCmd(selected.Path, now)
}

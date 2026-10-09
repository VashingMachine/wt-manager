package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type deleteResult struct {
	Path string
	Name string
	Err  error
}

type pendingDelete struct {
	Worktree  Worktree
	Index     int
	Completed bool
}

type pendingCreation struct {
	Worktree       Worktree
	TicketKey      string
	PreviousPath   string
	PreviousIndex  int
	Completed      bool
	DeferredAction string
}

type createResult struct {
	Path     string
	Name     string
	Existing bool
	Err      error
}

type deleteArmTick struct {
	Path    string
	ArmedAt time.Time
}

type statusExpired struct {
	Seq int
}

type actionResult struct {
	Message string
	Err     error
}

type remotePRDetailResult struct {
	Number      int
	RequestID   int
	PullRequest RemotePullRequest
	Err         error
}

type askPRResult struct {
	Question string
	Answer   string
	Err      error
}

type approvePRResult struct {
	Number int
	Err    error
}

type openPRWorktreeResult struct {
	Message string
	Err     error
}

type keyMap struct {
	Palette       key.Binding
	Up            key.Binding
	Down          key.Binding
	Focus         key.Binding
	Refresh       key.Binding
	ToggleAll     key.Binding
	PRRadar       key.Binding
	TicketMode    key.Binding
	StateFilter   key.Binding
	SprintOnly    key.Binding
	BoardMenu     key.Binding
	SetStatus     key.Binding
	WriteNote     key.Binding
	AssignSelf    key.Binding
	UserFilter    key.Binding
	MineFilter    key.Binding
	BranchView    key.Binding
	FailedGHA     key.Binding
	TicketFilters key.Binding
	HelpPage      key.Binding
	AskPR         key.Binding
	PRDiff        key.Binding
	ApprovePR     key.Binding
	Filter        key.Binding
	ToggleBody    key.Binding
	OpenPR        key.Binding
	OpenCode      key.Binding
	OpenAgent     key.Binding
	EnterAction   key.Binding
	AgentMenu     key.Binding
	ProfileMenu   key.Binding
	Setup         key.Binding
	New           key.Binding
	Delete        key.Binding
	Quit          key.Binding
}

const (
	deleteConfirmWindow = 1500 * time.Millisecond
	statusClearAfter    = 4 * time.Second
)

// viewMode is the app's top-level surface. This started life as a single
// prMode bool; it became an enum when tickets arrived as a third mode.
type viewMode int

const (
	modeWorktrees viewMode = iota
	modePRs
	modeTickets
)

const (
	loadingWorktrees = "worktrees"
	loadingPRs       = "prs"
	loadingDetail    = "detail"
	loadingAgent     = "agent"
	loadingMutation  = "mutation"
	loadingTickets   = "tickets"
)

type enterActionOption struct {
	Action      string
	Label       string
	Description string
}

var enterActionOptions = []enterActionOption{
	{Action: core.EnterActionVSCode, Label: "Editor", Description: "Open in the configured editor"},
	{Action: core.EnterActionAgent, Label: "Agent", Description: "Open with default agent in a terminal tab"},
	{Action: core.EnterActionTerminal, Label: "Terminal", Description: "Open terminal in worktree"},
}

func defaultKeys() keyMap {
	return keyMap{
		Palette:       key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "commands")),
		Up:            key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "move")),
		Down:          key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "move")),
		Focus:         key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		Refresh:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		ToggleAll:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle all worktrees")),
		PRRadar:       key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "PR radar")),
		TicketMode:    key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "JIRA tickets")),
		StateFilter:   key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "PR state")),
		SprintOnly:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "active sprint")),
		BoardMenu:     key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "board")),
		SetStatus:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "set status")),
		WriteNote:     key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "comment")),
		AssignSelf:    key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "assign")),
		UserFilter:    key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "PR user")),
		MineFilter:    key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "my PRs")),
		BranchView:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "title/branch")),
		FailedGHA:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "failed checks")),
		TicketFilters: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "ticket filters")),
		HelpPage:      key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "help")),
		AskPR:         key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "ask agent")),
		PRDiff:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/diff")),
		ApprovePR:     key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "approve PR")),
		Filter:        key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ToggleBody:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "toggle PR body")),
		OpenPR:        key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open PR in browser")),
		OpenCode:      key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "open editor")),
		OpenAgent:     key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "open agent")),
		EnterAction:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enter action")),
		AgentMenu:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "select agent")),
		ProfileMenu:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "switch repo")),
		Setup:         key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "setup")),
		New:           key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new worktree")),
		Delete:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d d", "delete worktree")),
		Quit:          key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Palette, k.HelpPage, k.Refresh, k.PRRadar, k.TicketMode, k.Filter, k.PRDiff, k.OpenPR, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Palette, k.Up, k.Down, k.Focus, k.HelpPage, k.Refresh, k.ToggleAll, k.PRRadar, k.TicketMode, k.Filter}, {k.StateFilter, k.SprintOnly, k.BoardMenu, k.SetStatus, k.WriteNote, k.AssignSelf, k.UserFilter, k.MineFilter, k.BranchView, k.FailedGHA, k.TicketFilters, k.PRDiff, k.ApprovePR, k.AskPR, k.New, k.ProfileMenu, k.Setup, k.ToggleBody, k.OpenPR, k.OpenCode, k.OpenAgent, k.EnterAction, k.AgentMenu, k.Delete, k.Quit}}
}

type model struct {
	services                       core.Services
	configSaveState                *configSaveState
	cfg                            Config
	keys                           keyMap
	help                           help.Model
	table                          table.Model
	detail                         viewport.Model
	filterInput                    textinput.Model
	paletteOpen                    bool
	paletteInput                   textinput.Model
	paletteCursor                  int
	paletteWorktrees               []Worktree
	paletteRequest                 int
	logsOpen                       bool
	logsViewport                   viewport.Model
	logsPath                       string
	logsContent                    string
	logsRequest                    int
	selectingEditor                bool
	editorCursor                   int
	editingEditor                  bool
	editorNameInput                textinput.Model
	editorCommandInput             textinput.Model
	editorField                    int
	newInput                       textinput.Model
	setup                          *setupModel
	spinner                        spinner.Model
	allWorktrees                   []Worktree
	visibleWorktrees               []Worktree
	remotePRs                      []RemotePullRequest
	visiblePRs                     []RemotePullRequest
	selectedPRNumber               int
	selectedPRIndex                int
	prDetail                       *RemotePullRequest
	prDetailLoading                bool
	prDetailRequest                int
	prDetailCancel                 context.CancelFunc
	mode                           viewMode
	prStateFilter                  string
	prUserFilter                   string
	prAuthorFilter                 string
	prCurrentUser                  string
	prShowBranch                   bool
	prShowFailedGHA                bool
	prDiffMode                     bool
	selectingPRUser                bool
	prUserOptions                  []string
	prUserCursor                   int
	askingPR                       bool
	confirmingPRApproval           bool
	prApprovalPendingNumber        int
	prApprovalAwaitingDetailNumber int
	prApprovalWarnings             []string
	prQuestionInput                textinput.Model
	prChatQuestion                 string
	prChatAnswer                   string
	tickets                        []Ticket
	visibleTickets                 []Ticket
	ticketBoard                    JiraBoard
	selectedTicketKey              string
	selectedTicketIndex            int
	ticketDetail                   *Ticket
	ticketDetailLoading            bool
	ticketDetailRequest            int
	ticketDetailCancel             context.CancelFunc
	ticketMineOnly                 bool
	ticketSprintOnly               bool
	ticketOpenOnly                 bool
	ticketStatusFilter             string
	ticketReadMode                 bool
	ticketCacheLabel               string
	ticketWarning                  string
	selectingTicketStatus          bool
	ticketStatusOptions            []string
	ticketStatusCursor             int
	enteringTicketStatus           bool
	ticketStatusInput              textinput.Model
	commentingTicket               bool
	ticketCommentInput             textinput.Model
	selectingTicketAssignee        bool
	ticketAssigneeOptions          []JiraUser
	ticketAssigneeCursor           int
	enteringTicketAssignee         bool
	ticketAssigneeInput            textinput.Model
	selectingBoard                 bool
	boardCursor                    int
	selectingTicketFilters         bool
	ticketFilterCursor             int
	ticketFilterMineOnly           bool
	ticketFilterSprintOnly         bool
	ticketFilterOpenOnly           bool
	width                          int
	height                         int
	showAll                        bool
	showPRBody                     bool
	loading                        bool
	loadingKind                    string
	loadingStartedAt               time.Time
	statusSeq                      int
	filtering                      bool
	creatingWorktree               bool
	selectingAgent                 bool
	selectingEnterAction           bool
	selectingProfile               bool
	showingHelp                    bool
	agentCursor                    int
	enterActionCursor              int
	profileCursor                  int
	focusTable                     bool
	statusMessage                  string
	statusError                    bool
	deleteArmedPath                string
	deleteArmedName                string
	deleteArmedAt                  time.Time
	pendingDeletes                 map[string]pendingDelete
	pendingCreations               map[string]pendingCreation
	pendingWorktreePath            string
	selectedPath                   string
	selectedIndex                  int
	filterQuery                    string
	filterPrevQuery                string
	ready                          bool
}

func NewModel(cfg Config, services core.Services) model {
	columns := []table.Column{{Title: "Worktree", Width: 20}, {Title: "Branch", Width: 28}, {Title: "State", Width: 10}, {Title: "Delta", Width: 10}, {Title: "PR", Width: 14}, {Title: "Last Commit", Width: 40}}
	tbl := table.New(table.WithColumns(columns), table.WithRows(nil), table.WithFocused(true), table.WithHeight(12))
	tbl.KeyMap.LineUp.SetEnabled(false)
	tbl.KeyMap.LineDown.SetEnabled(false)
	tbl.KeyMap.PageUp.SetEnabled(false)
	tbl.KeyMap.PageDown.SetEnabled(false)
	tbl.KeyMap.HalfPageUp.SetEnabled(false)
	tbl.KeyMap.HalfPageDown.SetEnabled(false)
	tbl.KeyMap.GotoTop.SetEnabled(false)
	tbl.KeyMap.GotoBottom.SetEnabled(false)
	tbl.SetStyles(tableStyles())

	vp := viewport.New(0, 0)
	vp.SetContent("Loading worktrees...")

	filterInput := textinput.New()
	filterInput.Prompt = "/ "
	filterInput.Placeholder = "filter worktree or branch"
	filterInput.CharLimit = 120
	filterInput.Width = 36

	paletteInput := textinput.New()
	paletteInput.Prompt = "› "
	paletteInput.Placeholder = "Search commands, settings, worktrees…"
	paletteInput.CharLimit = 180
	paletteInput.Width = 64
	paletteInput.PromptStyle = infoStyle.Bold(true)
	paletteInput.TextStyle = titleStyle

	editorNameInput := textinput.New()
	editorNameInput.Prompt = "name: "
	editorNameInput.Placeholder = "My IDE"
	editorNameInput.CharLimit = 80
	editorNameInput.Width = 48
	editorCommandInput := textinput.New()
	editorCommandInput.Prompt = "command: "
	editorCommandInput.Placeholder = "idea, cursor, or a custom command"
	editorCommandInput.CharLimit = 500
	editorCommandInput.Width = 48

	newInput := textinput.New()
	newInput.Prompt = "branch/worktree: "
	newInput.Placeholder = "feature/my-worktree"
	newInput.CharLimit = 180
	newInput.Width = 48

	prQuestionInput := textinput.New()
	prQuestionInput.Prompt = "ask: "
	prQuestionInput.Placeholder = "what should I review first?"
	prQuestionInput.CharLimit = 280
	prQuestionInput.Width = 64

	ticketStatusInput := textinput.New()
	ticketStatusInput.Prompt = "status: "
	ticketStatusInput.Placeholder = "In Progress"
	ticketStatusInput.CharLimit = 80
	ticketStatusInput.Width = 40

	ticketCommentInput := textinput.New()
	ticketCommentInput.Prompt = "comment: "
	ticketCommentInput.Placeholder = "add a comment to this ticket"
	ticketCommentInput.CharLimit = 1000
	ticketCommentInput.Width = 64

	ticketAssigneeInput := textinput.New()
	ticketAssigneeInput.Prompt = "assignee: "
	ticketAssigneeInput.Placeholder = "@me, email, or account ID"
	ticketAssigneeInput.CharLimit = 180
	ticketAssigneeInput.Width = 48

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = spinnerWorktreeStyle

	h := help.New()
	h.ShowAll = false

	m := model{
		services:            services,
		configSaveState:     &configSaveState{},
		cfg:                 cfg,
		keys:                defaultKeys(),
		help:                h,
		table:               tbl,
		detail:              vp,
		filterInput:         filterInput,
		paletteInput:        paletteInput,
		editorNameInput:     editorNameInput,
		editorCommandInput:  editorCommandInput,
		newInput:            newInput,
		prQuestionInput:     prQuestionInput,
		ticketStatusInput:   ticketStatusInput,
		ticketCommentInput:  ticketCommentInput,
		ticketAssigneeInput: ticketAssigneeInput,
		prStateFilter:       "open",
		ticketMineOnly:      true,
		ticketSprintOnly:    true,
		ticketOpenOnly:      true,
		spinner:             spin,
		loading:             true,
		loadingKind:         loadingWorktrees,
		loadingStartedAt:    time.Now(),
		focusTable:          true,
		statusMessage:       "Loading worktrees...",
	}
	if jira := cfg.ActiveProfile.Jira; jira != nil {
		if jira.FiltersConfigured {
			m.ticketMineOnly = jira.MineOnly
			m.ticketSprintOnly = jira.SprintOnly
			m.ticketOpenOnly = jira.OpenOnly
		}
		if board := jira.ActiveBoard(); board != nil {
			m.ticketBoard = *board
		}
	}
	if cfg.SetupNeeded {
		setup := newSetupModel(cfg, true, services)
		m.setup = &setup
		m.loading = false
		m.statusMessage = cfg.SetupReason
	}
	return m
}

func (m model) isWorktreeMode() bool { return m.mode == modeWorktrees }
func (m model) isPRMode() bool       { return m.mode == modePRs }
func (m model) isTicketMode() bool   { return m.mode == modeTickets }

func (m model) isBusy() bool {
	return m.loading || m.prDetailLoading || m.ticketDetailLoading
}

func (m *model) applySpinnerForKind(kind string) {
	m.loadingKind = kind
	m.loadingStartedAt = time.Now()
	switch kind {
	case loadingAgent:
		m.spinner.Spinner = spinner.Points
		m.spinner.Style = spinnerAgentStyle
	case loadingMutation:
		m.spinner.Spinner = spinner.Line
		m.spinner.Style = spinnerMutationStyle
	case loadingPRs, loadingDetail:
		m.spinner.Spinner = spinner.MiniDot
		m.spinner.Style = spinnerPRStyle
	default:
		m.spinner.Spinner = spinner.MiniDot
		m.spinner.Style = spinnerWorktreeStyle
	}
}

// startLoading flips the model into a busy state with a spinner styled for the
// kind of operation, and returns the tick command that drives the animation.
func (m *model) startLoading(kind, message string) tea.Cmd {
	m.loading = true
	m.statusMessage = message
	m.statusError = false
	m.applySpinnerForKind(kind)
	return m.spinner.Tick
}

// setTransientStatus shows a low-value confirmation that clears on its own
// after statusClearAfter, unless a newer status replaced it in the meantime.
func (m *model) setTransientStatus(message string) tea.Cmd {
	m.statusMessage = message
	m.statusError = false
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(statusClearAfter, func(time.Time) tea.Msg { return statusExpired{Seq: seq} })
}

func (m model) Init() tea.Cmd {
	if m.setup != nil {
		return nil
	}
	return tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	if updated, ok := next.(model); ok && updated.statusMessage != "" && (updated.statusMessage != m.statusMessage || updated.statusError != m.statusError) {
		if logger, ok := m.services.(interface{ RecordSessionEvent(string, bool) }); ok {
			logger.RecordSessionEvent(updated.statusMessage, updated.statusError)
		}
	}
	return next, cmd
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ready = true
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		m.updateDetailContent()
		if m.logsOpen {
			m.resizeSessionLogs()
		}
		return m, nil
	case sessionLogsResult:
		if !m.logsOpen || msg.Request != m.logsRequest {
			return m, nil
		}
		m.logsPath, m.logsContent = msg.Path, msg.Content
		if msg.Err != nil {
			m.logsContent = "Could not read session logs:\n" + msg.Err.Error()
		} else if strings.TrimSpace(m.logsContent) == "" {
			m.logsContent = "No commands have been executed in this session yet."
		}
		m.resizeSessionLogs()
		m.logsViewport.GotoBottom()
		return m, nil
	case paletteWorktreesResult:
		if !m.paletteOpen || msg.Request != m.paletteRequest || msg.RepositoryPath != m.cfg.ActiveProfile.RepositoryPath {
			return m, nil
		}
		if msg.Err == nil {
			m.paletteWorktrees = msg.Worktrees
			m.paletteCursor = min(m.paletteCursor, max(0, len(m.paletteMatches())-1))
		}
		return m, nil
	case spinner.TickMsg:
		if !m.isBusy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case loadResult:
		if msg.RepositoryPath != "" && (msg.RepositoryPath != m.cfg.ActiveProfile.RepositoryPath || msg.ShowAll != m.showAll) {
			return m, nil
		}
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("Refresh failed: %v", msg.Err)
			m.statusError = true
			return m, nil
		}

		m.rememberSelection()
		if m.pendingWorktreePath != "" {
			m.selectedPath = m.pendingWorktreePath
			m.selectedIndex = 0
		}
		m.allWorktrees = msg.Worktrees
		var unconfirmedDeletes []pendingDelete
		for path, deletion := range m.pendingDeletes {
			if deletion.Completed {
				if worktreeExists(msg.Worktrees, path) {
					unconfirmedDeletes = append(unconfirmedDeletes, deletion)
					m.selectedPath = deletion.Worktree.Path
					m.selectedIndex = deletion.Index
				}
				delete(m.pendingDeletes, path)
				continue
			}
			m.allWorktrees = withoutWorktree(m.allWorktrees, path)
		}
		var unconfirmedCreations []pendingCreation
		for path, creation := range m.pendingCreations {
			if worktreeExists(msg.Worktrees, path) {
				delete(m.pendingCreations, path)
				continue
			}
			if creation.Completed {
				unconfirmedCreations = append(unconfirmedCreations, creation)
				m.rollbackPendingCreation(path)
				continue
			}
			if !worktreeExists(m.allWorktrees, path) {
				m.allWorktrees = append(m.allWorktrees, creation.Worktree)
			}
		}
		m.statusError = false
		if msg.Warning != "" {
			m.statusMessage = msg.Warning
		} else {
			m.statusMessage = m.loadSummary()
		}
		m.applyFilter()
		m.pendingWorktreePath = ""
		if len(unconfirmedDeletes) > 0 {
			m.statusMessage = fmt.Sprintf("Delete could not be confirmed for %s; it was restored", unconfirmedDeletes[0].Worktree.Name)
			m.statusError = true
		}
		if len(unconfirmedCreations) > 0 {
			m.statusMessage = fmt.Sprintf("Create could not be confirmed for %s; it was removed", unconfirmedCreations[0].Worktree.Name)
			m.statusError = true
		}
		return m, nil
	case remotePRListResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("PR Radar failed: %v", msg.Err)
			m.statusError = true
			return m, nil
		}

		m.remotePRs = msg.PullRequests
		if msg.CurrentUser != "" {
			m.prCurrentUser = msg.CurrentUser
		}
		m.statusError = false
		m.statusMessage = fmt.Sprintf("Loaded %d remote PRs", len(m.remotePRs))
		m.applyPRFilter()
		if selected := m.selectedRemotePR(); selected != nil {
			return m.startPRDetailLoad(selected.Number)
		}
		return m, nil
	case remotePRDetailResult:
		if !m.isCurrentPRDetailResult(msg) {
			return m, nil
		}
		m.prDetailLoading = false
		m.prDetailCancel = nil
		if msg.Err != nil {
			m.prApprovalAwaitingDetailNumber = 0
			if errors.Is(msg.Err, context.Canceled) {
				m.updateDetailContent()
				return m, nil
			}
			m.statusMessage = fmt.Sprintf("PR detail failed: %v", msg.Err)
			m.statusError = true
			m.updateDetailContent()
			return m, nil
		}
		m.prDetail = &msg.PullRequest
		m.mergeRemotePRDetail(msg.PullRequest)
		if m.prApprovalAwaitingDetailNumber == msg.PullRequest.Number {
			m.prApprovalAwaitingDetailNumber = 0
			m.showPRApprovalConfirmation(msg.PullRequest)
			m.statusMessage = fmt.Sprintf("Confirm approval for PR #%d", msg.PullRequest.Number)
			m.statusError = false
			m.updateDetailContent()
			return m, nil
		}
		m.statusMessage = fmt.Sprintf("Loaded PR #%d details", msg.PullRequest.Number)
		m.statusError = false
		m.updateDetailContent()
		return m, nil
	case ticketListResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("JIRA tickets failed: %v", msg.Err)
			m.statusError = true
			return m, nil
		}
		m.tickets = msg.Tickets
		m.allWorktrees = msg.Worktrees
		m.reconcilePendingCreations()
		m.ticketCacheLabel = ""
		m.ticketWarning = msg.Warning
		if msg.Warning != "" {
			m.statusMessage = msg.Warning
		} else {
			m.statusMessage = m.ticketLoadSummary()
		}
		m.statusError = false
		m.applyTicketFilter()
		if selected := m.selectedTicket(); selected != nil {
			return m.startTicketDetailLoad(selected.Key)
		}
		return m, nil
	case ticketDetailResult:
		if !m.isCurrentTicketDetailResult(msg) {
			return m, nil
		}
		m.ticketDetailLoading = false
		m.ticketDetailCancel = nil
		if msg.Err != nil {
			if errors.Is(msg.Err, context.Canceled) {
				m.updateDetailContent()
				return m, nil
			}
			m.statusMessage = fmt.Sprintf("Ticket detail failed: %v", msg.Err)
			m.statusError = true
			m.updateDetailContent()
			return m, nil
		}
		m.ticketDetail = &msg.Ticket
		m.mergeTicketDetail(msg.Ticket)
		m.statusMessage = fmt.Sprintf("Loaded %s details", msg.Key)
		m.statusError = false
		m.updateDetailContent()
		return m, nil
	case ticketMutationResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("JIRA %s failed for %s: %v", msg.Action, msg.Key, msg.Err)
			m.statusError = true
			return m, nil
		}
		m.selectedTicketKey = msg.Key
		m.ticketDetail = nil
		m.statusMessage = msg.Message + ". Refreshing..."
		m.statusError = false
		return m, tea.Batch(m.loadTicketsCmd(true), m.startLoading(loadingTickets, m.statusMessage))
	case ticketWorktreeResult:
		m.loading = false
		if msg.Err != nil {
			m.rollbackPendingCreation(msg.Worktree.Path)
			m.statusMessage = fmt.Sprintf("Create worktree failed for %s: %v", msg.Key, msg.Err)
			m.statusError = true
			if m.hasPendingCreationInFlight() {
				m.loading = true
				return m, nil
			}
			if !m.hasCompletedPendingCreation() {
				return m, nil
			}
		}
		action := ""
		if msg.Err == nil {
			m.selectedTicketKey = msg.Key
			m.pendingWorktreePath = msg.Worktree.Path
			action = m.completePendingCreation(msg.Worktree.Path)
			if msg.Existing {
				m.statusMessage = fmt.Sprintf("Selected existing worktree %s. Refreshing tickets...", msg.Worktree.Name)
			} else {
				m.statusMessage = fmt.Sprintf("Created worktree %s. Refreshing tickets...", msg.Worktree.Name)
			}
		}
		if action != "" {
			_, cmd := m.openWorktreeWithAction(msg.Worktree, action)
			if m.hasPendingCreationInFlight() {
				m.loading = true
				return m, cmd
			}
			return m, tea.Batch(cmd, m.loadTicketsCmd(false), m.startLoading(loadingTickets, m.statusMessage))
		}
		if m.hasPendingCreationInFlight() {
			m.loading = true
			m.statusMessage += ". Waiting for remaining creations..."
			return m, nil
		}
		return m, tea.Batch(m.loadTicketsCmd(false), m.startLoading(loadingTickets, m.statusMessage))
	case askPRResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("Ask failed: %v", msg.Err)
			m.statusError = true
			return m, nil
		}
		m.prChatQuestion = msg.Question
		m.prChatAnswer = msg.Answer
		m.statusMessage = "Agent answered PR question; see Agent chat in Details"
		m.statusError = false
		m.updateDetailContent()
		return m, nil
	case approvePRResult:
		m.loading = false
		m.confirmingPRApproval = false
		m.prApprovalPendingNumber = 0
		m.prApprovalWarnings = nil
		if msg.Err != nil {
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
	case openPRWorktreeResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("Open PR worktree failed: %v", msg.Err)
			m.statusError = true
			return m, nil
		}
		m.statusMessage = msg.Message
		m.statusError = false
		return m, loadWorktreesCmd(m.services, m.cfg, m.showAll)
	case setupResult:
		if msg.Cancelled {
			m.setup = nil
			if msg.Startup {
				return m, tea.Quit
			}
			m.statusMessage = "Setup cancelled"
			m.statusError = false
			return m, nil
		}
		if msg.Err != nil {
			m.statusMessage = msg.Err.Error()
			m.statusError = true
			return m, nil
		}
		m.setup = nil
		m.cfg = msg.Config
		m.showAll = false
		m.showPRBody = false
		m.prDiffMode = false
		m.filtering = false
		m.creatingWorktree = false
		m.selectingAgent = false
		m.selectingEnterAction = false
		m.selectingProfile = false
		m.confirmingPRApproval = false
		m.prApprovalPendingNumber = 0
		m.prApprovalAwaitingDetailNumber = 0
		m.prApprovalWarnings = nil
		m.mode = modeWorktrees
		m.cancelTicketDetailLoad()
		m.tickets = nil
		m.visibleTickets = nil
		m.ticketDetail = nil
		m.ticketReadMode = false
		m.ticketBoard = JiraBoard{}
		m.ticketMineOnly = true
		m.ticketSprintOnly = true
		m.ticketOpenOnly = true
		if jira := m.cfg.ActiveProfile.Jira; jira != nil {
			if jira.FiltersConfigured {
				m.ticketMineOnly = jira.MineOnly
				m.ticketSprintOnly = jira.SprintOnly
				m.ticketOpenOnly = jira.OpenOnly
			}
			if board := jira.ActiveBoard(); board != nil {
				m.ticketBoard = *board
			}
		}
		m.filterQuery = ""
		m.filterInput.SetValue("")
		m.selectedPath = ""
		m.selectedIndex = 0
		m.clearDeleteArm()
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, msg.Message+". Loading worktrees..."))
	case deleteResult:
		if msg.Err != nil {
			m.restorePendingDelete(msg.Path)
			m.statusMessage = fmt.Sprintf("Delete failed for %s: %v", msg.Name, msg.Err)
			m.statusError = true
		} else {
			m.completePendingDelete(msg.Path)
			m.statusMessage = fmt.Sprintf("Deleted %s", msg.Name)
			m.statusError = false
		}
		m.clearDeleteArm()
		if m.hasPendingDeleteInFlight() {
			m.loading = true
			if msg.Err == nil {
				m.statusMessage += ". Waiting for remaining deletions..."
			}
			return m, nil
		}
		if msg.Err != nil && !m.hasCompletedPendingDelete() {
			m.loading = false
			return m, nil
		}
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, m.statusMessage+". Refreshing..."))
	case createResult:
		m.loading = false
		if msg.Err != nil {
			m.rollbackPendingCreation(msg.Path)
			m.statusMessage = fmt.Sprintf("Create failed for %s: %v", msg.Name, msg.Err)
			m.statusError = true
			if m.hasPendingCreationInFlight() {
				m.loading = true
				return m, nil
			}
			if !m.hasCompletedPendingCreation() {
				return m, nil
			}
		}

		action := ""
		message := m.statusMessage
		if msg.Err == nil {
			m.selectedPath = msg.Path
			action = m.completePendingCreation(msg.Path)
			message = fmt.Sprintf("Created %s. Refreshing...", msg.Name)
			if msg.Existing {
				message = fmt.Sprintf("Selected existing worktree %s. Refreshing...", msg.Name)
			}
		}
		if action != "" {
			if pending, ok := m.pendingCreations[msg.Path]; ok {
				_, cmd := m.openWorktreeWithAction(pending.Worktree, action)
				if m.hasPendingCreationInFlight() {
					m.loading = true
					return m, cmd
				}
				return m, tea.Batch(cmd, loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, message))
			}
		}
		if m.hasPendingCreationInFlight() {
			m.loading = true
			m.statusMessage = message + ". Waiting for remaining creations..."
			return m, nil
		}
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, message))
	case deleteArmTick:
		if m.deleteArmedPath != msg.Path || !m.deleteArmedAt.Equal(msg.ArmedAt) {
			return m, nil
		}
		if time.Since(m.deleteArmedAt) > deleteConfirmWindow {
			m.clearDeleteArm()
			return m, m.setTransientStatus("Delete confirmation expired")
		}
		return m, deleteArmTickCmd(msg.Path, msg.ArmedAt)
	case statusExpired:
		if msg.Seq == m.statusSeq && !m.isBusy() && !m.statusError {
			m.statusMessage = ""
		}
		return m, nil
	case actionResult:
		if msg.Err != nil {
			m.statusMessage = msg.Err.Error()
			m.statusError = true
			return m, nil
		}
		return m, m.setTransientStatus(msg.Message)
	case tea.KeyMsg:
		if m.paletteOpen {
			return m.updateCommandPalette(msg)
		}
		if m.setup != nil {
			setup := *m.setup
			var cmd tea.Cmd
			setup, cmd = setup.Update(msg)
			m.setup = &setup
			return m, cmd
		}
		if m.editingEditor {
			return m.updateEditorInput(msg)
		}
		if m.selectingEditor {
			return m.updateEditorSelector(msg)
		}
		if key.Matches(msg, m.keys.Palette) && !m.hasTextOrConfirmationModal() {
			return m.openCommandPalette()
		}
		if m.logsOpen {
			return m.updateSessionLogs(msg)
		}

		if m.selectingAgent {
			return m.updateAgentSelector(msg)
		}

		if m.selectingEnterAction {
			return m.updateEnterActionSelector(msg)
		}

		if m.selectingProfile {
			return m.updateProfileSelector(msg)
		}

		if m.showingHelp {
			return m.updateHelpPage(msg)
		}

		if m.selectingPRUser {
			return m.updatePRUserSelector(msg)
		}

		if m.askingPR {
			return m.updatePRQuestion(msg)
		}

		if m.confirmingPRApproval {
			return m.updatePRApprovalConfirmation(msg)
		}

		if m.selectingTicketFilters {
			return m.updateTicketFilters(msg)
		}

		if m.selectingTicketStatus {
			return m.updateTicketStatusSelector(msg)
		}

		if m.enteringTicketStatus {
			return m.updateTicketStatusInput(msg)
		}

		if m.commentingTicket {
			return m.updateTicketComment(msg)
		}

		if m.selectingTicketAssignee {
			return m.updateTicketAssigneeSelector(msg)
		}

		if m.enteringTicketAssignee {
			return m.updateTicketAssigneeInput(msg)
		}

		if m.selectingBoard {
			return m.updateBoardSelector(msg)
		}

		if m.isPRMode() && m.prDiffMode {
			return m.updatePRDiffMode(msg)
		}

		if m.isTicketMode() && m.ticketReadMode {
			return m.updateTicketReadMode(msg)
		}

		if m.creatingWorktree {
			return m.updateNewWorktree(msg)
		}

		if m.filtering {
			return m.updateFilter(msg)
		}

		if msg.String() == "esc" {
			if m.deleteArmedPath != "" {
				m.clearDeleteArm()
				return m, m.setTransientStatus("Delete cancelled")
			}
			if m.filterQuery != "" {
				switch {
				case m.isTicketMode():
					m.rememberTicketSelection()
				case m.isPRMode():
					m.rememberPRSelection()
				default:
					m.rememberSelection()
				}
				m.filterQuery = ""
				m.filterInput.SetValue("")
				m.applyFilter()
				return m, m.setTransientStatus("Filter cleared")
			}
		}

		if key.Matches(msg, m.keys.Filter) {
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

		if key.Matches(msg, m.keys.HelpPage) {
			m.showingHelp = true
			m.statusMessage = "Help"
			m.statusError = false
			return m, nil
		}

		if m.focusTable {
			if key.Matches(msg, m.keys.Up) {
				m.clearDeleteArm()
				if m.isPRMode() {
					return m.movePRSelectionAndLoad(-1)
				}
				if m.isTicketMode() {
					return m.moveTicketSelectionAndLoad(-1)
				}
				m.moveSelection(-1)
				return m, nil
			}
			if key.Matches(msg, m.keys.Down) {
				m.clearDeleteArm()
				if m.isPRMode() {
					return m.movePRSelectionAndLoad(1)
				}
				if m.isTicketMode() {
					return m.moveTicketSelectionAndLoad(1)
				}
				m.moveSelection(1)
				return m, nil
			}
		}

		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Focus) {
			m.clearDeleteArm()
			m.focusTable = !m.focusTable
			m.table.Focus()
			if !m.focusTable {
				m.table.Blur()
			}
			return m, m.setTransientStatus(map[bool]string{true: "Table focused", false: "Details focused"}[m.focusTable])
		}
		if key.Matches(msg, m.keys.Refresh) {
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
				return m, tea.Batch(loadRemotePullRequestsCmd(m.services, m.cfg), m.startLoading(loadingPRs, "Refreshing remote PRs..."))
			}
			m.rememberSelection()
			return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, "Refreshing worktrees..."))
		}
		if key.Matches(msg, m.keys.PRRadar) {
			return m.switchMode(m.toggleMode(modePRs))
		}
		if key.Matches(msg, m.keys.TicketMode) {
			return m.switchMode(m.toggleMode(modeTickets))
		}
		if m.isTicketMode() {
			if next, cmd, handled := m.updateTicketMode(msg); handled {
				return next, cmd
			}
		}
		if m.isPRMode() && key.Matches(msg, m.keys.StateFilter) {
			m.cyclePRStateFilter()
			m.applyPRFilter()
			m.statusMessage = m.prFilterSummary()
			m.statusError = false
			return m, nil
		}
		if m.isPRMode() && key.Matches(msg, m.keys.UserFilter) {
			m.prUserOptions = append([]string{"all"}, m.services.RemotePRUsers(m.remotePRs)...)
			m.prUserCursor = m.currentPRUserIndex()
			m.selectingPRUser = true
			m.statusMessage = "Filter PRs by user"
			m.statusError = false
			return m, nil
		}
		if m.isPRMode() && key.Matches(msg, m.keys.MineFilter) {
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
			return m, nil
		}
		if m.isPRMode() && key.Matches(msg, m.keys.BranchView) {
			m.prShowBranch = !m.prShowBranch
			m.rebuildRows()
			m.statusMessage = map[bool]string{true: "Showing PR branches", false: "Showing PR titles"}[m.prShowBranch]
			m.statusError = false
			return m, nil
		}
		if m.isPRMode() && key.Matches(msg, m.keys.FailedGHA) {
			m.prShowFailedGHA = !m.prShowFailedGHA
			m.updateDetailContent()
			m.statusMessage = map[bool]string{true: "Showing failed GitHub Actions in Details", false: "Hiding failed GitHub Actions"}[m.prShowFailedGHA]
			m.statusError = false
			return m, nil
		}
		if m.isPRMode() && key.Matches(msg, m.keys.AskPR) {
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
			return m, nil
		}
		if m.isPRMode() && key.Matches(msg, m.keys.PRDiff) {
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
			if m.prDiffMode && (m.prDetail == nil || m.prDetail.Number != selected.Number) && !m.prDetailLoading {
				return m.startPRDetailLoad(selected.Number)
			}
			if m.prDiffMode && m.prDetailLoading {
				m.statusMessage = fmt.Sprintf("Loading PR #%d diff...", selected.Number)
			}
			return m, nil
		}
		if m.isTicketMode() && key.Matches(msg, m.keys.PRDiff) {
			selected := m.selectedTicketForDetail()
			if selected == nil {
				m.statusMessage = "Select a JIRA ticket first"
				m.statusError = true
				return m, nil
			}
			m.ticketReadMode = true
			m.resize()
			if !selected.Detail && !m.ticketDetailLoading {
				return m.startTicketDetailLoad(selected.Key)
			}
			return m, nil
		}
		if m.isWorktreeMode() && key.Matches(msg, m.keys.PRDiff) {
			return m.openSelectedWorktreeWithAction(m.cfg.App.DefaultEnterAction)
		}
		if m.isPRMode() && key.Matches(msg, m.keys.ApprovePR) {
			return m.beginPRApproval()
		}
		if key.Matches(msg, m.keys.EnterAction) {
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
		if key.Matches(msg, m.keys.ToggleAll) {
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
		if key.Matches(msg, m.keys.ToggleBody) {
			if m.isTicketMode() {
				m.statusMessage = "PR body toggle is unavailable in JIRA tickets"
				m.statusError = true
				return m, nil
			}
			m.showPRBody = !m.showPRBody
			m.updateDetailContent()
			return m, nil
		}
		if key.Matches(msg, m.keys.OpenPR) {
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
		if key.Matches(msg, m.keys.OpenCode) {
			if m.isTicketMode() {
				selected := m.selectedTicket()
				if selected == nil {
					return m, nil
				}
				if selected.Worktree == nil || selected.Worktree.Missing || selected.Worktree.Prunable {
					return m.startTicketWorktreeCreation(*selected, core.EnterActionVSCode)
				}
				return m.openWorktreeWithAction(*selected.Worktree, core.EnterActionVSCode)
			}
			if m.isPRMode() {
				selected := m.selectedRemotePRForDetail()
				if selected == nil {
					m.statusMessage = "Select a remote PR first"
					m.statusError = true
					return m, nil
				}
				if m.deferPendingWorktreeAction(m.services.CreateWorktreePath(m.cfg, selected.HeadRefName), core.EnterActionVSCode) {
					return m, nil
				}
				return m, tea.Batch(openPRWorktreeInEditorCmd(m.services, m.services, m.cfg, *selected, m.configuredEditor()), m.startLoading(loadingMutation, fmt.Sprintf("Preparing PR #%d worktree for %s...", selected.Number, m.configuredEditor().Name)))
			}
			selected := m.selectedWorktree()
			if selected == nil {
				return m, nil
			}
			return m.openWorktreeWithAction(*selected, core.EnterActionVSCode)
		}
		if key.Matches(msg, m.keys.OpenAgent) {
			if m.isTicketMode() {
				selected := m.selectedTicket()
				if selected == nil {
					return m, nil
				}
				if selected.Worktree == nil || selected.Worktree.Missing || selected.Worktree.Prunable {
					return m.startTicketWorktreeCreation(*selected, core.EnterActionAgent)
				}
				return m.openWorktreeWithAction(*selected.Worktree, core.EnterActionAgent)
			}
			if m.isPRMode() {
				selected := m.selectedRemotePRForDetail()
				if selected == nil {
					m.statusMessage = "Select a remote PR first"
					m.statusError = true
					return m, nil
				}
				if m.deferPendingWorktreeAction(m.services.CreateWorktreePath(m.cfg, selected.HeadRefName), core.EnterActionAgent) {
					return m, nil
				}
				agent := m.defaultAgent()
				if agent == nil {
					m.statusMessage = "No AI agent configured"
					m.statusError = true
					return m, nil
				}
				return m, tea.Batch(openPRWorktreeAgentCmd(m.services, m.services, m.cfg, *selected, *agent), m.startLoading(loadingMutation, fmt.Sprintf("Preparing PR #%d worktree for %s...", selected.Number, agent.Name)))
			}
			selected := m.selectedWorktree()
			if selected == nil {
				return m, nil
			}
			return m.openWorktreeWithAction(*selected, core.EnterActionAgent)
		}
		if key.Matches(msg, m.keys.AgentMenu) {
			m.clearDeleteArm()
			m.selectingAgent = true
			m.agentCursor = m.defaultAgentIndex()
			m.statusMessage = "Select default AI agent"
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.ProfileMenu) {
			m.clearDeleteArm()
			m.selectingProfile = true
			m.profileCursor = m.defaultProfileIndex()
			m.statusMessage = "Select repository profile"
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.Setup) {
			m.clearDeleteArm()
			setupCfg := m.cfg
			if currentRepo, ok := m.services.InferRepoFromCWD(); ok {
				setupCfg.SetupRepo = currentRepo
			} else {
				setupCfg.SetupRepo = m.cfg.ActiveProfile.RepositoryPath
			}
			setupCfg.SetupReason = "Add or update a repository profile"
			setup := newSetupModel(setupCfg, false, m.services)
			m.setup = &setup
			m.loading = false
			m.statusMessage = "Setup"
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.New) {
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
				m.beginWorktreeCreation(Worktree{
					Name:    branch,
					Branch:  branch,
					Path:    m.services.CreateWorktreePath(m.cfg, branch),
					Managed: true,
				}, "")
				return m, tea.Batch(createWorktreeCmd(m.services, m.cfg, branch), m.startLoading(loadingMutation, fmt.Sprintf("Creating worktree for PR #%d (%s)...", selected.Number, branch)))
			}
			m.creatingWorktree = true
			m.newInput.SetValue("")
			m.newInput.Focus()
			m.statusMessage = "Enter branch/worktree name"
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.Delete) {
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
				return m, tea.Batch(removeWorktreeCmd(m.services, m.cfg, wt), m.startLoading(loadingMutation, fmt.Sprintf("Deleting %s...", wt.Name)))
			}

			m.deleteArmedPath = selected.Path
			m.deleteArmedName = selected.Name
			m.deleteArmedAt = now
			m.statusMessage = fmt.Sprintf("Press d again to delete %s", selected.Name)
			m.statusError = false
			return m, deleteArmTickCmd(selected.Path, now)
		}
	}

	if m.paletteOpen {
		var cmd tea.Cmd
		m.paletteInput, cmd = m.paletteInput.Update(msg)
		return m, cmd
	}
	if m.editingEditor {
		return m.updateEditorInputMessage(msg)
	}
	if m.focusTable {
		before := m.table.Cursor()
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		if m.table.Cursor() != before {
			m.clearDeleteArm()
			if m.isPRMode() {
				m.movePRSelection(0)
				selected := m.selectedRemotePR()
				if selected != nil {
					return m.startPRDetailLoad(selected.Number)
				}
			} else if m.isTicketMode() {
				m.moveTicketSelection(0)
				selected := m.selectedTicket()
				if selected != nil {
					return m.startTicketDetailLoad(selected.Key)
				}
			} else {
				m.moveSelection(0)
			}
		}
		return m, cmd
	}

	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

func (m model) updateNewWorktree(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.creatingWorktree = false
		m.newInput.Blur()
		return m, m.setTransientStatus("New worktree cancelled")
	case "enter":
		name := strings.TrimSpace(m.newInput.Value())
		if name == "" {
			m.statusMessage = "Branch/worktree name is required"
			m.statusError = true
			return m, nil
		}

		m.creatingWorktree = false
		m.newInput.Blur()
		m.beginWorktreeCreation(Worktree{
			Name:    name,
			Branch:  name,
			Path:    m.services.CreateWorktreePath(m.cfg, name),
			Managed: true,
		}, "")
		return m, tea.Batch(createWorktreeCmd(m.services, m.cfg, name), m.startLoading(loadingMutation, fmt.Sprintf("Creating %s...", name)))
	}

	var cmd tea.Cmd
	m.newInput, cmd = m.newInput.Update(msg)
	return m, cmd
}

func (m model) updatePRQuestion(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.askingPR = false
		m.prQuestionInput.Blur()
		return m, m.setTransientStatus("PR question cancelled")
	case "enter":
		question := strings.TrimSpace(m.prQuestionInput.Value())
		if question == "" {
			m.statusMessage = "Question is required"
			m.statusError = true
			return m, nil
		}
		selected := m.selectedRemotePRForDetail()
		if selected == nil {
			m.statusMessage = "Select a remote PR first"
			m.statusError = true
			return m, nil
		}
		agent := m.copilotAgent()
		if agent == nil {
			m.statusMessage = "No copilot/default agent configured"
			m.statusError = true
			return m, nil
		}
		m.askingPR = false
		m.prQuestionInput.Blur()
		return m, tea.Batch(askPullRequestAgentCmd(m.services, m.cfg, *agent, *selected, question), m.startLoading(loadingAgent, fmt.Sprintf("Asking %s about PR #%d...", agent.Name, selected.Number)))
	}

	var cmd tea.Cmd
	m.prQuestionInput, cmd = m.prQuestionInput.Update(msg)
	return m, cmd
}

func (m model) updatePRApprovalConfirmation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.confirmingPRApproval = false
		m.prApprovalPendingNumber = 0
		m.prApprovalWarnings = nil
		return m, m.setTransientStatus("PR approval cancelled")
	case "enter", "A":
		number := m.prApprovalPendingNumber
		if number <= 0 {
			m.confirmingPRApproval = false
			m.statusMessage = "No PR approval is pending"
			m.statusError = true
			return m, nil
		}
		m.confirmingPRApproval = false
		return m, tea.Batch(approveRemotePullRequestCmd(m.services, m.cfg, number), m.startLoading(loadingMutation, fmt.Sprintf("Approving PR #%d...", number)))
	}
	return m, nil
}

func (m model) updatePRDiffMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Refresh):
		m.rememberPRSelection()
		m.cancelPRDetailLoad()
		m.confirmingPRApproval = false
		m.prApprovalPendingNumber = 0
		m.prApprovalAwaitingDetailNumber = 0
		m.prApprovalWarnings = nil
		return m, tea.Batch(loadRemotePullRequestsCmd(m.services, m.cfg), m.startLoading(loadingPRs, "Refreshing remote PRs..."))
	case key.Matches(msg, m.keys.ApprovePR):
		return m.beginPRApproval()
	case key.Matches(msg, m.keys.OpenPR):
		selected := m.selectedRemotePRForDetail()
		if selected == nil {
			m.statusMessage = "Selected row has no PR"
			m.statusError = true
			return m, nil
		}
		return m, openBrowserCmd(m.services, selected.URL)
	}

	switch msg.String() {
	case "esc":
		m.prDiffMode = false
		m.resize()
		return m, m.setTransientStatus("Closed PR diff")
	case "enter":
		return m, m.setTransientStatus("Press esc to close PR diff")
	}

	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

func (m model) updatePRUserSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingPRUser = false
		return m, m.setTransientStatus("PR user filter cancelled")
	case "up", "k":
		if m.prUserCursor > 0 {
			m.prUserCursor--
		}
		return m, nil
	case "down", "j":
		if m.prUserCursor < len(m.prUserOptions)-1 {
			m.prUserCursor++
		}
		return m, nil
	case "enter":
		m.selectingPRUser = false
		if len(m.prUserOptions) == 0 || m.prUserCursor <= 0 {
			m.prUserFilter = ""
		} else {
			m.prUserFilter = m.prUserOptions[min(max(m.prUserCursor, 0), len(m.prUserOptions)-1)]
		}
		m.applyPRFilter()
		m.statusMessage = m.prFilterSummary()
		m.statusError = false
		return m, nil
	}
	return m, nil
}

func (m model) updateHelpPage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "h", "enter":
		m.showingHelp = false
		return m, m.setTransientStatus("Help closed")
	}
	return m, nil
}

func (m model) updateEnterActionSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingEnterAction = false
		return m, m.setTransientStatus("Enter action selection cancelled")
	case "up", "k":
		if m.enterActionCursor > 0 {
			m.enterActionCursor--
		}
		return m, nil
	case "down", "j":
		if m.enterActionCursor < len(enterActionOptions)-1 {
			m.enterActionCursor++
		}
		return m, nil
	case "enter":
		m.enterActionCursor = min(max(m.enterActionCursor, 0), len(enterActionOptions)-1)
		option := enterActionOptions[m.enterActionCursor]
		m.cfg.App.DefaultEnterAction = option.Action
		m.selectingEnterAction = false
		m.statusMessage = fmt.Sprintf("Saving default Enter action %s...", option.Label)
		m.statusError = false
		return m, m.persistConfig("Default Enter action set to " + m.configuredEnterActionLabel())
	}

	return m, nil
}

func (m model) updateAgentSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingAgent = false
		return m, m.setTransientStatus("AI agent selection cancelled")
	case "up", "k":
		if len(m.cfg.App.Agents) > 0 && m.agentCursor > 0 {
			m.agentCursor--
		}
		return m, nil
	case "down", "j":
		if len(m.cfg.App.Agents) > 0 && m.agentCursor < len(m.cfg.App.Agents)-1 {
			m.agentCursor++
		}
		return m, nil
	case "enter":
		if len(m.cfg.App.Agents) == 0 {
			m.statusMessage = "No AI agents configured"
			m.statusError = true
			return m, nil
		}

		m.agentCursor = min(max(m.agentCursor, 0), len(m.cfg.App.Agents)-1)
		m.cfg.App.DefaultAgent = m.cfg.App.Agents[m.agentCursor].Name
		m.selectingAgent = false
		m.statusMessage = fmt.Sprintf("Saving default AI agent %s...", m.cfg.App.DefaultAgent)
		m.statusError = false
		return m, m.persistConfig("Default AI agent set to " + m.cfg.App.DefaultAgent)
	}

	return m, nil
}

func (m model) updateProfileSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingProfile = false
		return m, m.setTransientStatus("Repository profile selection cancelled")
	case "up", "k":
		if len(m.cfg.App.Profiles) > 0 && m.profileCursor > 0 {
			m.profileCursor--
		}
		return m, nil
	case "down", "j":
		if len(m.cfg.App.Profiles) > 0 && m.profileCursor < len(m.cfg.App.Profiles)-1 {
			m.profileCursor++
		}
		return m, nil
	case "enter":
		if len(m.cfg.App.Profiles) == 0 {
			m.statusMessage = "No repository profiles configured"
			m.statusError = true
			return m, nil
		}

		m.profileCursor = min(max(m.profileCursor, 0), len(m.cfg.App.Profiles)-1)
		profile := m.cfg.App.Profiles[m.profileCursor]
		resolved := m.services.ResolveProfilePaths(profile, m.cfg.HomeDir)
		m.cfg.App.DefaultProfile = profile.Name
		m.cfg.ActiveProfile = resolved
		m.allWorktrees = nil
		m.visibleWorktrees = nil
		m.paletteWorktrees = nil
		m.pendingWorktreePath = ""
		m.table.SetRows(nil)
		m.cfg.RepoSlug = resolved.GitHubRepo
		if m.cfg.RepoSlug == "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			m.cfg.RepoSlug = m.services.InferGitHubRepo(ctx, resolved)
			cancel()
		}
		m.selectingProfile = false
		m.showAll = false
		m.showPRBody = false
		m.prDiffMode = false
		m.filtering = false
		m.creatingWorktree = false
		m.selectingEnterAction = false
		m.confirmingPRApproval = false
		m.prApprovalPendingNumber = 0
		m.prApprovalAwaitingDetailNumber = 0
		m.prApprovalWarnings = nil
		m.mode = modeWorktrees
		m.cancelTicketDetailLoad()
		m.tickets = nil
		m.visibleTickets = nil
		m.ticketDetail = nil
		m.ticketReadMode = false
		m.ticketBoard = JiraBoard{}
		m.ticketMineOnly = true
		m.ticketSprintOnly = true
		m.ticketOpenOnly = true
		if jira := resolved.Jira; jira != nil {
			if jira.FiltersConfigured {
				m.ticketMineOnly = jira.MineOnly
				m.ticketSprintOnly = jira.SprintOnly
				m.ticketOpenOnly = jira.OpenOnly
			}
			if board := jira.ActiveBoard(); board != nil {
				m.ticketBoard = *board
			}
		}
		m.filterQuery = ""
		m.filterInput.SetValue("")
		m.selectedPath = ""
		m.selectedIndex = 0
		m.clearDeleteArm()
		return m, tea.Batch(m.persistConfig("Saved config "+m.cfg.ConfigPath), loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, fmt.Sprintf("Switching to %s...", profile.Name)))
	}

	return m, nil
}

func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filterInput.Blur()
		if m.filterQuery != m.filterPrevQuery {
			switch {
			case m.isTicketMode():
				m.rememberTicketSelection()
			case m.isPRMode():
				m.rememberPRSelection()
			default:
				m.rememberSelection()
			}
			m.filterQuery = m.filterPrevQuery
			m.filterInput.SetValue(m.filterPrevQuery)
			m.applyFilter()
		}
		return m, m.setTransientStatus("Filter cancelled")
	case "enter":
		m.filtering = false
		m.filterInput.Blur()
		m.statusMessage = m.filterSummary()
		m.statusError = false
		return m, nil
	}

	switch {
	case m.isTicketMode():
		m.rememberTicketSelection()
	case m.isPRMode():
		m.rememberPRSelection()
	default:
		m.rememberSelection()
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filterQuery = strings.TrimSpace(m.filterInput.Value())
	m.applyFilter()
	m.statusMessage = m.filterSummary()
	m.statusError = false
	return m, cmd
}

func (m *model) applyFilter() {
	if m.isTicketMode() {
		m.applyTicketFilter()
		return
	}
	if m.isPRMode() {
		m.applyPRFilter()
		return
	}
	query := strings.ToLower(strings.TrimSpace(m.filterQuery))
	if query == "" {
		m.visibleWorktrees = append([]Worktree(nil), m.allWorktrees...)
	} else {
		filtered := make([]Worktree, 0, len(m.allWorktrees))
		for _, wt := range m.allWorktrees {
			if strings.Contains(strings.ToLower(wt.Name), query) || strings.Contains(strings.ToLower(wt.Branch), query) {
				filtered = append(filtered, wt)
			}
		}
		m.visibleWorktrees = filtered
	}

	m.rebuildRows()
	m.selectByPath()
	m.updateDetailContent()
}

func (m *model) applyPRFilter() {
	m.visiblePRs = m.services.FilterRemotePullRequestsWithAuthor(m.remotePRs, m.filterQuery, m.prStateFilter, m.prUserFilter, m.prAuthorFilter)
	m.rebuildRows()
	m.selectPRByNumber()
	m.updateDetailContent()
}

func (m *model) rememberSelection() {
	m.selectedIndex = m.table.Cursor()
	m.selectedPath = m.selectedWorktreePath()
}

func (m *model) rememberPRSelection() {
	m.selectedPRIndex = m.table.Cursor()
	if selected := m.selectedRemotePR(); selected != nil {
		m.selectedPRNumber = selected.Number
	}
}

func (m *model) clearDeleteArm() {
	m.deleteArmedPath = ""
	m.deleteArmedName = ""
	m.deleteArmedAt = time.Time{}
}

func (m *model) optimisticRemove(path string) {
	currentIndex := m.table.Cursor()
	var nextPath string
	if len(m.visibleWorktrees) > 1 {
		nextIndex := currentIndex - 1
		if nextIndex < 0 {
			nextIndex = currentIndex + 1
		}
		if nextIndex >= 0 && nextIndex < len(m.visibleWorktrees) {
			nextPath = m.visibleWorktrees[nextIndex].Path
		}
	}

	kept := make([]Worktree, 0, len(m.allWorktrees))
	for _, wt := range m.allWorktrees {
		if wt.Path == path {
			continue
		}
		kept = append(kept, wt)
	}
	m.allWorktrees = kept
	m.selectedPath = nextPath
	m.applyFilter()
}

func (m *model) beginWorktreeCreation(worktree Worktree, ticketKey string) {
	if m.pendingCreations == nil {
		m.pendingCreations = make(map[string]pendingCreation)
	}
	if _, ok := m.pendingCreations[worktree.Path]; ok {
		return
	}
	m.pendingCreations[worktree.Path] = pendingCreation{
		Worktree:      worktree,
		TicketKey:     ticketKey,
		PreviousPath:  m.selectedPath,
		PreviousIndex: m.selectedIndex,
	}
	if !worktreeExists(m.allWorktrees, worktree.Path) {
		m.allWorktrees = append(m.allWorktrees, worktree)
	}
	if ticketKey != "" {
		m.setTicketWorktree(ticketKey, &worktree)
	}
	m.pendingWorktreePath = worktree.Path
	m.selectedPath = worktree.Path
	m.selectedIndex = len(m.visibleWorktrees)
	if m.isWorktreeMode() {
		m.applyFilter()
	} else if m.isTicketMode() {
		m.rebuildRows()
		m.updateDetailContent()
	}
}

func (m *model) completePendingCreation(path string) string {
	creation, ok := m.pendingCreations[path]
	if !ok {
		return ""
	}
	creation.Completed = true
	m.pendingCreations[path] = creation
	m.selectedPath = path
	m.pendingWorktreePath = path
	return creation.DeferredAction
}

func (m *model) rollbackPendingCreation(path string) {
	creation, ok := m.pendingCreations[path]
	if !ok {
		return
	}
	delete(m.pendingCreations, path)
	m.allWorktrees = withoutWorktree(m.allWorktrees, path)
	if creation.TicketKey != "" {
		m.clearTicketWorktree(creation.TicketKey, path)
	}
	m.pendingWorktreePath = ""
	m.selectedPath = creation.PreviousPath
	m.selectedIndex = creation.PreviousIndex
	if m.isWorktreeMode() {
		m.applyFilter()
	} else if m.isTicketMode() {
		m.applyTicketFilter()
	}
}

func (m *model) reconcilePendingCreations() {
	for path, creation := range m.pendingCreations {
		if !worktreeExists(m.allWorktrees, path) {
			m.allWorktrees = append(m.allWorktrees, creation.Worktree)
		}
		if creation.TicketKey != "" {
			worktree := creation.Worktree
			m.setTicketWorktree(creation.TicketKey, &worktree)
		}
	}
}

func (m *model) deferPendingWorktreeAction(path string, action string) bool {
	creation, ok := m.pendingCreations[path]
	if !ok || creation.Completed {
		return false
	}
	creation.DeferredAction = action
	m.pendingCreations[path] = creation
	m.statusMessage = fmt.Sprintf("Waiting for %s to be created before opening it...", creation.Worktree.Name)
	m.statusError = false
	return true
}

func (m model) hasPendingCreationInFlight() bool {
	for _, creation := range m.pendingCreations {
		if !creation.Completed {
			return true
		}
	}
	return false
}

func (m model) hasCompletedPendingCreation() bool {
	for _, creation := range m.pendingCreations {
		if creation.Completed {
			return true
		}
	}
	return false
}

func (m *model) restorePendingDelete(path string) {
	deleted, ok := m.pendingDeletes[path]
	if !ok {
		return
	}
	delete(m.pendingDeletes, path)
	if !worktreeExists(m.allWorktrees, deleted.Worktree.Path) {
		index := min(max(deleted.Index, 0), len(m.allWorktrees))
		m.allWorktrees = append(m.allWorktrees, Worktree{})
		copy(m.allWorktrees[index+1:], m.allWorktrees[index:])
		m.allWorktrees[index] = deleted.Worktree
	}
	m.selectedPath = deleted.Worktree.Path
	m.selectedIndex = deleted.Index
	m.applyFilter()
}

func (m *model) completePendingDelete(path string) {
	deletion, ok := m.pendingDeletes[path]
	if !ok {
		return
	}
	deletion.Completed = true
	m.pendingDeletes[path] = deletion
}

func (m model) hasPendingDeleteInFlight() bool {
	for _, deletion := range m.pendingDeletes {
		if !deletion.Completed {
			return true
		}
	}
	return false
}

func (m model) hasCompletedPendingDelete() bool {
	for _, deletion := range m.pendingDeletes {
		if deletion.Completed {
			return true
		}
	}
	return false
}

func (m model) worktreeIndex(path string) int {
	for index, wt := range m.allWorktrees {
		if wt.Path == path {
			return index
		}
	}
	return 0
}

func worktreeExists(worktrees []Worktree, path string) bool {
	for _, wt := range worktrees {
		if wt.Path == path {
			return true
		}
	}
	return false
}

func withoutWorktree(worktrees []Worktree, path string) []Worktree {
	filtered := make([]Worktree, 0, len(worktrees))
	for _, wt := range worktrees {
		if wt.Path != path {
			filtered = append(filtered, wt)
		}
	}
	return filtered
}

func (m *model) rebuildRows() {
	if m.isTicketMode() {
		m.rebuildTicketRows()
		return
	}
	if m.isPRMode() {
		m.rebuildPRRows()
		return
	}
	rows := make([]table.Row, 0, len(m.visibleWorktrees))
	compact := m.table.Width() <= 70
	for _, wt := range m.visibleWorktrees {
		rows = append(rows, table.Row(worktreeRowCells(wt, compact)))
	}
	if len(rows) == 0 {
		columnCount := 6
		if compact {
			columnCount = 4
		}
		rows = append(rows, table.Row(emptyTableRow(columnCount, m.emptyWorktreesMessage())))
	}
	m.table.SetRows(rows)
}

func (m *model) rebuildPRRows() {
	rows := make([]table.Row, 0, len(m.visiblePRs))
	compact := m.table.Width() <= 70
	for _, pr := range m.visiblePRs {
		rows = append(rows, table.Row(m.prRowCells(pr, compact)))
	}
	if len(rows) == 0 {
		columnCount := 7
		if compact {
			columnCount = 4
		}
		rows = append(rows, table.Row(emptyTableRow(columnCount, m.emptyPRsMessage())))
	}
	m.table.SetRows(rows)
}

func (m *model) resize() {
	if !m.ready {
		return
	}

	if (m.isPRMode() && m.prDiffMode) || (m.isTicketMode() && m.ticketReadMode) {
		m.detail.Width = max(20, m.width)
		m.detail.Height = max(1, m.height-3)
		m.updateDetailContent()
		return
	}

	usableWidth := max(20, m.width-appStyle.GetHorizontalFrameSize())
	cardFrameWidth := cardStyle.GetHorizontalFrameSize()
	availableHeight := max(10, m.height-9)
	stacked := m.width < 130
	if stacked {
		m.table.SetHeight(max(6, availableHeight/2))
		panelWidth := max(20, usableWidth-cardFrameWidth)
		m.table.SetWidth(panelWidth)
		m.detail.Width = panelWidth
		m.detail.Height = max(5, availableHeight-m.table.Height())
	} else {
		panelWidth := max(80, usableWidth-(cardFrameWidth*2))
		detailWidth := max(36, int(float64(panelWidth)*0.44))
		tableWidth := panelWidth - detailWidth
		if tableWidth < 60 {
			tableWidth = 60
			detailWidth = panelWidth - tableWidth
		}
		m.table.SetHeight(availableHeight)
		m.table.SetWidth(tableWidth)
		m.detail.Width = max(20, detailWidth)
		m.detail.Height = availableHeight - 2
	}

	m.filterInput.Width = max(24, min(56, m.width/3))
	m.newInput.Width = max(24, min(54, m.width-18))
	m.prQuestionInput.Width = max(24, min(70, m.width-18))
	m.ticketStatusInput.Width = max(24, min(48, m.width-18))
	m.ticketCommentInput.Width = max(24, min(70, m.width-18))
	m.ticketAssigneeInput.Width = max(24, min(54, m.width-18))
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	switch m.mode {
	case modePRs:
		m.selectPRByNumber()
	case modeTickets:
		m.selectTicketByKey()
	default:
		m.selectByPath()
	}
	m.updateDetailContent()
}

func (m model) activeColumns() []table.Column {
	if m.isTicketMode() {
		return buildTicketColumns(m.table.Width())
	}
	if m.isPRMode() {
		return buildPRColumns(m.table.Width())
	}
	return buildColumns(m.table.Width())
}

func (m *model) selectedWorktree() *Worktree {
	if len(m.visibleWorktrees) == 0 {
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.visibleWorktrees) {
		return nil
	}
	return &m.visibleWorktrees[idx]
}

func (m *model) selectedRemotePR() *RemotePullRequest {
	if len(m.visiblePRs) == 0 {
		return nil
	}
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.visiblePRs) {
		return nil
	}
	return &m.visiblePRs[idx]
}

func (m *model) selectedRemotePRForDetail() *RemotePullRequest {
	if m.prDetail != nil && m.prDetail.Number == m.selectedPRNumber {
		return m.prDetail
	}
	return m.selectedRemotePR()
}

func (m model) beginPRApproval() (tea.Model, tea.Cmd) {
	selected := m.selectedRemotePR()
	if selected == nil {
		m.statusMessage = "Select a remote PR first"
		m.statusError = true
		return m, nil
	}
	if m.prDetail != nil && m.prDetail.Number == selected.Number {
		m.showPRApprovalConfirmation(*m.prDetail)
		m.statusMessage = fmt.Sprintf("Confirm approval for PR #%d", selected.Number)
		m.statusError = false
		return m, nil
	}
	m.prApprovalAwaitingDetailNumber = selected.Number
	if m.prDetailLoading && selected.Number == m.selectedPRNumber {
		m.statusMessage = fmt.Sprintf("Loading PR #%d details before approval...", selected.Number)
		m.statusError = false
		return m, nil
	}
	m.statusMessage = fmt.Sprintf("Loading PR #%d details before approval...", selected.Number)
	m.statusError = false
	return m.startPRDetailLoad(selected.Number)
}

func (m *model) showPRApprovalConfirmation(pr RemotePullRequest) {
	m.confirmingPRApproval = true
	m.prApprovalPendingNumber = pr.Number
	m.prApprovalWarnings = prApprovalWarnings(pr)
}

func (m *model) selectedWorktreePath() string {
	selected := m.selectedWorktree()
	if selected == nil {
		return ""
	}
	return selected.Path
}

func (m model) openSelectedWorktreeWithAction(action string) (tea.Model, tea.Cmd) {
	selected := m.selectedWorktree()
	if selected == nil {
		return m, nil
	}
	return m.openWorktreeWithAction(*selected, action)
}

func (m model) openWorktreeWithAction(wt Worktree, action string) (tea.Model, tea.Cmd) {
	if m.deferPendingWorktreeAction(wt.Path, action) {
		return m, nil
	}
	if wt.Missing || wt.Prunable {
		m.statusMessage = fmt.Sprintf("Cannot open missing worktree %s", wt.Name)
		m.statusError = true
		return m, nil
	}

	switch normalizedEnterAction(action) {
	case core.EnterActionAgent:
		agent := m.defaultAgent()
		if agent == nil {
			m.statusMessage = "No AI agent configured"
			m.statusError = true
			return m, nil
		}
		return m, openAgentCmd(m.services, wt, *agent)
	case core.EnterActionTerminal:
		return m, openTerminalWorktreeCmd(m.services, wt)
	default:
		return m, openEditorWorktreeCmd(m.services, wt, m.configuredEditor())
	}
}

func (m model) defaultAgent() *AgentTool {
	return m.services.FindAgent(m.cfg.App.Agents, m.cfg.App.DefaultAgent)
}

func (m model) copilotAgent() *AgentTool {
	if agent := m.services.FindAgent(m.cfg.App.Agents, "copilot"); agent != nil {
		return agent
	}
	return m.defaultAgent()
}

func (m model) prAgentLabel() string {
	agent := m.copilotAgent()
	if agent == nil {
		return "agent"
	}
	return agent.Name
}

func (m model) defaultAgentIndex() int {
	for idx, agent := range m.cfg.App.Agents {
		if agent.Name == m.cfg.App.DefaultAgent {
			return idx
		}
	}
	return 0
}

func (m model) defaultEnterActionIndex() int {
	action := normalizedEnterAction(m.cfg.App.DefaultEnterAction)
	for idx, option := range enterActionOptions {
		if option.Action == action {
			return idx
		}
	}
	return 0
}

func (m model) defaultProfileIndex() int {
	for idx, profile := range m.cfg.App.Profiles {
		if profile.Name == m.cfg.App.DefaultProfile {
			return idx
		}
	}
	return 0
}

func normalizedEnterAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case core.EnterActionVSCode:
		return core.EnterActionVSCode
	case core.EnterActionAgent:
		return core.EnterActionAgent
	case core.EnterActionTerminal:
		return core.EnterActionTerminal
	default:
		return core.EnterActionVSCode
	}
}

func enterActionLabel(action string) string {
	action = normalizedEnterAction(action)
	for _, option := range enterActionOptions {
		if option.Action == action {
			return option.Label
		}
	}
	return enterActionOptions[0].Label
}

func (m *model) selectByPath() {
	if len(m.visibleWorktrees) == 0 {
		m.selectedPath = ""
		m.selectedIndex = 0
		m.table.SetCursor(0)
		return
	}

	if m.selectedPath == "" {
		idx := min(max(m.selectedIndex, 0), len(m.visibleWorktrees)-1)
		m.table.SetCursor(idx)
		m.selectedIndex = idx
		m.selectedPath = m.visibleWorktrees[idx].Path
		return
	}

	for idx, wt := range m.visibleWorktrees {
		if wt.Path == m.selectedPath {
			m.table.SetCursor(idx)
			m.selectedIndex = idx
			m.selectedPath = wt.Path
			return
		}
	}

	// Prefer the item that shifted into the removed row's position; when the
	// removed item was last, fall back to the preceding item instead.
	fallback := min(max(m.selectedIndex, 0), len(m.visibleWorktrees)-1)
	m.table.SetCursor(fallback)
	m.selectedIndex = fallback
	m.selectedPath = m.visibleWorktrees[fallback].Path
}

func (m *model) selectPRByNumber() {
	if len(m.visiblePRs) == 0 {
		m.selectedPRNumber = 0
		m.selectedPRIndex = 0
		m.table.SetCursor(0)
		return
	}

	if m.selectedPRNumber == 0 {
		idx := min(max(m.selectedPRIndex, 0), len(m.visiblePRs)-1)
		m.table.SetCursor(idx)
		m.selectedPRIndex = idx
		m.selectedPRNumber = m.visiblePRs[idx].Number
		return
	}

	for idx, pr := range m.visiblePRs {
		if pr.Number == m.selectedPRNumber {
			m.table.SetCursor(idx)
			m.selectedPRIndex = idx
			return
		}
	}

	fallback := min(max(m.selectedPRIndex-1, 0), len(m.visiblePRs)-1)
	m.table.SetCursor(fallback)
	m.selectedPRIndex = fallback
	m.selectedPRNumber = m.visiblePRs[fallback].Number
}

func (m *model) updateDetailContent() {
	if !m.ready {
		return
	}

	if m.isPRMode() {
		selected := m.selectedRemotePRForDetail()
		if selected == nil {
			if m.filterQuery != "" || m.prUserFilter != "" {
				m.detail.SetContent("No remote PRs match the active filters")
			} else {
				m.detail.SetContent("No remote PR selected")
			}
			m.detail.GotoTop()
			return
		}
		if m.prDiffMode {
			m.detail.SetContent(remotePRDiffViewContent(selected, m.prDetailLoading, m.detail.Width))
			m.detail.GotoTop()
			return
		}
		m.detail.SetContent(remotePRDetailViewContent(selected, m.prDetailLoading, m.prShowFailedGHA, false, m.prChatQuestion, m.prChatAnswer, m.detail.Width))
		m.detail.GotoTop()
		return
	}

	if m.isTicketMode() {
		selected := m.selectedTicketForDetail()
		if selected == nil {
			if m.filterQuery != "" {
				m.detail.SetContent(fmt.Sprintf("No JIRA tickets match %q", m.filterQuery))
			} else {
				m.detail.SetContent("No JIRA ticket selected")
			}
			m.detail.GotoTop()
			return
		}
		m.detail.SetContent(ticketDetailViewContent(selected, m.ticketDetailLoading, m.detail.Width))
		m.detail.GotoTop()
		return
	}

	selected := m.selectedWorktree()
	if selected == nil {
		if m.filterQuery != "" {
			m.detail.SetContent(fmt.Sprintf("No worktrees match %q", m.filterQuery))
		} else {
			m.detail.SetContent("No worktree selected")
		}
		m.detail.GotoTop()
		return
	}

	m.detail.SetContent(detailViewContent(selected, m.showPRBody, m.detail.Width))
	m.detail.GotoTop()
}

func (m *model) moveSelection(delta int) {
	m.pendingWorktreePath = ""
	if len(m.visibleWorktrees) == 0 {
		return
	}

	next := m.table.Cursor() + delta
	if next < 0 {
		next = 0
	}
	if maxIndex := len(m.visibleWorktrees) - 1; next > maxIndex {
		next = maxIndex
	}

	m.table.SetCursor(next)
	m.selectedIndex = next
	m.selectedPath = m.visibleWorktrees[next].Path
	m.updateDetailContent()
}

func (m model) movePRSelectionAndLoad(delta int) (tea.Model, tea.Cmd) {
	m.movePRSelection(delta)
	selected := m.selectedRemotePR()
	if selected == nil {
		return m, nil
	}
	return m.startPRDetailLoad(selected.Number)
}

func (m model) startPRDetailLoad(number int) (tea.Model, tea.Cmd) {
	m.cancelPRDetailLoad()
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	m.prDetailRequest++
	m.prDetailCancel = cancel
	m.prDetailLoading = true
	m.prDetail = nil
	m.statusMessage = fmt.Sprintf("Loading PR #%d details...", number)
	m.statusError = false
	if !m.loading {
		m.applySpinnerForKind(loadingDetail)
	}
	return m, tea.Batch(loadRemotePullRequestDetailCmd(m.services, ctx, m.cfg, number, m.prDetailRequest), m.spinner.Tick)
}

func (m *model) cancelPRDetailLoad() {
	if m.prDetailCancel != nil {
		m.prDetailCancel()
		m.prDetailCancel = nil
	}
	m.prDetailRequest++
	m.prDetailLoading = false
	m.prDetail = nil
}

func (m model) isCurrentPRDetailResult(result remotePRDetailResult) bool {
	return result.RequestID == m.prDetailRequest && result.Number == m.selectedPRNumber
}

func (m *model) mergeRemotePRDetail(detail RemotePullRequest) {
	for idx := range m.remotePRs {
		if m.remotePRs[idx].Number == detail.Number {
			m.remotePRs[idx] = detail
			break
		}
	}
	for idx := range m.visiblePRs {
		if m.visiblePRs[idx].Number == detail.Number {
			m.visiblePRs[idx] = detail
			break
		}
	}
	m.rebuildRows()
	m.selectPRByNumber()
}

func (m *model) movePRSelection(delta int) {
	if len(m.visiblePRs) == 0 {
		return
	}
	next := m.table.Cursor() + delta
	if next < 0 {
		next = 0
	}
	if maxIndex := len(m.visiblePRs) - 1; next > maxIndex {
		next = maxIndex
	}
	m.table.SetCursor(next)
	m.selectedPRIndex = next
	m.selectedPRNumber = m.visiblePRs[next].Number
	m.confirmingPRApproval = false
	m.prApprovalPendingNumber = 0
	m.prApprovalAwaitingDetailNumber = 0
	m.prApprovalWarnings = nil
	m.updateDetailContent()
}

func (m model) loadSummary() string {
	if m.filterQuery != "" {
		return fmt.Sprintf("Loaded %d worktrees, %d match %q", len(m.allWorktrees), len(m.visibleWorktrees), m.filterQuery)
	}
	return fmt.Sprintf("Loaded %d worktrees", len(m.allWorktrees))
}

func (m model) filterSummary() string {
	if m.isTicketMode() {
		return m.ticketFilterSummary()
	}
	if m.isPRMode() {
		return m.prFilterSummary()
	}
	if m.filterQuery == "" {
		return fmt.Sprintf("Showing %d worktrees", len(m.visibleWorktrees))
	}
	return fmt.Sprintf("Showing %d/%d worktrees matching %q", len(m.visibleWorktrees), len(m.allWorktrees), m.filterQuery)
}

func (m model) prFilterSummary() string {
	parts := []string{fmt.Sprintf("Showing %d/%d remote PRs", len(m.visiblePRs), len(m.remotePRs))}
	if m.prStateFilter != "" && m.prStateFilter != "all" {
		parts = append(parts, "state="+m.prStateFilter)
	}
	if m.prUserFilter != "" {
		parts = append(parts, "user=@"+m.prUserFilter)
	}
	if m.prAuthorFilter != "" {
		parts = append(parts, "author=@"+m.prAuthorFilter)
	}
	if m.filterQuery != "" {
		parts = append(parts, fmt.Sprintf("query=%q", m.filterQuery))
	}
	return strings.Join(parts, " ")
}

func (m *model) cyclePRStateFilter() {
	states := []string{"open", "all", "closed", "merged"}
	for idx, state := range states {
		if state == m.prStateFilter {
			m.prStateFilter = states[(idx+1)%len(states)]
			return
		}
	}
	m.prStateFilter = "open"
}

func (m model) currentPRUserIndex() int {
	if m.prUserFilter == "" {
		return 0
	}
	for idx, user := range m.prUserOptions {
		if strings.EqualFold(user, m.prUserFilter) {
			return idx
		}
	}
	return 0
}

// Column minimums account for the two-cell selection pointer in the first
// column and the icon prefixes in State/Ready/PR cells.
func buildColumns(totalWidth int) []table.Column {
	if totalWidth <= 70 {
		widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*4, []int{10, 10, 7, 7}, []int{18, 20, 12, 14}, []int{1, 0, 3, 2})
		return []table.Column{{Title: "Worktree", Width: widths[0]}, {Title: "Branch", Width: widths[1]}, {Title: "State", Width: widths[2]}, {Title: "PR", Width: widths[3]}}
	}
	widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*6, []int{14, 16, 7, 6, 8, 12}, []int{20, 26, 10, 9, 13, 18}, []int{5, 1, 0, 4, 2, 3})
	return []table.Column{{Title: "Worktree", Width: widths[0]}, {Title: "Branch", Width: widths[1]}, {Title: "State", Width: widths[2]}, {Title: "Delta", Width: widths[3]}, {Title: "PR", Width: widths[4]}, {Title: "Last Commit", Width: widths[5]}}
}

func buildPRColumns(totalWidth int) []table.Column {
	if totalWidth <= 70 {
		widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*4, []int{7, 8, 7, 12}, []int{10, 14, 12, 26}, []int{3, 1, 2, 0})
		return []table.Column{{Title: "PR", Width: widths[0]}, {Title: "Author", Width: widths[1]}, {Title: "State", Width: widths[2]}, {Title: "Title", Width: widths[3]}}
	}
	widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*7, []int{7, 8, 7, 6, 8, 8, 14}, []int{10, 14, 10, 10, 11, 12, 30}, []int{6, 1, 4, 3, 5, 2, 0})
	return []table.Column{{Title: "PR", Width: widths[0]}, {Title: "Author", Width: widths[1]}, {Title: "State", Width: widths[2]}, {Title: "Checks", Width: widths[3]}, {Title: "Ready", Width: widths[4]}, {Title: "Updated", Width: widths[5]}, {Title: "Title", Width: widths[6]}}
}

func prStateLabel(pr RemotePullRequest) string {
	if pr.IsDraft {
		return "draft"
	}
	return strings.ToLower(emptyDash(pr.State))
}

func shortReviewDecision(decision string) string {
	switch strings.ToUpper(strings.TrimSpace(decision)) {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes"
	case "REVIEW_REQUIRED":
		return "needed"
	case "":
		return "-"
	default:
		return strings.ToLower(decision)
	}
}

func checksCounts(checks []StatusCheck) (failing, pending, passing int) {
	for _, check := range checks {
		conclusion := strings.ToLower(check.Conclusion)
		status := strings.ToLower(check.Status)
		switch {
		case conclusion == "failure" || conclusion == "timed_out" || conclusion == "cancelled" || conclusion == "action_required":
			failing++
		case conclusion == "success":
			passing++
		case status != "completed" || conclusion == "":
			pending++
		}
	}
	return failing, pending, passing
}

func checksSummary(checks []StatusCheck) string {
	if len(checks) == 0 {
		return "-"
	}
	failing, pending, passing := checksCounts(checks)
	if failing > 0 {
		return fmt.Sprintf("%d fail", failing)
	}
	if pending > 0 {
		return fmt.Sprintf("%d wait", pending)
	}
	return fmt.Sprintf("%d pass", passing)
}

func shortTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "-"
	}
	duration := time.Since(parsed)
	if duration < time.Hour {
		return fmt.Sprintf("%dm", max(0, int(duration.Minutes())))
	}
	if duration < 48*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}

func reviewReadiness(pr RemotePullRequest) string {
	if pr.IsDraft {
		return "draft"
	}
	if strings.EqualFold(pr.State, "closed") || strings.EqualFold(pr.State, "merged") {
		return "done"
	}
	if checksFailing(pr.StatusCheckRollup) {
		return "blocked"
	}
	if checksPending(pr.StatusCheckRollup) {
		return "waiting"
	}
	switch strings.ToUpper(pr.ReviewDecision) {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes"
	}
	return "ready"
}

func prApprovalWarnings(pr RemotePullRequest) []string {
	var warnings []string
	if pr.IsDraft {
		warnings = append(warnings, "PR is still marked as draft")
	}
	if !strings.EqualFold(pr.State, "open") {
		warnings = append(warnings, fmt.Sprintf("PR state is %s", strings.ToLower(emptyDash(pr.State))))
	}
	if checksFailing(pr.StatusCheckRollup) {
		warnings = append(warnings, "one or more status checks are failing")
	}
	if checksPending(pr.StatusCheckRollup) {
		warnings = append(warnings, "one or more status checks are still pending")
	}
	if strings.EqualFold(pr.ReviewDecision, "CHANGES_REQUESTED") {
		warnings = append(warnings, "latest reviews include requested changes")
	}
	return warnings
}

func checksFailing(checks []StatusCheck) bool {
	for _, check := range checks {
		conclusion := strings.ToLower(check.Conclusion)
		if conclusion == "failure" || conclusion == "timed_out" || conclusion == "cancelled" || conclusion == "action_required" {
			return true
		}
	}
	return false
}

func checksPending(checks []StatusCheck) bool {
	for _, check := range checks {
		if strings.ToLower(check.Status) != "completed" || strings.TrimSpace(check.Conclusion) == "" {
			return true
		}
	}
	return false
}

func (m model) prHeadline(pr RemotePullRequest) string {
	if m.prShowBranch {
		return pr.HeadRefName
	}
	return pr.Title
}

func distributeColumnWidths(total int, minimums, targets, order []int) []int {
	widths := append([]int(nil), minimums...)
	if total <= 0 {
		for idx := range widths {
			widths[idx] = 1
		}
		return widths
	}

	minimumTotal := 0
	for _, width := range widths {
		minimumTotal += width
	}
	if total <= minimumTotal {
		return shrinkColumnWidths(total, widths)
	}

	remaining := total - minimumTotal
	for _, idx := range order {
		if idx < 0 || idx >= len(widths) || idx >= len(targets) {
			continue
		}
		growth := min(remaining, max(0, targets[idx]-widths[idx]))
		widths[idx] += growth
		remaining -= growth
		if remaining == 0 {
			return widths
		}
	}
	widths[len(widths)-1] += remaining
	return widths
}

func shrinkColumnWidths(total int, widths []int) []int {
	if len(widths) == 0 {
		return widths
	}
	for idx := range widths {
		widths[idx] = max(1, widths[idx])
	}
	for sum := sumInts(widths); sum > total && sum > len(widths); sum = sumInts(widths) {
		for idx := len(widths) - 1; idx >= 0 && sum > total; idx-- {
			if widths[idx] > 1 {
				widths[idx]--
				sum--
			}
		}
	}
	return widths
}

func sumInts(values []int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func detailViewContent(wt *Worktree, showPRBody bool, width int) string {
	if wt == nil {
		return "No worktree selected"
	}

	var lines []string
	lines = append(lines,
		fmt.Sprintf("Worktree: %s", wt.Name),
		fmt.Sprintf("Path: %s", wt.Path),
		fmt.Sprintf("Branch: %s", wt.Branch),
		fmt.Sprintf("State: %s", stateDisplay(*wt)),
		fmt.Sprintf("Tracking: %s", trackingDisplay(wt.Status)),
		fmt.Sprintf("Changes: %s (staged=%d unstaged=%d untracked=%d conflicts=%d)", changeDisplay(wt.Status), wt.Status.Staged, wt.Status.Unstaged, wt.Status.Untracked, wt.Status.Conflicts),
	)

	if wt.Commit.Hash != "" {
		lines = append(lines,
			"",
			fmt.Sprintf("Last commit: %s %s", wt.Commit.Hash, wt.Commit.Relative),
			wt.Commit.Subject,
		)
	}

	if wt.LoadError != "" {
		lines = append(lines, "", fmt.Sprintf("Load note: %s", wt.LoadError))
	}

	lines = append(lines, "", detailSection("Changed files"))
	if len(wt.Status.Files) == 0 {
		lines = append(lines, "  none")
	} else {
		for idx, file := range wt.Status.Files {
			if idx == 15 {
				lines = append(lines, fmt.Sprintf("  ... and %d more", len(wt.Status.Files)-idx))
				break
			}
			lines = append(lines, fmt.Sprintf("  %s", file))
		}
	}

	lines = append(lines, "", detailSection("Pull request"))
	if wt.PR == nil {
		lines = append(lines, "  none")
	} else {
		author := wt.PR.Author.Login
		if wt.PR.Author.Name != "" {
			author = fmt.Sprintf("%s (%s)", wt.PR.Author.Login, wt.PR.Author.Name)
		}
		lines = append(lines,
			fmt.Sprintf("  %s", shortPRDisplay(wt.PR)),
			fmt.Sprintf("  %s", wt.PR.Title),
			fmt.Sprintf("  %s", wt.PR.URL),
		)
		if author != "" {
			lines = append(lines, fmt.Sprintf("  author: %s", author))
		}
		if showPRBody {
			body := strings.TrimSpace(wt.PR.Body)
			if body == "" {
				body = "No PR description"
			}
			lines = append(lines, "", "PR description:", body)
		} else {
			preview := strings.TrimSpace(firstLine(wt.PR.Body))
			if preview == "" {
				preview = "No PR description"
			}
			lines = append(lines, fmt.Sprintf("  preview: %s", truncateText(preview, max(20, width-12))))
			lines = append(lines, "  press p to toggle full body")
		}
	}

	return lipgloss.NewStyle().Width(max(1, width)).Render(strings.Join(lines, "\n"))
}

func remotePRDetailViewContent(pr *RemotePullRequest, loading bool, showFailedGHA bool, diffMode bool, chatQuestion string, chatAnswer string, width int) string {
	if pr == nil {
		return "No remote PR selected"
	}

	labels := make([]string, 0, len(pr.Labels))
	for _, label := range pr.Labels {
		labels = append(labels, label.Name)
	}

	assignees := actorList(pr.Assignees)
	reviewers := make([]GitHubActor, 0, len(pr.ReviewRequests))
	for _, request := range pr.ReviewRequests {
		reviewers = append(reviewers, request.RequestedReviewer)
	}

	contentWidth := max(1, width)
	lines := []string{
		detailTitleLine(fmt.Sprintf("PR #%d", pr.Number), pr.Title, contentWidth),
		detailField("URL", pr.URL, infoStyle, contentWidth),
		detailField("State", prStateDisplay(*pr), prStateCellStyle(*pr), contentWidth),
		detailField("Author", emptyDash(pr.Author.Login), magentaStyle, contentWidth),
		detailField("Branch", fmt.Sprintf("%s -> %s", emptyDash(pr.HeadRefName), emptyDash(pr.BaseRefName)), purpleStyle, contentWidth),
		detailField("Review", reviewDisplay(pr.ReviewDecision), mutedStyle, contentWidth),
		detailField("Checks", checksDetailDisplay(pr.StatusCheckRollup), checksCellStyle(pr.StatusCheckRollup), contentWidth),
		detailField("Ready", readinessDisplay(*pr), readinessCellStyle(*pr), contentWidth),
		detailField("Size", fmt.Sprintf("%d files, +%d/-%d", pr.ChangedFiles, pr.Additions, pr.Deletions), deltaSizeStyle(*pr), contentWidth),
		detailField("Updated", emptyDash(pr.UpdatedAt), mutedStyle, contentWidth),
	}
	if len(labels) > 0 {
		lines = append(lines, detailField("Labels", strings.Join(labels, ", "), warnStyle, contentWidth))
	}
	if assignees != "" {
		lines = append(lines, detailField("Assignees", assignees, magentaStyle, contentWidth))
	}
	if reviewerList := actorList(reviewers); reviewerList != "" {
		lines = append(lines, detailField("Review requests", reviewerList, magentaStyle, contentWidth))
	}
	if loading && strings.TrimSpace(pr.Body) == "" && len(pr.Files) == 0 {
		lines = append(lines, "", detailSection("Description"))
		lines = append(lines, skeletonLines(contentWidth, 3)...)
		lines = append(lines, "", detailSection("Files"))
		lines = append(lines, skeletonLines(contentWidth, 2)...)
		lines = append(lines, "", styledWrappedLine("Loading PR details...", infoStyle, contentWidth))
		return strings.Join(lines, "\n")
	}
	if loading {
		lines = append(lines, "", styledWrappedLine("Refreshing PR details...", infoStyle, contentWidth))
	}
	if diffMode {
		lines = append(lines, "", detailSection("Diff"))
		if loading && strings.TrimSpace(pr.Diff) == "" {
			lines = append(lines, styledWrappedLine("  loading diff...", infoStyle, contentWidth))
		} else {
			lines = append(lines, renderPullRequestDiff(pr.Diff, contentWidth)...)
		}
		lines = append(lines, "", styledWrappedLine("Actions: esc returns to overview, A approves PR, o opens PR", subtleStyle, contentWidth))
		return strings.Join(lines, "\n")
	}
	if showFailedGHA {
		lines = append(lines, "")
		lines = append(lines, failedGHADetailLines(pr.StatusCheckRollup, contentWidth)...)
	}
	if chatQuestion != "" || chatAnswer != "" {
		lines = append(lines, "", detailSection("Agent chat"))
		if chatQuestion != "" {
			lines = append(lines, detailField("  Q", chatQuestion, infoStyle, contentWidth))
		}
		if chatAnswer != "" {
			lines = append(lines, detailField("  A", chatAnswer, successStyle, contentWidth))
		}
	}

	body := strings.TrimSpace(pr.Body)
	if body != "" {
		lines = append(lines, "", detailSection("Description"))
		lines = append(lines, colorizeMarkdownish(truncateRunes(body, 1200), contentWidth)...)
	}

	lines = append(lines, "", detailSection("Files"))
	if len(pr.Files) == 0 {
		lines = append(lines, styledWrappedLine("  not loaded", mutedStyle, contentWidth))
	} else {
		for idx, file := range pr.Files {
			if idx == 20 {
				lines = append(lines, styledWrappedLine(fmt.Sprintf("  ... and %d more", len(pr.Files)-idx), mutedStyle, contentWidth))
				break
			}
			lines = append(lines, detailFileLine(file, contentWidth))
		}
	}

	if len(pr.Comments) > 0 {
		lines = append(lines, "", detailSection("Recent comments"))
		start := max(0, len(pr.Comments)-5)
		for _, comment := range pr.Comments[start:] {
			lines = append(lines, detailField("  "+emptyDash(comment.Author.Login), truncateRunes(strings.TrimSpace(comment.Body), 240), mutedStyle, contentWidth))
		}
	}

	lines = append(lines, "", styledWrappedLine("Actions: enter opens full diff, A approves PR, o opens PR, n creates worktree, f toggles failed checks, c asks agent", subtleStyle, contentWidth))
	return strings.Join(lines, "\n")
}

func remotePRDiffViewContent(pr *RemotePullRequest, loading bool, width int) string {
	if pr == nil {
		return "No remote PR selected"
	}
	contentWidth := max(1, width)
	if loading && strings.TrimSpace(pr.Diff) == "" {
		return styledWrappedLine("Loading PR diff...", infoStyle, contentWidth)
	}
	return strings.Join(renderPullRequestDiff(pr.Diff, contentWidth), "\n")
}

func failedGHADetailLines(checks []StatusCheck, width int) []string {
	failing := failedStatusChecks(checks)
	if len(failing) == 0 {
		return []string{detailSection("Failed GitHub Actions"), styledWrappedLine("  none", successStyle, width)}
	}
	lines := []string{detailSection("Failed GitHub Actions")}
	for idx, check := range failing {
		if idx == 10 {
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("  ... and %d more", len(failing)-idx)))
			break
		}
		state := strings.ToLower(check.Conclusion)
		if state == "" {
			state = strings.ToLower(check.Status)
		}
		lines = append(lines, detailField("  "+check.Name, emptyDash(state), errorStyle, width))
		if check.URL != "" {
			lines = append(lines, styledWrappedLine("    "+check.URL, infoStyle, width))
		}
		summary := strings.TrimSpace(firstLine(check.Summary))
		if summary != "" {
			lines = append(lines, styledWrappedLine("    "+truncateRunes(summary, 180), warnStyle, width))
		}
	}
	return lines
}

func detailTitleLine(prefix string, title string, width int) string {
	return styledWrappedField(prefix, title, titleStyle, lipgloss.NewStyle().Bold(true), width)
}

func detailSection(title string) string {
	return titleStyle.Render(icons.Section + " " + title + ":")
}

// skeletonLines renders dimmed placeholder bars of varying length, used while
// detail content is still being fetched.
func skeletonLines(width int, rows int) []string {
	width = max(1, width)
	fractions := []float64{0.9, 0.55, 0.75, 0.4}
	lines := make([]string, 0, rows)
	for i := 0; i < rows; i++ {
		barWidth := min(width-2, int(float64(width-2)*fractions[i%len(fractions)]))
		if barWidth < 1 {
			barWidth = 1
		}
		lines = append(lines, "  "+skeletonStyle.Render(strings.Repeat(icons.SkeletonBlock, barWidth)))
	}
	return lines
}

func detailField(label string, value string, valueStyle lipgloss.Style, width int) string {
	return styledWrappedField(label, value, mutedStyle, valueStyle, width)
}

func deltaSizeStyle(pr RemotePullRequest) lipgloss.Style {
	if pr.Deletions > pr.Additions {
		return warnStyle
	}
	if pr.Additions > 0 || pr.Deletions > 0 {
		return successStyle
	}
	return mutedStyle
}

func detailFileLine(file PullFile, width int) string {
	plain := fmt.Sprintf("  %s (+%d/-%d)", file.Path, file.Additions, file.Deletions)
	wrapped := wrapPlainLine(plain, width)
	if len(wrapped) == 0 {
		return ""
	}
	delta := fmt.Sprintf("(+%d/-%d)", file.Additions, file.Deletions)
	line := wrapped[0]
	if strings.Contains(line, delta) {
		pathPart := strings.TrimSpace(strings.TrimSuffix(line, delta))
		return fmt.Sprintf("  %s %s", purpleStyle.Render(strings.TrimSpace(pathPart)), deltaSizeStyle(RemotePullRequest{Additions: file.Additions, Deletions: file.Deletions}).Render(delta))
	}
	return styledWrappedLine(line, purpleStyle, width)
}

func styledWrappedField(label string, value string, labelStyle lipgloss.Style, valueStyle lipgloss.Style, width int) string {
	labelText := strings.TrimSpace(label) + ":"
	wrapped := wrapPlainLine(labelText+" "+singleLineText(value), width)
	if len(wrapped) == 0 {
		return labelStyle.Render(labelText)
	}
	for idx, line := range wrapped {
		line = clampRunewidth(line, width)
		if idx == 0 {
			valuePart := strings.TrimSpace(strings.TrimPrefix(line, labelText))
			if valuePart == line || valuePart == "" {
				wrapped[idx] = labelStyle.Render(line)
			} else {
				visibleLabel := clampRunewidth(labelText, max(1, width-runewidth.StringWidth(valuePart)-1))
				wrapped[idx] = labelStyle.Render(visibleLabel) + " " + valueStyle.Render(valuePart)
			}
			continue
		}
		wrapped[idx] = valueStyle.Render(line)
	}
	return strings.Join(wrapped, "\n")
}

func styledWrappedLine(text string, style lipgloss.Style, width int) string {
	return strings.Join(styledWrap(text, style, width), "\n")
}

func styledWrap(text string, style lipgloss.Style, width int) []string {
	wrapped := wrapPlainLine(text, width)
	if len(wrapped) == 0 {
		return []string{style.Render("")}
	}
	for idx, line := range wrapped {
		wrapped[idx] = style.Render(line)
	}
	return wrapped
}

func wrapPlainLine(text string, width int) []string {
	width = max(1, width)
	var result []string
	for _, raw := range strings.Split(text, "\n") {
		line := singleLineText(raw)
		if line == "" {
			result = append(result, "")
			continue
		}
		for runewidth.StringWidth(line) > width {
			cut := runewidth.Truncate(line, width, "")
			cut = clampRunewidth(cut, width)
			if cut == "" {
				break
			}
			breakAt := strings.LastIndex(cut, " ")
			if breakAt > 0 {
				cut = strings.TrimRight(cut[:breakAt], " ")
			}
			if cut == "" {
				cut = clampRunewidth(runewidth.Truncate(line, width, ""), width)
			}
			result = append(result, cut)
			line = strings.TrimLeft(strings.TrimPrefix(line, cut), " ")
		}
		result = append(result, clampRunewidth(line, width))
	}
	return result
}

func clampRunewidth(value string, width int) string {
	for runewidth.StringWidth(value) > width && value != "" {
		runes := []rune(value)
		value = string(runes[:len(runes)-1])
	}
	return value
}

func singleLineText(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ")
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func colorizeMarkdownish(text string, width int) []string {
	var result []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		style := lipgloss.NewStyle()
		switch {
		case strings.HasPrefix(trimmed, "###"):
			style = purpleStyle.Copy().Bold(true)
		case strings.HasPrefix(trimmed, "##"):
			style = infoStyle.Copy().Bold(true)
		case strings.HasPrefix(trimmed, "#"):
			style = titleStyle
		case strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "-"):
			style = warnStyle
		case strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://"):
			style = infoStyle
		}
		result = append(result, styledWrap(line, style, width)...)
	}
	return result
}

func failedStatusChecks(checks []StatusCheck) []StatusCheck {
	failed := make([]StatusCheck, 0)
	for _, check := range checks {
		conclusion := strings.ToLower(strings.TrimSpace(check.Conclusion))
		if conclusion == "failure" || conclusion == "timed_out" || conclusion == "cancelled" || conclusion == "action_required" {
			failed = append(failed, check)
		}
	}
	return failed
}

func actorList(actors []GitHubActor) string {
	items := make([]string, 0, len(actors))
	for _, actor := range actors {
		login := strings.TrimSpace(actor.Login)
		if login == "" {
			login = strings.TrimSpace(actor.Slug)
		}
		if login != "" {
			items = append(items, login)
		}
	}
	return strings.Join(items, ", ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func tableStyles() table.Styles {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("252"))
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("230")).
		Background(lipgloss.Color("62")).
		Bold(true)
	return styles
}

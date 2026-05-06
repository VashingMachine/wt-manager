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

type createResult struct {
	Path     string
	Name     string
	Existing bool
	Err      error
}

type deleteArmExpired struct {
	Path    string
	ArmedAt time.Time
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

type openPRWorktreeResult struct {
	Message string
	Err     error
}

type keyMap struct {
	Up          key.Binding
	Down        key.Binding
	Focus       key.Binding
	Refresh     key.Binding
	ToggleAll   key.Binding
	PRRadar     key.Binding
	StateFilter key.Binding
	UserFilter  key.Binding
	MineFilter  key.Binding
	BranchView  key.Binding
	FailedGHA   key.Binding
	HelpPage    key.Binding
	AskPR       key.Binding
	Filter      key.Binding
	ToggleBody  key.Binding
	OpenPR      key.Binding
	OpenCode    key.Binding
	OpenAgent   key.Binding
	AgentMenu   key.Binding
	ProfileMenu key.Binding
	Setup       key.Binding
	New         key.Binding
	Delete      key.Binding
	Quit        key.Binding
}

const deleteConfirmWindow = 1500 * time.Millisecond

func defaultKeys() keyMap {
	return keyMap{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("k/up", "move")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("j/down", "move")),
		Focus:       key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
		Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		ToggleAll:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle all worktrees")),
		PRRadar:     key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "PR radar")),
		StateFilter: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "PR state")),
		UserFilter:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "PR user")),
		MineFilter:  key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "my PRs")),
		BranchView:  key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "title/branch")),
		FailedGHA:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "failed checks")),
		HelpPage:    key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "help")),
		AskPR:       key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "ask agent")),
		Filter:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ToggleBody:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "toggle PR body")),
		OpenPR:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open PR in browser")),
		OpenCode:    key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "open VS Code")),
		OpenAgent:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "open agent")),
		AgentMenu:   key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "select agent")),
		ProfileMenu: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "switch repo")),
		Setup:       key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "setup")),
		New:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new worktree")),
		Delete:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d d", "delete worktree")),
		Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.HelpPage, k.Refresh, k.PRRadar, k.Filter, k.New, k.OpenPR, k.OpenAgent, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Focus, k.HelpPage, k.Refresh, k.ToggleAll, k.PRRadar, k.Filter}, {k.StateFilter, k.UserFilter, k.MineFilter, k.BranchView, k.FailedGHA, k.AskPR, k.New, k.ProfileMenu, k.Setup, k.ToggleBody, k.OpenPR, k.OpenCode, k.OpenAgent, k.AgentMenu, k.Delete, k.Quit}}
}

type model struct {
	services         core.Services
	cfg              Config
	keys             keyMap
	help             help.Model
	table            table.Model
	detail           viewport.Model
	filterInput      textinput.Model
	newInput         textinput.Model
	setup            *setupModel
	spinner          spinner.Model
	allWorktrees     []Worktree
	visibleWorktrees []Worktree
	remotePRs        []RemotePullRequest
	visiblePRs       []RemotePullRequest
	selectedPRNumber int
	selectedPRIndex  int
	prDetail         *RemotePullRequest
	prDetailLoading  bool
	prDetailRequest  int
	prDetailCancel   context.CancelFunc
	prMode           bool
	prStateFilter    string
	prUserFilter     string
	prAuthorFilter   string
	prCurrentUser    string
	prShowBranch     bool
	prShowFailedGHA  bool
	selectingPRUser  bool
	prUserOptions    []string
	prUserCursor     int
	askingPR         bool
	prQuestionInput  textinput.Model
	prChatQuestion   string
	prChatAnswer     string
	width            int
	height           int
	showAll          bool
	showPRBody       bool
	loading          bool
	filtering        bool
	creatingWorktree bool
	selectingAgent   bool
	selectingProfile bool
	showingHelp      bool
	agentCursor      int
	profileCursor    int
	focusTable       bool
	statusMessage    string
	statusError      bool
	deleteArmedPath  string
	deleteArmedName  string
	deleteArmedAt    time.Time
	selectedPath     string
	selectedIndex    int
	filterQuery      string
	ready            bool
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

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = subtleStyle.Copy()

	h := help.New()
	h.ShowAll = false

	m := model{
		services:        services,
		cfg:             cfg,
		keys:            defaultKeys(),
		help:            h,
		table:           tbl,
		detail:          vp,
		filterInput:     filterInput,
		newInput:        newInput,
		prQuestionInput: prQuestionInput,
		prStateFilter:   "open",
		spinner:         spin,
		loading:         true,
		focusTable:      true,
		statusMessage:   "Loading worktrees...",
	}
	if cfg.SetupNeeded {
		setup := newSetupModel(cfg, true, services)
		m.setup = &setup
		m.loading = false
		m.statusMessage = cfg.SetupReason
	}
	return m
}

func (m model) Init() tea.Cmd {
	if m.setup != nil {
		return nil
	}
	return tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ready = true
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		m.updateDetailContent()
		return m, nil
	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case loadResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("Refresh failed: %v", msg.Err)
			m.statusError = true
			return m, nil
		}

		m.allWorktrees = msg.Worktrees
		m.statusError = false
		if msg.Warning != "" {
			m.statusMessage = msg.Warning
		} else {
			m.statusMessage = m.loadSummary()
		}
		m.applyFilter()
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
		m.statusMessage = fmt.Sprintf("Loaded PR #%d details", msg.PullRequest.Number)
		m.statusError = false
		m.updateDetailContent()
		return m, nil
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
		m.filtering = false
		m.creatingWorktree = false
		m.selectingAgent = false
		m.selectingProfile = false
		m.filterQuery = ""
		m.filterInput.SetValue("")
		m.selectedPath = ""
		m.selectedIndex = 0
		m.clearDeleteArm()
		m.loading = true
		m.statusMessage = msg.Message + ". Loading worktrees..."
		m.statusError = false
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
	case deleteResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("Delete failed for %s: %v", msg.Name, msg.Err)
			m.statusError = true
			return m, nil
		}

		m.clearDeleteArm()
		m.optimisticRemove(msg.Path)
		m.statusMessage = fmt.Sprintf("Deleted %s. Refreshing...", msg.Name)
		m.statusError = false
		m.loading = true
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
	case createResult:
		m.loading = false
		if msg.Err != nil {
			m.statusMessage = fmt.Sprintf("Create failed for %s: %v", msg.Name, msg.Err)
			m.statusError = true
			return m, nil
		}

		m.selectedPath = msg.Path
		if msg.Existing {
			m.statusMessage = fmt.Sprintf("Selected existing worktree %s. Refreshing...", msg.Name)
		} else {
			m.statusMessage = fmt.Sprintf("Created %s. Refreshing...", msg.Name)
		}
		m.statusError = false
		m.loading = true
		return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
	case deleteArmExpired:
		if m.deleteArmedPath == msg.Path && m.deleteArmedAt.Equal(msg.ArmedAt) {
			m.clearDeleteArm()
			m.statusMessage = "Delete confirmation expired"
			m.statusError = false
		}
		return m, nil
	case actionResult:
		if msg.Err != nil {
			m.statusMessage = msg.Err.Error()
			m.statusError = true
		} else {
			m.statusMessage = msg.Message
			m.statusError = false
		}
		return m, nil
	case tea.KeyMsg:
		if m.setup != nil {
			setup := *m.setup
			var cmd tea.Cmd
			setup, cmd = setup.Update(msg)
			m.setup = &setup
			return m, cmd
		}

		if m.selectingAgent {
			return m.updateAgentSelector(msg)
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

		if m.creatingWorktree {
			return m.updateNewWorktree(msg)
		}

		if m.filtering {
			return m.updateFilter(msg)
		}

		if msg.String() == "esc" {
			if m.showingHelp {
				m.showingHelp = false
				m.statusMessage = "Help closed"
				m.statusError = false
				return m, nil
			}
			if m.deleteArmedPath != "" {
				m.clearDeleteArm()
				m.statusMessage = "Delete cancelled"
				m.statusError = false
				return m, nil
			}
			if m.filterQuery != "" {
				m.rememberSelection()
				m.filterQuery = ""
				m.filterInput.SetValue("")
				m.applyFilter()
				m.statusMessage = "Filter cleared"
				m.statusError = false
				return m, nil
			}
		}

		if key.Matches(msg, m.keys.Filter) {
			m.clearDeleteArm()
			m.filtering = true
			m.filterInput.SetValue(m.filterQuery)
			m.filterInput.CursorEnd()
			m.filterInput.Focus()
			m.statusMessage = "Filter by worktree or branch"
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
				if m.prMode {
					return m.movePRSelectionAndLoad(-1)
				}
				m.moveSelection(-1)
				return m, nil
			}
			if key.Matches(msg, m.keys.Down) {
				m.clearDeleteArm()
				if m.prMode {
					return m.movePRSelectionAndLoad(1)
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
			m.statusMessage = map[bool]string{true: "Table focused", false: "Details focused"}[m.focusTable]
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.Refresh) {
			m.clearDeleteArm()
			m.loading = true
			m.statusMessage = "Refreshing worktrees..."
			m.statusError = false
			if m.prMode {
				m.rememberPRSelection()
				m.cancelPRDetailLoad()
				m.statusMessage = "Refreshing remote PRs..."
				return m, tea.Batch(loadRemotePullRequestsCmd(m.services, m.cfg), m.spinner.Tick)
			}
			m.rememberSelection()
			return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
		}
		if key.Matches(msg, m.keys.PRRadar) {
			m.clearDeleteArm()
			m.cancelPRDetailLoad()
			wasPRMode := m.prMode
			if wasPRMode {
				m.rememberPRSelection()
			} else {
				m.rememberSelection()
			}
			m.prMode = !m.prMode
			m.filtering = false
			m.creatingWorktree = false
			m.askingPR = false
			m.selectingPRUser = false
			m.table.SetCursor(0)
			m.table.SetRows(nil)
			m.table.SetColumns(m.activeColumns())
			m.rebuildRows()
			m.updateDetailContent()
			m.statusError = false
			if m.prMode {
				m.loading = true
				m.statusMessage = "Loading remote PRs..."
				return m, tea.Batch(loadRemotePullRequestsCmd(m.services, m.cfg), m.spinner.Tick)
			}
			m.loading = true
			m.statusMessage = "Refreshing worktrees..."
			return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
		}
		if m.prMode && key.Matches(msg, m.keys.StateFilter) {
			m.cyclePRStateFilter()
			m.applyPRFilter()
			m.statusMessage = m.prFilterSummary()
			m.statusError = false
			return m, nil
		}
		if m.prMode && key.Matches(msg, m.keys.UserFilter) {
			m.prUserOptions = append([]string{"all"}, m.services.RemotePRUsers(m.remotePRs)...)
			m.prUserCursor = m.currentPRUserIndex()
			m.selectingPRUser = true
			m.statusMessage = "Filter PRs by user"
			m.statusError = false
			return m, nil
		}
		if m.prMode && key.Matches(msg, m.keys.MineFilter) {
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
		if m.prMode && key.Matches(msg, m.keys.BranchView) {
			m.prShowBranch = !m.prShowBranch
			m.rebuildRows()
			m.statusMessage = map[bool]string{true: "Showing PR branches", false: "Showing PR titles"}[m.prShowBranch]
			m.statusError = false
			return m, nil
		}
		if m.prMode && key.Matches(msg, m.keys.FailedGHA) {
			m.prShowFailedGHA = !m.prShowFailedGHA
			m.updateDetailContent()
			m.statusMessage = map[bool]string{true: "Showing failed GitHub Actions in Details", false: "Hiding failed GitHub Actions"}[m.prShowFailedGHA]
			m.statusError = false
			return m, nil
		}
		if m.prMode && key.Matches(msg, m.keys.AskPR) {
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
		if key.Matches(msg, m.keys.ToggleAll) {
			if m.prMode {
				m.statusMessage = "Toggle all worktrees is unavailable in PR Radar"
				m.statusError = true
				return m, nil
			}
			m.clearDeleteArm()
			m.showAll = !m.showAll
			m.loading = true
			m.statusMessage = map[bool]string{true: "Showing all repo worktrees", false: "Showing managed worktrees only"}[m.showAll]
			m.statusError = false
			m.rememberSelection()
			return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
		}
		if key.Matches(msg, m.keys.ToggleBody) {
			m.showPRBody = !m.showPRBody
			m.updateDetailContent()
			return m, nil
		}
		if key.Matches(msg, m.keys.OpenPR) {
			if m.prMode {
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
			if m.prMode {
				selected := m.selectedRemotePRForDetail()
				if selected == nil {
					m.statusMessage = "Select a remote PR first"
					m.statusError = true
					return m, nil
				}
				m.loading = true
				m.statusMessage = fmt.Sprintf("Preparing PR #%d worktree for VS Code...", selected.Number)
				m.statusError = false
				return m, tea.Batch(openPRWorktreeInVSCodeCmd(m.services, m.services, m.cfg, *selected), m.spinner.Tick)
			}
			selected := m.selectedWorktree()
			if selected == nil {
				return m, nil
			}
			if selected.Missing || selected.Prunable {
				m.statusMessage = fmt.Sprintf("Cannot open missing worktree %s", selected.Name)
				m.statusError = true
				return m, nil
			}
			return m, openVSCodeWorktreeCmd(m.services, *selected)
		}
		if key.Matches(msg, m.keys.OpenAgent) {
			if m.prMode {
				selected := m.selectedRemotePRForDetail()
				if selected == nil {
					m.statusMessage = "Select a remote PR first"
					m.statusError = true
					return m, nil
				}
				agent := m.defaultAgent()
				if agent == nil {
					m.statusMessage = "No AI agent configured"
					m.statusError = true
					return m, nil
				}
				m.loading = true
				m.statusMessage = fmt.Sprintf("Preparing PR #%d worktree for %s...", selected.Number, agent.Name)
				m.statusError = false
				return m, tea.Batch(openPRWorktreeAgentCmd(m.services, m.services, m.cfg, *selected, *agent), m.spinner.Tick)
			}
			selected := m.selectedWorktree()
			if selected == nil {
				return m, nil
			}
			if selected.Missing || selected.Prunable {
				m.statusMessage = fmt.Sprintf("Cannot open missing worktree %s", selected.Name)
				m.statusError = true
				return m, nil
			}
			agent := m.defaultAgent()
			if agent == nil {
				m.statusMessage = "No AI agent configured"
				m.statusError = true
				return m, nil
			}
			return m, openAgentCmd(m.services, *selected, *agent)
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
			if m.prMode {
				selected := m.selectedRemotePRForDetail()
				if selected == nil || strings.TrimSpace(selected.HeadRefName) == "" {
					m.statusMessage = "Selected PR has no branch name"
					m.statusError = true
					return m, nil
				}
				branch := selected.HeadRefName
				m.loading = true
				m.selectedPath = m.services.CreateWorktreePath(m.cfg, branch)
				m.statusMessage = fmt.Sprintf("Creating worktree for PR #%d (%s)...", selected.Number, branch)
				m.statusError = false
				return m, tea.Batch(createWorktreeCmd(m.services, m.cfg, branch), m.spinner.Tick)
			}
			m.creatingWorktree = true
			m.newInput.SetValue("")
			m.newInput.Focus()
			m.statusMessage = "Enter branch/worktree name"
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.Delete) {
			if m.prMode {
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
				m.loading = true
				m.statusMessage = fmt.Sprintf("Deleting %s...", wt.Name)
				m.statusError = false
				return m, tea.Batch(removeWorktreeCmd(m.services, m.cfg, wt), m.spinner.Tick)
			}

			m.deleteArmedPath = selected.Path
			m.deleteArmedName = selected.Name
			m.deleteArmedAt = now
			m.statusMessage = fmt.Sprintf("Press d again to delete %s", selected.Name)
			m.statusError = false
			return m, expireDeleteArmCmd(selected.Path, now)
		}
	}

	if m.focusTable {
		before := m.table.Cursor()
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		if m.table.Cursor() != before {
			m.clearDeleteArm()
			if m.prMode {
				m.movePRSelection(0)
				selected := m.selectedRemotePR()
				if selected != nil {
					return m.startPRDetailLoad(selected.Number)
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
		m.statusMessage = "New worktree cancelled"
		m.statusError = false
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.newInput.Value())
		if name == "" {
			m.statusMessage = "Branch/worktree name is required"
			m.statusError = true
			return m, nil
		}

		m.creatingWorktree = false
		m.newInput.Blur()
		m.loading = true
		m.selectedPath = m.services.CreateWorktreePath(m.cfg, name)
		m.statusMessage = fmt.Sprintf("Creating %s...", name)
		m.statusError = false
		return m, tea.Batch(createWorktreeCmd(m.services, m.cfg, name), m.spinner.Tick)
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
		m.statusMessage = "PR question cancelled"
		m.statusError = false
		return m, nil
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
		m.loading = true
		m.statusMessage = fmt.Sprintf("Asking %s about PR #%d...", agent.Name, selected.Number)
		m.statusError = false
		return m, tea.Batch(askPullRequestAgentCmd(m.services, m.cfg, *agent, *selected, question), m.spinner.Tick)
	}

	var cmd tea.Cmd
	m.prQuestionInput, cmd = m.prQuestionInput.Update(msg)
	return m, cmd
}

func (m model) updatePRUserSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingPRUser = false
		m.statusMessage = "PR user filter cancelled"
		m.statusError = false
		return m, nil
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
		m.statusMessage = "Help closed"
		m.statusError = false
		return m, nil
	}
	return m, nil
}

func (m model) updateAgentSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingAgent = false
		m.statusMessage = "AI agent selection cancelled"
		m.statusError = false
		return m, nil
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
		return m, saveAgentConfigCmd(m.services, m.cfg)
	}

	return m, nil
}

func (m model) updateProfileSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.selectingProfile = false
		m.statusMessage = "Repository profile selection cancelled"
		m.statusError = false
		return m, nil
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
		m.cfg.RepoSlug = resolved.GitHubRepo
		if m.cfg.RepoSlug == "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			m.cfg.RepoSlug = m.services.InferGitHubRepo(ctx, resolved)
			cancel()
		}
		m.selectingProfile = false
		m.showAll = false
		m.showPRBody = false
		m.filtering = false
		m.creatingWorktree = false
		m.filterQuery = ""
		m.filterInput.SetValue("")
		m.selectedPath = ""
		m.selectedIndex = 0
		m.clearDeleteArm()
		m.loading = true
		m.statusMessage = fmt.Sprintf("Switching to %s...", profile.Name)
		m.statusError = false
		return m, tea.Batch(saveAppConfigCmd(m.services, m.cfg), loadWorktreesCmd(m.services, m.cfg, m.showAll), m.spinner.Tick)
	}

	return m, nil
}

func (m model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		m.filtering = false
		m.filterInput.Blur()
		m.statusMessage = m.filterSummary()
		m.statusError = false
		return m, nil
	}

	m.rememberSelection()
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filterQuery = strings.TrimSpace(m.filterInput.Value())
	m.applyFilter()
	m.statusMessage = m.filterSummary()
	m.statusError = false
	return m, cmd
}

func (m *model) applyFilter() {
	if m.prMode {
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

func (m *model) rebuildRows() {
	if m.prMode {
		m.rebuildPRRows()
		return
	}
	rows := make([]table.Row, 0, len(m.visibleWorktrees))
	compact := m.table.Width() <= 70
	for _, wt := range m.visibleWorktrees {
		commit := wt.Commit.Subject
		if wt.Commit.Relative != "" {
			commit = fmt.Sprintf("%s %s", wt.Commit.Relative, wt.Commit.Subject)
		}
		if compact {
			rows = append(rows, table.Row{
				wt.Name,
				truncateText(wt.Branch, 20),
				stateLabel(wt),
				shortPR(wt.PR),
			})
			continue
		}
		rows = append(rows, table.Row{
			wt.Name,
			truncateText(wt.Branch, 28),
			stateLabel(wt),
			changeSummary(wt.Status),
			shortPR(wt.PR),
			truncateText(commit, 48),
		})
	}
	m.table.SetRows(rows)
	if len(rows) == 0 {
		if compact {
			m.table.SetRows([]table.Row{{"-", "-", "-", "No worktrees found"}})
		} else {
			m.table.SetRows([]table.Row{{"-", "-", "-", "-", "-", "No worktrees found"}})
		}
	}
}

func (m *model) rebuildPRRows() {
	rows := make([]table.Row, 0, len(m.visiblePRs))
	compact := m.table.Width() <= 70
	for _, pr := range m.visiblePRs {
		if compact {
			rows = append(rows, table.Row{
				fmt.Sprintf("#%d", pr.Number),
				truncateText(pr.Author.Login, 16),
				prStateLabel(pr),
				truncateText(m.prHeadline(pr), 48),
			})
			continue
		}
		rows = append(rows, table.Row{
			fmt.Sprintf("#%d", pr.Number),
			truncateText(pr.Author.Login, 18),
			prStateLabel(pr),
			checksSummary(pr.StatusCheckRollup),
			reviewReadiness(pr),
			shortTime(pr.UpdatedAt),
			truncateText(m.prHeadline(pr), 60),
		})
	}
	m.table.SetRows(rows)
	if len(rows) == 0 {
		if compact {
			m.table.SetRows([]table.Row{{"-", "-", "-", "No PRs found"}})
		} else {
			m.table.SetRows([]table.Row{{"-", "-", "-", "-", "-", "-", "No PRs found"}})
		}
	}
}

func (m *model) resize() {
	if !m.ready {
		return
	}

	usableWidth := max(20, m.width-appStyle.GetHorizontalFrameSize())
	cardFrameWidth := cardStyle.GetHorizontalFrameSize()
	availableHeight := max(10, m.height-8)
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
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.updateDetailContent()
}

func (m model) activeColumns() []table.Column {
	if m.prMode {
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

func (m *model) selectedWorktreePath() string {
	selected := m.selectedWorktree()
	if selected == nil {
		return ""
	}
	return selected.Path
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

func (m model) defaultProfileIndex() int {
	for idx, profile := range m.cfg.App.Profiles {
		if profile.Name == m.cfg.App.DefaultProfile {
			return idx
		}
	}
	return 0
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

	fallback := min(max(m.selectedIndex-1, 0), len(m.visibleWorktrees)-1)
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

	if m.prMode {
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
		m.detail.SetContent(remotePRDetailViewContent(selected, m.prDetailLoading, m.prShowFailedGHA, m.prChatQuestion, m.prChatAnswer, m.detail.Width))
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
	m.updateDetailContent()
}

func (m model) loadSummary() string {
	if m.filterQuery != "" {
		return fmt.Sprintf("Loaded %d worktrees, %d match %q", len(m.allWorktrees), len(m.visibleWorktrees), m.filterQuery)
	}
	return fmt.Sprintf("Loaded %d worktrees", len(m.allWorktrees))
}

func (m model) filterSummary() string {
	if m.prMode {
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

func buildColumns(totalWidth int) []table.Column {
	if totalWidth <= 70 {
		widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*4, []int{8, 10, 5, 5}, []int{16, 20, 10, 12}, []int{1, 0, 3, 2})
		return []table.Column{{Title: "Worktree", Width: widths[0]}, {Title: "Branch", Width: widths[1]}, {Title: "State", Width: widths[2]}, {Title: "PR", Width: widths[3]}}
	}
	widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*6, []int{12, 16, 5, 5, 6, 12}, []int{18, 26, 8, 8, 10, 18}, []int{5, 1, 0, 4, 2, 3})
	return []table.Column{{Title: "Worktree", Width: widths[0]}, {Title: "Branch", Width: widths[1]}, {Title: "State", Width: widths[2]}, {Title: "Delta", Width: widths[3]}, {Title: "PR", Width: widths[4]}, {Title: "Last Commit", Width: widths[5]}}
}

func buildPRColumns(totalWidth int) []table.Column {
	if totalWidth <= 70 {
		widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*4, []int{5, 8, 5, 12}, []int{8, 14, 10, 26}, []int{3, 1, 2, 0})
		return []table.Column{{Title: "PR", Width: widths[0]}, {Title: "Author", Width: widths[1]}, {Title: "State", Width: widths[2]}, {Title: "Title", Width: widths[3]}}
	}
	widths := distributeColumnWidths(totalWidth-tableCellStyle.GetHorizontalFrameSize()*7, []int{5, 8, 5, 6, 6, 8, 14}, []int{8, 14, 8, 10, 10, 12, 30}, []int{6, 1, 4, 3, 5, 2, 0})
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

func checksSummary(checks []StatusCheck) string {
	if len(checks) == 0 {
		return "-"
	}
	failing := 0
	pending := 0
	passing := 0
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
		fmt.Sprintf("State: %s", stateLabel(*wt)),
		fmt.Sprintf("Tracking: %s", trackingSummary(wt.Status)),
		fmt.Sprintf("Changes: staged=%d unstaged=%d untracked=%d conflicts=%d", wt.Status.Staged, wt.Status.Unstaged, wt.Status.Untracked, wt.Status.Conflicts),
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

	lines = append(lines, "", "Changed files:")
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

	lines = append(lines, "", "Pull request:")
	if wt.PR == nil {
		lines = append(lines, "  none")
	} else {
		author := wt.PR.Author.Login
		if wt.PR.Author.Name != "" {
			author = fmt.Sprintf("%s (%s)", wt.PR.Author.Login, wt.PR.Author.Name)
		}
		lines = append(lines,
			fmt.Sprintf("  #%d %s", wt.PR.Number, strings.ToLower(wt.PR.State)),
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

func remotePRDetailViewContent(pr *RemotePullRequest, loading bool, showFailedGHA bool, chatQuestion string, chatAnswer string, width int) string {
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
		detailField("State", prStateLabel(*pr), prStateCellStyle(*pr), contentWidth),
		detailField("Author", emptyDash(pr.Author.Login), magentaStyle, contentWidth),
		detailField("Branch", fmt.Sprintf("%s -> %s", emptyDash(pr.HeadRefName), emptyDash(pr.BaseRefName)), purpleStyle, contentWidth),
		detailField("Review", shortReviewDecision(pr.ReviewDecision), mutedStyle, contentWidth),
		detailField("Checks", checksSummary(pr.StatusCheckRollup), checksCellStyle(pr.StatusCheckRollup), contentWidth),
		detailField("Ready", reviewReadiness(*pr), readinessCellStyle(*pr), contentWidth),
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
	if loading {
		lines = append(lines, "", styledWrappedLine("Loading PR details...", infoStyle, contentWidth))
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

	lines = append(lines, "", styledWrappedLine("Actions: o opens PR, n creates worktree, f toggles failed checks, c asks agent", subtleStyle, contentWidth))
	return strings.Join(lines, "\n")
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
	return titleStyle.Render(title + ":")
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

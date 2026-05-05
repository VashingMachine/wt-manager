package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

type keyMap struct {
	Up          key.Binding
	Down        key.Binding
	Focus       key.Binding
	Refresh     key.Binding
	ToggleAll   key.Binding
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
	return []key.Binding{k.Refresh, k.Filter, k.New, k.ProfileMenu, k.Setup, k.OpenPR, k.OpenCode, k.OpenAgent, k.AgentMenu, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Focus, k.Refresh, k.ToggleAll, k.Filter}, {k.New, k.ProfileMenu, k.Setup, k.ToggleBody, k.OpenPR, k.OpenCode, k.OpenAgent, k.AgentMenu, k.Delete, k.Quit}}
}

type model struct {
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
	width            int
	height           int
	showAll          bool
	showPRBody       bool
	loading          bool
	filtering        bool
	creatingWorktree bool
	selectingAgent   bool
	selectingProfile bool
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

func newModel(cfg Config) model {
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

	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = subtleStyle.Copy()

	h := help.New()
	h.ShowAll = false

	m := model{
		cfg:           cfg,
		keys:          defaultKeys(),
		help:          h,
		table:         tbl,
		detail:        vp,
		filterInput:   filterInput,
		newInput:      newInput,
		spinner:       spin,
		loading:       true,
		focusTable:    true,
		statusMessage: "Loading worktrees...",
	}
	if cfg.SetupNeeded {
		setup := newSetupModel(cfg, true)
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
	return tea.Batch(loadWorktreesCmd(m.cfg, m.showAll), m.spinner.Tick)
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
		return m, tea.Batch(loadWorktreesCmd(m.cfg, m.showAll), m.spinner.Tick)
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
		return m, tea.Batch(loadWorktreesCmd(m.cfg, m.showAll), m.spinner.Tick)
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
		return m, tea.Batch(loadWorktreesCmd(m.cfg, m.showAll), m.spinner.Tick)
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

		if m.creatingWorktree {
			return m.updateNewWorktree(msg)
		}

		if m.filtering {
			return m.updateFilter(msg)
		}

		if msg.String() == "esc" {
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

		if m.focusTable {
			if key.Matches(msg, m.keys.Up) {
				m.clearDeleteArm()
				m.moveSelection(-1)
				return m, nil
			}
			if key.Matches(msg, m.keys.Down) {
				m.clearDeleteArm()
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
			m.rememberSelection()
			return m, tea.Batch(loadWorktreesCmd(m.cfg, m.showAll), m.spinner.Tick)
		}
		if key.Matches(msg, m.keys.ToggleAll) {
			m.clearDeleteArm()
			m.showAll = !m.showAll
			m.loading = true
			m.statusMessage = map[bool]string{true: "Showing all repo worktrees", false: "Showing managed worktrees only"}[m.showAll]
			m.statusError = false
			m.rememberSelection()
			return m, tea.Batch(loadWorktreesCmd(m.cfg, m.showAll), m.spinner.Tick)
		}
		if key.Matches(msg, m.keys.ToggleBody) {
			m.showPRBody = !m.showPRBody
			m.updateDetailContent()
			return m, nil
		}
		if key.Matches(msg, m.keys.OpenPR) {
			selected := m.selectedWorktree()
			if selected == nil || selected.PR == nil {
				m.statusMessage = "Selected worktree has no PR"
				m.statusError = true
				return m, nil
			}
			return m, openBrowserCmd(selected.PR.URL)
		}
		if key.Matches(msg, m.keys.OpenCode) {
			selected := m.selectedWorktree()
			if selected == nil {
				return m, nil
			}
			if selected.Missing || selected.Prunable {
				m.statusMessage = fmt.Sprintf("Cannot open missing worktree %s", selected.Name)
				m.statusError = true
				return m, nil
			}
			return m, openVSCodeWorktreeCmd(*selected)
		}
		if key.Matches(msg, m.keys.OpenAgent) {
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
			return m, openAgentInGhosttyCmd(*selected, *agent)
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
			if currentRepo, ok := inferRepoFromCWD(); ok {
				setupCfg.SetupRepo = currentRepo
			} else {
				setupCfg.SetupRepo = m.cfg.ActiveProfile.RepositoryPath
			}
			setupCfg.SetupReason = "Add or update a repository profile"
			setup := newSetupModel(setupCfg, false)
			m.setup = &setup
			m.loading = false
			m.statusMessage = "Setup"
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.New) {
			m.clearDeleteArm()
			m.creatingWorktree = true
			m.newInput.SetValue("")
			m.newInput.Focus()
			m.statusMessage = "Enter branch/worktree name"
			m.statusError = false
			return m, nil
		}
		if key.Matches(msg, m.keys.Delete) {
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
				return m, tea.Batch(removeWorktreeCmd(m.cfg, wt), m.spinner.Tick)
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
			m.moveSelection(0)
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
		m.selectedPath = createWorktreePath(m.cfg, name)
		m.statusMessage = fmt.Sprintf("Creating %s...", name)
		m.statusError = false
		return m, tea.Batch(createWorktreeCmd(m.cfg, name), m.spinner.Tick)
	}

	var cmd tea.Cmd
	m.newInput, cmd = m.newInput.Update(msg)
	return m, cmd
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
		return m, saveAgentConfigCmd(m.cfg)
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
		resolved := resolveProfilePaths(profile, m.cfg.HomeDir)
		m.cfg.App.DefaultProfile = profile.Name
		m.cfg.ActiveProfile = resolved
		m.cfg.RepoSlug = resolved.GitHubRepo
		if m.cfg.RepoSlug == "" {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			m.cfg.RepoSlug = inferGitHubRepo(ctx, resolved)
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
		return m, tea.Batch(saveAppConfigCmd(m.cfg), loadWorktreesCmd(m.cfg, m.showAll), m.spinner.Tick)
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

func (m *model) rememberSelection() {
	m.selectedIndex = m.table.Cursor()
	m.selectedPath = m.selectedWorktreePath()
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

func (m *model) resize() {
	if !m.ready {
		return
	}

	availableHeight := max(10, m.height-8)
	stacked := m.width < 130
	if stacked {
		m.table.SetHeight(max(6, availableHeight/2))
		m.table.SetWidth(max(40, m.width-4))
		m.detail.Width = max(20, m.width-4)
		m.detail.Height = max(5, availableHeight-m.table.Height())
	} else {
		tableWidth := max(60, int(float64(m.width)*0.56)-2)
		detailWidth := max(36, m.width-tableWidth-6)
		m.table.SetHeight(availableHeight)
		m.table.SetWidth(tableWidth)
		m.detail.Width = detailWidth - 2
		m.detail.Height = availableHeight - 2
	}

	m.filterInput.Width = max(24, min(56, m.width/3))
	m.newInput.Width = max(24, min(54, m.width-18))
	m.table.SetColumns(buildColumns(m.table.Width()))
	m.rebuildRows()
	m.updateDetailContent()
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

func (m *model) selectedWorktreePath() string {
	selected := m.selectedWorktree()
	if selected == nil {
		return ""
	}
	return selected.Path
}

func (m model) defaultAgent() *AgentTool {
	return findAgent(m.cfg.App.Agents, m.cfg.App.DefaultAgent)
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

func (m *model) updateDetailContent() {
	if !m.ready {
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

func (m model) loadSummary() string {
	if m.filterQuery != "" {
		return fmt.Sprintf("Loaded %d worktrees, %d match %q", len(m.allWorktrees), len(m.visibleWorktrees), m.filterQuery)
	}
	return fmt.Sprintf("Loaded %d worktrees", len(m.allWorktrees))
}

func (m model) filterSummary() string {
	if m.filterQuery == "" {
		return fmt.Sprintf("Showing %d worktrees", len(m.visibleWorktrees))
	}
	return fmt.Sprintf("Showing %d/%d worktrees matching %q", len(m.visibleWorktrees), len(m.allWorktrees), m.filterQuery)
}

func buildColumns(totalWidth int) []table.Column {
	if totalWidth <= 70 {
		return []table.Column{{Title: "Worktree", Width: 16}, {Title: "Branch", Width: 20}, {Title: "State", Width: 10}, {Title: "PR", Width: 12}}
	}
	commitWidth := max(18, totalWidth-84)
	return []table.Column{{Title: "Worktree", Width: 18}, {Title: "Branch", Width: 26}, {Title: "State", Width: 10}, {Title: "Delta", Width: 10}, {Title: "PR", Width: 12}, {Title: "Last Commit", Width: commitWidth}}
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

	return strings.Join(lines, "\n")
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

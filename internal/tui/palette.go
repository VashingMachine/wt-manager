package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

type paletteEntry struct {
	ID, Title, Description, Category, Shortcut string
	Key                                        string
}

type paletteWorktreesResult struct {
	Worktrees []Worktree
	Project   string
	Request   int
	Err       error
}

// Keep the palette from interrupting a draft or an existing confirmation.
func (m model) hasTextOrConfirmationModal() bool {
	return m.filtering || m.creatingWorktree || m.askingPR || m.confirmingPRApproval ||
		m.enteringTicketStatus || m.commentingTicket || m.enteringTicketAssignee ||
		m.selectingAgent || m.selectingEnterAction || m.selectingProfile || m.selectingPRUser ||
		m.pickingRepository || m.addingRepository != nil ||
		m.selectingTicketFilters || m.selectingTicketStatus || m.selectingTicketAssignee || m.selectingBoard
}

func (m model) openCommandPalette() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.paletteOpen = true
	m.showingHelp = false
	m.paletteCursor = 0
	m.paletteInput.SetValue("")
	// Searchable worktrees only need names, branches and paths: show the
	// cached list now and refresh it with discovery alone (no status, no gh).
	m.paletteWorktrees, _ = peekProjectWorktrees(m.services, m.cfg, true)
	m.paletteRequest++
	request, cfg, services := m.paletteRequest, m.cfg, m.services
	return m, tea.Batch(m.paletteInput.Focus(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		worktrees, _, err := discoverProjectWorktrees(ctx, services, cfg, true)
		return paletteWorktreesResult{Worktrees: worktrees, Project: projectKey(cfg), Request: request, Err: err}
	})
}

func (m model) paletteEntries() []paletteEntry {
	entries := []paletteEntry{
		{ID: "refresh", Title: "Refresh current view", Description: "Reload worktrees, pull requests, or tickets", Category: "Commands", Shortcut: "r", Key: "r"},
		{ID: "worktrees", Title: "Show worktrees", Description: "Browse the active repository's local worktrees", Category: "Commands"},
		{ID: "prs", Title: "Show PR Radar", Description: "Browse GitHub pull requests", Category: "Commands", Shortcut: "P"},
		{ID: "filter", Title: "Filter current view", Description: "Search the table's rows", Category: "Commands", Shortcut: "/", Key: "/"},
		{ID: "focus", Title: "Switch table / details focus", Category: "Commands", Shortcut: "Tab", Key: "tab"},
		{ID: "help", Title: "Show keyboard shortcuts", Description: "Keybinding help", Category: "Commands", Shortcut: "h", Key: "h"},
		{ID: "logs", Title: "View session logs", Description: "Inspect temporary execution logs, command output, and errors", Category: "Commands"},
		{ID: "editor", Title: "Configure editor", Description: "Choose the IDE opened by v: VS Code, IntelliJ IDEA, or a custom command", Category: "Settings"},
		{ID: "agent-menu", Title: "Choose default AI agent", Category: "Settings", Shortcut: "m", Key: "m"},
		{ID: "profile-menu", Title: "Switch project", Description: "Choose another repository profile", Category: "Profiles", Shortcut: "s", Key: "s"},
		{ID: "setup", Title: "Edit project " + m.cfg.ActiveProfile.Name + "…", Description: "Guided setup: repositories, agents, GitHub, and JIRA", Category: "Profiles", Shortcut: "I", Key: "I"},
		{ID: "project-new", Title: "New project…", Description: "Set up another project with its repositories, agents, and JIRA boards", Category: "Profiles"},
		{ID: "config-edit", Title: "Edit configuration file", Description: "Open " + m.services.CompactPath(m.cfg.ConfigPath, m.cfg.HomeDir) + " in " + m.configuredEditor().Name + "; saved changes apply automatically", Category: "Settings"},
		{ID: "config-reload", Title: "Reload configuration", Description: "Read " + m.services.CompactPath(m.cfg.ConfigPath, m.cfg.HomeDir) + " again", Category: "Settings"},
		{ID: "repo-add", Title: "Add repository to " + m.cfg.ActiveProfile.Name + "…", Description: "List another repository's worktrees in this project and create worktrees in it", Category: "Profiles"},
		{ID: "quit", Title: "Quit wt-manager", Category: "Commands", Shortcut: "q", Key: "q"},
	}
	if editor := terminalEditor(os.Getenv); editor != "" {
		entries = append(entries, paletteEntry{ID: "config-edit-terminal", Title: "Edit configuration file in terminal", Description: "Open the config in " + editor + " inside this terminal", Category: "Settings"})
	}
	if m.cfg.MultiRepository() {
		entries = append(entries,
			paletteEntry{ID: "repo-remove", Title: "Remove repository from " + m.cfg.ActiveProfile.Name + "…", Description: "Stop listing a repository in this project; files stay on disk", Category: "Profiles"},
			paletteEntry{ID: "pr-repo", Title: "Choose PR Radar repository", Description: "Currently " + m.prCfg().RepositoryName(), Category: "Profiles", Shortcut: "R"},
		)
	}
	add := func(id, title, description, shortcut, key string) {
		entries = append(entries, paletteEntry{ID: id, Title: title, Description: description, Category: "Commands", Shortcut: shortcut, Key: key})
	}
	if jira := m.cfg.ActiveProfile.Jira; jira != nil && jira.ActiveBoard() != nil {
		add("tickets", "Show JIRA tickets", "Browse configured boards", "J", "")
	}
	if m.filterQuery != "" {
		add("clear-filter", "Clear current filter", m.filterQuery, "Esc", "")
	}
	if m.isWorktreeMode() {
		add("toggle-all", "Toggle all / managed worktrees", "Include the main repository and unmanaged worktrees", "a", "a")
		add("enter-menu", "Choose default Enter action", "Editor, AI agent, or terminal", "e", "e")
		add("new", "Create a worktree", "Choose a branch name", "n", "n")
		add("body", "Toggle PR description", "Expand the local pull request description", "p", "p")
		if selected := m.selectedWorktree(); selected != nil {
			add("open", "Open selected worktree", m.configuredEnterActionLabel(), "Enter", "enter")
			add("terminal", "Open selected worktree in terminal", "Start a shell without an AI agent", "", "")
			if !selected.IsMain {
				add("delete", "Delete selected worktree…", "Arms deletion; press d again to confirm", "d d", "d")
			}
		}
	}
	if m.isPRMode() {
		add("state", "Cycle PR state filter", "Open, all, closed, or merged", "S", "S")
		add("user", "Choose PR user filter", "Author, assignee, or requested reviewer", "u", "u")
		add("mine", "Toggle my pull requests", "Filter by your GitHub account", "Y", "Y")
		add("branch", "Toggle PR title / branch column", "", "b", "b")
		add("checks", "Toggle failed GitHub Actions", "Show failed checks in details", "f", "f")
		add("diff", "Open selected PR diff", "Full-screen source diff", "Enter", "enter")
		add("approve", "Approve selected pull request…", "Review the confirmation before submitting", "A", "A")
		add("ask", "Ask agent about selected PR", "", "c", "c")
		add("new", "Create worktree from selected PR", "", "n", "n")
	}
	if m.isTicketMode() {
		add("ticket-read", "Read selected JIRA ticket", "Full-screen ticket reader", "Enter", "enter")
		add("ticket-filters", "Configure ticket filters", "Assigned to me, active sprint, open tickets", "f", "f")
		add("ticket-mine", "Toggle tickets assigned to me", "", "Y", "Y")
		add("ticket-sprint", "Toggle active sprint tickets", "", "S", "S")
		add("ticket-board", "Choose JIRA board", "", "b", "b")
		add("ticket-status", "Change selected ticket status", "", "t", "t")
		add("ticket-comment", "Comment on selected ticket", "", "C", "C")
		add("ticket-assign", "Assign selected ticket", "", "A", "A")
		add("new", "Create worktree from selected ticket", "", "n", "n")
	}
	add("open-editor", "Open selection in "+m.configuredEditor().Name, "Open or create the selected worktree in your editor", "v", "v")
	add("open-agent", "Open selection with AI agent", m.cfg.App.DefaultAgent, "i", "i")
	add("open-browser", "Open selected PR / ticket in browser", "", "o", "o")
	for idx, option := range enterActionOptions {
		label := option.Label
		if option.Action == core.EnterActionVSCode {
			label = m.configuredEditor().Name
		}
		entries = append(entries, paletteEntry{ID: fmt.Sprintf("enter:%d", idx), Title: "Set Enter action: " + label, Description: option.Description, Category: "Settings"})
	}
	for idx, agent := range m.cfg.App.Agents {
		entries = append(entries, paletteEntry{ID: fmt.Sprintf("agent:%d", idx), Title: "Use " + agent.Name + " as default agent", Description: agent.Command, Category: "Settings"})
	}
	for idx, editor := range editorPresets {
		entries = append(entries, paletteEntry{ID: fmt.Sprintf("editor:%d", idx), Title: "Use " + editor.Name + " as editor", Description: editor.Command, Category: "Settings"})
	}
	for idx, profile := range m.cfg.App.Profiles {
		entries = append(entries, paletteEntry{ID: fmt.Sprintf("profile:%d", idx), Title: "Switch to " + profile.Name, Description: profile.RepositoryPath, Category: "Profiles"})
	}
	seen := make(map[string]bool)
	for _, worktrees := range [][]Worktree{m.allWorktrees, m.paletteWorktrees} {
		for _, wt := range worktrees {
			if seen[wt.Path] {
				continue
			}
			seen[wt.Path] = true
			if _, deleting := m.pendingDeletes[wt.Path]; deleting {
				continue
			}
			description := wt.Branch + " · " + wt.Path
			if m.cfg.MultiRepository() {
				description = m.worktreeRepository(wt) + " · " + description
			}
			entries = append(entries, paletteEntry{ID: "worktree:" + wt.Path, Title: wt.Name, Description: description, Category: "Worktrees"})
		}
	}
	return entries
}

func (m model) paletteMatches() []paletteEntry {
	entries := m.paletteEntries()
	query := strings.ToLower(strings.TrimSpace(m.paletteInput.Value()))
	if query == "" {
		return entries
	}
	type scoredEntry struct {
		entry paletteEntry
		score int
	}
	var matches []scoredEntry
	for _, entry := range entries {
		if score, ok := paletteMatchScore(entry, query); ok {
			matches = append(matches, scoredEntry{entry, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	result := make([]paletteEntry, len(matches))
	for idx, match := range matches {
		result[idx] = match.entry
	}
	return result
}

func paletteMatchScore(entry paletteEntry, query string) (int, bool) {
	title := strings.ToLower(entry.Title)
	text := strings.ToLower(entry.Title + " " + entry.Description + " " + entry.Category + " " + entry.Shortcut)
	score := 0
	for _, term := range strings.Fields(query) {
		switch {
		case strings.Contains(title, term):
			score += 100
		case strings.Contains(text, term):
			score += 20
		case paletteSubsequence(term, text):
			score++
		default:
			return 0, false
		}
	}
	if title == query {
		score += 1000
	} else if strings.HasPrefix(title, query) {
		score += 500
	} else if strings.Contains(title, query) {
		score += 200
	}
	return score, true
}

func paletteSubsequence(query, text string) bool {
	wanted := []rune(query)
	idx := 0
	for _, r := range text {
		if idx < len(wanted) && unicode.ToLower(r) == wanted[idx] {
			idx++
		}
	}
	return idx == len(wanted)
}

func (m model) updateCommandPalette(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	matches := m.paletteMatches()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "ctrl+p":
		m.paletteOpen = false
		m.paletteInput.Blur()
		return m, nil
	case "up", "ctrl+k":
		m.paletteCursor = max(0, m.paletteCursor-1)
		return m, nil
	case "down", "ctrl+j", "tab":
		m.paletteCursor = min(max(0, len(matches)-1), m.paletteCursor+1)
		return m, nil
	case "enter":
		if len(matches) == 0 {
			return m, nil
		}
		entry := matches[min(max(0, m.paletteCursor), len(matches)-1)]
		m.paletteOpen = false
		m.paletteInput.Blur()
		return m.executePaletteEntry(entry)
	}
	previous := m.paletteInput.Value()
	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	if previous != m.paletteInput.Value() {
		m.paletteCursor = 0
	}
	return m, cmd
}

func (m model) executePaletteEntry(entry paletteEntry) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(entry.ID, "worktree:") {
		path := strings.TrimPrefix(entry.ID, "worktree:")
		for _, worktrees := range [][]Worktree{m.allWorktrees, m.paletteWorktrees} {
			for _, wt := range worktrees {
				if wt.Path == path {
					return m.selectPaletteWorktree(wt)
				}
			}
		}
		return m, nil
	}
	if entry.ID == "logs" {
		return m.openSessionLogs()
	}
	m.logsOpen = false
	wasFullscreen := m.prDiffMode || m.ticketReadMode
	m.prDiffMode = false
	m.ticketReadMode = false
	if wasFullscreen {
		m.resize()
	}
	switch entry.ID {
	case "worktrees":
		return m.switchMode(modeWorktrees)
	case "prs":
		return m.switchMode(modePRs)
	case "tickets":
		return m.switchMode(modeTickets)
	case "editor":
		return m.openEditorSelector()
	case "terminal":
		return m.openSelectedWorktreeWithAction(core.EnterActionTerminal)
	case "clear-filter":
		m.filterQuery = ""
		m.filterInput.SetValue("")
		m.applyFilter()
		return m, m.setTransientStatus("Filter cleared")
	case "config-edit":
		return m.editConfigInEditor()
	case "config-edit-terminal":
		return m.editConfigInTerminal()
	case "config-reload":
		return m.reloadConfig()
	case "repo-add":
		return m.openAddRepository()
	case "project-new":
		return m.openSetup(newProjectSetupModel(m.cfg, m.services))
	case "repo-remove":
		return m.openRepositoryPicker(pickRepoToRemove, Ticket{}, "")
	case "pr-repo":
		return m.openRepositoryPicker(pickRepoForPRRadar, Ticket{}, "")
	}
	var idx int
	if _, err := fmt.Sscanf(entry.ID, "enter:%d", &idx); err == nil && idx >= 0 && idx < len(enterActionOptions) {
		option := enterActionOptions[idx]
		m.cfg.App.DefaultEnterAction = option.Action
		return m, m.persistConfig("Default Enter action set to " + m.configuredEnterActionLabel())
	}
	if _, err := fmt.Sscanf(entry.ID, "agent:%d", &idx); err == nil && idx >= 0 && idx < len(m.cfg.App.Agents) {
		m.cfg.App.DefaultAgent = m.cfg.App.Agents[idx].Name
		return m, m.persistConfig("Default AI agent set to " + m.cfg.App.DefaultAgent)
	}
	if _, err := fmt.Sscanf(entry.ID, "editor:%d", &idx); err == nil && idx >= 0 && idx < len(editorPresets) {
		return m.saveEditor(editorPresets[idx])
	}
	if _, err := fmt.Sscanf(entry.ID, "profile:%d", &idx); err == nil && idx >= 0 && idx < len(m.cfg.App.Profiles) {
		m.profileCursor = idx
		return m.updateProfileSelector(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if entry.Key == "" {
		return m, nil
	}
	keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(entry.Key)}
	switch entry.Key {
	case "tab":
		keyMsg = tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		keyMsg = tea.KeyMsg{Type: tea.KeyEnter}
	}
	return m.update(keyMsg)
}

func (m model) selectPaletteWorktree(wt Worktree) (tea.Model, tea.Cmd) {
	expandingScope := !m.showAll && !wt.Managed
	m.showAll = m.showAll || !wt.Managed
	m.filterQuery = ""
	m.filterInput.SetValue("")
	if !worktreeExists(m.allWorktrees, wt.Path) {
		m.allWorktrees = append(m.allWorktrees, wt)
	}
	var cmd tea.Cmd
	if !m.isWorktreeMode() {
		next, switchCmd := m.switchMode(modeWorktrees)
		m, cmd = next.(model), switchCmd
	} else if expandingScope {
		cmd = tea.Batch(loadWorktreesCmd(m.services, m.cfg, true), m.startLoading(loadingWorktrees, "Loading all repository worktrees..."))
	}
	m.prDiffMode = false
	m.ticketReadMode = false
	m.selectedPath = wt.Path
	if cmd != nil || m.loading {
		m.pendingWorktreePath = wt.Path
	}
	m.selectedIndex = 0
	m.focusTable = true
	m.table.Focus()
	m.applyFilter()
	m.resize()
	if cmd == nil {
		return m, m.setTransientStatus("Selected worktree " + wt.Name)
	}
	return m, cmd
}

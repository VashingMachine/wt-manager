package tui

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// paletteEntry is a row of the palette. ID names the command it runs, or the
// worktree it selects.
type paletteEntry struct {
	ID, Title, Description, Category, Shortcut string
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
		m.pickingRepository || m.choosingTicketWorktree || m.addingRepository != nil ||
		m.pickingStackParent || m.confirmingRestack || m.sweeping ||
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
	km := m.keymap()
	// Every entry shows the key its command has right now.
	entry := func(id, title, description, category string) paletteEntry {
		return paletteEntry{ID: id, Title: title, Description: description, Category: category, Shortcut: km.shortcut(id)}
	}
	configPath := m.services.CompactPath(m.cfg.ConfigPath, m.cfg.HomeDir)
	editorHint := "Choose the IDE the editor commands open: VS Code, IntelliJ IDEA, or a custom command"
	if label := km.label("open-editor"); label != "" {
		editorHint = "Choose the IDE opened by " + label + ": VS Code, IntelliJ IDEA, or a custom command"
	}
	entries := []paletteEntry{
		entry("refresh", "Refresh current view", "Reload worktrees, pull requests, or tickets", "Commands"),
		entry("worktrees", "Show worktrees", "Browse the active repository's local worktrees", "Commands"),
		entry("prs", "Show PR Radar", "Browse GitHub pull requests", "Commands"),
		entry("filter", "Filter current view", "Search the table's rows", "Commands"),
		entry("focus", "Switch table / details focus", "", "Commands"),
		entry(cmdHelp, "Show keyboard shortcuts", "Keybinding help", "Commands"),
		entry("logs", "View session logs", "Inspect temporary execution logs, command output, and errors", "Commands"),
		entry(cmdKeybindings, "Customize keyboard shortcuts…", "Change, add, or remove the keys of every command", "Settings"),
		entry("editor", "Configure editor", editorHint, "Settings"),
		entry("agent-menu", "Choose default AI agent", "", "Settings"),
		entry("profile-menu", "Switch project", "Choose another repository profile", "Profiles"),
		entry("setup", "Edit project "+m.cfg.ActiveProfile.Name+"…", "Guided setup: repositories, agents, GitHub, and JIRA", "Profiles"),
		entry("project-new", "New project…", "Set up another project with its repositories, agents, and JIRA boards or spaces", "Profiles"),
		entry("config-edit", "Edit configuration file", "Open "+configPath+" in "+m.configuredEditor().Name+"; saved changes apply automatically", "Settings"),
		entry("config-reload", "Reload configuration", "Read "+configPath+" again", "Settings"),
		entry("repo-add", "Add repository to "+m.cfg.ActiveProfile.Name+"…", "List another repository's worktrees in this project and create worktrees in it", "Profiles"),
		entry(cmdQuit, "Quit wt-manager", "", "Commands"),
	}
	if editor := terminalEditor(os.Getenv); editor != "" {
		entries = append(entries, entry("config-edit-terminal", "Edit configuration file in terminal", "Open the config in "+editor+" inside this terminal", "Settings"))
	}
	if m.cfg.MultiRepository() {
		entries = append(entries,
			entry("repo-remove", "Remove repository from "+m.cfg.ActiveProfile.Name+"…", "Stop listing a repository in this project; files stay on disk", "Profiles"),
			entry("pr-repo", "Choose PR Radar repository", "Currently "+m.prCfg().RepositoryName(), "Profiles"),
		)
	}
	add := func(id, title, description string) {
		entries = append(entries, entry(id, title, description, "Commands"))
	}
	if jira := m.cfg.ActiveProfile.Jira; jira != nil && jira.ActiveBoard() != nil {
		add("tickets", "Show JIRA tickets", "Browse configured boards and spaces")
	}
	if m.filterQuery != "" {
		add(cmdClearFilter, "Clear current filter", m.filterQuery)
	}
	if m.isWorktreeMode() {
		add("toggle-all", "Toggle all / managed worktrees", "Include the main repository and unmanaged worktrees")
		add("enter-menu", "Choose default Enter action", "Editor, AI agent, or terminal")
		add("new", "Create a worktree", "Choose a branch name")
		add("sweep", "Sweep stale worktrees…", fmt.Sprintf("Remove clean worktrees untouched for %d+ days", m.sweepDays()))
		add("body", "Toggle PR description", "Expand the local pull request description")
		if selected := m.selectedWorktree(); selected != nil {
			add("open", "Open selected worktree", m.configuredEnterActionLabel())
			add("terminal", "Open selected worktree in terminal", "Start a shell without an AI agent")
			if !selected.IsMain {
				confirm := "Arms deletion; run it again to confirm"
				if label := km.label(cmdDelete); label != "" {
					confirm = "Arms deletion; press " + label + " again to confirm"
				}
				add(cmdDelete, "Delete selected worktree…", confirm)
			}
			if m.stackParentProblem(*selected) == "" {
				add("stack-new", "Create a worktree on top of selected", "Start a new branch from "+selected.Branch+" (stacked)")
				add("stack-on", "Stack selected worktree on another…", "Record which worktree it was started from")
			}
			if selected.Stacked() {
				add("restack", "Restack selected worktree…", "Rebase its own commits onto "+selected.Stack.Parent+", or the default branch once that is merged")
				add("unstack", "Take selected worktree out of its stack", "Forget that it was started from "+selected.Stack.Parent)
			}
		}
	}
	if m.isPRMode() {
		add("state", "Cycle PR state filter", "Open, all, closed, or merged")
		add("user", "Choose PR user filter", "Author, assignee, or requested reviewer")
		add("mine", "Toggle my pull requests", "Filter by your GitHub account")
		add("branch", "Toggle PR title / branch column", "")
		add("checks", "Toggle failed GitHub Actions", "Show failed checks in details")
		add("diff", "Open selected PR diff", "Full-screen source diff")
		add("approve", "Approve selected pull request…", "Review the confirmation before submitting")
		add("ask", "Ask agent about selected PR", "")
		add("new", "Create worktree from selected PR", "")
	}
	if m.isTicketMode() {
		add("ticket-read", "Read selected JIRA ticket", "Full-screen ticket reader")
		add("ticket-filters", "Configure ticket filters", "Assigned to me, active sprint, open tickets")
		add("ticket-mine", "Toggle tickets assigned to me", "")
		add("ticket-sprint", "Toggle active sprint tickets", "")
		add("ticket-board", "Choose JIRA board or space", "")
		add("ticket-status", "Change selected ticket status", "")
		add("ticket-comment", "Comment on selected ticket", "")
		add("ticket-assign", "Assign selected ticket", "")
		add("new", "Create worktree from selected ticket", "")
		add("stack-new", "Create ticket worktree on top of another…", "Start the ticket's branch from an existing worktree (stacked)")
	}
	add("open-editor", "Open selection in "+m.configuredEditor().Name, "Open or create the selected worktree in your editor")
	add("open-agent", "Open selection with AI agent", m.cfg.App.DefaultAgent)
	add("open-browser", "Open selected PR / ticket in browser", "")
	for _, option := range enterActionOptions {
		label := option.Label
		if option.Action == core.EnterActionVSCode {
			label = m.configuredEditor().Name
		}
		entries = append(entries, entry(enterActionCommandID(option), "Set Enter action: "+label, option.Description, "Settings"))
	}
	for _, agent := range m.cfg.App.Agents {
		entries = append(entries, entry(cmdAgentPrefix+agent.Name, "Use "+agent.Name+" as default agent", agent.Command, "Settings"))
	}
	for _, editor := range editorPresets {
		entries = append(entries, entry(cmdEditorPrefix+editor.Command, "Use "+editor.Name+" as editor", editor.Command, "Settings"))
	}
	for _, profile := range m.cfg.App.Profiles {
		entries = append(entries, entry(cmdProfilePrefix+profile.Name, "Switch to "+profile.Name, profile.RepositoryPath, "Profiles"))
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
	if m.paletteToggleKey(msg) {
		m.paletteOpen = false
		m.paletteInput.Blur()
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
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
	// The palette shows a view; its key toggles between that view and worktrees.
	switch entry.ID {
	case "prs":
		return m.switchMode(modePRs)
	case "tickets":
		return m.switchMode(modeTickets)
	}
	return m.runCommand(entry.ID)
}

// paletteToggleKey reports the key that opened the palette being pressed
// again to close it. A key that types a character belongs to the search.
func (m model) paletteToggleKey(msg tea.KeyMsg) bool {
	name := keyName(msg)
	if name == "" || textKey(name) {
		return false
	}
	return slices.ContainsFunc(m.keymap().keysOf(cmdPalette), keySeq{name}.equal)
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

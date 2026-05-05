package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	appStyle         = lipgloss.NewStyle().Padding(1, 2)
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	subtleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	mutedStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	successStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	infoStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	magentaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("171"))
	purpleStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("141"))
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("204"))
	cardStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
	focusStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(0, 1)
	headerCellStyle  = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("252"))
	tableCellStyle   = lipgloss.NewStyle().Padding(0, 1)
	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Bold(true)
	statusStyle      = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("252"))
	statusErrorStyle = statusStyle.Copy().Foreground(lipgloss.Color("204"))
	modalStyle       = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2).Width(64)
)

func (m model) View() string {
	if !m.ready {
		return "Loading wt-manager..."
	}
	if m.setup != nil {
		return appStyle.Render(m.setup.View(m.width))
	}

	headerMeta := fmt.Sprintf("Profile: %s | Repo: %s | Agent: %s | Mode: %s | Filter: %s | Focus: %s", m.cfg.ActiveProfile.Name, m.cfg.ActiveProfile.RepositoryPath, m.cfg.App.DefaultAgent, m.modeLabel(), m.filterLabel(), m.focusLabel())
	if m.filterQuery != "" {
		headerMeta += fmt.Sprintf(" | Query: %q", m.filterQuery)
	}

	header := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("wt-manager"),
		subtleStyle.Render(headerMeta),
	)
	if m.filtering {
		header = lipgloss.JoinVertical(lipgloss.Left, header, subtleStyle.Render(m.filterInput.View()))
	}

	tableCard := cardStyle.Copy()
	if m.focusTable {
		tableCard = focusStyle.Copy()
	}
	detailCard := cardStyle.Copy()
	if !m.focusTable {
		detailCard = focusStyle.Copy()
	}

	tableTitle := infoStyle.Render("Worktrees")
	if m.prMode {
		tableTitle = infoStyle.Render("PR Radar")
	}
	detailTitle := infoStyle.Render("Details")
	tableContent := lipgloss.JoinVertical(lipgloss.Left, tableTitle, m.tableView())
	detailContent := lipgloss.JoinVertical(lipgloss.Left, detailTitle, m.detail.View())
	if m.width >= 130 {
		panelHeight := max(lipgloss.Height(tableContent), lipgloss.Height(detailContent))
		tableContent = lipgloss.NewStyle().Height(panelHeight).Render(tableContent)
		detailContent = lipgloss.NewStyle().Height(panelHeight).Render(detailContent)
	}
	tableBlock := tableCard.Render(tableContent)
	detailBlock := detailCard.Render(detailContent)

	body := ""
	if m.width < 130 {
		body = lipgloss.JoinVertical(lipgloss.Left, tableBlock, detailBlock)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, tableBlock, detailBlock)
	}

	statusRenderer := statusStyle
	if m.statusError {
		statusRenderer = statusErrorStyle
	}
	status := statusRenderer.Render(m.statusMessage)
	if m.loading {
		status = statusRenderer.Render(fmt.Sprintf("%s %s", m.spinner.View(), m.statusMessage))
	}

	footer := lipgloss.JoinVertical(lipgloss.Left,
		status,
		subtleStyle.Render(m.help.View(m.keys)),
	)

	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", footer)
	view := appStyle.Render(content)

	if m.creatingWorktree {
		view = overlay(view, renderNewWorktreeModal(m), m.width, m.height)
	}
	if m.selectingAgent {
		view = overlay(view, renderAgentSelectorModal(m), m.width, m.height)
	}
	if m.selectingProfile {
		view = overlay(view, renderProfileSelectorModal(m), m.width, m.height)
	}
	if m.selectingPRUser {
		view = overlay(view, renderPRUserSelectorModal(m), m.width, m.height)
	}
	if m.askingPR {
		view = overlay(view, renderPRQuestionModal(m), m.width, m.height)
	}
	if m.showingHelp {
		view = overlay(view, renderHelpModal(m), m.width, m.height)
	}

	return view
}

func (m model) tableView() string {
	columns := m.activeColumns()
	rows := make([][]string, 0, len(m.visibleWorktrees))
	if m.prMode {
		return m.prTableView(columns)
	}
	if len(m.visibleWorktrees) == 0 {
		rows = append(rows, emptyTableRow(len(columns)))
	} else {
		compact := m.table.Width() <= 70
		start, end := visibleRowRange(m.table.Cursor(), len(m.visibleWorktrees), m.table.Height())
		for _, wt := range m.visibleWorktrees[start:end] {
			commit := wt.Commit.Subject
			if wt.Commit.Relative != "" {
				commit = fmt.Sprintf("%s %s", wt.Commit.Relative, wt.Commit.Subject)
			}

			if compact {
				rows = append(rows, []string{wt.Name, wt.Branch, stateLabel(wt), shortPR(wt.PR)})
			} else {
				rows = append(rows, []string{wt.Name, wt.Branch, stateLabel(wt), changeSummary(wt.Status), shortPR(wt.PR), commit})
			}
		}
	}

	lines := []string{renderTableHeader(columns)}
	start, _ := visibleRowRange(m.table.Cursor(), len(m.visibleWorktrees), m.table.Height())
	for idx, row := range rows {
		absoluteIndex := start + idx
		selected := len(m.visibleWorktrees) > 0 && absoluteIndex == m.table.Cursor()
		var wt *Worktree
		if len(m.visibleWorktrees) > 0 && absoluteIndex < len(m.visibleWorktrees) {
			wt = &m.visibleWorktrees[absoluteIndex]
		}
		lines = append(lines, renderTableRow(columns, row, wt, selected))
	}

	for len(lines)-1 < m.table.Height() {
		lines = append(lines, strings.Repeat(" ", m.table.Width()))
	}

	return strings.Join(lines, "\n")
}

func (m model) prTableView(columns []table.Column) string {
	rows := make([][]string, 0, len(m.visiblePRs))
	if len(m.visiblePRs) == 0 {
		rows = append(rows, emptyTableRow(len(columns)))
	} else {
		compact := m.table.Width() <= 70
		start, end := visibleRowRange(m.table.Cursor(), len(m.visiblePRs), m.table.Height())
		for _, pr := range m.visiblePRs[start:end] {
			if compact {
				rows = append(rows, []string{fmt.Sprintf("#%d", pr.Number), pr.Author.Login, prStateLabel(pr), m.prHeadline(pr)})
			} else {
				rows = append(rows, []string{fmt.Sprintf("#%d", pr.Number), pr.Author.Login, prStateLabel(pr), checksSummary(pr.StatusCheckRollup), reviewReadiness(pr), shortTime(pr.UpdatedAt), m.prHeadline(pr)})
			}
		}
	}

	lines := []string{renderTableHeader(columns)}
	start, _ := visibleRowRange(m.table.Cursor(), len(m.visiblePRs), m.table.Height())
	for idx, row := range rows {
		absoluteIndex := start + idx
		selected := len(m.visiblePRs) > 0 && absoluteIndex == m.table.Cursor()
		var pr *RemotePullRequest
		if len(m.visiblePRs) > 0 && absoluteIndex < len(m.visiblePRs) {
			pr = &m.visiblePRs[absoluteIndex]
		}
		lines = append(lines, renderPRTableRow(columns, row, pr, selected))
	}

	for len(lines)-1 < m.table.Height() {
		lines = append(lines, strings.Repeat(" ", m.table.Width()))
	}
	return strings.Join(lines, "\n")
}

func visibleRowRange(cursor, total, height int) (int, int) {
	if total <= 0 || height <= 0 {
		return 0, 0
	}
	start := cursor - height/2
	if start < 0 {
		start = 0
	}
	if start+height > total {
		start = max(total-height, 0)
	}
	end := min(start+height, total)
	return start, end
}

func emptyTableRow(columnCount int) []string {
	row := make([]string, columnCount)
	for i := range row {
		row[i] = "-"
	}
	if columnCount > 0 {
		row[columnCount-1] = "No worktrees found"
	}
	return row
}

func renderTableHeader(columns []table.Column) string {
	cells := make([]string, 0, len(columns))
	for _, column := range columns {
		cell := padCell(runewidth.Truncate(column.Title, column.Width, "…"), column.Width)
		cells = append(cells, headerCellStyle.Render(cell))
	}
	return strings.Join(cells, "")
}

func renderTableRow(columns []table.Column, row []string, wt *Worktree, selected bool) string {
	cells := make([]string, 0, len(columns))
	for idx, column := range columns {
		value := ""
		if idx < len(row) {
			value = singleLineCell(row[idx])
		}
		value = runewidth.Truncate(value, column.Width, "…")
		cell := padCell(value, column.Width)
		if wt != nil {
			cell = styleTableCell(cell, column.Title, wt)
		} else if wt == nil {
			cell = mutedStyle.Render(cell)
		}
		cells = append(cells, tableCellStyle.Render(cell))
	}

	line := strings.Join(cells, "")
	if selected {
		return selectedRowStyle.Render(line)
	}
	return line
}

func renderPRTableRow(columns []table.Column, row []string, pr *RemotePullRequest, selected bool) string {
	cells := make([]string, 0, len(columns))
	for idx, column := range columns {
		value := ""
		if idx < len(row) {
			value = singleLineCell(row[idx])
		}
		value = runewidth.Truncate(value, column.Width, "…")
		cell := padCell(value, column.Width)
		if pr != nil {
			cell = stylePRTableCell(cell, column.Title, pr)
		} else {
			cell = mutedStyle.Render(cell)
		}
		cells = append(cells, tableCellStyle.Render(cell))
	}
	line := strings.Join(cells, "")
	if selected {
		return selectedRowStyle.Render(line)
	}
	return line
}

func stylePRTableCell(cell, title string, pr *RemotePullRequest) string {
	switch title {
	case "State":
		return prStateCellStyle(*pr).Render(cell)
	case "Checks":
		return checksCellStyle(pr.StatusCheckRollup).Render(cell)
	case "Ready":
		return readinessCellStyle(*pr).Render(cell)
	case "Updated":
		return mutedStyle.Render(cell)
	default:
		return cell
	}
}

func styleTableCell(cell, title string, wt *Worktree) string {
	switch title {
	case "State":
		return stateCellStyle(*wt).Render(cell)
	case "Delta":
		return deltaCellStyle(wt.Status).Render(cell)
	case "PR":
		return prCellStyle(wt.PR).Render(cell)
	case "Last Commit":
		return mutedStyle.Render(cell)
	default:
		return cell
	}
}

func stateCellStyle(wt Worktree) lipgloss.Style {
	switch stateLabel(wt) {
	case "main":
		return infoStyle
	case "clean":
		return successStyle
	case "dirty":
		return warnStyle
	case "conflict", "missing", "prunable":
		return errorStyle
	case "detached":
		return purpleStyle
	default:
		return mutedStyle
	}
}

func deltaCellStyle(status RepoStatus) lipgloss.Style {
	if status.Conflicts > 0 {
		return errorStyle
	}
	if status.Dirty {
		return warnStyle
	}
	return mutedStyle
}

func prCellStyle(pr *PullRequest) lipgloss.Style {
	if pr == nil {
		return mutedStyle
	}
	switch strings.ToLower(pr.State) {
	case "open":
		return successStyle
	case "merged":
		return magentaStyle
	case "closed":
		return errorStyle
	default:
		return infoStyle
	}
}

func prStateCellStyle(pr RemotePullRequest) lipgloss.Style {
	if pr.IsDraft {
		return mutedStyle
	}
	switch strings.ToLower(pr.State) {
	case "open":
		return successStyle
	case "merged":
		return magentaStyle
	case "closed":
		return errorStyle
	default:
		return infoStyle
	}
}

func checksCellStyle(checks []StatusCheck) lipgloss.Style {
	summary := checksSummary(checks)
	if strings.Contains(summary, "fail") || strings.Contains(summary, "cancel") {
		return errorStyle
	}
	if strings.Contains(summary, "wait") {
		return warnStyle
	}
	if strings.Contains(summary, "pass") {
		return successStyle
	}
	return mutedStyle
}

func readinessCellStyle(pr RemotePullRequest) lipgloss.Style {
	switch reviewReadiness(pr) {
	case "ready", "approved":
		return successStyle
	case "blocked", "changes":
		return errorStyle
	case "waiting":
		return warnStyle
	default:
		return mutedStyle
	}
}

func padCell(value string, width int) string {
	padding := width - runewidth.StringWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func singleLineCell(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ")
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func (m model) filterLabel() string {
	if m.prMode {
		parts := []string{}
		state := m.prStateFilter
		if state == "" {
			state = "all"
		}
		parts = append(parts, "state="+state)
		if m.prUserFilter != "" {
			parts = append(parts, "user=@"+m.prUserFilter)
		}
		if m.prAuthorFilter != "" {
			parts = append(parts, "author=@"+m.prAuthorFilter)
		}
		if m.prShowBranch {
			parts = append(parts, "branch view")
		}
		if m.filterQuery != "" {
			parts = append(parts, "filtered")
		}
		return strings.Join(parts, ", ")
	}
	mode := "managed sibling worktrees"
	if m.showAll {
		mode = "all repo worktrees"
	}
	if m.filterQuery == "" {
		return mode
	}
	return fmt.Sprintf("%s, filtered", mode)
}

func (m model) modeLabel() string {
	if m.prMode {
		return "PR Radar"
	}
	return "Worktrees"
}

func (m model) focusLabel() string {
	if m.filtering {
		return "filter"
	}
	if m.creatingWorktree {
		return "new worktree"
	}
	if m.selectingAgent {
		return "agent selector"
	}
	if m.selectingProfile {
		return "profile selector"
	}
	if m.selectingPRUser {
		return "PR user selector"
	}
	if m.askingPR {
		return "PR chat"
	}
	if m.setup != nil {
		return "setup"
	}
	if m.focusTable {
		return "table"
	}
	return "details"
}

func renderNewWorktreeModal(m model) string {
	message := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("New Worktree"),
		"",
		"Create or open a branch for the active repository.",
		m.newInput.View(),
		"",
		subtleStyle.Render("enter creates, esc cancels"),
	)
	return modalStyle.Render(message)
}

func renderAgentSelectorModal(m model) string {
	lines := []string{
		titleStyle.Render("Default AI Agent"),
		"",
		"Choose which tool runs when pressing i.",
		"",
	}

	if len(m.cfg.App.Agents) == 0 {
		lines = append(lines, errorStyle.Render("No agents configured"))
	} else {
		for idx, agent := range m.cfg.App.Agents {
			cursor := "  "
			style := subtleStyle
			if idx == m.agentCursor {
				cursor = "> "
				style = infoStyle.Copy().Bold(true)
			}

			current := ""
			if agent.Name == m.cfg.App.DefaultAgent {
				current = " current"
			}

			lines = append(lines, style.Render(fmt.Sprintf("%s%-10s %s%s", cursor, agent.Name, agent.Command, current)))
		}
	}

	lines = append(lines,
		"",
		subtleStyle.Render(fmt.Sprintf("Config: %s", m.cfg.ConfigPath)),
		subtleStyle.Render("enter selects, esc cancels"),
	)

	return modalStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func renderProfileSelectorModal(m model) string {
	lines := []string{
		titleStyle.Render("Repository Profile"),
		"",
		"Choose which repository profile to manage.",
		"",
	}

	if len(m.cfg.App.Profiles) == 0 {
		lines = append(lines, errorStyle.Render("No profiles configured"))
	} else {
		for idx, profile := range m.cfg.App.Profiles {
			cursor := "  "
			style := subtleStyle
			if idx == m.profileCursor {
				cursor = "> "
				style = infoStyle.Copy().Bold(true)
			}

			current := ""
			if profile.Name == m.cfg.App.DefaultProfile {
				current = " current"
			}

			lines = append(lines, style.Render(fmt.Sprintf("%s%-14s %s%s", cursor, profile.Name, profile.RepositoryPath, current)))
		}
	}

	lines = append(lines,
		"",
		subtleStyle.Render(fmt.Sprintf("Config: %s", m.cfg.ConfigPath)),
		subtleStyle.Render("enter selects, esc cancels"),
	)

	return modalStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func renderPRUserSelectorModal(m model) string {
	lines := []string{
		titleStyle.Render("PR User Filter"),
		"",
		"Show PRs authored by, assigned to, or requesting this user.",
		"",
	}

	if len(m.prUserOptions) == 0 {
		lines = append(lines, mutedStyle.Render("No users discovered"))
	} else {
		for idx, user := range m.prUserOptions {
			cursor := "  "
			style := subtleStyle
			if idx == m.prUserCursor {
				cursor = "> "
				style = infoStyle.Copy().Bold(true)
			}
			label := user
			if idx > 0 {
				label = "@" + user
			}
			current := ""
			if (idx == 0 && m.prUserFilter == "") || strings.EqualFold(user, m.prUserFilter) {
				current = " current"
			}
			lines = append(lines, style.Render(fmt.Sprintf("%s%s%s", cursor, label, current)))
		}
	}
	lines = append(lines, "", subtleStyle.Render("enter selects, esc cancels"))
	return modalStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func renderPRQuestionModal(m model) string {
	selected := m.selectedRemotePRForDetail()
	title := "Ask Agent About PR"
	context := "No PR selected"
	if selected != nil {
		context = fmt.Sprintf("PR #%d: %s", selected.Number, selected.Title)
	}
	message := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		"",
		context,
		fmt.Sprintf("Agent: %s", m.prAgentLabel()),
		m.prQuestionInput.View(),
		"",
		subtleStyle.Render("enter asks, esc cancels"),
	)
	return modalStyle.Render(message)
}

func renderHelpModal(m model) string {
	lines := []string{
		titleStyle.Render("Keybindings"),
		"",
		infoStyle.Render("Global"),
		"  h        show or hide this help",
		"  q/ctrl+c quit",
		"  tab      switch table/details focus",
		"  r        refresh current mode",
		"  /        filter current mode",
		"  esc      cancel input, clear filter, or close help",
		"  s        switch repository profile",
		"  I        setup profiles and agents",
		"  m        choose default AI agent",
		"",
		infoStyle.Render("Worktrees"),
		"  j/k      move selection",
		"  a        toggle managed/all worktrees",
		"  n        create worktree",
		"  d d      delete selected worktree",
		"  o        open selected local PR",
		"  v        open worktree in VS Code",
		"  i        open worktree with agent in Ghostty",
		"  p        toggle local PR body in details",
		"",
		infoStyle.Render("PR Radar"),
		"  P        toggle PR Radar mode",
		"  S        cycle state filter",
		"  u        choose involved-user filter",
		"  Y        toggle PRs authored by @me",
		"  b        switch final column between title/branch",
		"  f        show failed GitHub Actions in details",
		"  c        ask configured agent about selected PR",
		"  n        create worktree from selected PR branch",
		"  o        open selected PR in browser",
		"",
		subtleStyle.Render("press h, enter, or esc to close"),
	}
	return modalStyle.Copy().Width(max(72, min(92, m.width-8))).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func overlay(base, modal string, width, height int) string {
	modalContent := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
	parts := []string{base, modalContent}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

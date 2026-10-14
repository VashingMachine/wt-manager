package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func (m model) View() string {
	if !m.ready {
		return "Loading wt-manager..."
	}
	if m.setup != nil {
		return appStyle.Render(m.setup.View(m.width))
	}
	if m.paletteOpen {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, renderCommandPaletteModal(m))
	}
	if m.logsOpen {
		return m.sessionLogsView()
	}
	if m.selectingEditor {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, renderEditorSelectorModal(m))
	}
	if m.editingEditor {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, renderEditorInputModal(m))
	}
	if m.isPRMode() && m.prDiffMode {
		view := m.fullScreenPRDiffView()
		if m.confirmingPRApproval {
			view = overlay(view, renderPRApprovalModal(m), m.width, m.height)
		}
		if m.showingHelp {
			view = overlay(view, renderHelpModal(m), m.width, m.height)
		}
		return view
	}
	if m.isTicketMode() && m.ticketReadMode {
		view := m.fullScreenTicketView()
		if m.showingHelp {
			view = overlay(view, renderHelpModal(m), m.width, m.height)
		}
		return view
	}

	header := m.headerView()

	tableCard := cardStyle
	if m.focusTable {
		tableCard = focusStyle
	}
	detailCard := cardStyle
	if !m.focusTable {
		detailCard = focusStyle
	}

	tableTitle := infoStyle.Render("Worktrees")
	if m.isPRMode() {
		tableTitle = infoStyle.Render("PR Radar")
		if m.cfg.MultiRepository() {
			tableTitle += " " + purpleStyle.Render(icons.Repo+" "+m.prCfg().RepositoryName()) + mutedStyle.Render("  R switches")
		}
	} else if m.isTicketMode() {
		tableTitle = infoStyle.Render("JIRA Tickets")
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

	footer := lipgloss.JoinVertical(lipgloss.Left,
		m.statusBarView(),
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
	if m.selectingEnterAction {
		view = overlay(view, renderEnterActionSelectorModal(m), m.width, m.height)
	}
	if m.selectingProfile {
		view = overlay(view, renderProfileSelectorModal(m), m.width, m.height)
	}
	if m.pickingRepository {
		view = overlay(view, renderRepositoryPickerModal(m), m.width, m.height)
	}
	if m.addingRepository != nil {
		view = overlay(view, renderAddRepositoryModal(m), m.width, m.height)
	}
	if m.selectingPRUser {
		view = overlay(view, renderPRUserSelectorModal(m), m.width, m.height)
	}
	if m.askingPR {
		view = overlay(view, renderPRQuestionModal(m), m.width, m.height)
	}
	if m.confirmingPRApproval {
		view = overlay(view, renderPRApprovalModal(m), m.width, m.height)
	}
	if m.selectingTicketStatus {
		view = overlay(view, renderTicketStatusModal(m), m.width, m.height)
	}
	if m.enteringTicketStatus {
		view = overlay(view, renderTicketStatusInputModal(m), m.width, m.height)
	}
	if m.commentingTicket {
		view = overlay(view, renderTicketCommentModal(m), m.width, m.height)
	}
	if m.selectingTicketAssignee {
		view = overlay(view, renderTicketAssigneeModal(m), m.width, m.height)
	}
	if m.enteringTicketAssignee {
		view = overlay(view, renderTicketAssigneeInputModal(m), m.width, m.height)
	}
	if m.selectingBoard {
		view = overlay(view, renderBoardSelectorModal(m), m.width, m.height)
	}
	if m.selectingTicketFilters {
		view = overlay(view, renderTicketFiltersModal(m), m.width, m.height)
	}
	if m.showingHelp {
		view = overlay(view, renderHelpModal(m), m.width, m.height)
	}

	return view
}

func (m model) headerView() string {
	badge := badgeWorktreesStyle.Render("WORKTREES")
	if m.isPRMode() {
		badge = badgePRRadarStyle.Render("PR RADAR")
	} else if m.isTicketMode() {
		badge = badgeTicketsStyle.Render("JIRA TICKETS")
	}
	chip := chipStyle.Render(icons.Dot + " " + m.cfg.ActiveProfile.Name)
	title := lipgloss.JoinHorizontal(lipgloss.Center, titleStyle.Render("wt-manager"), "  ", badge, " ", chip)

	usable := max(20, m.width-appStyle.GetHorizontalFrameSize())

	// The filter input line is always reserved so toggling "/" does not shift
	// the layout below it.
	filterLine := ""
	if m.filtering {
		filterLine = subtleStyle.Render(m.filterInput.View())
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, m.headerMetaLine(usable), filterLine)
}

func (m model) headerMetaLine(usable int) string {
	build := func(repoPath string) string {
		separator := mutedStyle.Render(" · ")
		parts := []string{
			mutedStyle.Render("repo ") + subtleStyle.Render(repoPath),
			mutedStyle.Render("agent ") + subtleStyle.Render(emptyDash(m.cfg.App.DefaultAgent)),
			mutedStyle.Render("enter ") + subtleStyle.Render(m.configuredEnterActionLabel()),
			mutedStyle.Render("focus ") + subtleStyle.Render(m.focusLabel()),
		}
		if label := m.filterLabel(); label != "" {
			parts = append(parts, mutedStyle.Render("filter ")+subtleStyle.Render(label))
		}
		line := strings.Join(parts, separator)
		if m.filterQuery != "" {
			line += " " + warnStyle.Render(fmt.Sprintf("/%q", m.filterQuery))
		}
		return line
	}

	repo := m.cfg.ActiveProfile.RepositoryPath
	if m.cfg.MultiRepository() {
		names := make([]string, 0, len(m.cfg.ActiveProfile.Repositories))
		for _, repository := range m.cfg.ActiveProfile.Repositories {
			names = append(names, repository.Name)
		}
		repo = strings.Join(names, ", ")
		if m.isPRMode() {
			repo = "PRs of " + m.prCfg().RepositoryName() + " (" + repo + ")"
		}
	}
	line := build(repo)
	for lipgloss.Width(line) > usable && len([]rune(repo)) > 8 {
		repo = truncateRunes(repo, len([]rune(repo))-1)
		line = build(repo)
	}
	if lipgloss.Width(line) > usable {
		plain := fmt.Sprintf("repo %s · agent %s · enter %s · focus %s", repo, emptyDash(m.cfg.App.DefaultAgent), m.configuredEnterActionLabel(), m.focusLabel())
		line = subtleStyle.Render(clampRunewidth(plain, usable))
	}
	return line
}

// elapsedHint reports how long the current operation has been running, once it
// is slow enough to be worth mentioning (agent calls earlier than the rest).
func (m model) elapsedHint() string {
	if m.loadingStartedAt.IsZero() {
		return ""
	}
	threshold := 5 * time.Second
	if m.loadingKind == loadingAgent {
		threshold = 3 * time.Second
	}
	elapsed := time.Since(m.loadingStartedAt)
	if elapsed < threshold {
		return ""
	}
	return fmt.Sprintf("(%ds)", int(elapsed.Seconds()))
}

func (m model) quietStatusHint() string {
	if m.isPRMode() {
		return fmt.Sprintf("%s %d PRs · ctrl+p commands · / filter", icons.Dot, len(m.visiblePRs))
	}
	if m.isTicketMode() {
		return fmt.Sprintf("%s %d tickets · ctrl+p commands · / filter", icons.Dot, len(m.visibleTickets))
	}
	return fmt.Sprintf("%s %d worktrees · ctrl+p commands · / filter", icons.Dot, len(m.visibleWorktrees))
}

func isSuccessStatus(message string) bool {
	for _, prefix := range []string{"Loaded ", "Deleted ", "Created ", "Approved ", "Saved ", "Opened ", "Selected "} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

func (m model) statusBarView() string {
	if m.isBusy() {
		message := m.statusMessage
		if progress := m.progressHint(); progress != "" {
			if strings.TrimSpace(message) != "" {
				message += " " + mutedStyle.Render("·") + " "
			}
			message += mutedStyle.Render(progress)
		}
		if hint := m.elapsedHint(); hint != "" && m.loading {
			message += " " + mutedStyle.Render(hint)
		}
		return statusStyle.Render(fmt.Sprintf("%s %s", m.spinner.View(), message))
	}
	if m.statusError {
		return statusErrorStyle.Render(fmt.Sprintf("%s %s", icons.Err, m.statusMessage))
	}
	if m.deleteArmedPath != "" {
		remaining := deleteConfirmWindow - time.Since(m.deleteArmedAt)
		if remaining < 0 {
			remaining = 0
		}
		return statusStyle.Render(warnStyle.Render(fmt.Sprintf("%s Press d again to delete %s (%.1fs)", icons.Warn, m.deleteArmedName, remaining.Seconds())))
	}
	if strings.TrimSpace(m.statusMessage) == "" {
		return statusStyle.Render(mutedStyle.Render(m.quietStatusHint()))
	}
	if isSuccessStatus(m.statusMessage) {
		return statusOKStyle.Render(fmt.Sprintf("%s %s", icons.OK, m.statusMessage))
	}
	return statusStyle.Render(m.statusMessage)
}

func (m model) fullScreenPRDiffView() string {
	selected := m.selectedRemotePRForDetail()
	title := "PR Diff"
	meta := "No remote PR selected"
	if selected != nil {
		title = fmt.Sprintf("PR #%d %s", selected.Number, selected.Title)
		meta = fmt.Sprintf("%s -> %s | %s | %s | %s", emptyDash(selected.HeadRefName), emptyDash(selected.BaseRefName), prStateDisplay(*selected), checksDisplay(selected.StatusCheckRollup), readinessDisplay(*selected))
		if m.prDetailLoading {
			meta += " | loading"
		}
	}

	width := max(1, m.width)
	header := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(clampRunewidth(title, width)),
		subtleStyle.Render(clampRunewidth(meta, width)),
	)
	footerText := "esc close | j/k scroll | A approve | o open PR | r refresh | q quit"
	status := m.statusMessage
	if m.isBusy() {
		status = fmt.Sprintf("%s %s", m.spinner.View(), status)
	} else if m.statusError && status != "" {
		status = fmt.Sprintf("%s %s", icons.Err, status)
	}
	if status != "" {
		footerText = fmt.Sprintf("%s | %s", footerText, status)
	}
	footerStyle := subtleStyle
	if m.statusError {
		footerStyle = errorStyle
	}
	footer := footerStyle.Render(clampRunewidth(footerText, width))

	return lipgloss.NewStyle().Width(width).Height(max(1, m.height)).Render(lipgloss.JoinVertical(lipgloss.Left, header, m.detail.View(), footer))
}

func (m model) fullScreenTicketView() string {
	selected := m.selectedTicketForDetail()
	title := "JIRA Ticket"
	meta := "No ticket selected"
	if selected != nil {
		title = fmt.Sprintf("%s %s", selected.Key, selected.Summary)
		meta = fmt.Sprintf("%s | %s | %s", emptyDash(selected.Status), emptyDash(selected.Assignee.Name()), emptyDash(m.ticketBoard.Name))
		if m.ticketDetailLoading {
			meta += " | loading"
		}
	}
	width := max(1, m.width)
	header := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(clampRunewidth(title, width)),
		subtleStyle.Render(clampRunewidth(meta, width)),
	)
	footer := subtleStyle.Render(clampRunewidth("esc close | j/k scroll | o open in browser | q quit", width))
	return lipgloss.NewStyle().Width(width).Height(max(1, m.height)).Render(lipgloss.JoinVertical(lipgloss.Left, header, m.detail.View(), footer))
}

func (m model) tableView() string {
	columns := m.activeColumns()
	rows := make([][]string, 0, len(m.visibleWorktrees))
	if m.isPRMode() {
		return m.prTableView(columns)
	}
	if m.isTicketMode() {
		return m.ticketTableView(columns)
	}
	loadingLine := ""
	if len(m.visibleWorktrees) == 0 {
		if m.loading {
			loadingLine = tableCellStyle.Render(m.spinner.View() + " " + subtleStyle.Render("Loading worktrees..."))
		} else {
			loadingLine = tableCellStyle.Render(mutedStyle.Render(m.emptyWorktreesMessage()))
		}
	} else {
		compact := m.table.Width() <= 70
		start, end := visibleRowRange(m.table.Cursor(), len(m.visibleWorktrees), m.table.Height())
		for _, wt := range m.visibleWorktrees[start:end] {
			rows = append(rows, m.worktreeRow(wt, compact))
		}
	}

	lines := []string{renderTableHeader(columns)}
	if loadingLine != "" {
		lines = append(lines, loadingLine)
	}
	start, _ := visibleRowRange(m.table.Cursor(), len(m.visibleWorktrees), m.table.Height())
	for idx, row := range rows {
		absoluteIndex := start + idx
		selected := len(m.visibleWorktrees) > 0 && absoluteIndex == m.table.Cursor()
		var wt *Worktree
		if len(m.visibleWorktrees) > 0 && absoluteIndex < len(m.visibleWorktrees) {
			wt = &m.visibleWorktrees[absoluteIndex]
		}
		armed := wt != nil && m.deleteArmedPath != "" && wt.Path == m.deleteArmedPath
		lines = append(lines, renderTableRow(columns, row, wt, selected, m.focusTable, armed))
	}

	for len(lines)-1 < m.table.Height() {
		lines = append(lines, strings.Repeat(" ", m.table.Width()))
	}

	return strings.Join(lines, "\n")
}

func (m model) ticketTableView(columns []table.Column) string {
	rows := make([][]string, 0, len(m.visibleTickets))
	loadingLine := ""
	if len(m.visibleTickets) == 0 {
		if m.loading {
			loadingLine = tableCellStyle.Render(m.spinner.View() + " " + subtleStyle.Render("Loading JIRA tickets..."))
		} else {
			loadingLine = tableCellStyle.Render(mutedStyle.Render(m.emptyTicketsMessage()))
		}
	} else {
		compact := m.table.Width() <= 70
		start, end := visibleRowRange(m.table.Cursor(), len(m.visibleTickets), m.table.Height())
		for _, ticket := range m.visibleTickets[start:end] {
			rows = append(rows, m.ticketRowCells(ticket, compact))
		}
	}
	lines := []string{renderTableHeader(columns)}
	if loadingLine != "" {
		lines = append(lines, loadingLine)
	}
	start, _ := visibleRowRange(m.table.Cursor(), len(m.visibleTickets), m.table.Height())
	for idx, row := range rows {
		absoluteIndex := start + idx
		selected := len(m.visibleTickets) > 0 && absoluteIndex == m.table.Cursor()
		var ticket *Ticket
		if absoluteIndex < len(m.visibleTickets) {
			ticket = &m.visibleTickets[absoluteIndex]
		}
		lines = append(lines, renderTicketTableRow(columns, row, ticket, selected, m.focusTable))
	}
	for len(lines)-1 < m.table.Height() {
		lines = append(lines, strings.Repeat(" ", m.table.Width()))
	}
	return strings.Join(lines, "\n")
}

func (m model) prTableView(columns []table.Column) string {
	rows := make([][]string, 0, len(m.visiblePRs))
	loadingLine := ""
	if len(m.visiblePRs) == 0 {
		if m.loading {
			loadingLine = tableCellStyle.Render(m.spinner.View() + " " + subtleStyle.Render("Loading remote PRs..."))
		} else {
			loadingLine = tableCellStyle.Render(mutedStyle.Render(m.emptyPRsMessage()))
		}
	} else {
		compact := m.table.Width() <= 70
		start, end := visibleRowRange(m.table.Cursor(), len(m.visiblePRs), m.table.Height())
		for _, pr := range m.visiblePRs[start:end] {
			rows = append(rows, m.prRowCells(pr, compact))
		}
	}

	lines := []string{renderTableHeader(columns)}
	if loadingLine != "" {
		lines = append(lines, loadingLine)
	}
	start, _ := visibleRowRange(m.table.Cursor(), len(m.visiblePRs), m.table.Height())
	for idx, row := range rows {
		absoluteIndex := start + idx
		selected := len(m.visiblePRs) > 0 && absoluteIndex == m.table.Cursor()
		var pr *RemotePullRequest
		if len(m.visiblePRs) > 0 && absoluteIndex < len(m.visiblePRs) {
			pr = &m.visiblePRs[absoluteIndex]
		}
		lines = append(lines, renderPRTableRow(columns, row, pr, selected, m.focusTable))
	}

	for len(lines)-1 < m.table.Height() {
		lines = append(lines, strings.Repeat(" ", m.table.Width()))
	}
	return strings.Join(lines, "\n")
}

func worktreeRowCells(wt Worktree, compact bool) []string {
	if compact {
		return []string{wt.Name, wt.Branch, stateDisplay(wt), shortPRDisplay(wt.PR)}
	}
	commit := wt.Commit.Subject
	if wt.Commit.Relative != "" {
		commit = fmt.Sprintf("%s %s", wt.Commit.Relative, wt.Commit.Subject)
	}
	delta := changeDisplay(wt.Status)
	if wt.StatusPending {
		delta = icons.Loading
	}
	return []string{wt.Name, wt.Branch, stateDisplay(wt), delta, shortPRDisplay(wt.PR), commit}
}

func (m model) prRowCells(pr RemotePullRequest, compact bool) []string {
	if compact {
		return []string{fmt.Sprintf("#%d", pr.Number), pr.Author.Login, prStateDisplay(pr), m.prHeadline(pr)}
	}
	checks := checksDisplay(pr.StatusCheckRollup)
	if checksUnknown(pr) {
		checks = icons.Loading
	}
	return []string{fmt.Sprintf("#%d", pr.Number), pr.Author.Login, prStateDisplay(pr), checks, readinessDisplay(pr), shortTime(pr.UpdatedAt), m.prHeadline(pr)}
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

func (m model) emptyWorktreesMessage() string {
	if m.filterQuery != "" {
		return "No matches — esc clears filter"
	}
	return "No worktrees — n creates one"
}

func (m model) emptyPRsMessage() string {
	if m.filterQuery != "" || m.prUserFilter != "" || m.prAuthorFilter != "" {
		return "No PRs match — esc clears"
	}
	if m.prStateFilter != "" && m.prStateFilter != "all" {
		return fmt.Sprintf("No %s PRs — S cycles state", m.prStateFilter)
	}
	return "No PRs found"
}

func emptyTableRow(columnCount int, message string) []string {
	row := make([]string, columnCount)
	for i := range row {
		row[i] = "-"
	}
	if columnCount > 0 {
		row[columnCount-1] = message
	}
	return row
}

func renderTableHeader(columns []table.Column) string {
	cells := make([]string, 0, len(columns))
	for idx, column := range columns {
		title := column.Title
		if idx == 0 {
			title = "  " + title
		}
		cell := padCellForColumn(runewidth.Truncate(title, column.Width, "…"), column)
		cells = append(cells, headerCellStyle.Render(cell))
	}
	return strings.Join(cells, "")
}

// rowCellValue prepares one cell: selected rows carry a pointer in the first
// column, every other row gets matching indentation so columns stay aligned.
func rowCellValue(raw string, columnIndex int, selected bool) string {
	value := singleLineCell(raw)
	if columnIndex == 0 {
		prefix := "  "
		if selected {
			prefix = icons.Pointer + " "
		}
		value = prefix + value
	}
	return value
}

// rowHighlight is the background a selected, blurred-selected or armed row
// paints under all of its cells.
func rowHighlight(selected, focused, armed bool) (lipgloss.Style, bool) {
	switch {
	case armed:
		return armedRowStyle, true
	case selected && focused:
		return selectedRowStyle, true
	case selected:
		return blurredRowStyle, true
	default:
		return lipgloss.Style{}, false
	}
}

// renderRowCells renders a table row. Each cell keeps its own colors and gets
// the row's highlight itself: wrapping the joined line instead would lose the
// highlight at the first colored cell, whose reset clears the background.
func renderRowCells(columns []table.Column, row []string, selected, focused, armed bool, cellStyle func(title string) lipgloss.Style) string {
	highlight, highlighted := rowHighlight(selected, focused, armed)
	cells := make([]string, 0, len(columns))
	for idx, column := range columns {
		value := ""
		if idx < len(row) {
			value = rowCellValue(row[idx], idx, selected)
		}
		value = runewidth.Truncate(value, column.Width, "…")
		style, padding := cellStyle(column.Title), tableCellStyle
		if highlighted {
			style, padding = style.Inherit(highlight), padding.Inherit(highlight)
		}
		cells = append(cells, padding.Render(style.Render(padCellForColumn(value, column))))
	}
	return strings.Join(cells, "")
}

func renderTableRow(columns []table.Column, row []string, wt *Worktree, selected, focused, armed bool) string {
	return renderRowCells(columns, row, selected, focused, armed, func(title string) lipgloss.Style {
		if wt == nil {
			return mutedStyle
		}
		return worktreeCellStyle(title, wt)
	})
}

func renderPRTableRow(columns []table.Column, row []string, pr *RemotePullRequest, selected, focused bool) string {
	return renderRowCells(columns, row, selected, focused, false, func(title string) lipgloss.Style {
		if pr == nil {
			return mutedStyle
		}
		return prTableCellStyle(title, pr)
	})
}

func renderTicketTableRow(columns []table.Column, row []string, ticket *Ticket, selected, focused bool) string {
	return renderRowCells(columns, row, selected, focused, false, func(title string) lipgloss.Style {
		if ticket == nil {
			return mutedStyle
		}
		return ticketCellStyle(title, ticket)
	})
}

func ticketCellStyle(title string, ticket *Ticket) lipgloss.Style {
	switch title {
	case "Status":
		switch strings.ToLower(ticket.StatusCategory) {
		case "done":
			return successStyle
		case "indeterminate":
			return warnStyle
		default:
			return infoStyle
		}
	case "PR":
		return prCellStyle(ticket.PR)
	case "Worktree", "WT":
		if ticket.Worktree == nil {
			return mutedStyle
		}
	}
	return lipgloss.NewStyle()
}

func prTableCellStyle(title string, pr *RemotePullRequest) lipgloss.Style {
	switch title {
	case "State":
		return prStateCellStyle(*pr)
	case "Checks":
		return checksCellStyle(pr.StatusCheckRollup)
	case "Ready":
		return readinessCellStyle(*pr)
	case "Updated":
		return mutedStyle
	default:
		return lipgloss.NewStyle()
	}
}

func worktreeCellStyle(title string, wt *Worktree) lipgloss.Style {
	switch title {
	case "State":
		return stateCellStyle(*wt)
	case "Delta":
		return deltaCellStyle(wt.Status)
	case "PR":
		return prCellStyle(wt.PR)
	case "Last Commit":
		return mutedStyle
	case "Repo":
		return purpleStyle
	default:
		return lipgloss.NewStyle()
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

func padCellLeft(value string, width int) string {
	padding := width - runewidth.StringWidth(value)
	if padding <= 0 {
		return value
	}
	return strings.Repeat(" ", padding) + value
}

// rightAlignedColumns lists the numeric-ish columns that read better right
// aligned in both tables.
func columnRightAligned(title string) bool {
	switch title {
	case "Delta", "Checks", "Updated":
		return true
	default:
		return false
	}
}

func padCellForColumn(value string, column table.Column) string {
	if columnRightAligned(column.Title) {
		return padCellLeft(value, column.Width)
	}
	return padCell(value, column.Width)
}

func singleLineCell(value string) string {
	replacer := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ")
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func (m model) filterLabel() string {
	if m.isTicketMode() {
		parts := []string{"board=" + emptyDash(m.ticketBoard.Name)}
		if m.ticketMineOnly {
			parts = append(parts, "assignee=me")
		} else {
			parts = append(parts, "assignee=all")
		}
		if m.ticketSprintOnly {
			parts = append(parts, "active sprint")
		}
		if m.ticketOpenOnly {
			parts = append(parts, "open only")
		}
		if m.ticketStatusFilter != "" {
			parts = append(parts, "status="+m.ticketStatusFilter)
		}
		if m.filterQuery != "" {
			parts = append(parts, "filtered")
		}
		return strings.Join(parts, ", ")
	}
	if m.isPRMode() {
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

func (m model) focusLabel() string {
	if m.paletteOpen {
		return "command palette"
	}
	if m.selectingEditor || m.editingEditor {
		return "editor settings"
	}
	if m.filtering {
		return "filter"
	}
	if m.creatingWorktree {
		return "new worktree"
	}
	if m.selectingAgent {
		return "agent selector"
	}
	if m.selectingEnterAction {
		return "enter action"
	}
	if m.selectingProfile {
		return "profile selector"
	}
	if m.pickingRepository {
		return "repository selector"
	}
	if m.addingRepository != nil {
		return "add repository"
	}
	if m.selectingPRUser {
		return "PR user selector"
	}
	if m.askingPR {
		return "PR chat"
	}
	if m.confirmingPRApproval {
		return "PR approval"
	}
	if m.selectingTicketStatus || m.enteringTicketStatus {
		return "ticket status"
	}
	if m.commentingTicket {
		return "ticket comment"
	}
	if m.selectingTicketAssignee || m.enteringTicketAssignee {
		return "ticket assignee"
	}
	if m.selectingBoard {
		return "board selector"
	}
	if m.selectingTicketFilters {
		return "ticket filters"
	}
	if m.ticketReadMode {
		return "ticket reader"
	}
	if m.setup != nil {
		return "setup"
	}
	if m.focusTable {
		return "table"
	}
	return "details"
}

type keyHint struct {
	Key    string
	Action string
}

func renderKeyHints(hints []keyHint) string {
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, keyHintKeyStyle.Render(hint.Key)+" "+subtleStyle.Render(hint.Action))
	}
	return strings.Join(parts, mutedStyle.Render("  ·  "))
}

func renderModal(title string, body []string, hints []keyHint) string {
	lines := []string{titleStyle.Render(title), ""}
	lines = append(lines, body...)
	if len(hints) > 0 {
		lines = append(lines, "", renderKeyHints(hints))
	}
	return modalStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// renderWideModal is renderModal for content that needs more room, such as
// folder paths.
func renderWideModal(title string, body []string, hints []keyHint, width int) string {
	lines := []string{titleStyle.Render(title), ""}
	lines = append(lines, body...)
	if len(hints) > 0 {
		lines = append(lines, "", renderKeyHints(hints))
	}
	return modalStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func selectorLine(selected bool, label string, current bool) string {
	cursor := "  "
	style := subtleStyle
	if selected {
		cursor = icons.Pointer + " "
		style = infoStyle.Bold(true)
	}
	line := style.Render(cursor + label)
	if current {
		line += " " + successStyle.Render(icons.OK+" current")
	}
	return line
}

var selectHints = []keyHint{{"j/k", "move"}, {"enter", "select"}, {"esc", "cancel"}}

func renderNewWorktreeModal(m model) string {
	intro := "Create or open a branch for the active repository."
	if m.cfg.MultiRepository() {
		repository := m.cfg.ForRepository(m.newWorktreeRepository).RepositoryName()
		intro = "Create or open a branch in " + purpleStyle.Render(icons.Repo+" "+repository) + "."
	}
	body := []string{
		intro,
		"",
		m.newInput.View(),
	}
	return renderModal("New Worktree", body, []keyHint{{"enter", "create"}, {"esc", "cancel"}})
}

func renderAgentSelectorModal(m model) string {
	body := []string{"Choose which tool runs when pressing i.", ""}
	if len(m.cfg.App.Agents) == 0 {
		body = append(body, errorStyle.Render("No agents configured"))
	} else {
		for idx, agent := range m.cfg.App.Agents {
			label := fmt.Sprintf("%-10s %s", agent.Name, agent.Command)
			body = append(body, selectorLine(idx == m.agentCursor, label, agent.Name == m.cfg.App.DefaultAgent))
		}
	}
	body = append(body, "", mutedStyle.Render(fmt.Sprintf("Config: %s", m.cfg.ConfigPath)))
	return renderModal("Default AI Agent", body, selectHints)
}

func renderEnterActionSelectorModal(m model) string {
	body := []string{"Choose what enter does for the selected worktree.", ""}
	currentAction := normalizedEnterAction(m.cfg.App.DefaultEnterAction)
	for idx, option := range enterActionOptions {
		name := option.Label
		if option.Action == core.EnterActionVSCode {
			name = m.configuredEditor().Name
		}
		label := fmt.Sprintf("%-9s %s", name, option.Description)
		body = append(body, selectorLine(idx == m.enterActionCursor, label, option.Action == currentAction))
	}
	body = append(body, "", mutedStyle.Render(fmt.Sprintf("Config: %s", m.cfg.ConfigPath)))
	return renderModal("Enter Action", body, selectHints)
}

func renderProfileSelectorModal(m model) string {
	body := []string{"Choose which repository profile to manage.", ""}
	if len(m.cfg.App.Profiles) == 0 {
		body = append(body, errorStyle.Render("No profiles configured"))
	} else {
		for idx, profile := range m.cfg.App.Profiles {
			location := profile.RepositoryPath
			if repositories := profile.RepositoryList(); len(repositories) > 1 {
				names := make([]string, 0, len(repositories))
				for _, repository := range repositories {
					names = append(names, repository.Name)
				}
				location = fmt.Sprintf("%d repositories: %s", len(repositories), strings.Join(names, ", "))
			}
			label := fmt.Sprintf("%-14s %s", profile.Name, location)
			body = append(body, selectorLine(idx == m.profileCursor, label, profile.Name == m.cfg.App.DefaultProfile))
		}
	}
	body = append(body, "", mutedStyle.Render(fmt.Sprintf("Config: %s", m.cfg.ConfigPath)))
	return renderModal("Repository Profile", body, selectHints)
}

func renderPRUserSelectorModal(m model) string {
	body := []string{"Show PRs authored by, assigned to, or requesting this user.", ""}
	if len(m.prUserOptions) == 0 {
		body = append(body, mutedStyle.Render("No users discovered"))
	} else {
		for idx, user := range m.prUserOptions {
			label := user
			if idx > 0 {
				label = "@" + user
			}
			current := (idx == 0 && m.prUserFilter == "") || strings.EqualFold(user, m.prUserFilter)
			body = append(body, selectorLine(idx == m.prUserCursor, label, current))
		}
	}
	return renderModal("PR User Filter", body, selectHints)
}

func renderPRQuestionModal(m model) string {
	selected := m.selectedRemotePRForDetail()
	context := "No PR selected"
	if selected != nil {
		context = fmt.Sprintf("PR #%d: %s", selected.Number, selected.Title)
	}
	body := []string{
		styledWrappedLine(context, lipgloss.NewStyle(), 58),
		mutedStyle.Render("Agent: ") + subtleStyle.Render(m.prAgentLabel()),
		m.prQuestionInput.View(),
	}
	return renderModal("Ask Agent About PR", body, []keyHint{{"enter", "ask"}, {"esc", "cancel"}})
}

func renderPRApprovalModal(m model) string {
	selected := m.selectedRemotePRForDetail()
	context := fmt.Sprintf("PR #%d", m.prApprovalPendingNumber)
	if selected != nil && selected.Number == m.prApprovalPendingNumber {
		context = fmt.Sprintf("PR #%d: %s", selected.Number, selected.Title)
	}

	body := []string{
		styledWrappedLine(context, lipgloss.NewStyle(), 58),
		"",
		"Submit an approval review through GitHub CLI.",
	}
	if len(m.prApprovalWarnings) > 0 {
		body = append(body, "", warnStyle.Render("Warnings:"))
		for _, warning := range m.prApprovalWarnings {
			body = append(body, styledWrappedLine("  "+icons.Warn+" "+warning, warnStyle, 58))
		}
	}
	return renderModal("Approve Pull Request", body, []keyHint{{"enter", "approve"}, {"esc", "cancel"}})
}

func renderTicketStatusModal(m model) string {
	selected := m.selectedTicket()
	body := []string{"Choose a target status.", ""}
	if selected != nil {
		body[0] = fmt.Sprintf("Change %s from %s.", selected.Key, emptyDash(selected.Status))
	}
	for idx, status := range m.ticketStatusOptions {
		current := selected != nil && strings.EqualFold(status, selected.Status)
		body = append(body, selectorLine(idx == m.ticketStatusCursor, status, current))
	}
	return renderModal("JIRA Status", body, []keyHint{{"j/k", "move"}, {"enter", "select"}, {"e", "type status"}, {"esc", "cancel"}})
}

func renderTicketStatusInputModal(m model) string {
	return renderModal("JIRA Status", []string{"Enter the exact target workflow status.", "", m.ticketStatusInput.View()}, []keyHint{{"enter", "change"}, {"esc", "cancel"}})
}

func renderTicketCommentModal(m model) string {
	selected := m.selectedTicket()
	context := "Comment on selected ticket"
	if selected != nil {
		context = "Comment on " + selected.Key
	}
	return renderModal("JIRA Comment", []string{context, "", m.ticketCommentInput.View()}, []keyHint{{"enter", "submit"}, {"esc", "cancel"}})
}

func renderTicketAssigneeModal(m model) string {
	body := []string{"Choose an assignee.", ""}
	for idx, user := range m.ticketAssigneeOptions {
		label := user.Name()
		if user.AccountID != "" && user.AccountID != core.TicketAssigneeSelf && user.AccountID != customTicketAssigneeLabel {
			label += "  " + user.AccountID
		}
		body = append(body, selectorLine(idx == m.ticketAssigneeCursor, label, false))
	}
	return renderModal("JIRA Assignee", body, selectHints)
}

func renderTicketAssigneeInputModal(m model) string {
	return renderModal("JIRA Assignee", []string{"Enter @me, an email address, or an account ID.", "", m.ticketAssigneeInput.View()}, []keyHint{{"enter", "assign"}, {"esc", "cancel"}})
}

func renderBoardSelectorModal(m model) string {
	body := []string{"Choose the configured JIRA board.", ""}
	if jira := m.cfg.ActiveProfile.Jira; jira != nil {
		for idx, board := range jira.Boards {
			label := board.Name
			if len(board.Projects) > 0 {
				label += "  [" + strings.Join(board.Projects, ", ") + "]"
			}
			body = append(body, selectorLine(idx == m.boardCursor, label, board.Name == m.ticketBoard.Name))
		}
	}
	return renderModal("JIRA Board", body, selectHints)
}

func renderTicketFiltersModal(m model) string {
	options := []struct {
		label   string
		checked bool
	}{
		{label: "Only me", checked: m.ticketFilterMineOnly},
		{label: "Current sprint", checked: m.ticketFilterSprintOnly},
		{label: "Open (not closed)", checked: m.ticketFilterOpenOnly},
	}
	body := []string{"Choose which tickets to display.", ""}
	for idx, option := range options {
		check := "[ ]"
		if option.checked {
			check = "[x]"
		}
		body = append(body, selectorLine(idx == m.ticketFilterCursor, check+" "+option.label, false))
	}
	return renderModal("JIRA Ticket Filters", body, []keyHint{{"j/k", "move"}, {"space", "toggle"}, {"enter", "apply"}, {"esc", "cancel"}})
}

func helpKeyLine(key, action string) string {
	return "  " + keyHintKeyStyle.Render(padCell(key, 8)) + " " + action
}

func helpSection(title string) string {
	return infoStyle.Render(icons.Section + " " + title)
}

func renderHelpModal(m model) string {
	lines := []string{
		titleStyle.Render("Keybindings"),
		"",
		helpSection("Global"),
		helpKeyLine("ctrl+p", "command palette (map ⌘P in your terminal)"),
		helpKeyLine("h", "show or hide this help"),
		helpKeyLine("q/ctrl+c", "quit"),
		helpKeyLine("tab", "switch table/details focus"),
		helpKeyLine("r", "refresh current mode"),
		helpKeyLine("/", "filter current mode"),
		helpKeyLine("esc", "cancel input, clear filter, or close help"),
		helpKeyLine("s", "switch project"),
		helpKeyLine("I", "edit project: repositories, agents, JIRA"),
		helpKeyLine("m", "choose default AI agent"),
		"",
		helpSection("Worktrees"),
		helpKeyLine("j/k", "move selection"),
		helpKeyLine("a", "toggle managed/all worktrees"),
		helpKeyLine("n", "create worktree"),
		helpKeyLine("d d", "delete selected worktree"),
		helpKeyLine("o", "open selected local PR"),
		helpKeyLine("enter", "run selected Enter action"),
		helpKeyLine("e", "choose Enter action"),
		helpKeyLine("v", "open worktree in "+m.configuredEditor().Name),
		helpKeyLine("i", "open worktree with agent in a new terminal tab"),
		"           terminal action opens worktree in a new terminal tab",
		helpKeyLine("p", "toggle local PR body in details"),
		"",
		helpSection("PR Radar"),
		helpKeyLine("P", "toggle PR Radar mode"),
		helpKeyLine("S", "cycle state filter"),
		helpKeyLine("u", "choose involved-user filter"),
		helpKeyLine("Y", "toggle PRs authored by @me"),
		helpKeyLine("b", "switch final column between title/branch"),
		helpKeyLine("f", "show failed GitHub Actions in details"),
		helpKeyLine("enter", "open full-screen PR diff"),
		helpKeyLine("esc", "close full-screen PR diff"),
		helpKeyLine("A", "approve selected PR"),
		helpKeyLine("c", "ask configured agent about selected PR"),
		helpKeyLine("n", "create worktree from selected PR branch"),
		helpKeyLine("R", "choose the repository PR Radar shows"),
		helpKeyLine("o", "open selected PR in browser"),
		"",
		helpSection("JIRA Tickets"),
		helpKeyLine("J", "toggle JIRA Tickets mode"),
		helpKeyLine("j/k", "move selection and load ticket details"),
		helpKeyLine("enter", "open full-screen ticket reader"),
		helpKeyLine("f", "configure ticket filters"),
		helpKeyLine("Y", "toggle assigned-to-me/all tickets"),
		helpKeyLine("S", "toggle active-sprint scope"),
		helpKeyLine("b", "choose configured board"),
		helpKeyLine("t", "change selected ticket status"),
		helpKeyLine("C", "comment on selected ticket"),
		helpKeyLine("A", "assign selected ticket"),
		helpKeyLine("n", "create or reuse ticket worktree"),
		helpKeyLine("o", "open selected ticket in browser"),
		"",
		renderKeyHints([]keyHint{{"h/enter/esc", "close"}}),
	}
	return modalStyle.Width(max(72, min(92, m.width-8))).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func overlay(base, modal string, width, height int) string {
	modalContent := lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
	parts := []string{base, modalContent}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

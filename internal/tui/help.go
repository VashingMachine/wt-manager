package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The help page: the keys in effect, grouped by what they are for. One column
// lists what works in every view; the other lists the keys of a single view,
// with a tab per view and the current view shown first. Showing one view at a
// time keeps the page short enough for an 80x24 terminal, and it scrolls on
// anything smaller.

// helpViews are the tabs of the help page, in the order of the views.
var helpViews = []viewMode{modeWorktrees, modePRs, modeTickets}

var helpViewNames = map[viewMode]string{modeWorktrees: groupWorktrees, modePRs: groupPRs, modeTickets: groupTickets}

// helpViewColors match the badge each view wears in the header.
var helpViewColors = map[viewMode]lipgloss.Color{modeWorktrees: "81", modePRs: "171", modeTickets: "42"}

var (
	badgeHelpStyle       = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("141"))
	helpEverywhereColor  = lipgloss.Color("141")
	helpTextStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	helpCaptionStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))
	helpColumnGap        = " │ "
	helpTwoColumnsWidth  = 70
	helpColumnWidthLimit = 56
)

// helpEntry is one line of the page: the keys of a command and what they do.
// A command without a key has no chips.
type helpEntry struct {
	chips []string
	text  string
}

// helpSection is a titled group of entries.
type helpSection struct {
	title   string
	entries []helpEntry
}

// helpWriter builds entries from the keymap in effect.
type helpWriter struct {
	km keymap
}

// keys is how a command's keys fit one chip: all of them when they are short,
// and otherwise the first with a count of the rest.
func (h helpWriter) keys(id string) string {
	if id == cmdDelete {
		return h.km.shortcut(id)
	}
	labels := keySeqLabels(h.km.keysOf(id))
	if len(labels) == 0 {
		return ""
	}
	if all := strings.Join(labels, "/"); lipgloss.Width(all) <= 9 {
		return all
	}
	if len(labels) == 1 {
		return labels[0]
	}
	return fmt.Sprintf("%s +%d", labels[0], len(labels)-1)
}

func (h helpWriter) command(id, text string) helpEntry {
	if keys := h.keys(id); keys != "" {
		return helpEntry{chips: []string{keys}, text: text}
	}
	return helpEntry{text: text}
}

// fixed is a key that cannot be changed.
func fixedHelpEntry(key, text string) helpEntry {
	return helpEntry{chips: []string{key}, text: text}
}

// helpEverywhere lists the keys that work in every view.
func (m model) helpEverywhere(km keymap) []helpSection {
	h := helpWriter{km}
	move := helpEntry{text: "move selection"}
	for _, id := range []string{"up", "down"} {
		if keys := h.keys(id); keys != "" {
			move.chips = append(move.chips, keys)
		}
	}
	around := []helpEntry{
		move,
		h.command("focus", "switch table / details"),
		h.command("filter", "filter the rows"),
		fixedHelpEntry("esc", "clear filter / go back"),
		h.command("refresh", "refresh this view"),
	}
	// Showing worktrees has no key of its own unless one was added.
	if keys := h.keys("worktrees"); keys != "" {
		around = append(around, h.command("worktrees", "worktrees"))
	}
	around = append(around,
		h.command("prs", "PR Radar on / off"),
		h.command("tickets", "JIRA tickets on / off"),
	)
	return []helpSection{
		{title: "Get around", entries: around},
		{title: "Commands & settings", entries: []helpEntry{
			h.command(cmdPalette, "command palette"),
			h.command(cmdKeybindings, "customize these keys"),
			h.command(cmdHelp, "this help"),
			h.command("profile-menu", "switch project"),
			h.command("setup", "edit project"),
			h.command("agent-menu", "default AI agent"),
			h.command(cmdQuit, "quit (ctrl+c too)"),
		}},
	}
}

// helpForView lists the keys of one view.
func (m model) helpForView(km keymap, view viewMode) []helpSection {
	h := helpWriter{km}
	editor := m.configuredEditor().Name
	agent := strings.TrimSpace(m.cfg.App.DefaultAgent)
	if agent == "" {
		agent = "the AI agent"
	}
	switch view {
	case modePRs:
		return []helpSection{
			{title: "Review", entries: []helpEntry{
				h.command("diff", "read the diff full screen"),
				h.command("approve", "approve the pull request"),
				h.command("ask", "ask the AI agent about it"),
				h.command("open-browser", "open in the browser"),
			}},
			{title: "Filter & show", entries: []helpEntry{
				h.command("state", "cycle the state filter"),
				h.command("user", "filter by involved user"),
				h.command("mine", "only my pull requests"),
				h.command("branch", "show title or branch"),
				h.command("checks", "failed checks in details"),
				h.command("pr-repo", "choose the repository"),
			}},
			{title: "Work on it", entries: []helpEntry{
				h.command("new", "create a worktree for it"),
				h.command("open-editor", "…and open it in "+editor),
				h.command("open-agent", "…and open it with "+agent),
			}},
		}
	case modeTickets:
		return []helpSection{
			{title: "Read & update", entries: []helpEntry{
				h.command("ticket-read", "read the ticket full screen"),
				h.command("open-browser", "open in the browser"),
				h.command("ticket-status", "change status"),
				h.command("ticket-comment", "add a comment"),
				h.command("ticket-assign", "assign"),
			}},
			{title: "Filter", entries: []helpEntry{
				h.command("ticket-filters", "choose filters"),
				h.command("ticket-mine", "mine / everyone's"),
				h.command("ticket-sprint", "active sprint only"),
				h.command("ticket-board", "choose board or space"),
			}},
			{title: "Work on it", entries: []helpEntry{
				h.command("new", "create a worktree for it"),
				h.command("stack-new", "…on top of another worktree"),
				h.command("open-editor", "…and open it in "+editor),
				h.command("open-agent", "…and open it with "+agent),
			}},
		}
	}
	enter := "open in " + editor
	switch normalizedEnterAction(m.cfg.App.DefaultEnterAction) {
	case core.EnterActionAgent:
		enter = "open with " + agent
	case core.EnterActionTerminal:
		enter = "open in a terminal"
	}
	return []helpSection{
		{title: "Open", entries: []helpEntry{
			h.command("open", enter),
			h.command("enter-menu", "choose the Enter action"),
			h.command("open-editor", "open in "+editor),
			h.command("open-agent", "open with "+agent),
			h.command("open-browser", "open its PR in the browser"),
		}},
		{title: "Create", entries: []helpEntry{
			h.command("new", "new worktree"),
			h.command("stack-new", "new, on top of selected"),
			h.command("restack", "restack onto its base"),
		}},
		{title: "Manage", entries: []helpEntry{
			h.command(cmdDelete, "delete (press twice)"),
			h.command("sweep", "sweep stale worktrees"),
			h.command("toggle-all", "managed / all worktrees"),
			h.command("body", "full PR description"),
		}},
	}
}

// --- layout ----------------------------------------------------------------------

// helpLayout is how the page divides the terminal.
type helpLayout struct {
	width, height int
	bodyHeight    int
	// twoColumns puts the keys of every view beside those of one view;
	// narrow terminals stack them.
	twoColumns bool
	// leftWidth and rightWidth are the widths of the two columns.
	leftWidth, rightWidth int
	// roomy terminals get an introduction.
	roomy bool
}

func (m model) helpLayout() helpLayout {
	width, height := m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	layout := helpLayout{
		width:  max(20, width-appStyle.GetHorizontalFrameSize()),
		height: max(4, height-appStyle.GetVerticalFrameSize()),
	}
	layout.twoColumns = layout.width >= helpTwoColumnsWidth
	layout.leftWidth, layout.rightWidth = layout.width, layout.width
	if layout.twoColumns {
		columns := layout.width - lipgloss.Width(helpColumnGap)
		layout.leftWidth = min(helpColumnWidthLimit, columns/2)
		layout.rightWidth = min(helpColumnWidthLimit, columns-layout.leftWidth)
	}
	layout.roomy = layout.height >= 24
	// Title, tabs and a rule above the keys, and the hints below them. With
	// room to spare there is an introduction and a rule above the hints too.
	chrome := 4
	if layout.roomy {
		chrome += 3
	}
	layout.bodyHeight = max(1, layout.height-chrome)
	return layout
}

func helpSectionsHeight(sections []helpSection, spaced bool) int {
	height := 0
	for idx, section := range sections {
		if idx > 0 && spaced {
			height++
		}
		height += 1 + len(section.entries)
	}
	return height
}

func helpChipsWidth(chips []string) int {
	if len(chips) == 0 {
		return 1
	}
	width := len(chips) - 1
	for _, chip := range chips {
		width += lipgloss.Width(chip) + keyChipStyle.GetHorizontalPadding()
	}
	return width
}

// renderHelpColumn draws sections one under another, with the keys of every
// entry in a column of their own.
func renderHelpColumn(sections []helpSection, accent lipgloss.Color, width int, spaced bool) []string {
	keyWidth := 1
	for _, section := range sections {
		for _, entry := range section.entries {
			keyWidth = max(keyWidth, helpChipsWidth(entry.chips))
		}
	}
	keyWidth = min(keyWidth, max(3, width/2))
	title := lipgloss.NewStyle().Bold(true).Foreground(accent)

	var lines []string
	for idx, section := range sections {
		if idx > 0 && spaced {
			lines = append(lines, "")
		}
		lines = append(lines, title.Render(ansi.Truncate(section.title, width, "…")))
		for _, entry := range section.entries {
			lines = append(lines, renderHelpEntry(entry, keyWidth, width))
		}
	}
	return lines
}

func renderHelpEntry(entry helpEntry, keyWidth, width int) string {
	textWidth := max(0, width-keyWidth-3)
	if len(entry.chips) == 0 {
		// No key: the command is still in the palette.
		return " " + mutedStyle.Render(paletteFit(" —", keyWidth)) + "  " + mutedStyle.Render(ansi.Truncate(entry.text, textWidth, "…"))
	}
	chips := make([]string, len(entry.chips))
	for idx, chip := range entry.chips {
		chips[idx] = keyChipStyle.Render(chip)
	}
	keys := ansi.Truncate(strings.Join(chips, " "), keyWidth, "")
	keys += strings.Repeat(" ", max(0, keyWidth-lipgloss.Width(keys)))
	return " " + keys + "  " + helpTextStyle.Render(ansi.Truncate(entry.text, textWidth, "…"))
}

// renderHelpTabs draws a tab per view. The view the app is in carries a dot.
func renderHelpTabs(m model, width int) string {
	build := func(short bool) string {
		tabs := make([]string, len(helpViews))
		for idx, view := range helpViews {
			label := helpViewNames[view]
			if short {
				label = keybindingTabShortLabels[label]
			}
			if view == m.mode {
				label = "● " + label
			}
			style := keybindingTabStyle
			if view == m.helpTab {
				style = keybindingTabOnStyle.Background(helpViewColors[view])
			}
			tabs[idx] = style.Render(label)
		}
		return strings.Join(tabs, " ")
	}
	tabs := build(false)
	if lipgloss.Width(tabs) > width {
		tabs = build(true)
	}
	return ansi.Truncate(tabs, width, "")
}

// helpBody returns the lines above the keys, which stay in place, and the
// keys themselves, which scroll when they do not fit.
func (m model) helpBody(km keymap, layout helpLayout) (fixed, rows []string) {
	everywhere := m.helpEverywhere(km)
	view := m.helpForView(km, m.helpTab)
	accent := helpViewColors[m.helpTab]
	caption := helpCaptionStyle.Render("EVERY VIEW")
	rule := func(width int) string { return strings.Repeat("─", max(0, width)) }

	if !layout.twoColumns {
		// The view comes first: it is what the page was opened for.
		spaced := helpSectionsHeight(view, true)+2+helpSectionsHeight(everywhere, true) <= layout.bodyHeight
		rows = renderHelpColumn(view, accent, layout.width, spaced)
		rows = append(rows, "", caption)
		rows = append(rows, renderHelpColumn(everywhere, helpEverywhereColor, layout.width, spaced)...)
		return []string{renderHelpTabs(m, layout.width), mutedStyle.Render(rule(layout.width))}, rows
	}

	spaced := max(helpSectionsHeight(everywhere, true), helpSectionsHeight(view, true)) <= layout.bodyHeight
	left := renderHelpColumn(everywhere, helpEverywhereColor, layout.leftWidth, spaced)
	right := renderHelpColumn(view, accent, layout.rightWidth, spaced)
	gap := mutedStyle.Render(helpColumnGap)
	for idx := 0; idx < max(len(left), len(right)); idx++ {
		var leftLine, rightLine string
		if idx < len(left) {
			leftLine = left[idx]
		}
		if idx < len(right) {
			rightLine = right[idx]
		}
		rows = append(rows, paletteFit(leftLine, layout.leftWidth)+gap+rightLine)
	}
	fixed = []string{
		paletteFit(caption, layout.leftWidth) + gap + renderHelpTabs(m, layout.rightWidth),
		mutedStyle.Render(rule(layout.leftWidth) + "─┼─" + rule(layout.rightWidth)),
	}
	return fixed, rows
}

// helpScrollLimit is how far the keys can scroll.
func (m model) helpScrollLimit() int {
	layout := m.helpLayout()
	_, rows := m.helpBody(m.keymap(), layout)
	return max(0, len(rows)-layout.bodyHeight)
}

func renderHelpPage(m model) string {
	layout := m.helpLayout()
	km := m.keymap()

	lines := []string{lipgloss.JoinHorizontal(lipgloss.Center, titleStyle.Render("wt-manager"), "  ", badgeHelpStyle.Render("HELP"), " ", chipStyle.Render("Keyboard shortcuts"))}
	if layout.roomy {
		lines = append(lines, mutedStyle.Render("These are the keys in effect right now. ● marks the view you are in."), "")
	}

	fixed, rows := m.helpBody(km, layout)
	lines = append(lines, fixed...)
	limit := max(0, len(rows)-layout.bodyHeight)
	offset := max(0, min(m.helpScroll, limit))
	shown := rows[offset:min(len(rows), offset+layout.bodyHeight)]
	lines = append(lines, shown...)
	for filler := len(shown); filler < layout.bodyHeight; filler++ {
		lines = append(lines, "")
	}

	// How much is hidden below, when the keys do not fit.
	position := ""
	switch {
	case limit > 0 && offset < limit:
		position = fmt.Sprintf("↓ %d more", len(rows)-offset-layout.bodyHeight)
	case limit > 0:
		position = "end"
	}
	if layout.roomy {
		label := ""
		if position != "" {
			label = " " + position + " "
		}
		lines = append(lines, mutedStyle.Render(strings.Repeat("─", max(0, layout.width-lipgloss.Width(label)-2))+label+strings.Repeat("─", min(2, layout.width))))
		position = ""
	}

	// Most needed first: the last hints are dropped when they do not fit.
	hints := []keyHint{{"tab", "other view"}}
	if !layout.twoColumns {
		hints[0].Action = "view"
	}
	closing := "esc"
	if label := km.label(cmdHelp); label != "" {
		closing = label + "/esc"
	}
	hints = append(hints, keyHint{closing, "close"})
	if limit > 0 {
		hints = append(hints, keyHint{"↑↓", "scroll"})
	}
	if label := km.label(cmdKeybindings); label != "" {
		hints = append(hints, keyHint{label, "customize keys"})
	}
	if label := km.label(cmdPalette); label != "" {
		hints = append(hints, keyHint{label, "all commands"})
	}
	if position == "" {
		lines = append(lines, fitKeyHints(hints, layout.width))
	} else {
		room := max(0, layout.width-lipgloss.Width(position)-2)
		lines = append(lines, paletteFit(fitKeyHints(hints, room), room)+"  "+warnStyle.Render(position))
	}

	if len(lines) > layout.height {
		lines = lines[:layout.height]
	}
	for idx := range lines {
		lines[idx] = ansi.Truncate(lines[idx], layout.width, "…")
	}
	return appStyle.Render(strings.Join(lines, "\n"))
}

// --- keys ------------------------------------------------------------------------

func (m model) toggleHelp() (tea.Model, tea.Cmd) {
	if m.showingHelp {
		return m.closeHelp()
	}
	m.showingHelp = true
	m.helpTab = m.mode
	m.helpScroll = 0
	m.statusMessage = "Help"
	m.statusError = false
	return m, nil
}

func (m model) closeHelp() (tea.Model, tea.Cmd) {
	m.showingHelp = false
	return m, m.setTransientStatus("Help closed")
}

func (m model) updateHelpPage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	name := keyName(msg)
	// The key that opens the page also closes it, even while something else
	// is waiting behind it.
	if name != "" && slices.ContainsFunc(m.keymap().keysOf(cmdHelp), keySeq{name}.equal) {
		return m.closeHelp()
	}
	tab := slices.Index(helpViews, m.helpTab)
	page := max(1, m.helpLayout().bodyHeight-1)
	switch name {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "enter":
		return m.closeHelp()
	case "tab", "right", "l":
		m.helpTab = helpViews[(tab+1)%len(helpViews)]
		m.helpScroll = 0
	case "shift+tab", "left":
		m.helpTab = helpViews[(tab+len(helpViews)-1)%len(helpViews)]
		m.helpScroll = 0
	case "up", "k":
		m.helpScroll--
	case "down", "j":
		m.helpScroll++
	case "pgup", "ctrl+u":
		m.helpScroll -= page
	case "pgdown", "ctrl+d", "space":
		m.helpScroll += page
	case "home", "g":
		m.helpScroll = 0
	case "end", "G":
		m.helpScroll = m.helpScrollLimit()
	}
	m.helpScroll = max(0, min(m.helpScroll, m.helpScrollLimit()))
	return m, nil
}

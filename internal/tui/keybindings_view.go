package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/fspath"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	badgeSettingsStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("214"))
	keyChipStyle          = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("252")).Background(lipgloss.Color("238"))
	keyChipSelectedStyle  = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("81"))
	keyChipRecordedStyle  = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("214"))
	keybindingTabStyle    = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("245"))
	keybindingTabOnStyle  = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("232")).Background(lipgloss.Color("81"))
	keybindingTabOffStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("239"))
	keyChipOnRowStyle     = lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("254")).Background(lipgloss.Color("241"))
	keybindingRowOnColor  = lipgloss.Color("237")
	keyCaptureBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("81")).Padding(1, 2)
)

// keybindingsLayout is how the screen divides the terminal.
type keybindingsLayout struct {
	width, height int
	listHeight    int
	// roomy terminals get an introduction and a second line of details.
	roomy bool
}

func (m model) keybindingsLayout() keybindingsLayout {
	width, height := m.width, m.height
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 30
	}
	layout := keybindingsLayout{
		width:  max(20, width-appStyle.GetHorizontalFrameSize()),
		height: max(4, height-appStyle.GetVerticalFrameSize()),
	}
	layout.roomy = layout.height >= 20
	// Title, filter, tabs and a rule above the list; a rule, one line of
	// details, the status and the hints below it.
	chrome := 8
	if layout.roomy {
		chrome += 3
	}
	layout.listHeight = max(1, layout.height-chrome)
	return layout
}

func renderKeybindingsScreen(m model) string {
	layout := m.keybindingsLayout()
	screen := m.keybindings
	km := m.keymap()
	tab := keybindingTabs[screen.tab]
	all := m.keybindingRowsFor(km, keybindingTabAll)
	rows := slices.DeleteFunc(slices.Clone(all), func(row keybindingRow) bool { return !keybindingTabIncludes(tab, row) })
	cursor := max(0, min(screen.cursor, len(rows)-1))

	title := lipgloss.JoinHorizontal(lipgloss.Center, titleStyle.Render("wt-manager"), "  ", badgeSettingsStyle.Render("SETTINGS"), " ", chipStyle.Render("Keybindings"))
	customized := mutedStyle.Render("all defaults")
	if count := km.modifiedCount(); count > 0 {
		customized = warnStyle.Render(fmt.Sprintf("● %d customized", count))
	}
	lines := []string{paletteSplitLine(title, customized, layout.width)}
	if layout.roomy {
		lines = append(lines, mutedStyle.Render("Every command and its keys. Changes are saved to "+fspath.Compact(m.cfg.ConfigPath, m.cfg.HomeDir)+" as you make them."), "")
	}
	lines = append(lines,
		renderKeybindingFilter(m, layout.width, len(rows)),
		renderKeybindingTabs(screen.tab, keybindingTabCounts(all), layout.width),
		mutedStyle.Render(strings.Repeat("─", layout.width)),
	)

	list := keybindingLines(rows, keybindingTabGrouped(tab))
	columns := keybindingColumnsFor(layout.width)
	offset := keybindingScroll(list, cursor, screen.offset, layout.listHeight)
	shown := 0
	for idx := offset; idx < len(list) && shown < layout.listHeight; idx++ {
		line := list[idx]
		switch {
		case line.isRow():
			selected := line.row == cursor
			chip := -1
			if selected {
				chip = screen.chip
			}
			lines = append(lines, renderKeybindingRow(rows[line.row], selected, chip, columns))
		case line.heading != "":
			lines = append(lines, infoStyle.Bold(true).Render(strings.ToUpper(line.heading))+mutedStyle.Render(fmt.Sprintf("  %d", line.count)))
		default:
			lines = append(lines, "")
		}
		shown++
	}
	if len(rows) == 0 {
		lines = append(lines, mutedStyle.Render(keybindingEmptyMessage(m, tab)))
		shown++
	}
	for ; shown < layout.listHeight; shown++ {
		lines = append(lines, "")
	}

	position := ""
	if len(rows) > 0 {
		position = fmt.Sprintf(" %d of %d ", cursor+1, len(rows))
	}
	lines = append(lines, mutedStyle.Render(strings.Repeat("─", max(0, layout.width-lipgloss.Width(position)-2))+position+strings.Repeat("─", min(2, layout.width))))
	lines = append(lines, renderKeybindingDetails(km, rows, cursor, layout.roomy)...)
	lines = append(lines, renderKeybindingStatus(m), fitKeyHints(keybindingHints(m, layout.width), layout.width))

	if len(lines) > layout.height {
		lines = lines[:layout.height]
	}
	for idx := range lines {
		lines[idx] = ansi.Truncate(lines[idx], layout.width, "…")
	}
	view := appStyle.Render(strings.Join(lines, "\n"))
	switch {
	case screen.capture.active:
		view = placeOverlay(view, renderKeyCaptureModal(m, km), m.width, m.height)
	case screen.confirmingReset:
		view = placeOverlay(view, renderKeybindingResetModal(m, km), m.width, m.height)
	}
	return view
}

func keybindingEmptyMessage(m model, tab string) string {
	query := strings.TrimSpace(m.keybindings.filter.Value())
	switch {
	case query != "" && tab != keybindingTabAll:
		return fmt.Sprintf("No command in %s matches “%s”. Press tab to look in the other groups.", tab, query)
	case query != "":
		return fmt.Sprintf("No command matches “%s”.", query)
	case tab == keybindingTabCustomized:
		return "Nothing customized yet: every command has its default keys."
	}
	return "No commands."
}

func renderKeybindingFilter(m model, width, matches int) string {
	screen := m.keybindings
	query := screen.filter.Value()
	switch {
	case screen.filtering:
		input := screen.filter
		input.Width = max(8, width-6)
		input.PlaceholderStyle = mutedStyle
		return input.View()
	case query != "":
		found := fmt.Sprintf("%d matches", matches)
		if matches == 1 {
			found = "1 match"
		}
		return infoStyle.Bold(true).Render("/ ") + titleStyle.Render(query) + mutedStyle.Render("   "+found+" · esc clears")
	}
	return mutedStyle.Render("/  filter by command, key, or “no key”")
}

// keybindingTabCounts counts the listed commands of each tab.
func keybindingTabCounts(all []keybindingRow) []int {
	counts := make([]int, len(keybindingTabs))
	for idx, tab := range keybindingTabs {
		for _, row := range all {
			if keybindingTabIncludes(tab, row) {
				counts[idx]++
			}
		}
	}
	return counts
}

var keybindingTabShortLabels = map[string]string{groupWorktrees: "Trees", groupPRs: "PRs", groupTickets: "JIRA", keybindingTabCustomized: "Custom"}

// renderKeybindingTabs draws the tab strip as wide as it fits: with counts,
// without them, with short names, and at last only the tabs around the
// active one.
func renderKeybindingTabs(active int, counts []int, width int) string {
	build := func(short, withCounts bool) []string {
		tabs := make([]string, len(keybindingTabs))
		for idx, tab := range keybindingTabs {
			label := tab
			if short {
				if abbreviated, ok := keybindingTabShortLabels[tab]; ok {
					label = abbreviated
				}
			}
			if withCounts {
				label = fmt.Sprintf("%s %d", label, counts[idx])
			}
			style := keybindingTabStyle
			switch {
			case idx == active:
				style = keybindingTabOnStyle
			case counts[idx] == 0:
				style = keybindingTabOffStyle
			}
			tabs[idx] = style.Render(label)
		}
		return tabs
	}
	for _, variant := range [][2]bool{{false, true}, {false, false}, {true, false}} {
		if line := strings.Join(build(variant[0], variant[1]), " "); lipgloss.Width(line) <= width {
			return line
		}
	}
	tabs := build(true, false)
	first, last := active, active
	used := lipgloss.Width(tabs[active])
	// Two columns on each side are kept for the markers of hidden tabs.
	for grew := true; grew; {
		grew = false
		if last+1 < len(tabs) && used+1+lipgloss.Width(tabs[last+1]) <= width-4 {
			last++
			used += 1 + lipgloss.Width(tabs[last])
			grew = true
		}
		if first > 0 && used+1+lipgloss.Width(tabs[first-1]) <= width-4 {
			first--
			used += 1 + lipgloss.Width(tabs[first])
			grew = true
		}
	}
	line := strings.Join(tabs[first:last+1], " ")
	if first > 0 {
		line = mutedStyle.Render("‹ ") + line
	}
	if last < len(tabs)-1 {
		line += mutedStyle.Render(" ›")
	}
	return line
}

// keybindingColumns are the widths of a command row after its two-column
// marker: title, keys, and the state of the binding.
type keybindingColumns struct {
	title, keys, state int
}

func keybindingColumnsFor(width int) keybindingColumns {
	columns := keybindingColumns{state: 2}
	if width >= 64 {
		columns.state = 11
	}
	rest := max(0, width-2-columns.state)
	columns.title = max(10, min(46, rest*58/100))
	columns.keys = max(4, rest-columns.title-1)
	return columns
}

// renderKeybindingRow draws a command with its keys as chips. Every fragment
// carries the row's background, since a nested style would otherwise end it.
func renderKeybindingRow(row keybindingRow, selected bool, chip int, columns keybindingColumns) string {
	base := lipgloss.NewStyle()
	if selected {
		base = base.Background(keybindingRowOnColor)
	}
	marker := base.Render("  ")
	titleStyle := base.Foreground(lipgloss.Color("250"))
	if selected {
		marker = base.Bold(true).Foreground(lipgloss.Color("81")).Render("› ")
		titleStyle = base.Bold(true).Foreground(lipgloss.Color("255"))
	}
	title := titleStyle.Render(paletteFit(row.spec.Title, columns.title))

	state := ""
	switch {
	case row.clash:
		state = base.Foreground(lipgloss.Color("204")).Render(keybindingStateLabel("⚠", "clash", columns.state))
	case row.modified:
		state = base.Foreground(lipgloss.Color("214")).Render(keybindingStateLabel("●", "modified", columns.state))
	default:
		state = base.Render(strings.Repeat(" ", columns.state))
	}
	chipStyle := keyChipStyle
	if selected {
		chipStyle = keyChipOnRowStyle
	}
	return marker + title + base.Render(" ") + renderKeyChips(row.keys, chip, columns.keys, base, chipStyle) + state
}

func keybindingStateLabel(icon, word string, width int) string {
	label := " " + icon
	if width > 2 {
		label += " " + word
	}
	return paletteFit(label, width)
}

// renderKeyChips draws as many keys as fit, then how many more there are.
func renderKeyChips(keys []keySeq, selected, width int, base, chipStyle lipgloss.Style) string {
	if len(keys) == 0 {
		return base.Foreground(lipgloss.Color("240")).Render(paletteFit(" no key", width))
	}
	chips := make([]string, len(keys))
	total := len(keys) - 1
	for idx, label := range keySeqLabels(keys) {
		style := chipStyle
		if idx == selected {
			style = keyChipSelectedStyle
		}
		chips[idx] = style.Render(label)
		total += lipgloss.Width(chips[idx])
	}

	var line strings.Builder
	used := 0
	for idx, chip := range chips {
		gap := min(idx, 1)
		// Unless every key fits, keep room to say how many are left out.
		reserve := 0
		if total > width {
			reserve = len(fmt.Sprintf(" +%d", len(keys)-idx-1))
		}
		if used+gap+lipgloss.Width(chip)+reserve > width {
			rest := ansi.Truncate(strings.Repeat(" ", gap)+fmt.Sprintf("+%d", len(keys)-idx), max(0, width-used), "")
			line.WriteString(base.Foreground(lipgloss.Color("245")).Render(rest))
			used += lipgloss.Width(rest)
			break
		}
		line.WriteString(base.Render(strings.Repeat(" ", gap)))
		line.WriteString(chip)
		used += gap + lipgloss.Width(chip)
	}
	return line.String() + base.Render(strings.Repeat(" ", max(0, width-used)))
}

func renderKeybindingDetails(km keymap, rows []keybindingRow, cursor int, roomy bool) []string {
	if len(rows) == 0 {
		if roomy {
			return []string{"", ""}
		}
		return []string{""}
	}
	row := rows[cursor]
	summary := row.spec.Description
	if summary == "" {
		summary = row.spec.Title
	}
	lines := []string{subtleStyle.Render(summary)}
	if row.clash {
		lines[0] = errorStyle.Render(icons.Warn + " " + keybindingClashMessage(km, row))
	}
	if roomy {
		defaults := "none"
		if keys := parseKeySeqs(row.spec.Keys); len(keys) > 0 {
			defaults = strings.Join(keySeqLabels(keys), ", ")
		}
		lines = append(lines, mutedStyle.Render(fmt.Sprintf("Works in %s · default %s · config id %s", row.spec.Scope.Label(), defaults, row.spec.ID)))
	}
	return lines
}

func keybindingClashMessage(km keymap, row keybindingRow) string {
	for _, keys := range row.keys {
		for _, conflict := range km.conflicts(row.spec.ID, keys) {
			title := conflict.ID
			if spec, ok := km.spec(conflict.ID); ok {
				title = spec.Title
			}
			return fmt.Sprintf("%s is also a key of “%s”, so only one of them runs. Change either one.", keys.Label(), title)
		}
	}
	return "This key belongs to another command too."
}

func renderKeybindingStatus(m model) string {
	message := strings.TrimSpace(m.statusMessage)
	switch {
	case message == "":
		return ""
	case m.statusError:
		return errorStyle.Render(icons.Err + " " + message)
	}
	return infoStyle.Render(icons.Dot+" ") + subtleStyle.Render(message)
}

// keybindingHints lists the keys of the screen, most needed first. A narrow
// terminal gets shorter words so that leaving stays among them.
func keybindingHints(m model, width int) []keyHint {
	if m.keybindings.filtering {
		return []keyHint{{"type", "filter"}, {"enter", "keep"}, {"esc", "clear"}}
	}
	closing := keyHint{"esc", "close"}
	if m.keybindings.filter.Value() != "" {
		closing = keyHint{"esc", "clear filter"}
	}
	if width < 100 {
		return []keyHint{{"↵", "set"}, {"a", "add"}, {"x", "del"}, closing, {"r", "reset"}, {"/", "find"}, {"tab", "group"}}
	}
	hints := []keyHint{{"enter", "change"}, {"a", "add"}, {"x", "remove"}, {"r", "reset"}, {"/", "filter"}, {"tab", "group"}, closing}
	if row, ok := m.selectedKeybindingRow(); ok && len(row.keys) > 1 {
		hints = append(hints, keyHint{"←→", "pick key"})
	}
	return append(hints, keyHint{"u", "undo"}, keyHint{"R", "reset all"})
}

// fitKeyHints renders the hints that fit the width, dropping the last ones
// first.
func fitKeyHints(hints []keyHint, width int) string {
	for count := len(hints); count > 0; count-- {
		if line := renderKeyHints(hints[:count]); lipgloss.Width(line) <= width {
			return line
		}
	}
	return ""
}

// --- dialogs ---------------------------------------------------------------------

func keybindingModalWidth(m model) int {
	return max(24, min(60, m.width-8))
}

func renderKeybindingModal(m model, lines []string) string {
	width := keybindingModalWidth(m)
	for idx := range lines {
		lines[idx] = ansi.Truncate(lines[idx], width, "…")
	}
	return keyCaptureBoxStyle.Width(width + keyCaptureBoxStyle.GetHorizontalPadding()).Render(strings.Join(lines, "\n"))
}

// renderKeyCaptureModal shows the keys recorded so far and what saving them
// would do, before anything changes.
func renderKeyCaptureModal(m model, km keymap) string {
	capture := m.keybindings.capture
	spec, _ := km.spec(capture.command)
	existing := km.keysOf(spec.ID)
	width := keybindingModalWidth(m)

	title := "Set keybinding"
	current := ""
	switch {
	case capture.replace >= 0 && capture.replace < len(existing):
		title = "Change keybinding"
		current = mutedStyle.Render("replaces ") + keyChipStyle.Render(existing[capture.replace].Label())
	case len(existing) > 0:
		title = "Add keybinding"
		current = mutedStyle.Render("in addition to ") + strings.Join(renderedChips(existing), " ")
	}
	lines := []string{
		titleStyle.Render(title),
		lipgloss.NewStyle().Bold(true).Render(spec.Title) + mutedStyle.Render(" · "+spec.Scope.Label()),
	}
	if current != "" {
		lines = append(lines, current)
	}
	lines = append(lines, "")

	hints := []keyHint{{"esc", "cancel"}}
	if len(capture.keys) == 0 {
		lines = append(lines,
			infoStyle.Bold(true).Render("Press the key for this command…"),
			mutedStyle.Render("A second key right after it makes a two-key sequence."),
		)
	} else {
		recorded := keyChipRecordedStyle.Render(capture.keys.Label())
		if len(capture.keys) < maxKeySteps {
			recorded += mutedStyle.Render("  + another key for a sequence")
		}
		lines = append(lines, recorded, "")
		lines = append(lines, keyCaptureVerdict(km, spec, capture, width)...)
		hints = []keyHint{{"enter", "save"}, {"backspace", "clear"}, {"esc", "cancel"}}
	}
	if capture.notice != "" {
		lines = append(lines, "", warnStyle.Render(icons.Warn+" "+capture.notice))
	}
	lines = append(lines, "", fitKeyHints(hints, width))
	return renderKeybindingModal(m, lines)
}

func renderedChips(keys []keySeq) []string {
	chips := make([]string, len(keys))
	for idx, label := range keySeqLabels(keys) {
		chips[idx] = keyChipStyle.Render(label)
	}
	return chips
}

// keyCaptureVerdict says whether the recorded keys are free, and which
// commands would lose theirs otherwise.
func keyCaptureVerdict(km keymap, spec commandSpec, capture keyCapture, width int) []string {
	conflicts := km.conflicts(spec.ID, capture.keys)
	if held := slices.IndexFunc(km.keysOf(spec.ID), capture.keys.equal); held >= 0 && (capture.replace < 0 || capture.replace == held) && len(conflicts) == 0 {
		return []string{mutedStyle.Render("● This command already has that key")}
	}
	if len(conflicts) == 0 {
		return []string{successStyle.Render(icons.OK + " Free in " + spec.Scope.Label())}
	}
	var lines []string
	for idx, conflict := range conflicts {
		if idx == 3 {
			lines = append(lines, warnStyle.Render(fmt.Sprintf("  and %d more", len(conflicts)-idx)))
			break
		}
		title := conflict.ID
		if other, ok := km.spec(conflict.ID); ok {
			title = other.Title
		}
		lines = append(lines, warnStyle.Render(ansi.Truncate(fmt.Sprintf("%s Takes %s from “%s”", icons.Warn, conflict.Keys.Label(), title), width, "…")))
	}
	return append(lines, mutedStyle.Render("Saving moves the key here."))
}

func renderKeybindingResetModal(m model, km keymap) string {
	count := km.modifiedCount()
	what := fmt.Sprintf("%d commands go back to their default keys.", count)
	if count == 1 {
		what = "1 command goes back to its default keys."
	}
	lines := []string{
		titleStyle.Render("Reset all keybindings?"),
		"",
		what,
		mutedStyle.Render("u undoes this afterwards."),
		"",
		fitKeyHints([]keyHint{{"enter", "reset all"}, {"esc", "cancel"}}, keybindingModalWidth(m)),
	}
	return renderKeybindingModal(m, lines)
}

// placeOverlay draws a dialog over the middle of a view, keeping what is
// around it visible.
func placeOverlay(base, dialog string, width, height int) string {
	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}
	dialogLines := strings.Split(dialog, "\n")
	dialogWidth := lipgloss.Width(dialog)
	top := max(0, (height-len(dialogLines))/2)
	left := max(0, (width-dialogWidth)/2)
	for idx, line := range dialogLines {
		y := top + idx
		if y >= len(baseLines) {
			break
		}
		row := baseLines[y]
		head := ansi.Truncate(row, left, "")
		head += strings.Repeat(" ", max(0, left-lipgloss.Width(head)))
		tail := ansi.TruncateLeft(row, left+dialogWidth, "")
		baseLines[y] = head + "\x1b[0m" + line + "\x1b[0m" + tail
	}
	return strings.Join(baseLines, "\n")
}

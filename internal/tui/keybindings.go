package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// The keybindings screen: every command with its keys, grouped into tabs and
// filterable, where a key is changed by pressing it. Each change is written
// to the config file at once.

const (
	keybindingTabAll        = "All"
	keybindingTabCustomized = "Customized"
)

// keybindingTabs are the views of the command list: everything, one group, or
// only what differs from the defaults.
var keybindingTabs = append(append([]string{keybindingTabAll}, keybindingGroups...), keybindingTabCustomized)

// keybindingHistoryLimit caps how many changes can be undone.
const keybindingHistoryLimit = 50

type keybindingsScreen struct {
	open bool
	tab  int
	// cursor is the selected row; cursorID keeps the selection on the same
	// command while the list is filtered or changes around it.
	cursor   int
	cursorID string
	// chip is the selected key of the selected command.
	chip      int
	offset    int
	filter    textinput.Model
	filtering bool
	capture   keyCapture
	// confirmingReset asks before every keybinding goes back to its default.
	confirmingReset bool
	// history holds the keybindings before each change, newest last.
	history []map[string][]string
}

// keyCapture records the keys pressed for a new keybinding.
type keyCapture struct {
	active  bool
	command string
	// replace is the key of the command being replaced, or -1 to add one.
	replace int
	keys    keySeq
	notice  string
}

func newKeybindingsScreen() keybindingsScreen {
	filter := textinput.New()
	filter.Prompt = "/ "
	filter.Placeholder = "command, key, or “no key”"
	filter.CharLimit = 80
	filter.Width = 40
	filter.PromptStyle = infoStyle.Bold(true)
	return keybindingsScreen{filter: filter}
}

func (m model) openKeybindings() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.showingHelp = false
	m.keybindings = newKeybindingsScreen()
	m.keybindings.open = true
	m.statusMessage = ""
	m.statusError = false
	m.syncKeybindingCursor()
	return m, nil
}

// keybindingRow is a command as the screen lists it.
type keybindingRow struct {
	spec     commandSpec
	keys     []keySeq
	modified bool
	// clash marks a key that another command of the same view also has.
	clash bool
}

func (row keybindingRow) matches(terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	text := []string{row.spec.Title, row.spec.Description, row.spec.Group, row.spec.ID, row.spec.Scope.Label()}
	text = append(text, keySeqLabels(row.keys)...)
	text = append(text, keySeqStrings(row.keys)...)
	if len(row.keys) == 0 {
		text = append(text, "no key unset unbound")
	}
	if row.modified {
		text = append(text, "modified customized")
	}
	haystack := strings.ToLower(strings.Join(text, " "))
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

func keybindingTabIncludes(tab string, row keybindingRow) bool {
	switch tab {
	case keybindingTabAll:
		return true
	case keybindingTabCustomized:
		return row.modified
	}
	return row.spec.Group == tab
}

// keybindingRowsFor lists the commands of a tab that match the filter, group
// by group.
func (m model) keybindingRowsFor(km keymap, tab string) []keybindingRow {
	terms := strings.Fields(strings.ToLower(m.keybindings.filter.Value()))
	var rows []keybindingRow
	for _, group := range keybindingGroups {
		for _, spec := range km.specs {
			if spec.Group != group {
				continue
			}
			row := keybindingRow{spec: spec, keys: km.keysOf(spec.ID), modified: km.modified(spec.ID), clash: km.clashes(spec.ID)}
			if keybindingTabIncludes(tab, row) && row.matches(terms) {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func (m model) keybindingRows() []keybindingRow {
	return m.keybindingRowsFor(m.keymap(), keybindingTabs[m.keybindings.tab])
}

func (m model) selectedKeybindingRow() (keybindingRow, bool) {
	rows := m.keybindingRows()
	if m.keybindings.cursor < 0 || m.keybindings.cursor >= len(rows) {
		return keybindingRow{}, false
	}
	return rows[m.keybindings.cursor], true
}

// keybindingLine is a line of the list: a command row, a group heading, or a
// blank line between groups.
type keybindingLine struct {
	row     int
	heading string
	count   int
}

func (line keybindingLine) isRow() bool { return line.row >= 0 }

// keybindingLines lays the rows out, with a heading over each group when the
// tab mixes groups.
func keybindingLines(rows []keybindingRow, grouped bool) []keybindingLine {
	lines := make([]keybindingLine, 0, len(rows)+2*len(keybindingGroups))
	for idx, row := range rows {
		if grouped && (idx == 0 || rows[idx-1].spec.Group != row.spec.Group) {
			if idx > 0 {
				lines = append(lines, keybindingLine{row: -1})
			}
			count := 0
			for _, other := range rows {
				if other.spec.Group == row.spec.Group {
					count++
				}
			}
			lines = append(lines, keybindingLine{row: -1, heading: row.spec.Group, count: count})
		}
		lines = append(lines, keybindingLine{row: idx})
	}
	return lines
}

func keybindingTabGrouped(tab string) bool {
	return tab == keybindingTabAll || tab == keybindingTabCustomized
}

// keybindingScroll returns the first visible line that keeps the selected row
// on screen, together with the heading above it when it starts a group.
func keybindingScroll(lines []keybindingLine, cursor, offset, height int) int {
	selected := 0
	for idx, line := range lines {
		if line.row == cursor {
			selected = idx
			break
		}
	}
	top := selected
	if selected > 0 && lines[selected-1].heading != "" {
		top = selected - 1
	}
	if top < offset {
		offset = top
	}
	if selected >= offset+height {
		offset = selected - height + 1
	}
	return max(0, min(offset, len(lines)-height))
}

// syncKeybindingCursor keeps the selection on its command after the list
// changed, or on the nearest row when the command left the list.
func (m *model) syncKeybindingCursor() {
	screen := &m.keybindings
	rows := m.keybindingRows()
	if len(rows) == 0 {
		screen.cursor, screen.cursorID, screen.chip, screen.offset = 0, "", 0, 0
		return
	}
	found := slices.IndexFunc(rows, func(row keybindingRow) bool { return row.spec.ID == screen.cursorID })
	if found >= 0 {
		screen.cursor = found
	} else {
		screen.cursor = max(0, min(screen.cursor, len(rows)-1))
		screen.chip = 0
	}
	row := rows[screen.cursor]
	screen.cursorID = row.spec.ID
	screen.chip = max(0, min(screen.chip, len(row.keys)-1))
	lines := keybindingLines(rows, keybindingTabGrouped(keybindingTabs[screen.tab]))
	screen.offset = keybindingScroll(lines, screen.cursor, screen.offset, m.keybindingsLayout().listHeight)
}

func (m *model) moveKeybindingCursor(delta int) {
	screen := &m.keybindings
	rows := m.keybindingRows()
	if len(rows) == 0 {
		return
	}
	screen.cursor = max(0, min(screen.cursor+delta, len(rows)-1))
	screen.cursorID = rows[screen.cursor].spec.ID
	screen.chip = 0
	m.syncKeybindingCursor()
}

func (m model) updateKeybindings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	screen := &m.keybindings
	switch {
	case screen.capture.active:
		return m.updateKeyCapture(msg)
	case screen.confirmingReset:
		return m.updateKeybindingReset(msg)
	case screen.filtering:
		return m.updateKeybindingFilter(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		if screen.filter.Value() != "" {
			screen.filter.SetValue("")
			m.syncKeybindingCursor()
			return m, nil
		}
		screen.open = false
		m.statusMessage = ""
		m.statusError = false
		return m, nil
	case "up", "k":
		m.moveKeybindingCursor(-1)
	case "down", "j":
		m.moveKeybindingCursor(1)
	case "pgup", "ctrl+u":
		m.moveKeybindingCursor(-max(1, m.keybindingsLayout().listHeight-1))
	case "pgdown", "ctrl+d":
		m.moveKeybindingCursor(max(1, m.keybindingsLayout().listHeight-1))
	case "home", "g":
		m.moveKeybindingCursor(-len(m.commandCatalog()))
	case "end", "G":
		m.moveKeybindingCursor(len(m.commandCatalog()))
	case "left", "h":
		screen.chip = max(0, screen.chip-1)
	case "right", "l":
		if row, ok := m.selectedKeybindingRow(); ok {
			screen.chip = max(0, min(screen.chip+1, len(row.keys)-1))
		}
	case "tab":
		screen.tab = (screen.tab + 1) % len(keybindingTabs)
		screen.offset = 0
		m.syncKeybindingCursor()
	case "shift+tab":
		screen.tab = (screen.tab + len(keybindingTabs) - 1) % len(keybindingTabs)
		screen.offset = 0
		m.syncKeybindingCursor()
	case "/":
		// A search looks through every command; tab narrows it down again.
		screen.tab = 0
		screen.filtering = true
		screen.filter.CursorEnd()
		m.syncKeybindingCursor()
		return m, m.keybindings.filter.Focus()
	case "enter":
		return m.startKeyCapture(false)
	case "a", "+":
		return m.startKeyCapture(true)
	case "x", "backspace", "delete":
		return m.removeSelectedKeybinding()
	case "r":
		return m.resetSelectedKeybinding()
	case "R":
		if m.keymap().modifiedCount() == 0 && len(m.cfg.App.Keybindings) == 0 {
			return m, m.setTransientStatus("Every keybinding already has its default")
		}
		screen.confirmingReset = true
	case "u":
		return m.undoKeybindingChange()
	}
	return m, nil
}

func (m model) updateKeybindingFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	screen := &m.keybindings
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		screen.filter.SetValue("")
		screen.filter.Blur()
		screen.filtering = false
		m.syncKeybindingCursor()
		return m, nil
	case "enter", "down", "up", "tab":
		screen.filter.Blur()
		screen.filtering = false
		return m, nil
	}
	before := screen.filter.Value()
	var cmd tea.Cmd
	screen.filter, cmd = screen.filter.Update(msg)
	if screen.filter.Value() != before {
		screen.cursor, screen.cursorID, screen.offset = 0, "", 0
		m.syncKeybindingCursor()
	}
	return m, cmd
}

// --- recording a key -------------------------------------------------------------

// startKeyCapture starts recording a key for the selected command: another
// one when adding, and otherwise a replacement for the selected key.
func (m model) startKeyCapture(adding bool) (tea.Model, tea.Cmd) {
	row, ok := m.selectedKeybindingRow()
	if !ok {
		return m, nil
	}
	replace := -1
	if !adding && len(row.keys) > 0 {
		replace = max(0, min(m.keybindings.chip, len(row.keys)-1))
	}
	m.keybindings.capture = keyCapture{active: true, command: row.spec.ID, replace: replace}
	m.statusMessage = ""
	m.statusError = false
	return m, nil
}

// captureKeyName is the name of a key press that can go into a keybinding, or
// "" for input that is not one key, such as pasted text.
func captureKeyName(msg tea.KeyMsg) string {
	if msg.Paste || (msg.Type == tea.KeyRunes && len(msg.Runes) != 1) {
		return ""
	}
	return keyName(msg)
}

func (m model) updateKeyCapture(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	capture := &m.keybindings.capture
	if msg.String() == "esc" {
		*capture = keyCapture{}
		return m, m.setTransientStatus("Keybinding left as it was")
	}
	name := captureKeyName(msg)
	switch {
	case name == "":
		capture.notice = "That was not a single key"
		return m, nil
	case name == "ctrl+c":
		capture.notice = "ctrl+c always quits, so no command can have it"
		return m, nil
	}
	// The first key is always recorded, so enter and backspace can be bound
	// too; after that they finish or clear the recording.
	if len(capture.keys) > 0 {
		switch name {
		case "enter":
			return m.commitKeyCapture()
		case "backspace":
			capture.keys, capture.notice = nil, ""
			return m, nil
		}
	}
	if len(capture.keys) >= maxKeySteps {
		capture.keys = nil
	}
	capture.keys = append(capture.keys, name)
	capture.notice = ""
	return m, nil
}

func (m model) commitKeyCapture() (tea.Model, tea.Cmd) {
	capture := m.keybindings.capture
	m.keybindings.capture = keyCapture{}
	km := m.keymap()
	spec, ok := km.spec(capture.command)
	if !ok || len(capture.keys) == 0 {
		return m, nil
	}
	// Recording a key the command already has changes nothing, unless another
	// command has it too: then this settles which one keeps it.
	if held := slices.IndexFunc(km.keysOf(spec.ID), capture.keys.equal); held >= 0 && (capture.replace < 0 || capture.replace == held) && len(km.conflicts(spec.ID, capture.keys)) == 0 {
		m.keybindings.chip = held
		return m, m.setTransientStatus(fmt.Sprintf("%s already has %s", spec.Title, capture.keys.Label()))
	}
	change := km.bind(m.cfg.App.Keybindings, spec.ID, capture.keys, capture.replace)
	message := fmt.Sprintf("%s is now on %s", spec.Title, capture.keys.Label())
	if capture.replace < 0 && len(km.keysOf(spec.ID)) > 0 {
		message = fmt.Sprintf("%s also answers to %s", spec.Title, capture.keys.Label())
	}
	next, cmd := m.applyKeybindings(change, message)
	updated := next.(model)
	if held := slices.IndexFunc(updated.keymap().keysOf(spec.ID), capture.keys.equal); held >= 0 {
		updated.keybindings.chip = held
	}
	return updated, cmd
}

// --- changing and saving ---------------------------------------------------------

// takenSummary says which commands lost a key to a change.
func (km keymap) takenSummary(taken []keyConflict) string {
	if len(taken) == 0 {
		return ""
	}
	parts := make([]string, 0, len(taken))
	for _, conflict := range taken {
		title := conflict.ID
		if spec, ok := km.spec(conflict.ID); ok {
			title = spec.Title
		}
		parts = append(parts, fmt.Sprintf("%s lost %s", title, conflict.Keys.Label()))
	}
	return strings.Join(parts, ", ")
}

// applyKeybindings adopts changed keybindings and saves them to the config
// file. The previous ones are kept for undo.
func (m model) applyKeybindings(change keybindingChange, message string) (tea.Model, tea.Cmd) {
	if keybindingsEqual(m.cfg.App.Keybindings, change.Overrides) {
		return m, nil
	}
	// Without a key for the palette or for this screen, nothing in the app
	// could bring either back.
	if next := newKeymap(m.commandCatalog(), change.Overrides); len(next.keysOf(cmdPalette)) == 0 && len(next.keysOf(cmdKeybindings)) == 0 {
		m.statusMessage = "Not changed: the command palette or this screen has to keep a key, or there would be no way back here"
		m.statusError = true
		return m, nil
	}
	if summary := m.keymap().takenSummary(change.Taken); summary != "" {
		message += " · " + summary
	}
	screen := &m.keybindings
	screen.history = append(screen.history, cloneKeybindings(m.cfg.App.Keybindings))
	if len(screen.history) > keybindingHistoryLimit {
		screen.history = screen.history[len(screen.history)-keybindingHistoryLimit:]
	}
	m.cfg.App.Keybindings = change.Overrides
	m.syncKeybindingCursor()
	m.statusMessage = message
	m.statusError = false
	return m, m.persistConfigWith("the keybinding change", true)
}

func (m model) removeSelectedKeybinding() (tea.Model, tea.Cmd) {
	row, ok := m.selectedKeybindingRow()
	if !ok {
		return m, nil
	}
	if len(row.keys) == 0 {
		return m, m.setTransientStatus(row.spec.Title + " has no key to remove")
	}
	index := max(0, min(m.keybindings.chip, len(row.keys)-1))
	message := fmt.Sprintf("Removed %s from %s", row.keys[index].Label(), row.spec.Title)
	if len(row.keys) == 1 {
		message += "; it has no key now"
	}
	return m.applyKeybindings(m.keymap().unbind(m.cfg.App.Keybindings, row.spec.ID, index), message)
}

func (m model) resetSelectedKeybinding() (tea.Model, tea.Cmd) {
	row, ok := m.selectedKeybindingRow()
	if !ok {
		return m, nil
	}
	if !row.modified {
		return m, m.setTransientStatus(row.spec.Title + " already has its default")
	}
	message := row.spec.Title + " has no key again, as by default"
	if defaults := parseKeySeqs(row.spec.Keys); len(defaults) > 0 {
		message = fmt.Sprintf("%s is back on %s", row.spec.Title, strings.Join(keySeqLabels(defaults), ", "))
	}
	m.keybindings.chip = 0
	return m.applyKeybindings(m.keymap().reset(m.cfg.App.Keybindings, row.spec.ID), message)
}

func (m model) updateKeybindingReset(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter", "y":
		m.keybindings.confirmingReset = false
		m.keybindings.chip = 0
		return m.applyKeybindings(keybindingChange{}, "Every keybinding is back to its default")
	case "esc", "n":
		m.keybindings.confirmingReset = false
		return m, m.setTransientStatus("Keybindings left as they were")
	}
	return m, nil
}

func (m model) undoKeybindingChange() (tea.Model, tea.Cmd) {
	screen := &m.keybindings
	if len(screen.history) == 0 {
		return m, m.setTransientStatus("Nothing to undo")
	}
	previous := screen.history[len(screen.history)-1]
	screen.history = screen.history[:len(screen.history)-1]
	m.cfg.App.Keybindings = previous
	m.syncKeybindingCursor()
	m.statusMessage = "Undid the last keybinding change"
	m.statusError = false
	return m, m.persistConfigWith("the keybinding change", true)
}

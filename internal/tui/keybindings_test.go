package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// openKeybindingsOn opens the keybindings screen with one command selected.
func openKeybindingsOn(t *testing.T, m model, command string) model {
	t.Helper()
	m, _ = tapKeys(m, ",")
	if !m.keybindings.open {
		t.Fatal("the keybindings screen did not open")
	}
	m.keybindings.cursorID = command
	m.syncKeybindingCursor()
	if row, ok := m.selectedKeybindingRow(); !ok || row.spec.ID != command {
		t.Fatalf("command %q is not listed", command)
	}
	return m
}

func keybindingRowIDs(m model) []string {
	var ids []string
	for _, row := range m.keybindingRows() {
		ids = append(ids, row.spec.ID)
	}
	return ids
}

// Assures that the keybindings screen opens from its key and from the
// palette, lists commands with their keys under group headings, and closes
// with esc back to the view it was opened from.
func TestKeybindingsScreenOpensAndListsCommands(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Profiles = []RepositoryProfile{{Name: "shop"}}
	fromPalette, _ := choosePaletteEntry(t, m, cmdKeybindings)
	fromKey, _ := tapKeys(m, ",")
	if !fromPalette.keybindings.open || !fromKey.keybindings.open {
		t.Fatalf("screen open: palette=%v key=%v", fromPalette.keybindings.open, fromKey.keybindings.open)
	}

	view := ansi.Strip(fromKey.View())
	for _, want := range []string{"SETTINGS", "Keybindings", "all defaults", "GENERAL", "Open command palette", "ctrl+p", "Refresh current view", "Move selection up", "↑", "no key", "All", "Customized"} {
		if !strings.Contains(view, want) {
			t.Fatalf("screen is missing %q:\n%s", want, view)
		}
	}
	titles := map[string]string{}
	for _, row := range fromKey.keybindingRows() {
		titles[row.spec.ID] = row.spec.Group
	}
	for id, group := range map[string]string{"refresh": groupGeneral, "delete": groupWorktrees, "approve": groupPRs, "ticket-status": groupTickets, "agent:opencode": groupSettings, "editor:idea": groupSettings, "enter:terminal": groupSettings, "profile:shop": groupProjects} {
		if titles[id] != group {
			t.Fatalf("command %q is listed under %q, want %q", id, titles[id], group)
		}
	}

	closed, _ := tapKeys(fromKey, "esc")
	if closed.keybindings.open || !closed.isWorktreeMode() || closed.selectedWorktreePath() != m.selectedWorktreePath() {
		t.Fatal("esc did not return to the view the screen was opened from")
	}
	if got, cmd := tapKeys(fromKey, "q"); !got.keybindings.open || cmd != nil {
		t.Fatal("a command key of the main view ran behind the screen")
	}
}

// Assures that tabs narrow the list to one group, that the Customized tab
// lists only changed commands, and that tab and shift+tab wrap around.
func TestKeybindingsScreenTabsShowOneGroup(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"refresh": {"f5"}, "logs": {"g l"}}
	m, _ = tapKeys(m, ",")

	seen := map[string]int{}
	for range keybindingTabs {
		tab := keybindingTabs[m.keybindings.tab]
		rows := m.keybindingRows()
		seen[tab] = len(rows)
		for _, row := range rows {
			if tab != keybindingTabAll && tab != keybindingTabCustomized && row.spec.Group != tab {
				t.Fatalf("tab %q lists %q of group %q", tab, row.spec.ID, row.spec.Group)
			}
		}
		if row, ok := m.selectedKeybindingRow(); len(rows) > 0 && (!ok || row.spec.ID != rows[0].spec.ID) {
			t.Fatalf("tab %q did not select its first command", tab)
		}
		m, _ = tapKeys(m, "tab")
	}
	if m.keybindings.tab != 0 || len(seen) != len(keybindingTabs) {
		t.Fatalf("tab did not wrap around: tab=%d seen=%v", m.keybindings.tab, seen)
	}
	if seen[keybindingTabAll] != len(m.commandCatalog()) || seen[groupPRs] == 0 || seen[groupTickets] == 0 {
		t.Fatalf("tab sizes = %v", seen)
	}
	m, _ = tapKeys(m, "shift+tab")
	if keybindingTabs[m.keybindings.tab] != keybindingTabCustomized || !reflect.DeepEqual(keybindingRowIDs(m), []string{"refresh", "logs"}) {
		t.Fatalf("Customized tab lists %v", keybindingRowIDs(m))
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "● 2 customized") || !strings.Contains(view, "modified") || !strings.Contains(view, "g › l") {
		t.Fatalf("Customized tab view:\n%s", view)
	}
}

// Assures that the filter searches every group by command name, key, or
// missing key, that esc first clears it and only then closes the screen, and
// that an empty result says so.
func TestKeybindingsScreenFiltersCommands(t *testing.T) {
	m, _ := enterActionTestModel()
	m, _ = tapKeys(m, ",", "tab", "tab")
	m, _ = tapKeys(m, "/")
	if !m.keybindings.filtering || keybindingTabs[m.keybindings.tab] != keybindingTabAll {
		t.Fatal("/ did not start a search over every group")
	}
	m, _ = tapKeys(m, "a", "p", "p", "r", "o", "v", "e")
	if got := keybindingRowIDs(m); !reflect.DeepEqual(got, []string{"approve"}) {
		t.Fatalf("filter \"approve\" lists %v", got)
	}
	if m.keybindings.capture.active || m.keybindings.filter.Value() != "approve" {
		t.Fatal("typing in the filter triggered a screen command")
	}
	m, _ = tapKeys(m, "enter")
	if m.keybindings.filtering || m.keybindings.filter.Value() != "approve" {
		t.Fatal("enter did not keep the filter")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "/ approve") || !strings.Contains(view, "1 match") {
		t.Fatalf("filtered view:\n%s", view)
	}

	for query, want := range map[string]string{"ctrl+p": cmdPalette, "no key": "logs", "jira board": "ticket-board", "PR RADAR state": "state"} {
		m.keybindings.filter.SetValue(query)
		m.syncKeybindingCursor()
		found := false
		for _, id := range keybindingRowIDs(m) {
			found = found || id == want
		}
		if !found {
			t.Fatalf("filter %q does not list %q: %v", query, want, keybindingRowIDs(m))
		}
	}
	m.keybindings.filter.SetValue("zzzz")
	m.syncKeybindingCursor()
	if view := ansi.Strip(m.View()); len(m.keybindingRows()) != 0 || !strings.Contains(view, "No command matches “zzzz”.") {
		t.Fatalf("empty result view:\n%s", view)
	}
	if got, cmd := tapKeys(m, "enter"); got.keybindings.capture.active || cmd != nil {
		t.Fatal("enter recorded a key with no command selected")
	}

	m, _ = tapKeys(m, "esc")
	if !m.keybindings.open || m.keybindings.filter.Value() != "" || len(m.keybindingRows()) != len(m.commandCatalog()) {
		t.Fatal("the first esc did not clear the filter and keep the screen open")
	}
	if m, _ = tapKeys(m, "esc"); m.keybindings.open {
		t.Fatal("the second esc did not close the screen")
	}
}

// Assures that pressing a key for a command and confirming it changes the
// keybinding, writes only that change to the config file, and takes effect
// at once.
func TestRecordingAKeySavesItToTheConfig(t *testing.T) {
	m, spy := enterActionTestModel()
	m = openKeybindingsOn(t, m, "refresh")

	recording, _ := tapKeys(m, "enter")
	if !recording.keybindings.capture.active {
		t.Fatal("enter did not start recording")
	}
	if view := ansi.Strip(recording.View()); !strings.Contains(view, "Change keybinding") || !strings.Contains(view, "Press the key for this command…") || !strings.Contains(view, "replaces  r") {
		t.Fatalf("recording dialog:\n%s", view)
	}
	recorded, cmd := tapKeys(recording, "f5")
	if cmd != nil || len(recorded.cfg.App.Keybindings) != 0 {
		t.Fatal("a key was saved before it was confirmed")
	}
	if view := ansi.Strip(recorded.View()); !strings.Contains(view, "f5") || !strings.Contains(view, "Free in every view") {
		t.Fatalf("dialog does not show the recorded key as free:\n%s", view)
	}

	saved, cmd := tapKeys(recorded, "enter")
	if saved.keybindings.capture.active || cmd == nil || !saved.keybindings.open {
		t.Fatal("enter did not save the recorded key")
	}
	if want := map[string][]string{"refresh": {"f5"}}; !reflect.DeepEqual(saved.cfg.App.Keybindings, want) {
		t.Fatalf("keybindings = %v, want %v", saved.cfg.App.Keybindings, want)
	}
	if saved.statusMessage != "Refresh current view is now on f5" {
		t.Fatalf("status = %q", saved.statusMessage)
	}
	if result := cmd(); result != nil {
		t.Fatalf("saving reported %#v", result)
	}
	if spy.saveCount != 1 || !reflect.DeepEqual(spy.saved.Keybindings, map[string][]string{"refresh": {"f5"}}) {
		t.Fatalf("config file got %v after %d saves", spy.saved.Keybindings, spy.saveCount)
	}

	back, _ := tapKeys(saved, "esc")
	if got, _ := tapKeys(back, "r"); got.loading {
		t.Fatal("the replaced key still refreshes")
	}
	if got, _ := tapKeys(back, "f5"); !got.loading {
		t.Fatal("the new key does not refresh")
	}
}

// Assures that a key another command of the same view already has is shown
// as taken before saving, and that saving moves it: the other command loses
// it, and both changes reach the config file.
func TestRecordingATakenKeyMovesIt(t *testing.T) {
	m, spy := enterActionTestModel()
	m = openKeybindingsOn(t, m, "refresh")

	recorded, _ := tapKeys(m, "enter", "P")
	view := ansi.Strip(recorded.View())
	if !strings.Contains(view, "Takes P from “Show PR Radar”") || !strings.Contains(view, "Saving moves the key here.") {
		t.Fatalf("dialog does not warn about the taken key:\n%s", view)
	}
	saved, cmd := tapKeys(recorded, "enter")
	if want := map[string][]string{"refresh": {"P"}, "prs": {}}; !reflect.DeepEqual(saved.cfg.App.Keybindings, want) {
		t.Fatalf("keybindings = %v, want %v", saved.cfg.App.Keybindings, want)
	}
	if saved.statusMessage != "Refresh current view is now on P · Show PR Radar lost P" {
		t.Fatalf("status = %q", saved.statusMessage)
	}
	cmd()
	if !reflect.DeepEqual(spy.saved.Keybindings, saved.cfg.App.Keybindings) {
		t.Fatalf("config file got %v", spy.saved.Keybindings)
	}
	back, _ := tapKeys(saved, "esc")
	if got, _ := tapKeys(back, "P"); !got.loading || got.isPRMode() {
		t.Fatal("the moved key still opens PR Radar")
	}
}

// Assures that commands of different views can share a key without either
// losing it, and that a key sequence takes the single key it starts with.
func TestRecordingRespectsViewsAndSequences(t *testing.T) {
	m, _ := enterActionTestModel()
	m = openKeybindingsOn(t, m, "ticket-comment")
	recorded, _ := tapKeys(m, "enter", "u")
	if view := ansi.Strip(recorded.View()); !strings.Contains(view, "Free in JIRA Tickets") {
		t.Fatalf("a key of another view was not offered as free:\n%s", view)
	}
	saved, _ := tapKeys(recorded, "enter")
	if want := map[string][]string{"ticket-comment": {"u"}}; !reflect.DeepEqual(saved.cfg.App.Keybindings, want) {
		t.Fatalf("keybindings = %v, want %v", saved.cfg.App.Keybindings, want)
	}

	m, _ = enterActionTestModel()
	m = openKeybindingsOn(t, m, "logs")
	sequence, _ := tapKeys(m, "enter", "r", "l")
	if view := ansi.Strip(sequence.View()); !strings.Contains(view, "r › l") || !strings.Contains(view, "Takes r from “Refresh current view”") {
		t.Fatalf("a sequence starting with a taken key was not flagged:\n%s", view)
	}
	saved, _ = tapKeys(sequence, "enter")
	if want := map[string][]string{"logs": {"r l"}, "refresh": {}}; !reflect.DeepEqual(saved.cfg.App.Keybindings, want) {
		t.Fatalf("keybindings = %v, want %v", saved.cfg.App.Keybindings, want)
	}
	back, _ := tapKeys(saved, "esc")
	if got, _ := tapKeys(back, "r", "l"); !got.logsOpen {
		t.Fatal("the recorded sequence does not run its command")
	}

	restarted, _ := tapKeys(m, "enter", "g", "l", "z")
	if !reflect.DeepEqual(restarted.keybindings.capture.keys, keySeq{"z"}) {
		t.Fatalf("a third key did not start the recording over: %v", restarted.keybindings.capture.keys)
	}
}

// Assures that a command can have several keys: one is added without
// replacing the others, any of them can be picked and removed, and removing
// the last one leaves the command without a key.
func TestAddingAndRemovingKeys(t *testing.T) {
	m, spy := enterActionTestModel()
	m = openKeybindingsOn(t, m, "refresh")

	adding, _ := tapKeys(m, "a")
	if view := ansi.Strip(adding.View()); !strings.Contains(view, "Add keybinding") || !strings.Contains(view, "in addition to  r") {
		t.Fatalf("add dialog:\n%s", view)
	}
	added, cmd := tapKeys(adding, "f5", "enter")
	if want := map[string][]string{"refresh": {"r", "f5"}}; !reflect.DeepEqual(added.cfg.App.Keybindings, want) || cmd == nil {
		t.Fatalf("keybindings = %v, want %v", added.cfg.App.Keybindings, want)
	}
	if added.statusMessage != "Refresh current view also answers to f5" || added.keybindings.chip != 1 {
		t.Fatalf("status = %q, selected key %d", added.statusMessage, added.keybindings.chip)
	}
	for _, key := range []string{"r", "f5"} {
		back, _ := tapKeys(added, "esc")
		if got, _ := tapKeys(back, key); !got.loading {
			t.Fatalf("%s does not refresh after adding a key", key)
		}
	}

	changed, _ := tapKeys(added, "left", "enter", "ctrl+r", "enter")
	if want := map[string][]string{"refresh": {"ctrl+r", "f5"}}; !reflect.DeepEqual(changed.cfg.App.Keybindings, want) {
		t.Fatalf("changing the picked key gave %v, want %v", changed.cfg.App.Keybindings, want)
	}

	removed, cmd := tapKeys(added, "x")
	if len(removed.cfg.App.Keybindings) != 0 || removed.statusMessage != "Removed f5 from Refresh current view" {
		t.Fatalf("removing the added key: keybindings=%v status=%q", removed.cfg.App.Keybindings, removed.statusMessage)
	}
	cmd()
	if spy.saved.Keybindings != nil {
		t.Fatalf("a command back on its default keys is still in the config: %v", spy.saved.Keybindings)
	}
	unbound, _ := tapKeys(removed, "x")
	if want := map[string][]string{"refresh": {}}; !reflect.DeepEqual(unbound.cfg.App.Keybindings, want) {
		t.Fatalf("keybindings = %v, want %v", unbound.cfg.App.Keybindings, want)
	}
	if unbound.statusMessage != "Removed r from Refresh current view; it has no key now" {
		t.Fatalf("status = %q", unbound.statusMessage)
	}
	if view := ansi.Strip(unbound.View()); !strings.Contains(view, "● 1 customized") {
		t.Fatalf("view after removing the last key:\n%s", view)
	}
	if again, cmd := tapKeys(unbound, "x"); cmd == nil || again.statusMessage != "Refresh current view has no key to remove" {
		t.Fatalf("removing from a command without keys: status=%q", again.statusMessage)
	}
}

// Assures that a command can be reset to its default keys, taking them back
// from a command they were moved to, that resetting everything asks first,
// and that each change can be undone.
func TestResettingAndUndoingKeybindings(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"refresh": {"f5"}, "logs": {"r"}, "prs": {}}
	m = openKeybindingsOn(t, m, "refresh")

	reset, cmd := tapKeys(m, "r")
	// The logs command has no key by default, so losing r puts it back there.
	if want := map[string][]string{"prs": {}}; !reflect.DeepEqual(reset.cfg.App.Keybindings, want) || cmd == nil {
		t.Fatalf("after reset = %v, want %v", reset.cfg.App.Keybindings, want)
	}
	if reset.statusMessage != "Refresh current view is back on r · View session logs lost r" {
		t.Fatalf("status = %q", reset.statusMessage)
	}
	if again, _ := tapKeys(reset, "r"); again.statusMessage != "Refresh current view already has its default" {
		t.Fatalf("resetting a default command: status=%q", again.statusMessage)
	}

	asking, cmd := tapKeys(reset, "R")
	if !asking.keybindings.confirmingReset || cmd != nil || len(asking.cfg.App.Keybindings) != 1 {
		t.Fatal("R reset everything without asking")
	}
	if view := ansi.Strip(asking.View()); !strings.Contains(view, "Reset all keybindings?") || !strings.Contains(view, "1 command goes back to its default keys.") {
		t.Fatalf("reset dialog:\n%s", view)
	}
	if kept, _ := tapKeys(asking, "esc"); kept.keybindings.confirmingReset || len(kept.cfg.App.Keybindings) != 1 {
		t.Fatal("esc did not cancel the reset")
	}
	all, cmd := tapKeys(asking, "enter")
	if all.cfg.App.Keybindings != nil || cmd == nil || all.statusMessage != "Every keybinding is back to its default" {
		t.Fatalf("after reset all = %v, status %q", all.cfg.App.Keybindings, all.statusMessage)
	}
	if none, _ := tapKeys(all, "R"); none.keybindings.confirmingReset {
		t.Fatal("R asked to reset when nothing is customized")
	}

	undone, cmd := tapKeys(all, "u")
	if want := map[string][]string{"prs": {}}; !reflect.DeepEqual(undone.cfg.App.Keybindings, want) || cmd == nil {
		t.Fatalf("after one undo = %v, want %v", undone.cfg.App.Keybindings, want)
	}
	undone, _ = tapKeys(undone, "u")
	if !reflect.DeepEqual(undone.cfg.App.Keybindings, m.cfg.App.Keybindings) {
		t.Fatalf("after two undos = %v, want %v", undone.cfg.App.Keybindings, m.cfg.App.Keybindings)
	}
	if last, cmd := tapKeys(undone, "u"); cmd == nil || last.statusMessage != "Nothing to undo" {
		t.Fatalf("undo with no history: status=%q", last.statusMessage)
	}
}

// Assures that recording never binds esc or ctrl+c, ignores pasted text,
// treats enter and backspace as keys only when they come first, and changes
// nothing when cancelled or when the command already has the key.
func TestRecordingHandlesSpecialKeys(t *testing.T) {
	m, _ := enterActionTestModel()
	m = openKeybindingsOn(t, m, "refresh")
	recording, _ := tapKeys(m, "enter")

	cancelled, cmd := tapKeys(recording, "f5", "esc")
	if cancelled.keybindings.capture.active || len(cancelled.cfg.App.Keybindings) != 0 || !cancelled.keybindings.open || cmd == nil {
		t.Fatal("esc did not cancel the recording and keep the screen open")
	}
	reserved, cmd := tapKeys(recording, "ctrl+c")
	if cmd != nil || !reserved.keybindings.capture.active || len(reserved.keybindings.capture.keys) != 0 || !strings.Contains(ansi.Strip(reserved.View()), "ctrl+c always quits") {
		t.Fatal("ctrl+c was recorded or quit the app while recording")
	}
	next, _ := recording.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("git status"), Paste: true})
	if pasted := next.(model); len(pasted.keybindings.capture.keys) != 0 || !strings.Contains(ansi.Strip(pasted.View()), "not a single key") {
		t.Fatal("pasted text was recorded as a key")
	}

	cleared, _ := tapKeys(recording, "f5", "backspace")
	if len(cleared.keybindings.capture.keys) != 0 || !cleared.keybindings.capture.active {
		t.Fatal("backspace did not clear the recorded key")
	}
	enterKey, _ := tapKeys(recording, "enter")
	if !reflect.DeepEqual(enterKey.keybindings.capture.keys, keySeq{"enter"}) {
		t.Fatalf("enter pressed first was not recorded: %v", enterKey.keybindings.capture.keys)
	}
	saved, _ := tapKeys(enterKey, "enter")
	if want := map[string][]string{"refresh": {"enter"}, "open": {}, "diff": {}, "ticket-read": {}}; !reflect.DeepEqual(saved.cfg.App.Keybindings, want) {
		t.Fatalf("binding enter gave %v, want %v", saved.cfg.App.Keybindings, want)
	}
	space, _ := tapKeys(recording, "space", "enter")
	if want := map[string][]string{"refresh": {"space"}}; !reflect.DeepEqual(space.cfg.App.Keybindings, want) {
		t.Fatalf("binding space gave %v, want %v", space.cfg.App.Keybindings, want)
	}
	if got, _ := tapKeys(space, "esc", "space"); !got.loading {
		t.Fatal("the space key does not run its command")
	}

	same, cmd := tapKeys(recording, "r")
	if view := ansi.Strip(same.View()); !strings.Contains(view, "This command already has that key") {
		t.Fatalf("dialog for the key a command already has:\n%s", view)
	}
	same, cmd = tapKeys(same, "enter")
	if len(same.cfg.App.Keybindings) != 0 || cmd == nil || same.statusMessage != "Refresh current view already has r" {
		t.Fatalf("re-recording the same key: keybindings=%v status=%q", same.cfg.App.Keybindings, same.statusMessage)
	}
}

// Assures that the selection follows the list: it scrolls into view, stays on
// its command when keys change, and moves to a neighbour when its command
// leaves the tab.
func TestKeybindingsScreenKeepsSelectionVisible(t *testing.T) {
	m, _ := enterActionTestModel()
	m.width, m.height = 100, 20
	m, _ = tapKeys(m, ",")
	height := m.keybindingsLayout().listHeight
	rows := len(m.keybindingRows())

	m, _ = tapKeys(m, "G")
	if m.keybindings.cursor != rows-1 || m.keybindings.offset == 0 {
		t.Fatalf("G: cursor=%d offset=%d", m.keybindings.cursor, m.keybindings.offset)
	}
	last, _ := m.selectedKeybindingRow()
	if view := ansi.Strip(m.View()); !strings.Contains(view, "› "+last.spec.Title) {
		t.Fatalf("the last command is not on screen:\n%s", view)
	}
	m, _ = tapKeys(m, "g")
	if m.keybindings.cursor != 0 || m.keybindings.offset != 0 {
		t.Fatalf("g: cursor=%d offset=%d", m.keybindings.cursor, m.keybindings.offset)
	}
	for range height + 2 {
		m, _ = tapKeys(m, "j")
	}
	selected, _ := m.selectedKeybindingRow()
	if view := ansi.Strip(m.View()); m.keybindings.cursor != height+2 || !strings.Contains(view, "› "+selected.spec.Title) {
		t.Fatalf("scrolling down lost the selection: cursor=%d\n%s", m.keybindings.cursor, view)
	}
	if up, _ := tapKeys(m, "k", "up"); up.keybindings.cursor != height {
		t.Fatalf("k and up moved to %d", up.keybindings.cursor)
	}

	m, _ = enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"refresh": {"f5"}, "logs": {"g l"}, "new": {"c"}}
	m, _ = tapKeys(m, ",", "shift+tab", "j")
	if row, _ := m.selectedKeybindingRow(); row.spec.ID != "new" {
		t.Fatalf("second customized command = %q", row.spec.ID)
	}
	m, _ = tapKeys(m, "r")
	if row, ok := m.selectedKeybindingRow(); !ok || row.spec.ID != "logs" || !reflect.DeepEqual(keybindingRowIDs(m), []string{"refresh", "logs"}) {
		t.Fatalf("after its command left the tab the selection is on %q in %v", row.spec.ID, keybindingRowIDs(m))
	}
}

// Assures that the screen and its dialogs fit the terminal at any size.
func TestKeybindingsScreenFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{160, 50}, {120, 30}, {100, 24}, {80, 24}, {60, 16}, {40, 12}, {30, 8}} {
		m, _ := enterActionTestModel()
		m.cfg.App.Keybindings = map[string][]string{"refresh": {"r", "f5", "ctrl+r", "g r"}, "logs": {"g l"}, "prs": {"r"}}
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m, _ = tapKeys(next.(model), ",")
		for name, view := range map[string]string{
			"list":      m.View(),
			"last":      func() string { got, _ := tapKeys(m, "G"); return got.View() }(),
			"filter":    func() string { got, _ := tapKeys(m, "/", "r"); return got.View() }(),
			"customize": func() string { got, _ := tapKeys(m, "shift+tab"); return got.View() }(),
			"recording": func() string { got, _ := tapKeys(m, "j", "j", "j", "enter", "P"); return got.View() }(),
			"reset":     func() string { got, _ := tapKeys(m, "R"); return got.View() }(),
		} {
			if width, height := lipgloss.Width(view), lipgloss.Height(view); width > size[0] || height > size[1] {
				t.Fatalf("%s view overflowed %v: %dx%d\n%s", name, size, width, height, ansi.Strip(view))
			}
		}
		for idx := range keybindingTabs {
			m.keybindings.tab = idx
			line := ansi.Strip(renderKeybindingTabs(idx, keybindingTabCounts(m.keybindingRowsFor(m.keymap(), keybindingTabAll)), m.keybindingsLayout().width))
			short, abbreviated := keybindingTabShortLabels[keybindingTabs[idx]]
			if !strings.Contains(line, keybindingTabs[idx]) && !(abbreviated && strings.Contains(line, short)) {
				t.Fatalf("size %v: the active tab %q is not shown in %q", size, keybindingTabs[idx], line)
			}
			if lipgloss.Width(line) > m.keybindingsLayout().width {
				t.Fatalf("size %v: tab strip %q is wider than the screen", size, line)
			}
		}
	}
}

// Assures that a config file edited by hand so that two commands share a key
// is pointed out, and that giving the key to one of them settles it.
func TestKeybindingsScreenFlagsClashesFromTheConfig(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"logs": {"r"}}
	m = openKeybindingsOn(t, m, "logs")
	if row, _ := m.selectedKeybindingRow(); !row.clash {
		t.Fatal("a key shared by two commands was not flagged")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "clash") || !strings.Contains(view, "r is also a key of “Refresh current view”") {
		t.Fatalf("clash is not explained:\n%s", view)
	}
	settled, _ := tapKeys(m, "enter", "r", "enter")
	if want := map[string][]string{"logs": {"r"}, "refresh": {}}; !reflect.DeepEqual(settled.cfg.App.Keybindings, want) {
		t.Fatalf("keybindings = %v, want %v", settled.cfg.App.Keybindings, want)
	}
	if row, _ := settled.selectedKeybindingRow(); row.clash {
		t.Fatal("the clash was not settled")
	}
}

// Assures that keybindings edited in the config file while the app runs take
// effect when the file is reloaded.
func TestConfigReloadAppliesKeybindings(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Profiles = []RepositoryProfile{m.cfg.ActiveProfile}
	m.cfg.App.DefaultProfile = m.cfg.ActiveProfile.Name
	app := cloneAppConfig(m.cfg.App)
	app.Keybindings = map[string][]string{"refresh": {"f5"}}

	next, _ := m.Update(configFileResult{App: app, Changed: true})
	reloaded := next.(model)
	if got, _ := tapKeys(reloaded, "r"); got.loading {
		t.Fatal("the key removed in the file still works")
	}
	if got, _ := tapKeys(reloaded, "f5"); !got.loading {
		t.Fatal("the key added in the file does not work")
	}
}

// Assures that a queued config save keeps the keybindings it was queued
// with, whatever is changed afterwards.
func TestQueuedSaveKeepsItsKeybindings(t *testing.T) {
	app := AppConfig{Keybindings: map[string][]string{"refresh": {"f5"}}}
	clone := cloneAppConfig(app)
	app.Keybindings["refresh"][0] = "f6"
	app.Keybindings["logs"] = []string{"L"}
	if want := map[string][]string{"refresh": {"f5"}}; !reflect.DeepEqual(clone.Keybindings, want) {
		t.Fatalf("clone = %v, want %v", clone.Keybindings, want)
	}
	if cloneAppConfig(AppConfig{}).Keybindings != nil {
		t.Fatal("a config without keybindings gained an empty list")
	}
}

// Assures that the app cannot be left without a way to reach its commands:
// the palette and the keybindings screen never both lose their keys.
func TestPaletteOrKeybindingsScreenKeepsAKey(t *testing.T) {
	m, _ := enterActionTestModel()
	m = openKeybindingsOn(t, m, cmdKeybindings)
	m, cmd := tapKeys(m, "x")
	if want := map[string][]string{cmdKeybindings: {}}; !reflect.DeepEqual(m.cfg.App.Keybindings, want) || cmd == nil {
		t.Fatalf("removing the key of the screen gave %v", m.cfg.App.Keybindings)
	}

	m.keybindings.cursorID = cmdPalette
	m.syncKeybindingCursor()
	refused, cmd := tapKeys(m, "x")
	if cmd != nil || !refused.statusError || !strings.Contains(refused.statusMessage, "has to keep a key") || !reflect.DeepEqual(refused.cfg.App.Keybindings, m.cfg.App.Keybindings) {
		t.Fatalf("removing the last way in: status=%q keybindings=%v", refused.statusMessage, refused.cfg.App.Keybindings)
	}
	m.keybindings.cursorID = "refresh"
	m.syncKeybindingCursor()
	stolen, cmd := tapKeys(m, "enter", "ctrl+p", "enter")
	if cmd != nil || !stolen.statusError || !reflect.DeepEqual(stolen.cfg.App.Keybindings, m.cfg.App.Keybindings) {
		t.Fatalf("taking the palette's only key: status=%q keybindings=%v", stolen.statusMessage, stolen.cfg.App.Keybindings)
	}
	if len(refused.keybindings.history) != len(m.keybindings.history) {
		t.Fatal("a refused change was recorded for undo")
	}
}

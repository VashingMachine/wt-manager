package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// keyPress builds the key event a terminal sends for a named key.
func keyPress(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "f5":
		return tea.KeyMsg{Type: tea.KeyF5}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// tapKeys sends keys one after another and returns the last command.
func tapKeys(m model, names ...string) (model, tea.Cmd) {
	var cmd tea.Cmd
	for _, name := range names {
		var next tea.Model
		next, cmd = m.Update(keyPress(name))
		m = next.(model)
	}
	return m, cmd
}

// keybindingModeModel is a model in the given view with one selected row.
func keybindingModeModel(mode viewMode) (model, *enterActionSpyServices) {
	m, spy := enterActionTestModel()
	m.mode = mode
	switch mode {
	case modePRs:
		m.remotePRs = []RemotePullRequest{testkit.RemotePR(7, "OPEN", "dev", "Fix checkout", "fix/checkout")}
		m.visiblePRs = m.remotePRs
		m.prDetail = &m.visiblePRs[0]
	case modeTickets:
		m.tickets = []Ticket{{Key: "APP-1", Summary: "Fix checkout"}}
		m.visibleTickets = m.tickets
	}
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)
	return m, spy
}

// Assures that out of the box no two commands of one view share a key or the
// start of a key sequence, and that every default key is one the app accepts.
func TestDefaultKeybindingsNeverClashWithinAView(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Profiles = []RepositoryProfile{{Name: "shop"}, {Name: "billing"}}
	km := m.keymap()
	seen := map[string]bool{}
	for _, spec := range km.specs {
		if seen[spec.ID] {
			t.Fatalf("command ID %q is listed twice", spec.ID)
		}
		seen[spec.ID] = true
		if spec.Title == "" || spec.Scope == 0 || !strings.Contains(strings.Join(keybindingGroups, "|"), spec.Group) {
			t.Fatalf("command %q is missing a title, a view, or a known group: %#v", spec.ID, spec)
		}
		if len(km.keysOf(spec.ID)) != len(spec.Keys) {
			t.Fatalf("command %q has a default key the app cannot use: %v", spec.ID, spec.Keys)
		}
		for _, keys := range km.keysOf(spec.ID) {
			if conflicts := km.conflicts(spec.ID, keys); len(conflicts) > 0 {
				t.Fatalf("default key %q of %q clashes with %v", keys, spec.ID, conflicts)
			}
		}
	}
}

// Assures that every command the palette offers, in every view, can be given a
// key, and that every command with a key can be run.
func TestEveryPaletteCommandCanBeBoundAndRun(t *testing.T) {
	t.Setenv("EDITOR", "vi")
	for _, mode := range []viewMode{modeWorktrees, modePRs, modeTickets} {
		m, _ := keybindingModeModel(mode)
		m.filterQuery = "fix"
		m.cfg.App.Profiles = []RepositoryProfile{{Name: "shop", RepositoryPath: "/tmp/shop", WorktreesDir: "/tmp/trees"}}
		m.cfg.ActiveProfile.Jira = &JiraConfig{Boards: []JiraBoard{{Name: "Board", ID: 1, URL: "https://example.atlassian.net/jira/software/c/projects/APP/boards/1"}}}
		m.cfg.ActiveProfile.Repositories = []core.Repository{
			{Name: "api", RepositoryPath: "/tmp/api", WorktreesDir: "/tmp/trees"},
			{Name: "web", RepositoryPath: "/tmp/web", WorktreesDir: "/tmp/trees"},
		}
		km := m.keymap()
		offered := 0
		for _, entry := range m.paletteEntries() {
			if strings.HasPrefix(entry.ID, "worktree:") {
				continue
			}
			offered++
			if _, ok := km.spec(entry.ID); !ok {
				t.Fatalf("mode %d: palette command %q cannot be given a key", mode, entry.ID)
			}
		}
		if offered < 25 {
			t.Fatalf("mode %d: the palette offered only %d commands", mode, offered)
		}
		for _, spec := range km.specs {
			if _, _, handled := m.execCommand(spec.ID); !handled {
				t.Fatalf("mode %d: command %q has no action", mode, spec.ID)
			}
		}
	}
	m, _ := enterActionTestModel()
	if _, _, handled := m.execCommand("no-such-command"); handled {
		t.Fatal("an unknown command ID was reported as run")
	}
}

// Assures that a key set in the config runs its command and that the key it
// replaced no longer does.
func TestCustomKeybindingReplacesTheDefaultKey(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"refresh": {"f5"}}

	got, cmd := tapKeys(m, "r")
	if got.loading || cmd != nil {
		t.Fatal("the replaced default key still refreshed")
	}
	got, cmd = tapKeys(m, "f5")
	if !got.loading || cmd == nil || got.statusMessage != "Refreshing worktrees..." {
		t.Fatalf("the configured key did not refresh: loading=%v status=%q", got.loading, got.statusMessage)
	}
}

// Assures that a command left without a key cannot be triggered from the
// keyboard, shows no shortcut, and still runs from the palette.
func TestCommandWithoutAKeyStillRunsFromThePalette(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"toggle-all": {}}

	got, cmd := tapKeys(m, "a")
	if got.showAll || cmd != nil {
		t.Fatal("a removed key still toggled all worktrees")
	}
	for _, entry := range m.paletteEntries() {
		if entry.ID == "toggle-all" && entry.Shortcut != "" {
			t.Fatalf("palette shows shortcut %q for a command without a key", entry.Shortcut)
		}
	}
	got, cmd = choosePaletteEntry(t, m, "toggle-all")
	if !got.showAll || cmd == nil {
		t.Fatal("the palette did not run a command that has no key")
	}
}

// Assures that the palette runs a command through its ID, whatever key the
// command has been moved to.
func TestPaletteRunsCommandsMovedToAnotherKey(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"filter": {"ctrl+r"}, "refresh": {"/"}}
	got, _ := choosePaletteEntry(t, m, "filter")
	if !got.filtering || got.loading {
		t.Fatalf("palette ran the wrong command: filtering=%v loading=%v", got.filtering, got.loading)
	}
	got, _ = tapKeys(m, "/")
	if got.filtering || !got.loading {
		t.Fatal("the moved key did not run its new command")
	}
}

// Assures that one key can run a different command in each view, and that a
// worktree-only key explains itself in other views unless that view uses the
// key for its own command.
func TestSameKeyRunsADifferentCommandPerView(t *testing.T) {
	bindings := map[string][]string{"ask": {"x"}, "ticket-comment": {"x"}}
	prs, _ := keybindingModeModel(modePRs)
	prs.cfg.App.Keybindings = bindings
	if conflicts := prs.keymap().conflicts("ask", keySeq{"x"}); len(conflicts) != 0 {
		t.Fatalf("keys of different views were reported as clashing: %v", conflicts)
	}
	if got, _ := tapKeys(prs, "x"); !got.askingPR || got.commentingTicket {
		t.Fatal("x did not ask about the PR in PR Radar")
	}
	tickets, _ := keybindingModeModel(modeTickets)
	tickets.cfg.App.Keybindings = bindings
	if got, _ := tapKeys(tickets, "x"); !got.commentingTicket || got.askingPR {
		t.Fatal("x did not comment on the ticket in JIRA Tickets")
	}
	worktrees, _ := keybindingModeModel(modeWorktrees)
	worktrees.cfg.App.Keybindings = bindings
	if got, cmd := tapKeys(worktrees, "x"); got.askingPR || got.commentingTicket || cmd != nil {
		t.Fatal("x ran a command of another view in Worktrees")
	}

	if got, _ := tapKeys(prs, "d"); got.statusMessage != "Delete is available for local worktrees" || !got.statusError {
		t.Fatalf("d in PR Radar = %q, want the explanation", got.statusMessage)
	}
	prs.cfg.App.Keybindings = map[string][]string{"ask": {"d"}}
	if got, _ := tapKeys(prs, "d"); !got.askingPR {
		t.Fatal("PR Radar's own command did not win over a worktree-only key")
	}
	worktrees.cfg.App.Keybindings = map[string][]string{"ask": {"d"}}
	if got, _ := tapKeys(worktrees, "d"); got.deleteArmedPath == "" {
		t.Fatal("d stopped deleting in Worktrees after PR Radar reused it")
	}
}

// Assures that a keybinding of two keys waits for the second one, shows what
// it waits for, runs on completion, and is dropped by esc or by a key that
// does not continue it.
func TestKeySequenceRunsAfterItsLastKey(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"logs": {"g l"}}

	waiting, cmd := tapKeys(m, "g")
	if waiting.logsOpen || cmd != nil || !reflect.DeepEqual(waiting.pendingKeys, keySeq{"g"}) {
		t.Fatalf("first key of a sequence: logs=%v pending=%v", waiting.logsOpen, waiting.pendingKeys)
	}
	if status := ansi.Strip(waiting.statusBarView()); !strings.Contains(status, "g › …") || !strings.Contains(status, "esc cancels") {
		t.Fatalf("status bar does not show the pending keys: %q", status)
	}
	if got, _ := tapKeys(waiting, "l"); !got.logsOpen || len(got.pendingKeys) != 0 {
		t.Fatal("completing the sequence did not open the logs")
	}
	if got, _ := tapKeys(waiting, "esc"); got.logsOpen || len(got.pendingKeys) != 0 || got.statusMessage != "Cancelled g" {
		t.Fatalf("esc did not drop the sequence: status=%q", got.statusMessage)
	}
	got, _ := tapKeys(waiting, "r")
	if got.loading || got.logsOpen || len(got.pendingKeys) != 0 || got.statusMessage != "g › r is not a keybinding" {
		t.Fatalf("a key outside the sequence: loading=%v status=%q", got.loading, got.statusMessage)
	}
	if got, _ := tapKeys(got, "r"); !got.loading {
		t.Fatal("the next key press was not handled on its own")
	}
}

// Assures that the palette, the help page, the footer and status hints all
// name the keys in effect instead of the built-in ones.
func TestHintsShowCustomizedKeys(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{"refresh": {"f5"}, "delete": {"x"}, "body": {}, cmdPalette: {"ctrl+r"}}

	shortcuts := map[string]string{}
	for _, entry := range m.paletteEntries() {
		shortcuts[entry.ID] = entry.Shortcut
	}
	if shortcuts["refresh"] != "f5" || shortcuts["delete"] != "x x" || shortcuts["body"] != "" || shortcuts["filter"] != "/" {
		t.Fatalf("palette shortcuts = %v", shortcuts)
	}
	help := ansi.Strip(renderHelpPage(m))
	for key, text := range map[string]string{"f5": "refresh this view", "x x": "delete (press twice)", "—": "full PR description", "ctrl+r": "command palette", ",": "customize these keys"} {
		if !helpPageLists(help, key, text) {
			t.Fatalf("help page does not list %q as %q:\n%s", key, text, help)
		}
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "ctrl+r commands") || !strings.Contains(view, "f5 refresh") || strings.Contains(view, "ctrl+p") {
		t.Fatalf("footer does not show the configured keys:\n%s", view)
	}
	m.statusMessage = ""
	if hint := m.quietStatusHint(); !strings.Contains(hint, "ctrl+r commands · / filter") {
		t.Fatalf("status hint = %q", hint)
	}
	armed, _ := tapKeys(m, "x")
	if armed.statusMessage != "Press x again to delete "+armed.deleteArmedName || armed.deleteArmedPath == "" {
		t.Fatalf("delete hint = %q", armed.statusMessage)
	}
	if detail := m.stackDetailLines(Worktree{}); len(detail) != 0 {
		t.Fatalf("an unstacked worktree has stack details: %v", detail)
	}
	if hint := m.howTo("body", "toggle full body"); hint != "use the command palette to toggle full body" {
		t.Fatalf("hint for a command without a key = %q", hint)
	}
}

// Assures that deleting is confirmed with the key it is bound to, and that the
// key it had by default no longer arms it.
func TestDeleteConfirmsOnItsConfiguredKey(t *testing.T) {
	m, _ := enterActionTestModel()
	m.allWorktrees[0].IsMain = false
	m.visibleWorktrees = m.allWorktrees
	m.cfg.App.Keybindings = map[string][]string{"delete": {"g d"}}

	if got, _ := tapKeys(m, "d"); got.deleteArmedPath != "" {
		t.Fatal("the default key still armed deletion")
	}
	armed, _ := tapKeys(m, "g", "d")
	if armed.deleteArmedPath == "" || len(armed.pendingDeletes) != 0 || armed.statusMessage != "Press g › d again to delete "+armed.deleteArmedName {
		t.Fatalf("first press: armed=%q status=%q", armed.deleteArmedPath, armed.statusMessage)
	}
	deleted, cmd := tapKeys(armed, "g", "d")
	if len(deleted.pendingDeletes) != 1 || cmd == nil {
		t.Fatal("the second press of the configured keys did not delete")
	}
}

// Assures that the palette closes on the key that opened it, unless that key
// types a character the search needs.
func TestPaletteClosesOnItsConfiguredKey(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{cmdPalette: {"ctrl+r", ":"}}

	open, _ := tapKeys(m, "ctrl+r")
	if !open.paletteOpen {
		t.Fatal("the configured key did not open the palette")
	}
	if got, _ := tapKeys(open, "ctrl+p"); !got.paletteOpen {
		t.Fatal("the replaced default key closed the palette")
	}
	if got, _ := tapKeys(open, "ctrl+r"); got.paletteOpen {
		t.Fatal("the configured key did not close the palette")
	}
	typed, _ := tapKeys(open, ":")
	if !typed.paletteOpen || typed.paletteInput.Value() != ":" {
		t.Fatalf("a character key closed the palette instead of typing: open=%v text=%q", typed.paletteOpen, typed.paletteInput.Value())
	}
}

// Assures that full-screen readers, the logs and the help page answer only to
// the few commands they support, on the keys those commands have now.
func TestOverlaysAnswerToTheConfiguredKeysOfTheirCommands(t *testing.T) {
	prs, _ := keybindingModeModel(modePRs)
	prs.cfg.App.Keybindings = map[string][]string{"approve": {"y"}, cmdQuit: {"Q"}, cmdHelp: {"?"}}
	prs.prDiffMode = true

	if got, _ := tapKeys(prs, "y"); !got.confirmingPRApproval {
		t.Fatal("the approve key did not work in the full-screen diff")
	}
	if got, _ := tapKeys(prs, "A"); got.confirmingPRApproval {
		t.Fatal("the replaced approve key still worked in the full-screen diff")
	}
	if _, cmd := tapKeys(prs, "Q"); cmd == nil {
		t.Fatal("the quit key did not quit from the full-screen diff")
	}
	if got, _ := tapKeys(prs, "u"); got.selectingPRUser {
		t.Fatal("a command the diff does not support ran from it")
	}
	if footer := ansi.Strip(prs.fullScreenPRDiffView()); !strings.Contains(footer, "y approve") || !strings.Contains(footer, "Q quit") {
		t.Fatalf("diff footer does not name the configured keys: %q", footer)
	}

	help, _ := tapKeys(prs, "esc", "?")
	if !help.showingHelp {
		t.Fatal("the help key did not open the help page")
	}
	if got, _ := tapKeys(help, "r"); got.loading || !got.showingHelp {
		t.Fatal("a command ran from behind the help page")
	}
	if got, _ := tapKeys(help, "?"); got.showingHelp {
		t.Fatal("the help key did not close the help page")
	}
	if _, cmd := tapKeys(help, "ctrl+c"); cmd == nil {
		t.Fatal("ctrl+c did not quit")
	}
}

// Assures that keys are read from the config leniently: reserved keys and
// blanks are dropped, spelling is canonical, and unknown commands are ignored.
func TestKeymapReadsConfiguredKeysLeniently(t *testing.T) {
	m, _ := enterActionTestModel()
	m.cfg.App.Keybindings = map[string][]string{
		"refresh":         {" Ctrl+R ", "esc", "", "ctrl+r", "g  R", "ctrl+c x"},
		"no-such-command": {"z"},
	}
	km := m.keymap()
	if got := keySeqStrings(km.keysOf("refresh")); !reflect.DeepEqual(got, []string{"ctrl+r", "g R"}) {
		t.Fatalf("keys of refresh = %v", got)
	}
	if id, _ := km.resolve(keySeq{"z"}, scopeAll, nil); id != "" {
		t.Fatalf("an unknown command answered to its key: %q", id)
	}
	if got, cmd := tapKeys(m, "esc"); got.loading || cmd != nil {
		t.Fatal("esc ran a command")
	}
	for value, want := range map[string]string{"up": "↑", "ctrl+down": "ctrl+↓", "g l": "g › l", "+": "+", "ctrl++": "ctrl++", "space": "space"} {
		if got := parseKeySeq(value).Label(); got != want {
			t.Fatalf("label of %q = %q, want %q", value, got, want)
		}
	}
	if name := keyName(keyPress("space")); name != "space" {
		t.Fatalf("the space key is named %q", name)
	}
	if name := keyName(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r"), Paste: true}); name != "" {
		t.Fatalf("pasted text was read as key %q", name)
	}
}

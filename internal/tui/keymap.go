package tui

import (
	"maps"
	"slices"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Keybindings: every command has an ID, the views its keys work in, and
// default keys. The config file lists only the commands whose keys differ from
// those defaults (see core.AppConfig.Keybindings), so the keymap in effect is
// always the catalog with those overrides laid on top. Nothing else decides
// what a key does in the main view: the palette, the help page and the hints
// all read the same keymap.

// commandScope is the set of views in which a command's keys work.
type commandScope uint8

const (
	scopeWorktrees commandScope = 1 << iota
	scopePRs
	scopeTickets
	scopeAll = scopeWorktrees | scopePRs | scopeTickets
)

func scopeOf(mode viewMode) commandScope {
	switch mode {
	case modePRs:
		return scopePRs
	case modeTickets:
		return scopeTickets
	}
	return scopeWorktrees
}

// Label names the views of a scope for the keybindings screen.
func (s commandScope) Label() string {
	if s == scopeAll {
		return "every view"
	}
	var views []string
	for _, view := range []struct {
		scope commandScope
		name  string
	}{{scopeWorktrees, "Worktrees"}, {scopePRs, "PR Radar"}, {scopeTickets, "JIRA Tickets"}} {
		if s&view.scope != 0 {
			views = append(views, view.name)
		}
	}
	return strings.Join(views, " and ")
}

const (
	groupGeneral   = "General"
	groupWorktrees = "Worktrees"
	groupPRs       = "PR Radar"
	groupTickets   = "JIRA Tickets"
	groupSettings  = "Settings"
	groupProjects  = "Projects"
)

// keybindingGroups orders the groups of the keybindings screen.
var keybindingGroups = []string{groupGeneral, groupWorktrees, groupPRs, groupTickets, groupSettings, groupProjects}

// commandSpec describes a command that can be given a key.
type commandSpec struct {
	ID          string
	Title       string
	Description string
	Group       string
	Scope       commandScope
	// Keys are the default keybindings.
	Keys []string
	// Explains marks a command that says why it does nothing in a view outside
	// its scope. Its keys keep answering there, unless that view gave them to
	// another command.
	Explains bool
}

// Command IDs referenced outside the catalog and the dispatcher.
const (
	cmdPalette     = "palette"
	cmdHelp        = "help"
	cmdKeybindings = "keybindings"
	cmdQuit        = "quit"
	cmdDelete      = "delete"
	cmdClearFilter = "clear-filter"
)

// Prefixes of the commands that exist once per configured choice.
const (
	cmdEnterActionPrefix = "enter:"
	cmdAgentPrefix       = "agent:"
	cmdEditorPrefix      = "editor:"
	cmdProfilePrefix     = "profile:"
)

// baseCommands is every command that does not depend on the configuration, in
// the order the keybindings screen lists them within a group.
var baseCommands = []commandSpec{
	{ID: cmdPalette, Title: "Open command palette", Description: "Search commands, settings, and worktrees", Group: groupGeneral, Scope: scopeAll, Keys: []string{"ctrl+p"}},
	{ID: cmdHelp, Title: "Show keyboard shortcuts", Description: "A page listing the keys of the current setup", Group: groupGeneral, Scope: scopeAll, Keys: []string{"h"}},
	{ID: cmdKeybindings, Title: "Customize keyboard shortcuts…", Description: "Change, add, or remove the keys of every command", Group: groupGeneral, Scope: scopeAll, Keys: []string{","}},
	{ID: "refresh", Title: "Refresh current view", Description: "Reload worktrees, pull requests, or tickets", Group: groupGeneral, Scope: scopeAll, Keys: []string{"r"}},
	{ID: "filter", Title: "Filter current view", Description: "Search the table's rows", Group: groupGeneral, Scope: scopeAll, Keys: []string{"/"}},
	{ID: cmdClearFilter, Title: "Clear current filter", Description: "Show every row again; esc does this too", Group: groupGeneral, Scope: scopeAll},
	{ID: "focus", Title: "Switch table / details focus", Description: "Move between the table and the details pane", Group: groupGeneral, Scope: scopeAll, Keys: []string{"tab"}},
	{ID: "up", Title: "Move selection up", Description: "Previous row, or scroll the details pane when it has focus", Group: groupGeneral, Scope: scopeAll, Keys: []string{"up", "k"}},
	{ID: "down", Title: "Move selection down", Description: "Next row, or scroll the details pane when it has focus", Group: groupGeneral, Scope: scopeAll, Keys: []string{"down", "j"}},
	{ID: "worktrees", Title: "Show worktrees", Description: "Browse the active project's local worktrees", Group: groupGeneral, Scope: scopeAll},
	{ID: "prs", Title: "Show PR Radar", Description: "Browse GitHub pull requests; again to return to worktrees", Group: groupGeneral, Scope: scopeAll, Keys: []string{"P"}},
	{ID: "tickets", Title: "Show JIRA tickets", Description: "Browse configured boards and spaces; again to return to worktrees", Group: groupGeneral, Scope: scopeAll, Keys: []string{"J"}},
	{ID: "new", Title: "Create a worktree", Description: "From a branch name, the selected PR, or the selected ticket", Group: groupGeneral, Scope: scopeAll, Keys: []string{"n"}},
	{ID: "open-editor", Title: "Open selection in editor", Description: "Open or create the selected worktree in your editor", Group: groupGeneral, Scope: scopeAll, Keys: []string{"v"}},
	{ID: "open-agent", Title: "Open selection with AI agent", Description: "Open or create the selected worktree with the default agent", Group: groupGeneral, Scope: scopeAll, Keys: []string{"i"}},
	{ID: "open-browser", Title: "Open selected PR / ticket in browser", Group: groupGeneral, Scope: scopeAll, Keys: []string{"o"}},
	{ID: "logs", Title: "View session logs", Description: "Inspect temporary execution logs, command output, and errors", Group: groupGeneral, Scope: scopeAll},
	{ID: cmdQuit, Title: "Quit wt-manager", Description: "ctrl+c always quits as well", Group: groupGeneral, Scope: scopeAll, Keys: []string{"q"}},

	{ID: "open", Title: "Open selected worktree", Description: "Run the default Enter action: editor, AI agent, or terminal", Group: groupWorktrees, Scope: scopeWorktrees, Keys: []string{"enter"}},
	{ID: "terminal", Title: "Open selected worktree in terminal", Description: "Start a shell without an AI agent", Group: groupWorktrees, Scope: scopeWorktrees},
	{ID: "toggle-all", Title: "Toggle all / managed worktrees", Description: "Include the main repository and unmanaged worktrees", Group: groupWorktrees, Scope: scopeWorktrees, Keys: []string{"a"}, Explains: true},
	{ID: "enter-menu", Title: "Choose default Enter action", Description: "Editor, AI agent, or terminal", Group: groupWorktrees, Scope: scopeWorktrees, Keys: []string{"e"}, Explains: true},
	{ID: "stack-new", Title: "Create a stacked worktree", Description: "On top of the selected worktree, or for the selected ticket on top of another", Group: groupWorktrees, Scope: scopeWorktrees | scopeTickets, Keys: []string{"N"}, Explains: true},
	{ID: "stack-on", Title: "Stack selected worktree on another…", Description: "Record which worktree it was started from", Group: groupWorktrees, Scope: scopeWorktrees},
	{ID: "restack", Title: "Restack selected worktree…", Description: "Rebase its own commits onto the worktree it is stacked on", Group: groupWorktrees, Scope: scopeWorktrees, Keys: []string{"B"}, Explains: true},
	{ID: "unstack", Title: "Take selected worktree out of its stack", Description: "Forget which worktree it was started from", Group: groupWorktrees, Scope: scopeWorktrees},
	{ID: cmdDelete, Title: "Delete selected worktree…", Description: "Arms deletion; press the key again to confirm", Group: groupWorktrees, Scope: scopeWorktrees, Keys: []string{"d"}, Explains: true},
	{ID: "sweep", Title: "Sweep stale worktrees…", Description: "Remove clean worktrees nobody has touched for a while", Group: groupWorktrees, Scope: scopeWorktrees, Keys: []string{"X"}, Explains: true},
	{ID: "body", Title: "Toggle PR description", Description: "Expand the pull request description in the details pane", Group: groupWorktrees, Scope: scopeWorktrees | scopePRs, Keys: []string{"p"}, Explains: true},

	{ID: "diff", Title: "Open selected PR diff", Description: "Full-screen source diff", Group: groupPRs, Scope: scopePRs, Keys: []string{"enter"}},
	{ID: "state", Title: "Cycle PR state filter", Description: "Open, all, closed, or merged", Group: groupPRs, Scope: scopePRs, Keys: []string{"S"}},
	{ID: "user", Title: "Choose PR user filter", Description: "Author, assignee, or requested reviewer", Group: groupPRs, Scope: scopePRs, Keys: []string{"u"}},
	{ID: "mine", Title: "Toggle my pull requests", Description: "Filter by your GitHub account", Group: groupPRs, Scope: scopePRs, Keys: []string{"Y"}},
	{ID: "branch", Title: "Toggle PR title / branch column", Group: groupPRs, Scope: scopePRs, Keys: []string{"b"}},
	{ID: "checks", Title: "Toggle failed GitHub Actions", Description: "Show failed checks in details", Group: groupPRs, Scope: scopePRs, Keys: []string{"f"}},
	{ID: "approve", Title: "Approve selected pull request…", Description: "Review the confirmation before submitting", Group: groupPRs, Scope: scopePRs, Keys: []string{"A"}},
	{ID: "ask", Title: "Ask agent about selected PR", Group: groupPRs, Scope: scopePRs, Keys: []string{"c"}},
	{ID: "pr-repo", Title: "Choose PR Radar repository", Description: "For projects with several repositories", Group: groupPRs, Scope: scopePRs, Keys: []string{"R"}},

	{ID: "ticket-read", Title: "Read selected JIRA ticket", Description: "Full-screen ticket reader", Group: groupTickets, Scope: scopeTickets, Keys: []string{"enter"}},
	{ID: "ticket-filters", Title: "Configure ticket filters", Description: "Assigned to me, active sprint, open tickets", Group: groupTickets, Scope: scopeTickets, Keys: []string{"f"}},
	{ID: "ticket-mine", Title: "Toggle tickets assigned to me", Group: groupTickets, Scope: scopeTickets, Keys: []string{"Y"}},
	{ID: "ticket-sprint", Title: "Toggle active sprint tickets", Group: groupTickets, Scope: scopeTickets, Keys: []string{"S"}},
	{ID: "ticket-board", Title: "Choose JIRA board or space", Group: groupTickets, Scope: scopeTickets, Keys: []string{"b"}},
	{ID: "ticket-status", Title: "Change selected ticket status", Group: groupTickets, Scope: scopeTickets, Keys: []string{"t"}},
	{ID: "ticket-comment", Title: "Comment on selected ticket", Group: groupTickets, Scope: scopeTickets, Keys: []string{"C"}},
	{ID: "ticket-assign", Title: "Assign selected ticket", Group: groupTickets, Scope: scopeTickets, Keys: []string{"A"}},

	{ID: "agent-menu", Title: "Choose default AI agent", Group: groupSettings, Scope: scopeAll, Keys: []string{"m"}},
	{ID: "editor", Title: "Configure editor", Description: "VS Code, IntelliJ IDEA, or a custom command", Group: groupSettings, Scope: scopeAll},
	{ID: "config-edit", Title: "Edit configuration file", Description: "Open the config in your editor; saved changes apply automatically", Group: groupSettings, Scope: scopeAll},
	{ID: "config-edit-terminal", Title: "Edit configuration file in terminal", Description: "Open the config in $VISUAL or $EDITOR inside this terminal", Group: groupSettings, Scope: scopeAll},
	{ID: "config-reload", Title: "Reload configuration", Description: "Read the config file again", Group: groupSettings, Scope: scopeAll},

	{ID: "profile-menu", Title: "Switch project", Description: "Choose another project", Group: groupProjects, Scope: scopeAll, Keys: []string{"s"}},
	{ID: "setup", Title: "Edit project…", Description: "Guided setup: repositories, agents, GitHub, and JIRA", Group: groupProjects, Scope: scopeAll, Keys: []string{"I"}},
	{ID: "project-new", Title: "New project…", Description: "Set up another project with its repositories, agents, and JIRA boards or spaces", Group: groupProjects, Scope: scopeAll},
	{ID: "repo-add", Title: "Add repository to project…", Description: "List another repository's worktrees in the active project", Group: groupProjects, Scope: scopeAll},
	{ID: "repo-remove", Title: "Remove repository from project…", Description: "Stop listing a repository; files stay on disk", Group: groupProjects, Scope: scopeAll},
}

func enterActionCommandID(option enterActionOption) string {
	return cmdEnterActionPrefix + strings.ToLower(option.Label)
}

// commandCatalog lists every command that can be given a key: the built-in
// ones, and one per Enter action, agent, editor and project to choose
// directly.
func (m model) commandCatalog() []commandSpec {
	specs := make([]commandSpec, 0, len(baseCommands)+len(enterActionOptions)+len(m.cfg.App.Agents)+len(editorPresets)+len(m.cfg.App.Profiles))
	specs = append(specs, baseCommands...)
	for _, option := range enterActionOptions {
		specs = append(specs, commandSpec{ID: enterActionCommandID(option), Title: "Set Enter action: " + option.Label, Description: option.Description, Group: groupSettings, Scope: scopeAll})
	}
	for _, agent := range m.cfg.App.Agents {
		specs = append(specs, commandSpec{ID: cmdAgentPrefix + agent.Name, Title: "Use " + agent.Name + " as default agent", Description: agent.Command, Group: groupSettings, Scope: scopeAll})
	}
	for _, editor := range editorPresets {
		specs = append(specs, commandSpec{ID: cmdEditorPrefix + editor.Command, Title: "Use " + editor.Name + " as editor", Description: editor.Command, Group: groupSettings, Scope: scopeAll})
	}
	for _, profile := range m.cfg.App.Profiles {
		specs = append(specs, commandSpec{ID: cmdProfilePrefix + profile.Name, Title: "Switch to " + profile.Name, Description: "Make " + profile.Name + " the active project", Group: groupProjects, Scope: scopeAll})
	}
	return specs
}

// --- keys ----------------------------------------------------------------------

// keySeq is one keybinding: the keys pressed one after another. Most have a
// single key.
type keySeq []string

// maxKeySteps is how many keys the keybindings screen records for one binding.
const maxKeySteps = 2

// reservedKey reports keys no command can have: esc always cancels or goes
// back, and ctrl+c always quits.
func reservedKey(name string) bool {
	return name == "esc" || name == "ctrl+c"
}

// parseKeySeq reads a keybinding from its config spelling. It returns nil for
// one that cannot be used.
func parseKeySeq(value string) keySeq {
	steps := strings.Fields(core.NormalizeKeybinding(value))
	if len(steps) == 0 || slices.ContainsFunc(steps, reservedKey) {
		return nil
	}
	return keySeq(steps)
}

func parseKeySeqs(values []string) []keySeq {
	seqs := make([]keySeq, 0, len(values))
	for _, value := range values {
		seq := parseKeySeq(value)
		if seq == nil || slices.ContainsFunc(seqs, seq.equal) {
			continue
		}
		seqs = append(seqs, seq)
	}
	return seqs
}

// String is the config spelling.
func (s keySeq) String() string { return strings.Join(s, " ") }

// Label is the spelling shown in the app.
func (s keySeq) Label() string {
	labels := make([]string, len(s))
	for idx, step := range s {
		labels[idx] = keyStepLabel(step)
	}
	return strings.Join(labels, " › ")
}

var keyGlyphs = map[string]string{"up": "↑", "down": "↓", "left": "←", "right": "→"}

func keyStepLabel(step string) string {
	name := step
	prefix := ""
	if idx := strings.LastIndex(step[:max(0, len(step)-1)], "+"); idx >= 0 {
		prefix, name = step[:idx+1], step[idx+1:]
	}
	if glyph, ok := keyGlyphs[name]; ok {
		return prefix + glyph
	}
	return step
}

func (s keySeq) equal(other keySeq) bool { return slices.Equal(s, other) }

func (s keySeq) startsWith(prefix keySeq) bool {
	return len(prefix) <= len(s) && slices.Equal(s[:len(prefix)], prefix)
}

// overlaps reports that two keybindings cannot both work in one view: they
// are the same, or one is the start of the other and would always win.
func (s keySeq) overlaps(other keySeq) bool {
	return s.startsWith(other) || other.startsWith(s)
}

func keySeqsEqual(a, b []keySeq) bool {
	return slices.EqualFunc(a, b, keySeq.equal)
}

func keySeqStrings(seqs []keySeq) []string {
	values := make([]string, len(seqs))
	for idx, seq := range seqs {
		values[idx] = seq.String()
	}
	return values
}

func keySeqLabels(seqs []keySeq) []string {
	labels := make([]string, len(seqs))
	for idx, seq := range seqs {
		labels[idx] = seq.Label()
	}
	return labels
}

// keyName is the name a key press goes by in keybindings.
func keyName(msg tea.KeyMsg) string {
	if msg.Paste {
		return ""
	}
	name := msg.String()
	// The space key reports itself as " ", which a config file cannot hold.
	if strings.HasSuffix(name, " ") {
		return strings.TrimSuffix(name, " ") + "space"
	}
	return name
}

// textKey reports a key that types a character, which a text input needs for
// itself.
func textKey(name string) bool {
	return name == "space" || len([]rune(name)) == 1
}

// --- the keymap in effect ------------------------------------------------------

// keymap is the catalog with the configured keys of every command.
type keymap struct {
	specs []commandSpec
	keys  [][]keySeq
	byID  map[string]int
}

func newKeymap(specs []commandSpec, overrides map[string][]string) keymap {
	km := keymap{specs: specs, keys: make([][]keySeq, len(specs)), byID: make(map[string]int, len(specs))}
	for idx, spec := range specs {
		km.byID[spec.ID] = idx
		keys := spec.Keys
		if custom, ok := overrides[spec.ID]; ok {
			keys = custom
		}
		km.keys[idx] = parseKeySeqs(keys)
	}
	return km
}

func (m model) keymap() keymap {
	return newKeymap(m.commandCatalog(), m.cfg.App.Keybindings)
}

func (km keymap) spec(id string) (commandSpec, bool) {
	idx, ok := km.byID[id]
	if !ok {
		return commandSpec{}, false
	}
	return km.specs[idx], true
}

func (km keymap) keysOf(id string) []keySeq {
	idx, ok := km.byID[id]
	if !ok {
		return nil
	}
	return km.keys[idx]
}

// label is the first key of a command as shown in the app, or "" when it has
// none.
func (km keymap) label(id string) string {
	if keys := km.keysOf(id); len(keys) > 0 {
		return keys[0].Label()
	}
	return ""
}

// shortcut is what the palette and the help page show next to a command.
func (km keymap) shortcut(id string) string {
	label := km.label(id)
	switch {
	case id == cmdDelete && label != "":
		// Deleting takes the key twice.
		if keys := km.keysOf(id); len(keys[0]) > 1 {
			return label + " ×2"
		}
		return label + " " + label
	case id == cmdClearFilter && label == "":
		return "esc"
	}
	return label
}

func (km keymap) modified(id string) bool {
	spec, ok := km.spec(id)
	return ok && !keySeqsEqual(km.keysOf(id), parseKeySeqs(spec.Keys))
}

func (km keymap) modifiedCount() int {
	count := 0
	for _, spec := range km.specs {
		if km.modified(spec.ID) {
			count++
		}
	}
	return count
}

// resolve finds the command of a view that the pressed keys run. pending
// reports that they are the start of a longer keybinding instead. reachable,
// when set, limits the commands that can answer.
func (km keymap) resolve(pressed keySeq, scope commandScope, reachable func(string) bool) (id string, pending bool) {
	for idx, spec := range km.specs {
		if spec.Scope&scope == 0 || (reachable != nil && !reachable(spec.ID)) {
			continue
		}
		for _, keys := range km.keys[idx] {
			if keys.equal(pressed) {
				return spec.ID, false
			}
			if keys.startsWith(pressed) {
				pending = true
			}
		}
	}
	return "", pending
}

// explained finds a command of another view that the pressed keys belong to
// and that explains why it is unavailable in this one.
func (km keymap) explained(pressed keySeq, scope commandScope) string {
	for idx, spec := range km.specs {
		if !spec.Explains || spec.Scope&scope != 0 {
			continue
		}
		if slices.ContainsFunc(km.keys[idx], pressed.equal) {
			return spec.ID
		}
	}
	return ""
}

// keyConflict is a keybinding of another command that cannot coexist with a
// new one.
type keyConflict struct {
	ID   string
	Keys keySeq
}

// conflicts lists the keybindings that giving keys to a command would break:
// those of other commands that share a view with it and use the same keys, or
// keys that start the same way.
func (km keymap) conflicts(id string, keys keySeq) []keyConflict {
	spec, ok := km.spec(id)
	if !ok {
		return nil
	}
	var found []keyConflict
	for idx, other := range km.specs {
		if other.ID == id || other.Scope&spec.Scope == 0 {
			continue
		}
		for _, existing := range km.keys[idx] {
			if existing.overlaps(keys) {
				found = append(found, keyConflict{ID: other.ID, Keys: existing})
			}
		}
	}
	return found
}

// clashes reports that a key of the command also belongs to another command
// in the same view. The app never creates that state; a config file edited by
// hand can.
func (km keymap) clashes(id string) bool {
	for _, keys := range km.keysOf(id) {
		if len(km.conflicts(id, keys)) > 0 {
			return true
		}
	}
	return false
}

// --- changing keybindings ------------------------------------------------------

// keybindingChange is the config's keybindings after an edit, and the
// keybindings other commands lost to it.
type keybindingChange struct {
	Overrides map[string][]string
	Taken     []keyConflict
}

func cloneKeybindings(overrides map[string][]string) map[string][]string {
	if overrides == nil {
		return nil
	}
	clone := make(map[string][]string, len(overrides))
	for id, keys := range overrides {
		clone[id] = append([]string{}, keys...)
	}
	return clone
}

func keybindingsEqual(a, b map[string][]string) bool {
	return maps.EqualFunc(a, b, func(x, y []string) bool { return slices.Equal(x, y) })
}

// record stores the keys of a command in overrides: as an override when they
// differ from its defaults, and not at all when they match them.
func (km keymap) record(overrides map[string][]string, id string, keys []keySeq) {
	spec, ok := km.spec(id)
	if ok && keySeqsEqual(keys, parseKeySeqs(spec.Keys)) {
		delete(overrides, id)
		return
	}
	overrides[id] = keySeqStrings(keys)
}

// give returns the keybindings after a command gets the given keys. Other
// commands hand over whatever would conflict with the keys that are new to it.
func (km keymap) give(overrides map[string][]string, id string, keys []keySeq, added []keySeq) keybindingChange {
	next := cloneKeybindings(overrides)
	if next == nil {
		next = map[string][]string{}
	}
	km.record(next, id, keys)

	var taken []keyConflict
	var losers []string
	for _, seq := range added {
		for _, conflict := range km.conflicts(id, seq) {
			if !slices.ContainsFunc(taken, func(seen keyConflict) bool { return seen.ID == conflict.ID && seen.Keys.equal(conflict.Keys) }) {
				taken = append(taken, conflict)
			}
			if !slices.Contains(losers, conflict.ID) {
				losers = append(losers, conflict.ID)
			}
		}
	}
	for _, loser := range losers {
		remaining := slices.DeleteFunc(slices.Clone(km.keysOf(loser)), func(existing keySeq) bool {
			return slices.ContainsFunc(added, existing.overlaps)
		})
		km.record(next, loser, remaining)
	}
	if len(next) == 0 {
		next = nil
	}
	return keybindingChange{Overrides: next, Taken: taken}
}

// bind gives a command a keybinding: in place of the one at index replace, or
// in addition to its others when replace is out of range.
func (km keymap) bind(overrides map[string][]string, id string, keys keySeq, replace int) keybindingChange {
	var own []keySeq
	placed := false
	for idx, existing := range km.keysOf(id) {
		switch {
		case idx == replace:
			own = append(own, keys)
			placed = true
		case !existing.overlaps(keys):
			own = append(own, existing)
		}
	}
	if !placed {
		own = append(own, keys)
	}
	return km.give(overrides, id, own, []keySeq{keys})
}

// unbind removes one keybinding of a command.
func (km keymap) unbind(overrides map[string][]string, id string, index int) keybindingChange {
	own := slices.Clone(km.keysOf(id))
	if index < 0 || index >= len(own) {
		return keybindingChange{Overrides: overrides}
	}
	return km.give(overrides, id, slices.Delete(own, index, index+1), nil)
}

// reset gives a command its default keys back, taking them from any command
// they were moved to.
func (km keymap) reset(overrides map[string][]string, id string) keybindingChange {
	spec, ok := km.spec(id)
	if !ok {
		return keybindingChange{Overrides: overrides}
	}
	defaults := parseKeySeqs(spec.Keys)
	return km.give(overrides, id, defaults, defaults)
}

// --- hints -----------------------------------------------------------------------

// keyLabel is the first key of a command as shown in hints, or "" when it has
// none.
func (m model) keyLabel(id string) string {
	return m.keymap().label(id)
}

// howTo phrases a hint for running a command, such as "press I to run setup".
// A command without a key is still in the palette.
func (m model) howTo(id, purpose string) string {
	if label := m.keyLabel(id); label != "" {
		return "press " + label + " to " + purpose
	}
	return "use the command palette to " + purpose
}

// footerBindings are the keys advertised under the main view.
func (m model) footerBindings() []key.Binding {
	km := m.keymap()
	open := "open"
	if m.isPRMode() {
		open = "diff"
	} else if m.isTicketMode() {
		open = "ticket-read"
	}
	var bindings []key.Binding
	for _, hint := range []struct{ id, help string }{
		{cmdPalette, "commands"}, {cmdHelp, "help"}, {"refresh", "refresh"}, {"prs", "PR radar"}, {"tickets", "JIRA tickets"},
		{"filter", "filter"}, {open, "open/diff"}, {"open-browser", "open PR in browser"}, {cmdQuit, "quit"},
	} {
		if label := km.label(hint.id); label != "" {
			bindings = append(bindings, key.NewBinding(key.WithKeys(label), key.WithHelp(label, hint.help)))
		}
	}
	return bindings
}

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Stacked worktrees. A worktree can be started on top of another worktree's
// branch instead of the default branch, so a follow-up change does not wait
// for the first one to be merged:
//
//   - N on a worktree asks for a branch name and starts it from the selected
//     worktree's branch; N on a ticket asks which worktree to build on. In the
//     new-worktree input, tab switches between the two starting points.
//   - Stacked worktrees are listed below their parent, and say "restack" once
//     the parent has new commits, was merged, or is gone.
//   - B restacks the selected worktree: its own commits are rebased onto the
//     parent, or onto the default branch once the parent is merged.
//
// The link is kept in the repository's git config by the services.

// restackTimeout leaves room for a fetch and a rebase in a large repository.
const restackTimeout = 5 * time.Minute

type stackState int

const (
	stackNone stackState = iota
	stackCurrent
	stackParentMoved
	stackParentMerged
	stackParentGone
)

// stackStateOf says how a stacked worktree relates to its parent branch.
func stackStateOf(wt Worktree) stackState {
	switch {
	case !wt.Stacked():
		return stackNone
	case wt.Status.ParentGone:
		return stackParentGone
	case wt.ParentMerged:
		return stackParentMerged
	case wt.Status.ParentBehind > 0:
		return stackParentMoved
	default:
		return stackCurrent
	}
}

func (s stackState) needsRestack() bool {
	return s == stackParentMoved || s == stackParentMerged || s == stackParentGone
}

// stackedName draws a stacked worktree as a branch of the tree under its parent.
func stackedName(wt Worktree) string {
	if !wt.Stacked() {
		return wt.Name
	}
	return strings.Repeat("  ", max(0, wt.StackDepth-1)) + icons.StackChild + " " + wt.Name
}

// markMergedParents records on every stacked worktree whether the pull
// request of its parent branch has been merged.
func (m *model) markMergedParents() {
	mark := func(list []Worktree) {
		for i := range list {
			wt := &list[i]
			wt.ParentMerged = wt.Stacked() && core.BranchMerged(m.prIndexes[m.worktreeRepository(*wt)], wt.Stack.Parent)
		}
	}
	mark(m.allWorktrees)
	mark(m.visibleWorktrees)
}

// stackParentWorktree finds the worktree holding the parent branch of wt.
func (m model) stackParentWorktree(wt Worktree) *Worktree {
	if !wt.Stacked() {
		return nil
	}
	repository := m.worktreeRepository(wt)
	for i := range m.allWorktrees {
		candidate := &m.allWorktrees[i]
		if candidate.Branch == wt.Stack.Parent && !candidate.Detached && m.worktreeRepository(*candidate) == repository {
			return candidate
		}
	}
	return nil
}

// stackChildren lists the worktrees stacked directly on wt.
func (m model) stackChildren(wt Worktree) []Worktree {
	if wt.Detached || wt.Branch == "" {
		return nil
	}
	repository := m.worktreeRepository(wt)
	var children []Worktree
	for _, candidate := range m.allWorktrees {
		if candidate.Stack.Parent == wt.Branch && candidate.Path != wt.Path && m.worktreeRepository(candidate) == repository {
			children = append(children, candidate)
		}
	}
	return children
}

// mergedParentPR is the merged pull request of a branch, for naming it.
func (m model) mergedParentPR(wt Worktree) *PullRequest {
	prs := m.prIndexes[m.worktreeRepository(wt)]
	for i := range prs {
		if prs[i].HeadRefName == wt.Stack.Parent && !prs[i].IsCrossRepository && strings.EqualFold(prs[i].State, "merged") {
			return &prs[i]
		}
	}
	return nil
}

// stackStatusLine says in words where a stacked worktree stands.
func (m model) stackStatusLine(wt Worktree) string {
	parent := wt.Stack.Parent
	switch stackStateOf(wt) {
	case stackParentGone:
		return fmt.Sprintf("branch %s is gone", parent)
	case stackParentMerged:
		if pr := m.mergedParentPR(wt); pr != nil {
			return fmt.Sprintf("%s was merged (#%d)", parent, pr.Number)
		}
		return fmt.Sprintf("%s was merged", parent)
	case stackParentMoved:
		if wt.Status.ParentBehind == 1 {
			return fmt.Sprintf("%s has 1 new commit", parent)
		}
		return fmt.Sprintf("%s has %d new commits", parent, wt.Status.ParentBehind)
	}
	if wt.StatusPending {
		return "checking " + parent + "..."
	}
	return "up to date with " + parent
}

// stackDetailLines describes the stack around a worktree for the details pane.
func (m model) stackDetailLines(wt Worktree) []string {
	var lines []string
	if wt.Stacked() {
		where := wt.Stack.Parent
		if parent := m.stackParentWorktree(wt); parent == nil {
			where += " (no worktree)"
		} else if parent.Name != parent.Branch {
			where += " (worktree " + parent.Name + ")"
		}
		lines = append(lines, "Stacked on: "+where)
		if stackStateOf(wt).needsRestack() {
			lines = append(lines, fmt.Sprintf("  %s %s: press B to restack", icons.Restack, m.stackStatusLine(wt)))
		} else {
			lines = append(lines, "  "+m.stackStatusLine(wt))
		}
	}
	if children := m.stackChildren(wt); len(children) > 0 {
		names := make([]string, 0, len(children))
		for _, child := range children {
			names = append(names, child.Name)
		}
		lines = append(lines, "Stacked on this: "+strings.Join(names, ", "))
	}
	return lines
}

// stackParentProblem explains why a worktree cannot have another one stacked
// on it, or returns "".
func (m model) stackParentProblem(wt Worktree) string {
	switch {
	case wt.Missing || wt.Prunable:
		return wt.Name + " is missing on disk"
	case wt.Detached || wt.Branch == "" || wt.Branch == "detached":
		return wt.Name + " has no branch to build on"
	case m.isPendingCreation(wt.Path):
		return wt.Name + " is still being created"
	}
	return ""
}

// insertBelowStackParent puts a new stacked row under its parent and the
// parent's other descendants; rows without a listed parent go last.
func (m model) insertBelowStackParent(worktrees []Worktree, worktree Worktree) []Worktree {
	if worktree.Stacked() {
		for i, candidate := range worktrees {
			if candidate.Branch != worktree.Stack.Parent || candidate.Detached || m.worktreeRepository(candidate) != m.worktreeRepository(worktree) {
				continue
			}
			at := i + 1
			for at < len(worktrees) && worktrees[at].StackDepth > candidate.StackDepth {
				at++
			}
			worktrees = append(worktrees, Worktree{})
			copy(worktrees[at+1:], worktrees[at:])
			worktrees[at] = worktree
			return worktrees
		}
	}
	return append(worktrees, worktree)
}

// --- new worktree on top of another --------------------------------------------

// startStackedWorktree handles N: on a worktree it asks for the branch to
// start on top of it, on a ticket it first asks which worktree to build on.
func (m model) startStackedWorktree() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	switch {
	case m.isTicketMode():
		selected := m.selectedTicket()
		if selected == nil {
			m.statusMessage = "Select a JIRA ticket first"
			m.statusError = true
			return m, nil
		}
		return m.openStackParentPicker(stackParentPicker{Purpose: stackPickForTicket, Ticket: *selected})
	case m.isPRMode():
		m.statusMessage = "Stacking starts from a local worktree or a JIRA ticket"
		m.statusError = true
		return m, nil
	}
	selected := m.selectedWorktree()
	if selected == nil {
		m.statusMessage = "Select the worktree to build on first"
		m.statusError = true
		return m, nil
	}
	if problem := m.stackParentProblem(*selected); problem != "" {
		m.statusMessage = problem
		m.statusError = true
		return m, nil
	}
	parent := *selected
	repository := ""
	if m.cfg.MultiRepository() {
		repository = m.worktreeRepository(parent)
	}
	next, cmd := m.openNewWorktreeInput(repository)
	m = next.(model)
	m.newWorktreeParentOption = parent
	m.setNewWorktreeParent(parent)
	return m, cmd
}

// offerStackParent lets tab in the new-worktree input switch to building on
// the selected worktree, when that worktree is in the repository being asked
// about.
func (m *model) offerStackParent(repository string) {
	m.newWorktreeParent = Worktree{}
	m.newWorktreeParentOption = Worktree{}
	if !m.isWorktreeMode() {
		return
	}
	selected := m.selectedWorktree()
	if selected == nil || m.stackParentProblem(*selected) != "" {
		return
	}
	if repository != "" && m.worktreeRepository(*selected) != repository {
		return
	}
	m.newWorktreeParentOption = *selected
}

func (m *model) setNewWorktreeParent(parent Worktree) {
	m.newWorktreeParent = parent
	m.statusError = false
	if parent.Path == "" {
		m.statusMessage = "Enter branch/worktree name"
		return
	}
	m.statusMessage = "Enter the branch to start on top of " + parent.Branch
}

func (m *model) toggleNewWorktreeParent() {
	if m.newWorktreeParentOption.Path == "" || m.newWorktreeTicketKey != "" {
		return
	}
	if m.newWorktreeParent.Path != "" {
		m.setNewWorktreeParent(Worktree{})
		return
	}
	m.setNewWorktreeParent(m.newWorktreeParentOption)
}

func (m *model) clearNewWorktreeParent() {
	m.newWorktreeParent = Worktree{}
	m.newWorktreeParentOption = Worktree{}
}

// stackedWorktree is the row shown for a worktree being created on parent.
func (m model) stackedWorktree(cfg Config, branch string, parent Worktree) Worktree {
	return Worktree{
		Name:       branch,
		Branch:     branch,
		Path:       m.services.CreateWorktreePath(cfg, branch),
		Managed:    true,
		Repository: cfg.RepositoryName(),
		Stack:      core.StackLink{Parent: parent.Branch},
		StackDepth: parent.StackDepth + 1,
	}
}

// createStackedWorktree starts branch in a new worktree on top of parent.
func (m model) createStackedWorktree(cfg Config, branch string, parent Worktree) (tea.Model, tea.Cmd) {
	worktree := m.stackedWorktree(cfg, branch, parent)
	m.beginWorktreeCreation(worktree, "")
	creating := fmt.Sprintf("Creating %s on top of %s...", branch, parent.Branch)
	return m, tea.Batch(
		m.ops.enqueue(gitQueueKey(cfg), createStackedWorktreeCmd(m.services, cfg, worktree, parent.Branch)),
		m.startLoading(loadingMutation, creating),
	)
}

func createStackedWorktreeCmd(services core.WorktreeService, cfg Config, worktree Worktree, parent string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		err := services.CreateStackedWorktree(ctx, cfg, worktree.Branch, worktree.Path, parent)
		return createResult{Path: worktree.Path, Name: worktree.Name, Err: err}
	}
}

// createStackedTicketWorktree creates another worktree for a ticket on top of
// parent, in the parent's repository, under a ticket branch name still free.
func (m model) createStackedTicketWorktree(ticket Ticket, parent Worktree) (tea.Model, tea.Cmd) {
	repository := ""
	if m.cfg.MultiRepository() {
		repository = m.worktreeRepository(parent)
	}
	cfg := m.cfg.ForRepository(repository)
	worktree := m.stackedWorktree(cfg, m.freeTicketBranchName(ticket, cfg), parent)
	m.beginWorktreeCreation(worktree, ticket.Key)
	creating := fmt.Sprintf("Creating %s for %s on top of %s...", worktree.Branch, ticket.Key, parent.Branch)
	services, key := m.services, ticket.Key
	create := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		err := services.CreateStackedWorktree(ctx, cfg, worktree.Branch, worktree.Path, parent.Branch)
		return ticketWorktreeResult{Key: key, Worktree: worktree, Err: err}
	}
	return m, tea.Batch(
		m.ops.enqueue(gitQueueKey(cfg), create),
		m.startLoading(loadingMutation, creating),
		m.rememberRepository(repository),
	)
}

// --- choosing a parent -----------------------------------------------------------

type stackPickPurpose int

const (
	// stackPickForTicket creates the ticket's worktree on the chosen one.
	stackPickForTicket stackPickPurpose = iota
	// stackPickForWorktree records the chosen one as the parent of Child.
	stackPickForWorktree
)

const stackPickerRows = 10

type stackParentPicker struct {
	Purpose stackPickPurpose
	Ticket  Ticket
	Child   Worktree
	Options []Worktree
	Cursor  int
}

// stackParentOptions lists the worktrees another one can be stacked on. For
// an existing child that is its own repository minus the child and everything
// already stacked on it, which would close a loop.
func (m model) stackParentOptions(child *Worktree) []Worktree {
	descendant := map[string]bool{}
	if child != nil {
		descendant[child.Branch] = true
		for grew := true; grew; {
			grew = false
			for _, wt := range m.allWorktrees {
				if wt.Stacked() && descendant[wt.Stack.Parent] && !descendant[wt.Branch] && m.worktreeRepository(wt) == m.worktreeRepository(*child) {
					descendant[wt.Branch] = true
					grew = true
				}
			}
		}
	}
	var options []Worktree
	for _, wt := range m.allWorktrees {
		if m.stackParentProblem(wt) != "" {
			continue
		}
		if child != nil && (m.worktreeRepository(wt) != m.worktreeRepository(*child) || descendant[wt.Branch]) {
			continue
		}
		options = append(options, wt)
	}
	return options
}

func (m model) openStackParentPicker(picker stackParentPicker) (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	var child *Worktree
	if picker.Purpose == stackPickForWorktree {
		child = &picker.Child
	}
	picker.Options = m.stackParentOptions(child)
	if len(picker.Options) == 0 {
		m.statusMessage = "There is no worktree with a branch to stack on"
		m.statusError = true
		return m, nil
	}
	// Start on the current parent, so enter keeps what is recorded.
	for idx, option := range picker.Options {
		if child != nil && option.Branch == child.Stack.Parent {
			picker.Cursor = idx
		}
	}
	m.pickingStackParent = true
	m.stackPicker = picker
	m.statusMessage = "Choose the worktree to build on"
	m.statusError = false
	return m, nil
}

func (m model) updateStackParentPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	picker := &m.stackPicker
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.pickingStackParent = false
		return m, m.setTransientStatus("Stacking cancelled")
	case "up", "k":
		picker.Cursor = max(0, picker.Cursor-1)
		return m, nil
	case "down", "j":
		picker.Cursor = min(len(picker.Options)-1, picker.Cursor+1)
		return m, nil
	case "enter":
		m.pickingStackParent = false
		parent := picker.Options[min(max(picker.Cursor, 0), len(picker.Options)-1)]
		if picker.Purpose == stackPickForTicket {
			return m.createStackedTicketWorktree(picker.Ticket, parent)
		}
		return m.setStackParent(picker.Child, parent.Branch)
	}
	return m, nil
}

func renderStackParentPickerModal(m model) string {
	picker := m.stackPicker
	intro := fmt.Sprintf("Start the %s worktree on top of which worktree?", picker.Ticket.Key)
	if picker.Purpose == stackPickForWorktree {
		intro = fmt.Sprintf("Which worktree was %s started from?", picker.Child.Branch)
	}
	body := []string{intro, ""}
	start, end := visibleRowRange(picker.Cursor, len(picker.Options), stackPickerRows)
	if start > 0 {
		body = append(body, mutedStyle.Render(fmt.Sprintf("  %d more above", start)))
	}
	for idx := start; idx < end; idx++ {
		option := picker.Options[idx]
		label := option.Branch
		if m.cfg.MultiRepository() {
			label = fmt.Sprintf("%-10s %s", m.worktreeRepository(option), label)
		}
		body = append(body, selectorLine(idx == picker.Cursor, label, picker.Purpose == stackPickForWorktree && option.Branch == picker.Child.Stack.Parent))
	}
	if end < len(picker.Options) {
		body = append(body, mutedStyle.Render(fmt.Sprintf("  %d more below", len(picker.Options)-end)))
	}
	return renderModal("Stack On", body, selectHints)
}

// --- recording and clearing the parent of an existing worktree -------------------

type stackLinkResult struct {
	Path    string
	Message string
	Err     error
}

// openStackOnSelected asks which worktree the selected one was started from,
// for worktrees created outside the app or before it knew about stacks.
func (m model) openStackOnSelected() (tea.Model, tea.Cmd) {
	selected := m.selectedWorktree()
	if !m.isWorktreeMode() || selected == nil {
		m.statusMessage = "Select a worktree first"
		m.statusError = true
		return m, nil
	}
	if problem := m.stackParentProblem(*selected); problem != "" {
		m.statusMessage = problem
		m.statusError = true
		return m, nil
	}
	return m.openStackParentPicker(stackParentPicker{Purpose: stackPickForWorktree, Child: *selected})
}

func (m model) unstackSelected() (tea.Model, tea.Cmd) {
	selected := m.selectedWorktree()
	if !m.isWorktreeMode() || selected == nil || !selected.Stacked() {
		m.statusMessage = "The selected worktree is not stacked on another one"
		m.statusError = true
		return m, nil
	}
	return m.setStackParent(*selected, "")
}

func (m model) setStackParent(child Worktree, parent string) (tea.Model, tea.Cmd) {
	cfg, services := m.worktreeCfg(child), m.services
	message := fmt.Sprintf("%s is now stacked on %s", child.Branch, parent)
	if parent == "" {
		message = fmt.Sprintf("%s left its stack", child.Branch)
	}
	set := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		return stackLinkResult{Path: child.Path, Message: message, Err: services.SetStackParent(ctx, cfg, child, parent)}
	}
	return m, tea.Batch(m.ops.enqueue(gitQueueKey(cfg), set), m.startLoading(loadingMutation, "Updating the stack..."))
}

func (m model) handleStackLinkResult(msg stackLinkResult) (tea.Model, tea.Cmd) {
	m.loading = false
	if msg.Err != nil {
		m.statusMessage = fmt.Sprintf("Stack update failed: %v", msg.Err)
		m.statusError = true
		return m, nil
	}
	return m.reloadWorktreesKeeping(msg.Path, msg.Message)
}

// reloadWorktreesKeeping lists worktrees again with path selected and leaves
// message in the status bar once the list is back.
func (m model) reloadWorktreesKeeping(path string, message string) (tea.Model, tea.Cmd) {
	if m.isWorktreeMode() {
		m.selectedPath = path
		m.pendingWorktreePath = path
	}
	m.statusAfterLoad = message
	return m, tea.Batch(loadWorktreesCmd(m.services, m.cfg, m.showAll), m.startLoading(loadingWorktrees, message))
}

// --- restacking --------------------------------------------------------------------

type restackResult struct {
	Worktree Worktree
	Result   core.RestackResult
	Err      error
}

// openRestackConfirmation handles B: it shows what restacking the selected
// worktree will do before any history is rewritten.
func (m model) openRestackConfirmation() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	if !m.isWorktreeMode() {
		m.statusMessage = "Restack is available for local worktrees"
		m.statusError = true
		return m, nil
	}
	selected := m.selectedWorktree()
	if selected == nil {
		return m, nil
	}
	if !selected.Stacked() {
		m.statusMessage = selected.Name + " is not stacked on another worktree; use the command palette to stack it"
		m.statusError = true
		return m, nil
	}
	if selected.Missing || selected.Prunable || m.isPendingCreation(selected.Path) {
		m.statusMessage = selected.Name + " is not ready to restack"
		m.statusError = true
		return m, nil
	}
	m.confirmingRestack = true
	m.restackTarget = *selected
	m.statusMessage = "Restack " + selected.Name
	m.statusError = false
	return m, nil
}

func (m model) updateRestackConfirmation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.confirmingRestack = false
		return m, m.setTransientStatus("Restack cancelled")
	case "enter":
		return m.startRestack(m.restackTarget, false)
	case "d":
		return m.startRestack(m.restackTarget, true)
	}
	return m, nil
}

// mergedBranches names the branches of a repository whose pull request the
// loaded PR labels show as merged.
func (m model) mergedBranches(repository string) []string {
	prs := m.prIndexes[repository]
	seen := map[string]bool{}
	var merged []string
	for _, pr := range prs {
		if seen[pr.HeadRefName] {
			continue
		}
		seen[pr.HeadRefName] = true
		if core.BranchMerged(prs, pr.HeadRefName) {
			merged = append(merged, pr.HeadRefName)
		}
	}
	return merged
}

func (m model) startRestack(wt Worktree, ontoDefault bool) (tea.Model, tea.Cmd) {
	m.confirmingRestack = false
	cfg, services := m.worktreeCfg(wt), m.services
	options := core.RestackOptions{MergedBranches: m.mergedBranches(m.worktreeRepository(wt)), OntoDefault: ontoDefault}
	restack := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), restackTimeout)
		defer cancel()
		result, err := services.RestackWorktree(ctx, cfg, wt, options)
		return restackResult{Worktree: wt, Result: result, Err: err}
	}
	return m, tea.Batch(m.ops.enqueue(gitQueueKey(cfg), restack), m.startLoading(loadingMutation, fmt.Sprintf("Restacking %s...", wt.Name)))
}

func (m model) handleRestackResult(msg restackResult) (tea.Model, tea.Cmd) {
	m.loading = false
	wt := msg.Worktree
	if msg.Err != nil {
		m.statusMessage = fmt.Sprintf("Restack failed for %s: %v", wt.Name, msg.Err)
		m.statusError = true
		// A rebase stopped on conflicts changes the row; re-read only that
		// one so the explanation stays in the status bar.
		return m, loadWorktreeStatusCmd(m.services, m.worktreeCfg(wt), wt, m.worktreeLoadSeq)
	}
	result := msg.Result
	message := fmt.Sprintf("%s already sits on %s", wt.Branch, result.Onto)
	if result.Rebased {
		commits := fmt.Sprintf("%d commits", result.Commits)
		if result.Commits == 1 {
			commits = "1 commit"
		}
		message = fmt.Sprintf("Restacked %s onto %s (%s); push it with --force-with-lease", wt.Branch, result.Onto, commits)
	}
	if result.Parent == "" {
		message += "; it left its stack"
	}
	next, cmd := m.reloadWorktreesKeeping(wt.Path, message)
	return next, tea.Batch(cmd, notifyCmd(m.services, "Worktree restacked", message))
}

func renderRestackModal(m model) string {
	wt := m.restackTarget
	parent := wt.Stack.Parent
	var plan string
	switch stackStateOf(wt) {
	case stackParentMerged, stackParentGone:
		plan = "Its commits move onto the default branch, or onto what " + parent + " was stacked on."
	case stackParentMoved:
		plan = "Its commits move onto the tip of " + parent + "."
	default:
		plan = "Nothing to rebase unless GitHub says " + parent + " was merged."
	}
	body := []string{
		infoStyle.Render(wt.Branch) + " is stacked on " + infoStyle.Render(parent) + ".",
		warnStyle.Render(m.stackStatusLine(wt)),
		"",
		plan,
		mutedStyle.Render("Only the commits made on " + wt.Branch + " are replayed. The worktree"),
		mutedStyle.Render("must be clean, and a pushed branch needs a force push afterwards."),
	}
	return renderModal("Restack Worktree", body, []keyHint{{"enter", "restack"}, {"d", "onto default branch"}, {"esc", "cancel"}})
}

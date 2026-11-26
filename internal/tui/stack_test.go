package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/app"
	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	"github.com/VashingMachine/wt-manager/internal/testkit"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// stackSpy records the stack and sweep calls the UI makes instead of running git.
type stackSpy struct {
	core.Services
	mu            sync.Mutex
	worktrees     []Worktree
	stacked       []string
	parents       []string
	restacks      []core.RestackOptions
	restackResult core.RestackResult
	restackErr    error
	candidates    []core.SweepCandidate
	sweepAges     []time.Duration
	sweepErr      error
	removed       []string
}

func (s *stackSpy) PeekWorktrees(Config, bool) ([]Worktree, bool) { return nil, false }

func (s *stackSpy) DiscoverWorktrees(context.Context, Config, bool) ([]Worktree, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Worktree(nil), s.worktrees...), nil
}

func (s *stackSpy) LoadWorktreeStatus(_ context.Context, _ Config, wt Worktree) (Worktree, error) {
	return wt, nil
}

func (s *stackSpy) LoadPullRequestIndex(context.Context, Config, bool) ([]PullRequest, string, error) {
	return nil, "", nil
}

func (s *stackSpy) Notify(context.Context, string, string) error { return nil }

func (s *stackSpy) CreateStackedWorktree(_ context.Context, _ Config, branch string, path string, parent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stacked = append(s.stacked, branch+" on "+parent+" at "+path)
	return nil
}

func (s *stackSpy) SetStackParent(_ context.Context, _ Config, wt Worktree, parent string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parents = append(s.parents, wt.Branch+" on "+parent)
	return nil
}

func (s *stackSpy) RestackWorktree(_ context.Context, _ Config, _ Worktree, options core.RestackOptions) (core.RestackResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restacks = append(s.restacks, options)
	return s.restackResult, s.restackErr
}

func (s *stackSpy) SweepCandidates(_ context.Context, _ Config, olderThan time.Duration) ([]core.SweepCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepAges = append(s.sweepAges, olderThan)
	return s.candidates, s.sweepErr
}

func (s *stackSpy) RemoveWorktree(_ context.Context, _ Config, wt Worktree) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removed = append(s.removed, wt.Name)
	return nil
}

func stackWorktrees() []Worktree {
	return []Worktree{
		{Name: "app", Path: "/repo/app", Branch: "main", IsMain: true, Managed: true},
		{Name: "base", Path: "/wt/base", Branch: "base", Managed: true},
		{Name: "child", Path: "/wt/child", Branch: "child", Managed: true, Stack: core.StackLink{Parent: "base", Base: "abc"}, StackDepth: 1},
		{Name: "other", Path: "/wt/other", Branch: "other", Managed: true},
	}
}

// stackTestModel shows a project where "child" is stacked on "base".
func stackTestModel(spy *stackSpy) model {
	spy.Services = app.NewServices(nil)
	spy.worktrees = stackWorktrees()
	m := NewModel(Config{
		HomeDir:    "/home/me",
		ConfigPath: "/home/me/.wt-manager.json",
		RepoSlug:   "owner/app",
		ActiveProfile: RepositoryProfile{
			Name:           "app",
			RepositoryPath: "/repo/app",
			WorktreesDir:   "/wt",
			RemoteName:     "origin",
			GitHubRepo:     "owner/app",
		},
		App: AppConfig{
			DefaultAgent: "codex",
			Agents:       []AgentTool{{Name: "codex", Command: "codex"}},
			Sweep:        core.SweepConfig{OlderThanDays: 5},
		},
	}, spy)
	m.ready = true
	m.loading = false
	m.width, m.height = 160, 34
	m.allWorktrees = stackWorktrees()
	m.resize()
	m.applyFilter()
	return m
}

func selectWorktree(t *testing.T, m model, name string) model {
	t.Helper()
	for idx, wt := range m.visibleWorktrees {
		if wt.Name == name {
			m.table.SetCursor(idx)
			m.rememberSelection()
			m.updateDetailContent()
			return m
		}
	}
	t.Fatalf("no visible worktree named %q", name)
	return m
}

// feed delivers command results back to the model, as Bubble Tea would.
func feed(m model, msgs ...tea.Msg) (model, []tea.Msg) {
	var produced []tea.Msg
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = next.(model)
		produced = append(produced, runPromptCommands(cmd)...)
	}
	return m, produced
}

func worktreeNames(worktrees []Worktree) []string {
	names := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		names = append(names, wt.Name)
	}
	return names
}

// Assures that N asks for a branch to start on top of the selected worktree and creates it from that worktree's branch, listed right below its stack.
func TestStackNewCreatesWorktreeOnTopOfSelected(t *testing.T) {
	spy := &stackSpy{}
	m := selectWorktree(t, stackTestModel(spy), "base")

	m, _ = pressKeys(t, m, "N")
	if !m.creatingWorktree || m.newWorktreeParent.Branch != "base" {
		t.Fatalf("N did not open the stacked input: creating=%v parent=%q", m.creatingWorktree, m.newWorktreeParent.Branch)
	}
	if modal := ansi.Strip(renderNewWorktreeModal(m)); !strings.Contains(modal, "New Stacked Worktree") || !strings.Contains(modal, "on top of base") {
		t.Fatalf("input does not say what it builds on:\n%s", modal)
	}

	m.newInput.SetValue("follow-up")
	m, msgs := pressKeys(t, m, "enter")
	if want := []string{"follow-up on base at /wt/follow-up"}; !reflect.DeepEqual(spy.stacked, want) {
		t.Fatalf("stacked creations = %v, want %v", spy.stacked, want)
	}
	if results := messagesOf[createResult](msgs); len(results) != 1 || results[0].Err != nil {
		t.Fatalf("creation result = %#v", results)
	}
	if got, want := worktreeNames(m.allWorktrees), []string{"app", "base", "child", "follow-up", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want the new worktree below base and its stack: %v", got, want)
	}
	created := m.allWorktrees[3]
	if created.Stack.Parent != "base" || created.StackDepth != 1 || m.newWorktreeParent.Path != "" {
		t.Fatalf("pending row = %#v, leftover parent = %q", created, m.newWorktreeParent.Path)
	}
}

// Assures that in the ordinary new-worktree input tab switches to building on the selected worktree and back.
func TestNewWorktreeInputTabSwitchesStartingPoint(t *testing.T) {
	spy := &stackSpy{}
	m := selectWorktree(t, stackTestModel(spy), "child")

	m, _ = pressKeys(t, m, "n")
	if m.newWorktreeParent.Path != "" {
		t.Fatal("n started stacked")
	}
	if modal := ansi.Strip(renderNewWorktreeModal(m)); !strings.Contains(modal, "on top of child") {
		t.Fatalf("input does not offer the selected worktree:\n%s", modal)
	}
	m, _ = pressKeys(t, m, "tab")
	if m.newWorktreeParent.Branch != "child" {
		t.Fatalf("tab did not switch to the selected worktree: %q", m.newWorktreeParent.Branch)
	}
	m, _ = pressKeys(t, m, "tab")
	if m.newWorktreeParent.Path != "" {
		t.Fatal("a second tab did not return to the default branch")
	}

	m, _ = pressKeys(t, m, "tab")
	m.newInput.SetValue("deeper")
	m, _ = pressKeys(t, m, "enter")
	if want := []string{"deeper on child at /wt/deeper"}; !reflect.DeepEqual(spy.stacked, want) {
		t.Fatalf("stacked creations = %v, want %v", spy.stacked, want)
	}
	if created := m.allWorktrees[m.worktreeIndex("/wt/deeper")]; created.StackDepth != 2 {
		t.Fatalf("depth of a worktree stacked on a stacked one = %d, want 2", created.StackDepth)
	}
}

// Assures that a worktree without a branch cannot be built on.
func TestStackNewRefusesDetachedWorktree(t *testing.T) {
	m := stackTestModel(&stackSpy{})
	m.allWorktrees = append(m.allWorktrees, Worktree{Name: "loose", Path: "/wt/loose", Branch: "detached", Detached: true, Managed: true})
	m.applyFilter()
	m = selectWorktree(t, m, "loose")

	m, _ = pressKeys(t, m, "N")
	if m.creatingWorktree || !m.statusError || !strings.Contains(m.statusMessage, "no branch to build on") {
		t.Fatalf("N on a detached worktree: creating=%v status=%q", m.creatingWorktree, m.statusMessage)
	}
}

// Assures that N on a ticket asks which worktree to build on and creates the ticket's branch on top of it.
func TestStackNewOnTicketAsksForParent(t *testing.T) {
	spy := &stackSpy{}
	m := stackTestModel(spy)
	m.mode = modeTickets
	m.tickets = []Ticket{{Key: "APP-7", Summary: "Follow up"}}
	m.visibleTickets = m.tickets
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)

	m, _ = pressKeys(t, m, "N")
	if !m.pickingStackParent || m.stackPicker.Purpose != stackPickForTicket {
		t.Fatal("N on a ticket did not ask for a worktree to build on")
	}
	modal := ansi.Strip(renderStackParentPickerModal(m))
	for _, want := range []string{"APP-7", "main", "base", "child", "other"} {
		if !strings.Contains(modal, want) {
			t.Fatalf("picker is missing %q:\n%s", want, modal)
		}
	}

	m, msgs := pressKeys(t, m, "j", "enter")
	if want := []string{"APP-7-follow-up on base at /wt/APP-7-follow-up"}; !reflect.DeepEqual(spy.stacked, want) {
		t.Fatalf("stacked creations = %v, want %v", spy.stacked, want)
	}
	if results := messagesOf[ticketWorktreeResult](msgs); len(results) != 1 || results[0].Key != "APP-7" || results[0].Worktree.Stack.Parent != "base" {
		t.Fatalf("ticket creation result = %#v", results)
	}
	if selected := m.selectedTicket(); selected == nil || selected.Worktree == nil || selected.Worktree.Branch != "APP-7-follow-up" {
		t.Fatalf("ticket does not show its new worktree: %#v", selected)
	}
}

// Assures that a stacked worktree is drawn under its parent and turns to "restack" once the parent's pull request is merged or the parent gained commits.
func TestStackedRowShowsTreeAndRestackState(t *testing.T) {
	m := selectWorktree(t, stackTestModel(&stackSpy{}), "child")
	row := m.worktreeRow(m.visibleWorktrees[2], false)
	if row[0] != icons.StackChild+" child" || !strings.Contains(row[2], "clean") {
		t.Fatalf("stacked row = %v", row)
	}
	if detail := ansi.Strip(m.detail.View()); !strings.Contains(detail, "Stacked on: base") || !strings.Contains(detail, "up to date with base") {
		t.Fatalf("details do not describe the stack:\n%s", detail)
	}

	merged, _ := feed(m, prIndexResult{Repository: "app", RepoSlug: "owner/app", PullRequests: []PullRequest{{HeadRefName: "base", Number: 12, State: "MERGED"}}})
	if state := stackStateOf(merged.visibleWorktrees[2]); state != stackParentMerged {
		t.Fatalf("state after the parent PR merged = %v", state)
	}
	if row := merged.worktreeRow(merged.visibleWorktrees[2], false); !strings.Contains(row[2], "restack") {
		t.Fatalf("row after the parent PR merged = %v", row)
	}
	detail := ansi.Strip(merged.detail.View())
	if !strings.Contains(detail, "base was merged (#12)") || !strings.Contains(detail, "press B to restack") {
		t.Fatalf("details do not ask for a restack:\n%s", detail)
	}

	moved := stackWorktrees()[2]
	moved.Status.ParentBehind = 2
	if stackStateOf(moved) != stackParentMoved || stateLabel(moved) != "restack" {
		t.Fatalf("a parent with new commits gives state %v / %q", stackStateOf(moved), stateLabel(moved))
	}
	moved.Status.Dirty = true
	if stateLabel(moved) != "dirty" {
		t.Fatalf("uncommitted work must win over restack, got %q", stateLabel(moved))
	}
	if parent := selectWorktree(t, m, "base"); !strings.Contains(ansi.Strip(parent.detail.View()), "Stacked on this: child") {
		t.Fatalf("the parent's details do not list what is stacked on it:\n%s", ansi.Strip(parent.detail.View()))
	}
}

// Assures that B explains the restack first, runs it on enter with the merged branches it knows, and keeps the outcome on screen after the refresh.
func TestRestackConfirmsRunsAndKeepsOutcome(t *testing.T) {
	spy := &stackSpy{restackResult: core.RestackResult{Onto: "origin/main", Rebased: true, Commits: 2}}
	m := selectWorktree(t, stackTestModel(spy), "child")
	m, _ = feed(m, prIndexResult{Repository: "app", RepoSlug: "owner/app", PullRequests: []PullRequest{{HeadRefName: "base", Number: 12, State: "MERGED"}, {HeadRefName: "other", Number: 13, State: "OPEN"}}})

	m, _ = pressKeys(t, m, "B")
	if !m.confirmingRestack || m.restackTarget.Name != "child" || len(spy.restacks) != 0 {
		t.Fatalf("B did not ask first: confirming=%v restacks=%d", m.confirmingRestack, len(spy.restacks))
	}
	if modal := ansi.Strip(renderRestackModal(m)); !strings.Contains(modal, "base was merged (#12)") || !strings.Contains(modal, "default branch") {
		t.Fatalf("confirmation does not say what will happen:\n%s", modal)
	}

	m, msgs := pressKeys(t, m, "enter")
	if len(spy.restacks) != 1 || spy.restacks[0].OntoDefault || !reflect.DeepEqual(spy.restacks[0].MergedBranches, []string{"base"}) {
		t.Fatalf("restack options = %#v", spy.restacks)
	}
	m, msgs = feed(m, msgs...)
	m, _ = feed(m, msgs...)
	if m.statusError || !strings.Contains(m.statusMessage, "Restacked child onto origin/main (2 commits)") || !strings.Contains(m.statusMessage, "--force-with-lease") || !strings.Contains(m.statusMessage, "left its stack") {
		t.Fatalf("status after restack and refresh = %q (error=%v)", m.statusMessage, m.statusError)
	}
}

// Assures that d in the confirmation restacks onto the default branch, and esc does nothing.
func TestRestackOntoDefaultAndCancel(t *testing.T) {
	spy := &stackSpy{restackResult: core.RestackResult{Onto: "origin/main", Rebased: true, Commits: 1}}
	m := selectWorktree(t, stackTestModel(spy), "child")

	cancelled, _ := pressKeys(t, m, "B", "esc")
	if cancelled.confirmingRestack || len(spy.restacks) != 0 {
		t.Fatalf("esc: confirming=%v restacks=%d", cancelled.confirmingRestack, len(spy.restacks))
	}
	pressKeys(t, m, "B", "d")
	if len(spy.restacks) != 1 || !spy.restacks[0].OntoDefault {
		t.Fatalf("restack options after d = %#v", spy.restacks)
	}
}

// Assures that B on a worktree that is not stacked says so, and that a failed restack keeps its explanation in the status bar.
func TestRestackNeedsStackAndReportsFailure(t *testing.T) {
	spy := &stackSpy{restackErr: errors.New("stopped on conflicts: resolve them and run `git rebase --continue`")}
	m := selectWorktree(t, stackTestModel(spy), "other")
	m, _ = pressKeys(t, m, "B")
	if m.confirmingRestack || !m.statusError || !strings.Contains(m.statusMessage, "not stacked") {
		t.Fatalf("B on an unstacked worktree: confirming=%v status=%q", m.confirmingRestack, m.statusMessage)
	}

	m = selectWorktree(t, m, "child")
	m, msgs := pressKeys(t, m, "B", "enter")
	m, msgs = feed(m, msgs...)
	m, _ = feed(m, msgs...)
	if !m.statusError || !strings.Contains(m.statusMessage, "Restack failed for child") || !strings.Contains(m.statusMessage, "git rebase --continue") {
		t.Fatalf("status after a failed restack = %q (error=%v)", m.statusMessage, m.statusError)
	}
}

// Assures that an existing worktree can be put on a stack from the command palette, without offering itself or what is stacked on it, and taken out again.
func TestPaletteStacksAndUnstacksExistingWorktree(t *testing.T) {
	spy := &stackSpy{}
	m := selectWorktree(t, stackTestModel(spy), "base")
	for _, id := range []string{"stack-new", "stack-on", "sweep"} {
		if !paletteHas(m, id) {
			t.Fatalf("palette is missing %q for a worktree", id)
		}
	}
	if paletteHas(m, "restack") || paletteHas(m, "unstack") {
		t.Fatal("palette offers restack or unstack for a worktree that is not stacked")
	}

	next, _ := m.executePaletteEntry(paletteEntry{ID: "stack-on"})
	m = next.(model)
	if got, want := worktreeNames(m.stackPicker.Options), []string{"app", "other"}; !m.pickingStackParent || !reflect.DeepEqual(got, want) {
		t.Fatalf("parents offered for base = %v (picking=%v), want %v", got, m.pickingStackParent, want)
	}
	m, msgs := pressKeys(t, m, "j", "enter")
	if want := []string{"base on other"}; !reflect.DeepEqual(spy.parents, want) {
		t.Fatalf("stack links written = %v, want %v", spy.parents, want)
	}
	m, _ = feed(m, msgs...)
	if m.statusError || !strings.Contains(m.statusMessage, "base is now stacked on other") {
		t.Fatalf("status = %q (error=%v)", m.statusMessage, m.statusError)
	}

	m = selectWorktree(t, stackTestModel(spy), "child")
	if !paletteHas(m, "restack") || !paletteHas(m, "unstack") {
		t.Fatal("palette does not offer restack and unstack for a stacked worktree")
	}
	next, cmd := m.executePaletteEntry(paletteEntry{ID: "unstack"})
	runPromptCommands(cmd)
	if got := spy.parents[len(spy.parents)-1]; got != "child on " {
		t.Fatalf("unstack wrote %q, want the parent cleared", got)
	}
	if next.(model).pickingStackParent {
		t.Fatal("unstack opened the parent picker")
	}
}

func paletteHas(m model, id string) bool {
	for _, entry := range m.paletteEntries() {
		if entry.ID == id {
			return true
		}
	}
	return false
}

// Assures that in a project with several repositories N builds in the selected worktree's repository without asking for one.
func TestStackNewUsesParentRepository(t *testing.T) {
	m := multiRepoTestModel(newMultiRepoSpy())
	for idx, wt := range m.visibleWorktrees {
		if wt.Path == "/wt/web/feature" {
			m.table.SetCursor(idx)
		}
	}

	m, _ = pressKeys(t, m, "N")
	if m.pickingRepository || !m.creatingWorktree || m.newWorktreeRepository != "web" || m.newWorktreeParent.Path != "/wt/web/feature" {
		t.Fatalf("N in a multi-repository project: picking=%v creating=%v repository=%q parent=%q", m.pickingRepository, m.creatingWorktree, m.newWorktreeRepository, m.newWorktreeParent.Path)
	}
}

// Assures that the help page documents the stack and sweep keys.
func TestHelpListsStackAndSweepKeys(t *testing.T) {
	m := stackTestModel(&stackSpy{})
	m.showingHelp = true
	help := ansi.Strip(renderHelpPage(m))
	for key, text := range map[string]string{"N": "new, on top of selected", "B": "restack onto its base", "X": "sweep stale worktrees"} {
		if !helpPageLists(help, key, text) {
			t.Fatalf("help does not list %q as %q:\n%s", key, text, help)
		}
	}
	m.helpTab = modeTickets
	if help = ansi.Strip(renderHelpPage(m)); !helpPageLists(help, "N", "…on top of another worktree") {
		t.Fatalf("JIRA help does not list stacking a ticket worktree:\n%s", help)
	}
}

// loadFromGit lists the worktrees with the real services and reads every
// status, the way a refresh does, without waiting on timers.
func loadFromGit(t *testing.T, m model, svc *services.Service) model {
	t.Helper()
	msg := loadWorktreesCmd(svc, m.cfg, m.showAll)()
	next, _ := m.Update(msg)
	m = next.(model)
	for _, wt := range append([]Worktree(nil), m.allWorktrees...) {
		loaded, err := svc.LoadWorktreeStatus(context.Background(), m.cfg, wt)
		if err != nil {
			t.Fatalf("LoadWorktreeStatus(%s) error = %v", wt.Name, err)
		}
		next, _ = m.Update(worktreeStatusResult{RepositoryPath: m.cfg.ActiveProfile.RepositoryPath, Seq: m.worktreeLoadSeq, Worktree: loaded})
		m = next.(model)
	}
	return m
}

func rowFor(t *testing.T, m model, name string) (Worktree, []string) {
	t.Helper()
	for _, wt := range m.visibleWorktrees {
		if wt.Name == name {
			return wt, m.worktreeRow(wt, false)
		}
	}
	t.Fatalf("no row for %q in %v", name, worktreeNames(m.visibleWorktrees))
	return Worktree{}, nil
}

// Assures, against a real repository, that a stacked worktree is listed under its parent, asks for a restack once the parent gains a commit, and is clean again after restacking.
func TestStackLifecycleAgainstGit(t *testing.T) {
	cfg := testkit.GitRepo(t)
	ctx := context.Background()
	svc := app.NewServices(nil)
	parentPath := core.CreateWorktreePath(cfg, "parent")
	if _, err := svc.CreateWorktree(ctx, cfg, "parent", parentPath); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	childPath := core.CreateWorktreePath(cfg, "child")
	if err := svc.CreateStackedWorktree(ctx, cfg, "child", childPath, "parent"); err != nil {
		t.Fatalf("CreateStackedWorktree() error = %v", err)
	}
	testkit.WriteRepoFile(t, childPath, "child.txt", "child\n")
	testkit.RunGit(t, childPath, "add", ".")
	testkit.RunGit(t, childPath, "commit", "-m", "child work")

	m := NewModel(cfg, svc)
	m.ready = true
	m.loading = false
	m.width, m.height = 160, 34
	m.resize()
	m = loadFromGit(t, m, svc)

	if got, want := worktreeNames(m.visibleWorktrees), []string{"test", "parent", "child"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	child, row := rowFor(t, m, "child")
	if row[0] != icons.StackChild+" child" || stateLabel(child) != "clean" {
		t.Fatalf("fresh stacked row = %v (state %q)", row, stateLabel(child))
	}

	testkit.WriteRepoFile(t, parentPath, "parent.txt", "parent\n")
	testkit.RunGit(t, parentPath, "add", ".")
	testkit.RunGit(t, parentPath, "commit", "-m", "parent work")
	m = loadFromGit(t, m, svc)
	child, _ = rowFor(t, m, "child")
	if stateLabel(child) != "restack" || child.Status.ParentBehind != 1 {
		t.Fatalf("after the parent moved: state %q, behind %d", stateLabel(child), child.Status.ParentBehind)
	}

	m = selectWorktree(t, m, "child")
	next, _ := m.Update(teaKey("B"))
	m = next.(model)
	if !m.confirmingRestack || !strings.Contains(ansi.Strip(renderRestackModal(m)), "parent has 1 new commit") {
		t.Fatalf("restack confirmation:\n%s", ansi.Strip(renderRestackModal(m)))
	}
	result, err := svc.RestackWorktree(ctx, cfg, child, core.RestackOptions{})
	next, _ = m.Update(restackResult{Worktree: child, Result: result, Err: err})
	m = next.(model)
	if err != nil || m.statusError || !strings.Contains(m.statusMessage, "Restacked child onto parent (1 commit)") {
		t.Fatalf("restack: err=%v status=%q", err, m.statusMessage)
	}

	m = loadFromGit(t, m, svc)
	child, _ = rowFor(t, m, "child")
	if stateLabel(child) != "clean" || child.Status.ParentBehind != 0 {
		t.Fatalf("after the restack: state %q, behind %d", stateLabel(child), child.Status.ParentBehind)
	}
	if !strings.Contains(m.statusMessage, "Restacked child onto parent") {
		t.Fatalf("the outcome did not survive the refresh: %q", m.statusMessage)
	}
	for _, name := range []string{"parent.txt", "child.txt"} {
		if _, err := os.Stat(filepath.Join(childPath, name)); err != nil {
			t.Fatalf("%s is missing in the child after the restack: %v", name, err)
		}
	}
}

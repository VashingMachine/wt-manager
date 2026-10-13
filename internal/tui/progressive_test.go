package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type progressiveSpy struct {
	core.Services
	mu            sync.Mutex
	calls         []string
	peekWorktrees []Worktree
	peekPRs       []RemotePullRequest
	peekPRsAt     time.Time
	peekDetail    *RemotePullRequest
	peekDetailAt  time.Time
	peekTickets   []Ticket
	prIndex       []PullRequest
	mutationErr   error
}

func newProgressiveSpy() *progressiveSpy {
	return &progressiveSpy{Services: services.NewService()}
}

func (s *progressiveSpy) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *progressiveSpy) called(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, call := range s.calls {
		if strings.HasPrefix(call, prefix) {
			count++
		}
	}
	return count
}

func (s *progressiveSpy) PeekWorktrees(Config, bool) ([]Worktree, bool) {
	return s.peekWorktrees, len(s.peekWorktrees) > 0
}

func (s *progressiveSpy) DiscoverWorktrees(context.Context, Config, bool) ([]Worktree, error) {
	s.record("discover")
	return s.peekWorktrees, nil
}

func (s *progressiveSpy) LoadWorktreeStatus(_ context.Context, _ Config, wt Worktree) (Worktree, error) {
	s.record("status:" + wt.Path)
	wt.StatusPending = false
	wt.Status = RepoStatus{Dirty: true, Unstaged: 2}
	wt.Commit = CommitInfo{Hash: "abc", Subject: "fresh commit"}
	return wt, nil
}

func (s *progressiveSpy) LoadPullRequestIndex(_ context.Context, _ Config, refresh bool) ([]PullRequest, string, error) {
	if refresh {
		s.record("pr-index:refresh")
	} else {
		s.record("pr-index")
	}
	return s.prIndex, "", nil
}

func (s *progressiveSpy) PeekRemotePullRequests(Config) ([]RemotePullRequest, time.Time, bool) {
	return s.peekPRs, s.peekPRsAt, len(s.peekPRs) > 0
}

func (s *progressiveSpy) PeekRemotePullRequestDetail(Config, int) (RemotePullRequest, time.Time, bool) {
	if s.peekDetail == nil {
		return RemotePullRequest{}, time.Time{}, false
	}
	return *s.peekDetail, s.peekDetailAt, true
}

func (s *progressiveSpy) PeekTicketCache(Config, JiraBoard, bool, bool) ([]Ticket, time.Time, bool) {
	return s.peekTickets, time.Now(), len(s.peekTickets) > 0
}

func (s *progressiveSpy) TransitionTicket(context.Context, string, string) error {
	s.record("transition")
	return s.mutationErr
}

func (s *progressiveSpy) ApproveRemotePullRequest(context.Context, Config, int) error {
	s.record("approve")
	return s.mutationErr
}

func progressiveTestModel(spy *progressiveSpy) model {
	m := NewModel(Config{
		RepoSlug: "owner/app",
		ActiveProfile: RepositoryProfile{
			Name:           "app",
			RepositoryPath: "/repo/app",
			WorktreesDir:   "/repo",
			RemoteName:     "origin",
			Jira:           &JiraConfig{Site: "example.atlassian.net", Boards: []JiraBoard{{ID: 7, Name: "Board"}}},
		},
	}, spy)
	m.ready = true
	m.loading = false
	m.width, m.height = 140, 30
	m.resize()
	return m
}

// runCommands executes a command tree and returns the messages it produces.
// Spinner ticks are dropped so assertions see only data messages.
func runCommands(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, inner := range batch {
			out = append(out, runCommands(inner)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	if _, tick := msg.(spinner.TickMsg); tick {
		return nil
	}
	return []tea.Msg{msg}
}

func messagesOf[T any](msgs []tea.Msg) []T {
	var out []T
	for _, msg := range msgs {
		if typed, ok := msg.(T); ok {
			out = append(out, typed)
		}
	}
	return out
}

// Assures that the first frame shows worktrees from the persistent cache instead of an empty table.
func TestStartupPaintsCachedWorktrees(t *testing.T) {
	spy := newProgressiveSpy()
	spy.peekWorktrees = []Worktree{
		{Name: "app", Path: "/repo/app", Branch: "main", IsMain: true, Managed: true},
		{Name: "feature", Path: "/repo/feature", Branch: "feature", Managed: true, Status: RepoStatus{Dirty: true, Unstaged: 1}},
	}
	m := progressiveTestModel(spy)
	if len(m.visibleWorktrees) != 2 {
		t.Fatalf("visible worktrees = %d, want cached rows painted at startup", len(m.visibleWorktrees))
	}
	if view := ansi.Strip(m.tableView()); !strings.Contains(view, "feature") || !strings.Contains(view, "dirty") {
		t.Fatalf("cached rows not rendered: %q", view)
	}
}

// Assures that worktree discovery repaints rows at once and then requests each status and the PR labels separately.
func TestWorktreeDiscoveryStreamsStatusesAndPRLabels(t *testing.T) {
	spy := newProgressiveSpy()
	spy.prIndex = []PullRequest{{Number: 7, HeadRefName: "feature", State: "OPEN"}}
	m := progressiveTestModel(spy)
	discovered := []Worktree{
		{Name: "app", Path: "/repo/app", Branch: "main", IsMain: true, Managed: true, StatusPending: true},
		{Name: "feature", Path: "/repo/feature", Branch: "feature", Managed: true, StatusPending: true},
		{Name: "gone", Path: "/repo/gone", Branch: "gone", Managed: true, Missing: true},
	}

	next, cmd := m.Update(loadResult{Worktrees: discovered, RepositoryPath: "/repo/app"})
	m = next.(model)
	if len(m.visibleWorktrees) != 3 {
		t.Fatalf("rows = %d, want discovered rows before statuses arrive", len(m.visibleWorktrees))
	}
	if view := ansi.Strip(m.tableView()); strings.Contains(view, "clean") || stateLabel(m.visibleWorktrees[1]) != "loading" {
		t.Fatalf("pending statuses must render as loading, not clean: %q", view)
	}
	if !m.isBusy() || !strings.Contains(ansi.Strip(m.statusBarView()), "status 0/2") {
		t.Fatalf("status bar does not show progress: %q", ansi.Strip(m.statusBarView()))
	}

	msgs := runCommands(cmd)
	statuses := messagesOf[worktreeStatusResult](msgs)
	if len(statuses) != 2 || spy.called("status:/repo/gone") != 0 {
		t.Fatalf("status loads = %d (missing loaded: %d), want one per present worktree", len(statuses), spy.called("status:/repo/gone"))
	}
	if len(messagesOf[prIndexResult](msgs)) != 1 {
		t.Fatal("PR labels were not requested alongside statuses")
	}

	m.table.SetCursor(1)
	m.rememberSelection()
	for _, msg := range msgs {
		next, _ = m.Update(msg)
		m = next.(model)
	}
	if m.selectedWorktreePath() != "/repo/feature" {
		t.Fatalf("streamed statuses moved the selection to %q", m.selectedWorktreePath())
	}
	feature := m.visibleWorktrees[1]
	if feature.StatusPending || !feature.Status.Dirty || feature.PR == nil || feature.PR.Number != 7 {
		t.Fatalf("feature row after streaming = %#v", feature)
	}
	if m.backgroundBusy() {
		t.Fatal("still busy after every status and the PR labels arrived")
	}
}

// Assures that pressing r forces fresh PR labels while ordinary reloads reuse cached ones.
func TestWorktreeRefreshForcesPRLabelRefetch(t *testing.T) {
	spy := newProgressiveSpy()
	m := progressiveTestModel(spy)
	discovered := []Worktree{{Name: "app", Path: "/repo/app", IsMain: true, Managed: true}}

	_, cmd := m.Update(loadResult{Worktrees: discovered, RepositoryPath: "/repo/app"})
	runCommands(cmd)
	if spy.called("pr-index:refresh") != 0 || spy.called("pr-index") != 1 {
		t.Fatalf("ordinary reload calls = %v, want cached PR labels", spy.calls)
	}

	next, _ := m.Update(teaKey("r"))
	_, cmd = next.(model).Update(loadResult{Worktrees: discovered, RepositoryPath: "/repo/app"})
	runCommands(cmd)
	if spy.called("pr-index:refresh") != 1 {
		t.Fatalf("refresh calls = %v, want a forced PR label refetch", spy.calls)
	}
}

// Assures that results for another repository or a superseded load cannot change the current rows.
func TestStreamedResultsFromOtherRepositoryAreIgnored(t *testing.T) {
	m := progressiveTestModel(newProgressiveSpy())
	m.allWorktrees = []Worktree{{Name: "feature", Path: "/repo/feature", Branch: "feature", StatusPending: true}}
	m.applyFilter()

	next, _ := m.Update(worktreeStatusResult{RepositoryPath: "/other", Worktree: Worktree{Path: "/repo/feature", Status: RepoStatus{Dirty: true}}})
	next, _ = next.(model).Update(prIndexResult{RepoSlug: "other/repo", PullRequests: []PullRequest{{Number: 1, HeadRefName: "feature"}}})
	got := next.(model)
	if !got.visibleWorktrees[0].StatusPending || got.visibleWorktrees[0].PR != nil {
		t.Fatalf("foreign results changed the row: %#v", got.visibleWorktrees[0])
	}
}

// Assures that PR Radar shows prefetched PRs immediately, without the blocking loading state, and only revalidates stale lists.
func TestPRModeUsesPrefetchedPRs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fetchedAt   time.Time
		revalidates bool
	}{
		{name: "fresh", fetchedAt: time.Now(), revalidates: false},
		{name: "stale", fetchedAt: time.Now().Add(-time.Hour), revalidates: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := progressiveTestModel(newProgressiveSpy())
			m.remotePRs = []RemotePullRequest{remotePRFixture(5, "OPEN", "alice", "Prefetched", "feature")}
			m.prListSlug, m.prListFetchedAt = "owner/app", tc.fetchedAt

			next, _ := m.Update(teaKey("P"))
			got := next.(model)
			if !got.isPRMode() || got.loading || len(got.visiblePRs) != 1 {
				t.Fatalf("PR mode = %v loading=%v rows=%d, want prefetched rows without blocking", got.isPRMode(), got.loading, len(got.visiblePRs))
			}
			if got.prListLoading != tc.revalidates {
				t.Fatalf("background revalidation = %v, want %v", got.prListLoading, tc.revalidates)
			}
		})
	}
}

// Assures that PR Radar falls back to the persistent cache when nothing was prefetched in this session.
func TestPRModePaintsCachedPRs(t *testing.T) {
	spy := newProgressiveSpy()
	spy.peekPRs = []RemotePullRequest{remotePRFixture(9, "OPEN", "bob", "Cached", "cached")}
	spy.peekPRsAt = time.Now().Add(-time.Hour)
	m := progressiveTestModel(spy)
	m.remotePRs = nil
	next, _ := m.Update(teaKey("P"))
	got := next.(model)
	if got.loading || len(got.visiblePRs) != 1 || !got.prListLoading {
		t.Fatalf("loading=%v rows=%d revalidating=%v, want cached rows with background refresh", got.loading, len(got.visiblePRs), got.prListLoading)
	}
}

// Assures that a background PR prefetch stores PRs for later without touching the worktree view.
func TestBackgroundPRPrefetchKeepsWorktreeView(t *testing.T) {
	m := progressiveTestModel(newProgressiveSpy())
	m.allWorktrees = []Worktree{{Name: "app", Path: "/repo/app"}}
	m.applyFilter()
	m.statusMessage = "Loaded 1 worktrees"

	next, _ := m.Update(remotePRListResult{RepoSlug: "owner/app", PullRequests: []RemotePullRequest{remotePRFixture(5, "OPEN", "alice", "Later", "later")}, FetchedAt: time.Now(), Background: true})
	got := next.(model)
	if len(got.remotePRs) != 1 || got.prListFetchedAt.IsZero() {
		t.Fatal("prefetched PRs were not kept")
	}
	if !got.isWorktreeMode() || got.statusMessage != "Loaded 1 worktrees" || len(got.visibleWorktrees) != 1 || len(got.visiblePRs) != 0 {
		t.Fatalf("prefetch disturbed the worktree view: mode=%v status=%q", got.mode, got.statusMessage)
	}
}

// Assures that a background ticket prefetch does not replace the enriched worktree rows.
func TestBackgroundTicketPrefetchKeepsWorktreeRows(t *testing.T) {
	m := progressiveTestModel(newProgressiveSpy())
	m.allWorktrees = []Worktree{{Name: "app", Path: "/repo/app", Status: RepoStatus{Dirty: true}}}
	m.applyFilter()

	next, _ := m.Update(ticketListResult{Source: m.ticketSource(), Tickets: []Ticket{{Key: "APP-1"}}, Worktrees: []Worktree{{Name: "other", Path: "/repo/other"}}, FetchedAt: time.Now(), Background: true})
	got := next.(model)
	if len(got.tickets) != 1 || got.ticketsSource != got.ticketSource() {
		t.Fatal("prefetched tickets were not kept")
	}
	if len(got.allWorktrees) != 1 || got.allWorktrees[0].Path != "/repo/app" || !got.allWorktrees[0].Status.Dirty {
		t.Fatalf("ticket prefetch replaced worktree rows: %#v", got.allWorktrees)
	}
}

// Assures that tickets prefetched for the current board paint immediately on entering the mode and revalidate quietly.
func TestTicketModeUsesPrefetchedTickets(t *testing.T) {
	m := progressiveTestModel(newProgressiveSpy())
	m.tickets = []Ticket{{Key: "APP-1", Summary: "Prefetched", StatusCategory: core.StatusCategoryNew}}
	m.ticketsSource = m.ticketSource()

	next, _ := m.Update(teaKey("J"))
	got := next.(model)
	if !got.isTicketMode() || got.loading || len(got.visibleTickets) != 1 || !got.ticketsLoading {
		t.Fatalf("ticket mode loading=%v rows=%d revalidating=%v, want prefetched rows", got.loading, len(got.visibleTickets), got.ticketsLoading)
	}
}

// Assures that the prefetch timer loads only the modes the user is not looking at, and stops for stale timers.
func TestPrefetchTickLoadsInactiveModes(t *testing.T) {
	m := progressiveTestModel(newProgressiveSpy())
	next, cmd := m.Update(prefetchTick{Seq: m.prefetchSeq})
	got := next.(model)
	if cmd == nil || !got.prListLoading || !got.ticketsLoading {
		t.Fatalf("worktree mode prefetch: PRs=%v tickets=%v, want both inactive modes", got.prListLoading, got.ticketsLoading)
	}

	m = progressiveTestModel(newProgressiveSpy())
	m.mode = modePRs
	next, _ = m.Update(prefetchTick{Seq: m.prefetchSeq})
	if got := next.(model); got.prListLoading || !got.ticketsLoading {
		t.Fatalf("PR mode prefetch: PRs=%v tickets=%v, want only tickets", got.prListLoading, got.ticketsLoading)
	}

	m = progressiveTestModel(newProgressiveSpy())
	if _, cmd := m.Update(prefetchTick{Seq: m.prefetchSeq + 1}); cmd != nil {
		t.Fatal("a superseded prefetch timer kept running")
	}
}

// Assures that check runs fill PR rows as they arrive, and PRs are not reported ready while their checks are unknown.
func TestPRChecksStreamIntoRows(t *testing.T) {
	m := progressiveTestModel(newProgressiveSpy())
	m.mode = modePRs
	m.table.SetColumns(m.activeColumns())
	pr := remotePRFixture(5, "OPEN", "alice", "Checks", "checks")
	pr.HeadSHA = "sha5"

	next, cmd := m.Update(remotePRListResult{RepoSlug: "owner/app", PullRequests: []RemotePullRequest{pr}, FetchedAt: time.Now()})
	m = next.(model)
	if cmd == nil || !m.prChecksLoading["sha5"] {
		t.Fatal("check runs were not requested after the list loaded")
	}
	if got := reviewReadiness(m.visiblePRs[0]); got != "checking" {
		t.Fatalf("readiness while checks load = %q, want checking", got)
	}

	next, _ = m.Update(prChecksResult{RepoSlug: "owner/app", SHA: "sha5", Checks: []StatusCheck{{Name: "ci", Status: "completed", Conclusion: "failure"}}})
	m = next.(model)
	if len(m.prChecksLoading) != 0 || reviewReadiness(m.visiblePRs[0]) != "blocked" {
		t.Fatalf("checks did not land: pending=%v readiness=%q", m.prChecksLoading, reviewReadiness(m.visiblePRs[0]))
	}
}

// Assures that a cached PR detail is shown at once and is not refetched while the PR is unchanged.
func TestCachedPRDetailSkipsRefetchWhenUnchanged(t *testing.T) {
	spy := newProgressiveSpy()
	listed := remotePRFixture(5, "OPEN", "alice", "Detail", "detail")
	cached := listed
	cached.Body = "cached body"
	spy.peekDetail, spy.peekDetailAt = &cached, time.Now()
	m := progressiveTestModel(spy)
	m.mode = modePRs
	m.table.SetColumns(m.activeColumns())
	m.remotePRs = []RemotePullRequest{listed}
	m.applyPRFilter()

	next, cmd := m.startPRDetailLoad(5)
	got := next.(model)
	if cmd != nil || got.prDetailLoading || got.prDetail == nil || got.prDetail.Body != "cached body" {
		t.Fatalf("unchanged PR: cmd=%v loading=%v detail=%v, want cached detail without refetch", cmd != nil, got.prDetailLoading, got.prDetail)
	}

	m.remotePRs[0].UpdatedAt = "2026-10-12T00:00:00Z"
	next, cmd = m.startPRDetailLoad(5)
	got = next.(model)
	if cmd == nil || !got.prDetailLoading || got.prDetail == nil || got.prDetail.Body != "cached body" {
		t.Fatal("changed PR must refetch while still showing the cached detail")
	}
}

func ticketTestModel(spy *progressiveSpy) model {
	m := progressiveTestModel(spy)
	m.mode = modeTickets
	m.ticketOpenOnly = false
	m.ticketBoard = JiraBoard{ID: 7, Name: "Board"}
	m.tickets = []Ticket{{Key: "APP-1", Status: "To Do", StatusCategory: core.StatusCategoryNew, Comments: []TicketComment{{Author: "Ann", Body: "first"}}}}
	m.table.SetColumns(m.activeColumns())
	m.applyTicketFilter()
	m.table.SetCursor(0)
	m.rememberTicketSelection()
	return m
}

// Assures that a status change shows immediately and is confirmed through the queue without a blocking spinner.
func TestTicketTransitionIsOptimistic(t *testing.T) {
	spy := newProgressiveSpy()
	m := ticketTestModel(spy)
	next, cmd := m.submitTicketStatus("In Progress")
	got := next.(model)
	ticket, _ := got.ticketByKey("APP-1")
	if ticket.Status != "In Progress" || ticket.StatusCategory != core.StatusCategoryInProgress || got.loading {
		t.Fatalf("optimistic transition = %#v loading=%v", ticket, got.loading)
	}

	results := messagesOf[ticketMutationResult](runCommands(cmd))
	if len(results) != 1 || spy.called("transition") != 1 {
		t.Fatalf("mutation results=%d transition calls=%d", len(results), spy.called("transition"))
	}
	next, _ = got.Update(results[0])
	got = next.(model)
	ticket, _ = got.ticketByKey("APP-1")
	if got.statusError || ticket.Status != "In Progress" || got.ticketMutationsInFlight() != 0 || !got.ticketsLoading {
		t.Fatalf("confirmed transition: error=%v status=%q in flight=%d revalidating=%v", got.statusError, ticket.Status, got.ticketMutationsInFlight(), got.ticketsLoading)
	}
}

// Assures that a failed status change is rolled back and reported.
func TestTicketTransitionRollsBackOnFailure(t *testing.T) {
	spy := newProgressiveSpy()
	spy.mutationErr = errors.New("transition not allowed")
	m := ticketTestModel(spy)
	next, cmd := m.submitTicketStatus("Done")
	got := next.(model)
	for _, msg := range runCommands(cmd) {
		next, _ = got.Update(msg)
		got = next.(model)
	}
	ticket, _ := got.ticketByKey("APP-1")
	if ticket.Status != "To Do" || ticket.StatusCategory != core.StatusCategoryNew || !got.statusError || !strings.Contains(got.statusMessage, "transition not allowed") {
		t.Fatalf("after failure ticket=%#v error=%v message=%q", ticket, got.statusError, got.statusMessage)
	}
}

// Assures that a new comment and a reassignment show immediately, and a comment survives a detail response fetched before it.
func TestTicketCommentAndAssignAreOptimistic(t *testing.T) {
	m := ticketTestModel(newProgressiveSpy())
	m.commentingTicket = true
	m.ticketCommentInput.SetValue("looks good")
	next, _ := m.Update(teaKey("enter"))
	got := next.(model)
	ticket, _ := got.ticketByKey("APP-1")
	if len(ticket.Comments) != 2 || ticket.Comments[1].Body != "looks good" {
		t.Fatalf("comment not shown immediately: %#v", ticket.Comments)
	}

	got.ticketDetailRequest = 3
	next, _ = got.Update(ticketDetailResult{Key: "APP-1", RequestID: 3, Ticket: Ticket{Key: "APP-1", Detail: true, Comments: []TicketComment{{Author: "Ann", Body: "first"}}}})
	got = next.(model)
	if detail := got.selectedTicketForDetail(); detail == nil || len(detail.Comments) != 2 {
		t.Fatalf("stale detail response dropped the pending comment: %#v", detail)
	}

	next, _ = got.submitTicketAssignee(core.TicketAssigneeSelf)
	got = next.(model)
	ticket, _ = got.ticketByKey("APP-1")
	if ticket.Assignee.Name() != "@me" || got.ticketMutationsInFlight() != 2 {
		t.Fatalf("assignment not shown immediately: %#v in flight=%d", ticket.Assignee, got.ticketMutationsInFlight())
	}
}

// Assures that approving a PR shows approved immediately and reverts the review state when GitHub rejects it.
func TestPRApprovalIsOptimisticAndRollsBack(t *testing.T) {
	spy := newProgressiveSpy()
	spy.mutationErr = errors.New("cannot approve your own PR")
	m := progressiveTestModel(spy)
	m.mode = modePRs
	m.remotePRs = []RemotePullRequest{remotePRFixture(5, "OPEN", "alice", "Approve", "approve")}
	m.table.SetColumns(m.activeColumns())
	m.applyPRFilter()

	next, cmd := m.startPRApproval(5)
	got := next.(model)
	if got.visiblePRs[0].ReviewDecision != "APPROVED" || got.loading {
		t.Fatalf("approval not shown immediately: decision=%q loading=%v", got.visiblePRs[0].ReviewDecision, got.loading)
	}
	for _, msg := range runCommands(cmd) {
		if _, ok := msg.(approvePRResult); ok {
			next, _ = got.Update(msg)
			got = next.(model)
		}
	}
	if got.visiblePRs[0].ReviewDecision != "" || !got.statusError || len(got.prApprovals) != 0 {
		t.Fatalf("failed approval: decision=%q error=%v pending=%d", got.visiblePRs[0].ReviewDecision, got.statusError, len(got.prApprovals))
	}
}

// Assures that queued operations on one key run one at a time in the order they were enqueued, while other keys run freely.
func TestOpQueueSerializesPerKeyInIssueOrder(t *testing.T) {
	queue := &opQueue{}
	var mu sync.Mutex
	var order []string
	releaseFirst := make(chan struct{})
	step := func(name string, wait chan struct{}) tea.Cmd {
		return func() tea.Msg {
			if wait != nil {
				<-wait
			}
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}
	first := queue.enqueue("git:/repo", step("first", releaseFirst))
	second := queue.enqueue("git:/repo", step("second", nil))
	other := queue.enqueue("jira:APP-1", step("other", nil))

	done := make(chan struct{}, 3)
	go func() { second(); done <- struct{}{} }()
	go func() { other(); done <- struct{}{} }()
	<-done // the independent key finishes without waiting
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	if len(order) != 1 || order[0] != "other" {
		mu.Unlock()
		t.Fatalf("order before release = %v, want the second git operation to wait", order)
	}
	mu.Unlock()
	go func() { first(); done <- struct{}{} }()
	close(releaseFirst)
	<-done
	<-done
	if strings.Join(order, ",") != "other,first,second" {
		t.Fatalf("order = %v, want other,first,second", order)
	}
}

// Assures that two deletions in one repository reach git one after another instead of racing for its locks.
func TestWorktreeDeletionsAreQueued(t *testing.T) {
	spy := &deleteQueueSpy{progressiveSpy: newProgressiveSpy(), release: make(chan struct{}), started: make(chan string, 2)}
	m := progressiveTestModel(spy.progressiveSpy)
	m.services = spy
	m.allWorktrees = []Worktree{{Name: "app", Path: "/repo/app", IsMain: true}, {Name: "a", Path: "/repo/a"}, {Name: "b", Path: "/repo/b"}}
	m.applyFilter()

	m.table.SetCursor(1)
	next, _ := m.Update(teaKey("d"))
	next, firstCmd := next.(model).Update(teaKey("d"))
	m = next.(model)
	m.table.SetCursor(1)
	next, _ = m.Update(teaKey("d"))
	_, secondCmd := next.(model).Update(teaKey("d"))

	go runCommands(secondCmd)
	go runCommands(firstCmd)
	if got := <-spy.started; got != "/repo/a" {
		t.Fatalf("first deletion to reach git = %q, want /repo/a", got)
	}
	select {
	case got := <-spy.started:
		t.Fatalf("%s started while the first deletion was still running", got)
	case <-time.After(50 * time.Millisecond):
	}
	close(spy.release)
	if got := <-spy.started; got != "/repo/b" {
		t.Fatalf("second deletion = %q, want /repo/b", got)
	}
}

type deleteQueueSpy struct {
	*progressiveSpy
	release chan struct{}
	started chan string
}

func (s *deleteQueueSpy) RemoveWorktree(_ context.Context, _ Config, wt Worktree) error {
	s.started <- wt.Path
	<-s.release
	return nil
}

// Assures that switching profiles drops the previous repository's PRs and ignores its late results.
func TestProfileSwitchDropsPreviousProfileData(t *testing.T) {
	m := progressiveTestModel(newProgressiveSpy())
	m.cfg.App.Profiles = []RepositoryProfile{m.cfg.ActiveProfile, {Name: "other", RepositoryPath: "/repo/other", WorktreesDir: "/repo", GitHubRepo: "owner/other"}}
	m.remotePRs = []RemotePullRequest{remotePRFixture(5, "OPEN", "alice", "Old", "old")}
	m.prListSlug = "owner/app"
	m.selectingProfile = true
	m.profileCursor = 1

	next, _ := m.Update(teaKey("enter"))
	got := next.(model)
	if got.cfg.RepoSlug != "owner/other" || len(got.remotePRs) != 0 {
		t.Fatalf("profile switch kept PRs: slug=%q prs=%d", got.cfg.RepoSlug, len(got.remotePRs))
	}
	next, _ = got.Update(remotePRListResult{RepoSlug: "owner/app", PullRequests: m.remotePRs, Background: true})
	if len(next.(model).remotePRs) != 0 {
		t.Fatal("late result from the previous profile was applied")
	}
}

// Assures that the command palette lists cached worktrees immediately and refreshes them with discovery only.
func TestPaletteShowsCachedWorktreesImmediately(t *testing.T) {
	spy := newProgressiveSpy()
	spy.peekWorktrees = []Worktree{{Name: "cached", Path: "/repo/cached", Managed: true}}
	m := progressiveTestModel(spy)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	got := next.(model)
	if len(got.paletteWorktrees) != 1 {
		t.Fatal("palette did not show cached worktrees before loading")
	}
	runCommands(cmd)
	if spy.called("discover") != 1 || spy.called("status:") != 0 || spy.called("pr-index") != 0 {
		t.Fatalf("palette load calls = %v, want discovery only", spy.calls)
	}
}

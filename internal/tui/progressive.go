package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// Every list paints from what is already known (model state, else the cache)
// and is then revalidated in the background, one piece at a time:
//
//   - Worktrees: `git worktree list` replaces the rows, then each worktree's
//     status arrives on its own, and the PR labels arrive separately.
//   - PR Radar: one request lists the PRs; check runs fill rows in per commit.
//   - Tickets: cached rows paint at once and the board revalidates quietly.
//
// Modes the user is not looking at are prefetched on a timer, so switching to
// them shows current data instead of a spinner.

const (
	prefetchDelay    = 2 * time.Second
	prefetchInterval = 4 * time.Minute
	// prListTTL is how old the PR Radar list may be before entering the mode
	// revalidates it; prefetching usually keeps it younger.
	prListTTL = time.Minute
	// prDetailTTL bounds how long an unchanged PR's cached detail is shown
	// without refetching it on selection.
	prDetailTTL = 10 * time.Minute
	// prChecksLimit matches how many PRs had checks fetched before checks
	// became progressive.
	prChecksLimit = 40
)

type worktreeStatusResult struct {
	RepositoryPath string
	Seq            int
	Worktree       Worktree
	Err            error
}

type prIndexResult struct {
	// Repository names the project repository the PRs label; empty means the
	// first one.
	Repository   string
	RepoSlug     string
	PullRequests []PullRequest
	Warning      string
	Err          error
}

type prChecksResult struct {
	RepoSlug string
	SHA      string
	Checks   []StatusCheck
	Err      error
}

type prefetchTick struct {
	Seq int
}

// opQueue runs mutations of one thing (a repository's git state, a JIRA ticket,
// a PR review) one at a time, in the order the user issued them, while the UI
// already shows their optimistic result. The order is fixed when Update
// enqueues the command, not when Bubble Tea happens to start its goroutine.
type opQueue struct {
	mu    sync.Mutex
	tails map[string]chan struct{}
}

func (q *opQueue) enqueue(key string, cmd tea.Cmd) tea.Cmd {
	if q == nil || cmd == nil {
		return cmd
	}
	q.mu.Lock()
	if q.tails == nil {
		q.tails = map[string]chan struct{}{}
	}
	previous := q.tails[key]
	done := make(chan struct{})
	q.tails[key] = done
	q.mu.Unlock()

	return func() tea.Msg {
		if previous != nil {
			<-previous
		}
		defer func() {
			q.mu.Lock()
			if q.tails[key] == done {
				delete(q.tails, key)
			}
			q.mu.Unlock()
			close(done)
		}()
		return cmd()
	}
}

func gitQueueKey(cfg Config) string {
	return "git:" + cfg.ActiveProfile.RepositoryPath
}

func loadWorktreeStatusCmd(services core.WorktreeService, cfg Config, wt Worktree, seq int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		loaded, err := services.LoadWorktreeStatus(ctx, cfg, wt)
		return worktreeStatusResult{RepositoryPath: cfg.ActiveProfile.RepositoryPath, Seq: seq, Worktree: loaded, Err: err}
	}
}

func loadPRIndexCmd(services core.PullRequestService, cfg Config, refresh bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		prs, warning, err := services.LoadPullRequestIndex(ctx, cfg, refresh)
		return prIndexResult{Repository: cfg.RepositoryName(), RepoSlug: cfg.RepoSlug, PullRequests: prs, Warning: warning, Err: err}
	}
}

// loadPRIndexesCmd requests the PR labels of every repository with GitHub.
// Background revalidation skips repositories whose labels are still loading.
func (m *model) loadPRIndexesCmd(refresh bool, background bool) tea.Cmd {
	if m.prIndexLoading == nil {
		m.prIndexLoading = map[string]bool{}
	}
	var cmds []tea.Cmd
	for _, repository := range m.cfg.Repositories() {
		if repository.GitHubRepo == "" || (background && m.prIndexLoading[repository.Name]) {
			continue
		}
		m.prIndexLoading[repository.Name] = true
		cmds = append(cmds, loadPRIndexCmd(m.services, m.cfg.ForRepository(repository.Name), refresh))
	}
	return tea.Batch(cmds...)
}

func (m model) prIndexBusy() bool {
	return len(m.prIndexLoading) > 0
}

// allPRIndexes joins every repository's PR labels, for matching tickets.
func (m model) allPRIndexes() []PullRequest {
	var prs []PullRequest
	for _, repository := range m.cfg.Repositories() {
		prs = append(prs, m.prIndexes[repository.Name]...)
	}
	return prs
}

func loadPRChecksCmd(services core.PullRequestService, cfg Config, sha string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		checks, err := services.LoadRemotePullRequestChecks(ctx, cfg, sha)
		return prChecksResult{RepoSlug: cfg.RepoSlug, SHA: sha, Checks: checks, Err: err}
	}
}

func prefetchTickCmd(seq int, after time.Duration) tea.Cmd {
	return tea.Tick(after, func(time.Time) tea.Msg { return prefetchTick{Seq: seq} })
}

// hydrateFromCache fills every mode from the persistent cache so the first
// frame after start or a profile switch shows the last known state.
func (m *model) hydrateFromCache() {
	if m.cfg.ActiveProfile.RepositoryPath == "" {
		return
	}
	if worktrees, ok := peekProjectWorktrees(m.services, m.cfg, m.showAll); ok {
		m.allWorktrees = worktrees
	}
	for _, repository := range m.cfg.Repositories() {
		if prs, _, ok := m.services.PeekPullRequestIndex(m.cfg.ForRepository(repository.Name)); ok {
			if m.prIndexes == nil {
				m.prIndexes = map[string][]PullRequest{}
			}
			m.prIndexes[repository.Name] = prs
		}
	}
	m.attachPRIndex()
	prCfg := m.prCfg()
	if prs, fetchedAt, ok := m.services.PeekRemotePullRequests(prCfg); ok {
		m.remotePRs = prs
		m.prListFetchedAt = fetchedAt
		m.prListSlug = prCfg.RepoSlug
	}
	m.paintCachedTickets()
	if len(m.allWorktrees) > 0 {
		m.applyFilter()
	}
}

// resetProfileData forgets everything loaded for the previous profile and
// restarts prefetching for the new one.
func (m *model) resetProfileData() tea.Cmd {
	m.remotePRs = nil
	m.visiblePRs = nil
	m.prListSlug = ""
	m.prListFetchedAt = time.Time{}
	m.prListLoading = false
	m.prChecksLoading = map[string]bool{}
	m.selectedPRNumber = 0
	m.selectedPRIndex = 0
	m.cancelPRDetailLoad()
	m.prRepository = ""
	m.prIndexes = nil
	m.prIndexLoading = nil
	m.worktreeStatusTotal = 0
	m.worktreeStatusDone = 0
	m.ticketsSource = ""
	m.ticketsFetchedAt = time.Time{}
	m.ticketsLoading = false
	m.ticketMutations = map[int]ticketMutation{}
	m.prApprovals = map[int]string{}
	m.prefetchSeq++
	m.hydrateFromCache()
	return prefetchTickCmd(m.prefetchSeq, prefetchDelay)
}

// startWorktreeEnrichment issues one status read per worktree plus the PR
// labels, after discovery has replaced the rows.
func (m *model) startWorktreeEnrichment() tea.Cmd {
	m.worktreeLoadSeq++
	seq := m.worktreeLoadSeq
	cmds := []tea.Cmd{}
	m.worktreeStatusTotal, m.worktreeStatusDone = 0, 0
	for _, wt := range m.allWorktrees {
		if wt.Missing || wt.Prunable || m.isPendingCreation(wt.Path) {
			continue
		}
		m.worktreeStatusTotal++
		cmds = append(cmds, loadWorktreeStatusCmd(m.services, m.worktreeCfg(wt), wt, seq))
	}
	if labels := m.loadPRIndexesCmd(m.refreshPRIndex, false); labels != nil {
		cmds = append(cmds, labels)
	}
	m.refreshPRIndex = false
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(append(cmds, m.spinner.Tick)...)
}

func (m model) isPendingCreation(path string) bool {
	creation, ok := m.pendingCreations[path]
	return ok && !creation.Completed
}

// mergeWorktreeStatus updates one row in place; the row set and the selection
// do not move while statuses stream in.
func (m *model) mergeWorktreeStatus(loaded Worktree) {
	update := func(list []Worktree) bool {
		found := false
		for i := range list {
			if list[i].Path != loaded.Path {
				continue
			}
			list[i].Status = loaded.Status
			list[i].Commit = loaded.Commit
			list[i].LoadError = loaded.LoadError
			list[i].StatusPending = false
			found = true
		}
		return found
	}
	update(m.allWorktrees)
	if update(m.visibleWorktrees) && m.isWorktreeMode() {
		m.rebuildRows()
		if m.selectedWorktreePath() == loaded.Path {
			m.updateDetailContent()
		}
	}
}

// attachPRIndex labels worktrees with their branch's PR from the latest index
// of their own repository: the same branch often exists in several
// repositories of a project, each with its own PR.
func (m *model) attachPRIndex() {
	indexes := make(map[string]map[string]*PullRequest, len(m.prIndexes))
	for repository, prs := range m.prIndexes {
		if prs == nil {
			continue
		}
		index := make(map[string]*PullRequest, len(prs))
		for i := range prs {
			if prs[i].HeadRefName != "" {
				pr := prs[i]
				index[pr.HeadRefName] = &pr
			}
		}
		indexes[repository] = index
	}
	attach := func(list []Worktree) {
		for i := range list {
			wt := &list[i]
			if wt.Detached || wt.Branch == "" || wt.Branch == "detached" {
				continue
			}
			index, loaded := indexes[m.worktreeRepository(*wt)]
			if !loaded {
				continue
			}
			wt.PR = index[wt.Branch]
		}
	}
	attach(m.allWorktrees)
	attach(m.visibleWorktrees)
}

// loadMissingChecks requests check runs for the most relevant PRs, open ones
// first. The service answers from its cache while runs are fresh, so this is
// cheap to call after every list load.
func (m *model) loadMissingChecks() tea.Cmd {
	if m.prChecksLoading == nil {
		m.prChecksLoading = map[string]bool{}
	}
	ordered := make([]RemotePullRequest, 0, len(m.remotePRs))
	for _, pr := range m.remotePRs {
		if strings.EqualFold(pr.State, "open") {
			ordered = append(ordered, pr)
		}
	}
	for _, pr := range m.remotePRs {
		if !strings.EqualFold(pr.State, "open") {
			ordered = append(ordered, pr)
		}
	}
	var cmds []tea.Cmd
	for _, pr := range ordered[:min(len(ordered), prChecksLimit)] {
		if pr.HeadSHA == "" || m.prChecksLoading[pr.HeadSHA] {
			continue
		}
		m.prChecksLoading[pr.HeadSHA] = true
		cmds = append(cmds, loadPRChecksCmd(m.services, m.prCfg(), pr.HeadSHA))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(append(cmds, m.spinner.Tick)...)
}

func (m *model) mergePRChecks(sha string, checks []StatusCheck) {
	apply := func(pr *RemotePullRequest) {
		if pr != nil && pr.HeadSHA == sha {
			pr.StatusCheckRollup = checks
			pr.ChecksKnown = true
		}
	}
	for i := range m.remotePRs {
		apply(&m.remotePRs[i])
	}
	for i := range m.visiblePRs {
		apply(&m.visiblePRs[i])
	}
	apply(m.prDetail)
	if m.isPRMode() {
		m.rebuildRows()
		if selected := m.selectedRemotePRForDetail(); selected != nil && selected.HeadSHA == sha {
			m.updateDetailContent()
		}
	}
}

func (m model) handlePrefetchTick(msg prefetchTick) (tea.Model, tea.Cmd) {
	if msg.Seq != m.prefetchSeq {
		return m, nil
	}
	cmds := []tea.Cmd{prefetchTickCmd(m.prefetchSeq, prefetchInterval)}
	if m.setup != nil {
		return m, cmds[0]
	}
	if prCfg := m.prCfg(); !m.isPRMode() && prCfg.RepoSlug != "" && !m.prListLoading && !isRecent(m.prListFetchedAt, prefetchInterval) {
		m.prListLoading = true
		cmds = append(cmds, loadRemotePullRequestsCmd(m.services, prCfg, true))
	}
	if !m.isTicketMode() && m.ticketBoard.ID != 0 && m.cfg.ActiveProfile.Jira != nil && !m.ticketsLoading && !isRecent(m.ticketsFetchedAt, prefetchInterval) {
		m.ticketsLoading = true
		cmds = append(cmds, m.backgroundTicketsCmd(false))
	}
	// Keep worktree PR labels current while the user stays on worktrees; the
	// service answers from its cache unless the index has aged out.
	if m.isWorktreeMode() {
		cmds = append(cmds, m.loadPRIndexesCmd(false, true))
	}
	return m, tea.Batch(cmds...)
}

func isRecent(at time.Time, within time.Duration) bool {
	return !at.IsZero() && time.Since(at) < within
}

func (m model) ticketMutationsInFlight() int {
	return len(m.ticketMutations)
}

// backgroundBusy reports work that should animate the spinner without
// blocking the view: revalidation of the visible mode and queued mutations.
func (m model) backgroundBusy() bool {
	if m.ticketMutationsInFlight() > 0 || len(m.prApprovals) > 0 {
		return true
	}
	switch m.mode {
	case modePRs:
		return m.prListLoading || len(m.prChecksLoading) > 0
	case modeTickets:
		return m.ticketsLoading
	default:
		return m.worktreeStatusDone < m.worktreeStatusTotal || m.prIndexBusy()
	}
}

// progressHint describes background work for the status bar.
func (m model) progressHint() string {
	var parts []string
	switch m.mode {
	case modePRs:
		if m.prListLoading && !m.loading {
			parts = append(parts, "refreshing PRs")
		}
		if pending := len(m.prChecksLoading); pending > 0 {
			parts = append(parts, fmt.Sprintf("checks %d left", pending))
		}
	case modeTickets:
		if m.ticketsLoading && !m.loading {
			parts = append(parts, "refreshing tickets")
		}
	default:
		if m.worktreeStatusDone < m.worktreeStatusTotal {
			parts = append(parts, fmt.Sprintf("status %d/%d", m.worktreeStatusDone, m.worktreeStatusTotal))
		}
		if m.prIndexBusy() {
			parts = append(parts, "PR labels")
		}
	}
	if n := m.ticketMutationsInFlight() + len(m.prApprovals); n > 0 {
		parts = append(parts, fmt.Sprintf("%d queued", n))
	}
	return strings.Join(parts, " · ")
}

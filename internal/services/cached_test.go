package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// fakeGitHub installs a scripted gh on PATH (in front of git) that records
// every invocation and answers the API calls the services make.
type fakeGitHub struct {
	t   *testing.T
	log string
}

const fakeGHScript = `#!/bin/sh
echo "$*" >> "$WT_FAKE_GH_LOG"
sleep "${WT_FAKE_GH_SLEEP:-0}"
if [ -n "$WT_FAKE_GH_FAIL" ]; then echo "gh is offline" >&2; exit 1; fi
case "$*" in
  *check-runs*) echo '{"check_runs":[{"name":"ci","status":"completed","conclusion":"success"}]}' ;;
  *"pr list"*) echo '[{"headRefName":"feature/a","number":7,"title":"Feature A","url":"https://github.com/o/r/pull/7","state":"OPEN","body":"","author":{"login":"octo"}}]' ;;
  *"api user"*) echo '{"login":"octo"}' ;;
  *"pulls?state=all"*) echo '[{"number":42,"title":"Fix","html_url":"https://github.com/o/r/pull/42","state":"open","user":{"login":"octo"},"head":{"ref":"fix","sha":"abc123"},"base":{"ref":"main"},"updated_at":"2026-10-01T10:00:00Z"}]' ;;
  *v3.diff*) echo 'diff --git a/a.go b/a.go' ;;
  */files*) echo '[{"filename":"a.go","additions":1,"deletions":0}]' ;;
  */comments*|*/commits\?*|*/reviews*) echo '[]' ;;
  *"pr review"*) echo '' ;;
  *pulls/42*) echo '{"number":42,"title":"Fix","html_url":"https://github.com/o/r/pull/42","state":"open","user":{"login":"octo"},"head":{"ref":"fix","sha":"abc123"},"base":{"ref":"main"},"updated_at":"2026-10-01T10:00:00Z"}' ;;
  *) echo "unexpected gh call: $*" >&2; exit 2 ;;
esac
`

func installFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "gh"), fakeGHScript)
	log := filepath.Join(bin, "calls.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_FAKE_GH_LOG", log)
	return &fakeGitHub{t: t, log: log}
}

func (f *fakeGitHub) calls(substring string) int {
	f.t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && strings.Contains(line, substring) {
			count++
		}
	}
	return count
}

func githubConfig(t *testing.T) Config {
	return Config{RepoSlug: "o/r", ActiveProfile: RepositoryProfile{Name: "app", RepositoryPath: t.TempDir()}}
}

// Assures that branch PR labels reuse a recent `gh pr list` and refetch only when refresh is requested.
func TestPullRequestIndexServedFromCacheWithinTTL(t *testing.T) {
	gh := installFakeGitHub(t)
	svc, cfg, ctx := NewService(), githubConfig(t), context.Background()

	for range 2 {
		prs, _, err := svc.LoadPullRequestIndex(ctx, cfg, false)
		if err != nil || len(prs) != 1 || prs[0].HeadRefName != "feature/a" {
			t.Fatalf("LoadPullRequestIndex() = %#v, %v", prs, err)
		}
	}
	if got := gh.calls("pr list"); got != 1 {
		t.Fatalf("gh pr list calls = %d, want 1 within the TTL", got)
	}
	if _, _, err := svc.LoadPullRequestIndex(ctx, cfg, true); err != nil {
		t.Fatalf("refresh error = %v", err)
	}
	if got := gh.calls("pr list"); got != 2 {
		t.Fatalf("gh pr list calls after refresh = %d, want 2", got)
	}
	if _, at, ok := svc.PeekPullRequestIndex(cfg); !ok || at.IsZero() {
		t.Fatal("PeekPullRequestIndex() did not return the cached index")
	}
}

// Assures that a failed PR refresh keeps showing cached PR labels with a warning instead of an error.
func TestPullRequestIndexFallsBackToCacheWhenRefreshFails(t *testing.T) {
	installFakeGitHub(t)
	svc, cfg, ctx := NewService(), githubConfig(t), context.Background()
	if _, _, err := svc.LoadPullRequestIndex(ctx, cfg, false); err != nil {
		t.Fatalf("seed error = %v", err)
	}
	t.Setenv("WT_FAKE_GH_FAIL", "1")
	prs, warning, err := svc.LoadPullRequestIndex(ctx, cfg, true)
	if err != nil || len(prs) != 1 || !strings.Contains(warning, "cached") {
		t.Fatalf("LoadPullRequestIndex() during outage = %#v warning=%q err=%v", prs, warning, err)
	}
}

// Assures that PR Radar lists PRs with one request, then fills check runs per commit and reuses completed ones.
func TestRemotePullRequestsListBeforeChecksAndCachesChecks(t *testing.T) {
	gh := installFakeGitHub(t)
	svc, cfg, ctx := NewService(), githubConfig(t), context.Background()

	prs, err := svc.LoadRemotePullRequests(ctx, cfg)
	if err != nil || len(prs) != 1 {
		t.Fatalf("LoadRemotePullRequests() = %#v, %v", prs, err)
	}
	if prs[0].ChecksKnown || prs[0].HeadSHA != "abc123" || gh.calls("check-runs") != 0 {
		t.Fatalf("list waited for checks or lost the head commit: %#v, check calls=%d", prs[0], gh.calls("check-runs"))
	}

	for range 2 {
		checks, err := svc.LoadRemotePullRequestChecks(ctx, cfg, "abc123")
		if err != nil || len(checks) != 1 || checks[0].Conclusion != "success" {
			t.Fatalf("LoadRemotePullRequestChecks() = %#v, %v", checks, err)
		}
	}
	if got := gh.calls("check-runs"); got != 1 {
		t.Fatalf("check-runs calls = %d, want completed checks reused from cache", got)
	}

	cached, fetchedAt, ok := svc.PeekRemotePullRequests(cfg)
	if !ok || fetchedAt.IsZero() || len(cached) != 1 || !cached[0].ChecksKnown || len(cached[0].StatusCheckRollup) != 1 || cached[0].HeadSHA != "abc123" {
		t.Fatalf("PeekRemotePullRequests() = %#v ok=%v", cached, ok)
	}
}

// Assures that a PR detail issues its GitHub requests concurrently rather than one after another.
func TestPullRequestDetailRequestsRunConcurrently(t *testing.T) {
	gh := installFakeGitHub(t)
	t.Setenv("WT_FAKE_GH_SLEEP", "0.4")
	svc, cfg := NewService(), githubConfig(t)

	started := time.Now()
	detail, err := svc.LoadRemotePullRequestDetail(context.Background(), cfg, 42)
	elapsed := time.Since(started)
	if err != nil || detail.Number != 42 || len(detail.Files) != 1 || detail.Diff == "" || !detail.ChecksKnown {
		t.Fatalf("LoadRemotePullRequestDetail() = %#v, %v", detail, err)
	}
	if calls := gh.calls(""); calls != 7 {
		t.Fatalf("gh calls = %d, want 7", calls)
	}
	// Seven sequential 0.4s calls take 2.8s; the detail and its checks are the
	// only dependent pair.
	if elapsed > 2*time.Second {
		t.Fatalf("detail took %v, want concurrent requests", elapsed)
	}
}

// Assures that a loaded PR detail, including its diff and head commit, is available instantly on the next visit, and reuses fresh check runs.
func TestPullRequestDetailIsCachedForRevisits(t *testing.T) {
	gh := installFakeGitHub(t)
	svc, cfg := NewService(), githubConfig(t)
	if _, err := svc.LoadRemotePullRequestChecks(context.Background(), cfg, "abc123"); err != nil {
		t.Fatalf("LoadRemotePullRequestChecks() error = %v", err)
	}
	if _, err := svc.LoadRemotePullRequestDetail(context.Background(), cfg, 42); err != nil {
		t.Fatalf("LoadRemotePullRequestDetail() error = %v", err)
	}
	if got := gh.calls("check-runs"); got != 1 {
		t.Fatalf("check-runs calls = %d, want the detail to reuse checks fetched moments ago", got)
	}
	cached, fetchedAt, ok := svc.PeekRemotePullRequestDetail(cfg, 42)
	if !ok || fetchedAt.IsZero() || cached.Diff == "" || cached.HeadSHA != "abc123" || len(cached.Files) != 1 {
		t.Fatalf("PeekRemotePullRequestDetail() = %#v ok=%v", cached, ok)
	}

	if err := svc.ApproveRemotePullRequest(context.Background(), cfg, 42); err != nil {
		t.Fatalf("ApproveRemotePullRequest() error = %v", err)
	}
	if _, fetchedAt, ok := svc.PeekRemotePullRequestDetail(cfg, 42); !ok || !fetchedAt.IsZero() {
		t.Fatalf("approved PR detail fetchedAt=%v ok=%v, want stale but kept", fetchedAt, ok)
	}
}

// Assures that the authenticated GitHub user is looked up once and then served from cache.
func TestGitHubCurrentUserIsCached(t *testing.T) {
	gh := installFakeGitHub(t)
	svc, cfg := NewService(), githubConfig(t)
	for range 3 {
		if login, err := svc.LoadGitHubCurrentUser(context.Background(), cfg); err != nil || login != "octo" {
			t.Fatalf("LoadGitHubCurrentUser() = %q, %v", login, err)
		}
	}
	if got := gh.calls("api user"); got != 1 {
		t.Fatalf("gh api user calls = %d, want 1", got)
	}
}

// Assures that discovery reports unknown statuses as pending, reuses cached ones, and that the cached list paints without git.
func TestDiscoverWorktreesOverlaysCachedStatus(t *testing.T) {
	cfg := testGitRepo(t)
	svc, ctx := NewService(), context.Background()
	writeRepoFile(t, cfg.ActiveProfile.RepositoryPath, "dirty.txt", "x\n")

	discovered, err := svc.DiscoverWorktrees(ctx, cfg, true)
	if err != nil || len(discovered) != 1 || !discovered[0].StatusPending {
		t.Fatalf("first DiscoverWorktrees() = %#v, %v; want one pending worktree", discovered, err)
	}
	loaded, err := svc.LoadWorktreeStatus(ctx, cfg, discovered[0])
	if err != nil || loaded.StatusPending || !loaded.Status.Dirty || loaded.Commit.Subject != "initial" {
		t.Fatalf("LoadWorktreeStatus() = %#v, %v", loaded, err)
	}

	again, err := svc.DiscoverWorktrees(ctx, cfg, true)
	if err != nil || again[0].StatusPending || !again[0].Status.Dirty || again[0].Commit.Relative == "" {
		t.Fatalf("second DiscoverWorktrees() = %#v, %v; want cached dirty status", again, err)
	}

	// A relative commit time read long ago is no longer true; keep the commit but drop it.
	_ = svc.cache.PutAt(bucketWorktreeStatus, worktreeStatusKey(cfg, loaded.Path), cachedWorktreeStatus{Status: loaded.Status, Commit: loaded.Commit}, time.Now().Add(-time.Hour))
	if aged, _ := svc.DiscoverWorktrees(ctx, cfg, true); aged[0].Commit.Relative != "" || aged[0].Commit.Subject != "initial" {
		t.Fatalf("aged cached commit = %#v, want subject without stale relative time", aged[0].Commit)
	}

	if err := os.RemoveAll(cfg.ActiveProfile.RepositoryPath); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}
	peeked, ok := svc.PeekWorktrees(cfg, true)
	if !ok || len(peeked) != 1 || !peeked[0].Status.Dirty || peeked[0].Name != "test" {
		t.Fatalf("PeekWorktrees() without git = %#v ok=%v", peeked, ok)
	}
}

// Assures that a confirmed ticket mutation is written into every cached list and detail, and lists are marked for revalidation.
func TestApplyTicketPatchUpdatesCachedCopies(t *testing.T) {
	svc := NewService()
	cfg := Config{ActiveProfile: RepositoryProfile{Name: "app", Jira: &JiraConfig{Site: "example.atlassian.net"}}}
	board := JiraBoard{ID: 9, Name: "Board"}
	for _, mine := range []bool{true, false} {
		_ = svc.cache.Put(bucketTickets, ticketCacheKey(cfg, board, mine, true), []Ticket{{Key: "APP-1", Status: "To Do"}, {Key: "APP-2", Status: "To Do"}})
	}
	_ = svc.cache.Put(bucketTicketDetail, ticketDetailCacheKey(cfg, "APP-1"), Ticket{Key: "APP-1", Status: "To Do", Detail: true})

	svc.ApplyTicketPatch(cfg, board, core.TicketPatch{Key: "APP-1", Status: "Done", StatusCategory: core.StatusCategoryDone})

	for _, mine := range []bool{true, false} {
		tickets, fetchedAt, ok := svc.PeekTicketCache(cfg, board, mine, true)
		if !ok || !fetchedAt.IsZero() || tickets[0].Status != "Done" || tickets[1].Status != "To Do" {
			t.Fatalf("scope mine=%v = %#v at=%v ok=%v", mine, tickets, fetchedAt, ok)
		}
	}
	detail, ok := svc.PeekTicketDetail(cfg, "APP-1")
	if !ok || detail.Status != "Done" || detail.StatusCategory != core.StatusCategoryDone || !detail.Detail {
		t.Fatalf("PeekTicketDetail() = %#v ok=%v", detail, ok)
	}
}

// Assures that concurrent identical fetches, such as a prefetch and a view load, share one request.
func TestFlightGroupSharesConcurrentFetches(t *testing.T) {
	var group flightGroup
	var runs atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			value, err := group.do("key", func() (any, error) {
				runs.Add(1)
				<-release
				return 42, nil
			})
			if err != nil || value.(int) != 42 {
				t.Errorf("do() = %v, %v", value, err)
			}
		})
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if got := runs.Load(); got != 1 {
		t.Fatalf("fetch ran %d times, want 1", got)
	}
}

// Assures that creating a worktree for an existing local branch does not wait on a network fetch.
func TestCreateWorktreeForLocalBranchSkipsFetch(t *testing.T) {
	cfg := testGitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	runGit(t, repo, "branch", "local-only")
	fetches := 0
	fetch := func(context.Context, string, string, bool) bool { fetches++; return true }

	if _, err := createWorktreeWith(context.Background(), cfg, "local-only", createWorktreePath(cfg, "local-only"), fetch); err != nil {
		t.Fatalf("createWorktreeWith() error = %v", err)
	}
	if fetches != 0 {
		t.Fatalf("fetches = %d, want none for a local branch", fetches)
	}
	if _, err := createWorktreeWith(context.Background(), cfg, "brand-new", createWorktreePath(cfg, "brand-new"), fetch); err != nil {
		t.Fatalf("createWorktreeWith() error = %v", err)
	}
	if fetches == 0 {
		t.Fatal("a new branch was created without fetching the remote first")
	}
}

// Assures that queued worktree creations reuse a fetch from moments ago, unless forced.
func TestFetchCoalescerReusesRecentFetch(t *testing.T) {
	now := time.Now()
	coalescer := fetchCoalescer{now: func() time.Time { return now }}
	repo := t.TempDir()
	ctx := context.Background()
	if !coalescer.fetch(ctx, repo, "origin", false) {
		t.Fatal("first fetch was skipped")
	}
	if coalescer.fetch(ctx, repo, "origin", false) {
		t.Fatal("fetch within the reuse window was not skipped")
	}
	if !coalescer.fetch(ctx, repo, "origin", true) {
		t.Fatal("forced fetch was skipped")
	}
	now = now.Add(fetchReuseWindow + time.Second)
	if !coalescer.fetch(ctx, repo, "origin", false) {
		t.Fatal("fetch after the reuse window was skipped")
	}
}

// Assures that a reused fetch never hides a remote branch pushed after it: creation still tracks the remote branch.
func TestCreateWorktreeRefetchesWhenReusedFetchMissesBranch(t *testing.T) {
	cfg := testGitRepo(t)
	repo := cfg.ActiveProfile.RepositoryPath
	writeRepoFile(t, repo, "late.txt", "late\n")
	runGit(t, repo, "checkout", "-b", "late-branch")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "late")
	runGit(t, repo, "push", "origin", "late-branch")
	runGit(t, repo, "checkout", "main")
	runGit(t, repo, "branch", "-D", "late-branch")
	runGit(t, repo, "update-ref", "-d", "refs/remotes/origin/late-branch")

	svc := NewService()
	svc.fetches.last = map[string]time.Time{repo + "\x00origin": time.Now()}
	path := createWorktreePath(cfg, "late-branch")
	if _, err := svc.CreateWorktree(context.Background(), cfg, "late-branch", path); err != nil {
		t.Fatalf("CreateWorktree() error = %v", err)
	}
	upstream := strings.TrimSpace(runGit(t, path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"))
	if upstream != "origin/late-branch" {
		t.Fatalf("upstream = %q, want origin/late-branch", upstream)
	}
}

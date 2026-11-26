package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
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
	testkit.WriteExecutable(t, filepath.Join(bin, "gh"), fakeGHScript)
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

func githubConfig(t *testing.T) core.Config {
	return core.Config{RepoSlug: "o/r", ActiveProfile: core.RepositoryProfile{Name: "app", RepositoryPath: t.TempDir()}}
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

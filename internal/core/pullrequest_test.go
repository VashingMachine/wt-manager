package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestGitHubSlugFromRemoteURL(t *testing.T) {
	tests := map[string]string{
		"git@github.com:owner/repo.git":       "owner/repo",
		"ssh://git@github.com/owner/repo.git": "owner/repo",
		"https://github.com/owner/repo.git":   "owner/repo",
		"http://github.com/owner/repo":        "owner/repo",
		"https://example.com/owner/repo.git":  "",
	}
	for input, want := range tests {
		if got := GitHubSlugFromRemoteURL(input); got != want {
			t.Fatalf("GitHubSlugFromRemoteURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFilterRemotePullRequests(t *testing.T) {
	prs := []RemotePullRequest{
		remotePRFixture(10, "OPEN", "alice", "Add auth cache", "feature/auth"),
		remotePRFixture(11, "CLOSED", "bob", "Remove cache", "bug/remove-cache"),
		remotePRFixture(12, "MERGED", "clara", "Update docs", "docs/readme"),
	}
	prs[0].Assignees = []GitHubActor{{Login: "dariusz"}}
	prs[1].ReviewRequests = []ReviewRequest{{RequestedReviewer: GitHubActor{Login: "dariusz"}}}
	prs[2].Labels = []PullLabel{{Name: "docs"}}

	got := FilterRemotePullRequestsWithAuthor(prs, "cache", "open", "", "")
	if len(got) != 1 || got[0].Number != 10 {
		t.Fatalf("filter open cache = %#v, want PR 10", got)
	}

	got = FilterRemotePullRequestsWithAuthor(prs, "", "all", "dariusz", "")
	if len(got) != 2 || got[0].Number != 10 || got[1].Number != 11 {
		t.Fatalf("filter user dariusz = %#v, want PRs 10 and 11", got)
	}

	got = FilterRemotePullRequestsWithAuthor(prs, "docs", "merged", "", "")
	if len(got) != 1 || got[0].Number != 12 {
		t.Fatalf("filter merged docs = %#v, want PR 12", got)
	}

	got = FilterRemotePullRequestsWithAuthor(prs, "", "all", "", "alice")
	if len(got) != 1 || got[0].Number != 10 {
		t.Fatalf("filter author alice = %#v, want PR 10", got)
	}
}

func TestRemotePRUsers(t *testing.T) {
	prs := []RemotePullRequest{
		remotePRFixture(10, "OPEN", "alice", "Add auth cache", "feature/auth"),
		remotePRFixture(11, "OPEN", "bob", "Remove cache", "bug/remove-cache"),
	}
	prs[0].Assignees = []GitHubActor{{Login: "zoe"}}
	prs[1].ReviewRequests = []ReviewRequest{{RequestedReviewer: GitHubActor{Login: "alice"}}}

	got := strings.Join(RemotePRUsers(prs), ",")
	for _, want := range []string{"alice", "bob", "zoe"} {
		if !strings.Contains(got, want) {
			t.Fatalf("RemotePRUsers() = %q, missing %s", got, want)
		}
	}
	if strings.Count(got, "alice") != 1 {
		t.Fatalf("RemotePRUsers() = %q, want alice once", got)
	}
}

func TestBuildPullRequestChatPrompt(t *testing.T) {
	pr := remotePRFixture(42, "OPEN", "alice", "Fix token refresh", "feature/token-refresh")
	pr.URL = "https://github.com/owner/repo/pull/42"
	pr.Body = "This changes token refresh behavior."
	pr.Files = []PullFile{{Path: "auth/session.go", Additions: 40, Deletions: 12}}
	pr.Comments = []PullComment{{Author: GitHubActor{Login: "reviewer"}, Body: "Please check retry behavior."}}
	pr.Diff = strings.Repeat("diff --git a/auth/session.go b/auth/session.go\n", 500)

	prompt := BuildPullRequestChatPrompt(pr, "what is risky?")
	for _, want := range []string{"Question: what is risky?", "PR #42: Fix token refresh", "auth/session.go", "reviewer", "Diff:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q in %q", want, prompt)
		}
	}
	if len([]rune(prompt)) > 18000 {
		t.Fatalf("prompt length = %d, want truncation", len([]rune(prompt)))
	}
}

func TestDeriveReviewDecision(t *testing.T) {
	tests := map[string]struct {
		reviews []PullReview
		want    string
	}{
		"approved": {
			reviews: []PullReview{{Author: GitHubActor{Login: "alice"}, State: "APPROVED"}},
			want:    "APPROVED",
		},
		"changes requested wins": {
			reviews: []PullReview{
				{Author: GitHubActor{Login: "alice"}, State: "APPROVED"},
				{Author: GitHubActor{Login: "bob"}, State: "CHANGES_REQUESTED"},
			},
			want: "CHANGES_REQUESTED",
		},
		"comment only": {
			reviews: []PullReview{{Author: GitHubActor{Login: "alice"}, State: "COMMENTED"}},
			want:    "",
		},
		"dismissed latest clears reviewer": {
			reviews: []PullReview{
				{Author: GitHubActor{Login: "alice"}, State: "APPROVED"},
				{Author: GitHubActor{Login: "alice"}, State: "DISMISSED"},
			},
			want: "",
		},
		"latest reviewer state wins": {
			reviews: []PullReview{
				{Author: GitHubActor{Login: "alice"}, State: "CHANGES_REQUESTED"},
				{Author: GitHubActor{Login: "alice"}, State: "APPROVED"},
			},
			want: "APPROVED",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := DeriveReviewDecision(tc.reviews); got != tc.want {
				t.Fatalf("DeriveReviewDecision() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Assures that a branch counts as merged only through a merged pull request of the repository itself with no open one beside it.
func TestBranchMerged(t *testing.T) {
	prs := []PullRequest{
		{HeadRefName: "done", State: "MERGED"},
		{HeadRefName: "reopened", State: "MERGED"},
		{HeadRefName: "reopened", State: "OPEN"},
		{HeadRefName: "main", State: "MERGED", IsCrossRepository: true},
		{HeadRefName: "dropped", State: "CLOSED"},
	}
	for branch, want := range map[string]bool{"done": true, "reopened": false, "main": false, "dropped": false, "unknown": false} {
		if got := BranchMerged(prs, branch); got != want {
			t.Errorf("BranchMerged(%q) = %v, want %v", branch, got, want)
		}
	}
}

func remotePRFixture(number int, state string, author string, title string, branch string) RemotePullRequest {
	return RemotePullRequest{
		Number:      number,
		Title:       title,
		URL:         fmt.Sprintf("https://github.com/owner/repo/pull/%d", number),
		State:       state,
		HeadRefName: branch,
		BaseRefName: "main",
		Author:      GitHubActor{Login: author},
		UpdatedAt:   "2026-05-05T12:00:00Z",
	}
}

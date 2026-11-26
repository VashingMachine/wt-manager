package core

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PullRequest struct {
	HeadRefName string `json:"headRefName"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	State       string `json:"state"`
	Body        string `json:"body"`
	// IsCrossRepository marks a PR opened from a fork, whose head branch name
	// says nothing about a local branch of the same name.
	IsCrossRepository bool `json:"isCrossRepository,omitempty"`
	Author            struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"author"`
}

// BranchMerged reports whether a branch of the repository itself has a merged
// pull request and no open one: its changes are in, and what was stacked on
// it should move on. Pull requests from forks are ignored, because their head
// branch only shares a name with a local one.
func BranchMerged(pullRequests []PullRequest, branch string) bool {
	merged := false
	for _, pr := range pullRequests {
		if pr.HeadRefName != branch || pr.IsCrossRepository {
			continue
		}
		switch strings.ToUpper(pr.State) {
		case "OPEN":
			return false
		case "MERGED":
			merged = true
		}
	}
	return merged
}

type RemotePullRequest struct {
	Number            int             `json:"number"`
	Title             string          `json:"title"`
	URL               string          `json:"url"`
	State             string          `json:"state"`
	Body              string          `json:"body"`
	HeadRefName       string          `json:"headRefName"`
	HeadSHA           string          `json:"-"`
	BaseRefName       string          `json:"baseRefName"`
	IsDraft           bool            `json:"isDraft"`
	Author            GitHubActor     `json:"author"`
	Assignees         []GitHubActor   `json:"assignees"`
	ReviewRequests    []ReviewRequest `json:"reviewRequests"`
	Labels            []PullLabel     `json:"labels"`
	ReviewDecision    string          `json:"reviewDecision"`
	StatusCheckRollup []StatusCheck   `json:"statusCheckRollup"`
	ChangedFiles      int             `json:"changedFiles"`
	Additions         int             `json:"additions"`
	Deletions         int             `json:"deletions"`
	CreatedAt         string          `json:"createdAt"`
	UpdatedAt         string          `json:"updatedAt"`
	Files             []PullFile      `json:"files"`
	Comments          []PullComment   `json:"comments"`
	Commits           []PullCommit    `json:"commits"`
	Reviews           []PullReview    `json:"reviews"`
	Diff              string          `json:"-"`
	// ChecksKnown reports whether StatusCheckRollup reflects check runs read
	// for HeadSHA (now or from the cache), so an empty list while checks are
	// still loading is not mistaken for a PR without CI.
	ChecksKnown bool `json:"-"`
}

type GitHubActor struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
}

type ReviewRequest struct {
	RequestedReviewer GitHubActor `json:"requestedReviewer"`
}

type PullLabel struct {
	Name string `json:"name"`
}

type StatusCheck struct {
	Name       string `json:"name"`
	Workflow   string `json:"workflowName"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
	Summary    string `json:"summary"`
}

type PullFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type PullComment struct {
	Author    GitHubActor `json:"author"`
	Body      string      `json:"body"`
	CreatedAt string      `json:"createdAt"`
	URL       string      `json:"url"`
}

type PullCommit struct {
	Oid             string `json:"oid"`
	MessageHeadline string `json:"messageHeadline"`
	CommittedDate   string `json:"committedDate"`
}

type PullReview struct {
	Author      GitHubActor `json:"author"`
	State       string      `json:"state"`
	Body        string      `json:"body"`
	SubmittedAt string      `json:"submittedAt"`
	URL         string      `json:"url"`
}

// DeriveReviewDecision reduces reviews to one verdict from the latest review
// of each reviewer: CHANGES_REQUESTED wins over APPROVED, and comments do not
// count.
func DeriveReviewDecision(reviews []PullReview) string {
	latestByUser := map[string]PullReview{}
	for _, review := range reviews {
		user := strings.TrimSpace(review.Author.Login)
		if user == "" {
			user = strings.TrimSpace(review.Author.Slug)
		}
		if user == "" {
			continue
		}
		state := strings.ToUpper(strings.TrimSpace(review.State))
		if state == "" || state == "COMMENTED" {
			continue
		}
		latestByUser[strings.ToLower(user)] = review
	}

	approved := false
	for _, review := range latestByUser {
		switch strings.ToUpper(strings.TrimSpace(review.State)) {
		case "CHANGES_REQUESTED":
			return "CHANGES_REQUESTED"
		case "APPROVED":
			approved = true
		}
	}
	if approved {
		return "APPROVED"
	}
	return ""
}

// FilterRemotePullRequestsWithAuthor narrows a pull request list by text, by
// state, by a user involved in any role and by author. Empty filters match
// everything.
func FilterRemotePullRequestsWithAuthor(pullRequests []RemotePullRequest, query string, state string, user string, author string) []RemotePullRequest {
	query = strings.ToLower(strings.TrimSpace(query))
	state = strings.ToLower(strings.TrimSpace(state))
	user = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(user), "@"))
	author = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(author), "@"))

	filtered := make([]RemotePullRequest, 0, len(pullRequests))
	for _, pr := range pullRequests {
		if state != "" && state != "all" && strings.ToLower(pr.State) != state {
			continue
		}
		if author != "" && !strings.EqualFold(pr.Author.Login, author) {
			continue
		}
		if user != "" && !remotePullRequestHasUser(pr, user) {
			continue
		}
		if query != "" && !remotePullRequestMatchesQuery(pr, query) {
			continue
		}
		filtered = append(filtered, pr)
	}
	return filtered
}

// RemotePRUsers lists everybody involved in the pull requests as author,
// assignee or requested reviewer, sorted.
func RemotePRUsers(pullRequests []RemotePullRequest) []string {
	seen := map[string]struct{}{}
	var users []string
	add := func(actor GitHubActor) {
		login := strings.TrimSpace(actor.Login)
		if login == "" {
			login = strings.TrimSpace(actor.Slug)
		}
		if login == "" {
			return
		}
		key := strings.ToLower(login)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		users = append(users, login)
	}

	for _, pr := range pullRequests {
		add(pr.Author)
		for _, assignee := range pr.Assignees {
			add(assignee)
		}
		for _, request := range pr.ReviewRequests {
			add(request.RequestedReviewer)
		}
	}
	sort.Strings(users)
	return users
}

func remotePullRequestHasUser(pr RemotePullRequest, user string) bool {
	matches := func(actor GitHubActor) bool {
		return strings.EqualFold(actor.Login, user) || strings.EqualFold(actor.Slug, user)
	}
	if matches(pr.Author) {
		return true
	}
	for _, assignee := range pr.Assignees {
		if matches(assignee) {
			return true
		}
	}
	for _, request := range pr.ReviewRequests {
		if matches(request.RequestedReviewer) {
			return true
		}
	}
	return false
}

func remotePullRequestMatchesQuery(pr RemotePullRequest, query string) bool {
	fields := []string{strconv.Itoa(pr.Number), pr.Title, pr.HeadRefName, pr.BaseRefName, pr.Author.Login, pr.ReviewDecision, strings.ToLower(pr.State)}
	for _, label := range pr.Labels {
		fields = append(fields, label.Name)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

// SortRemotePullRequests orders pull requests by last update, newest first.
func SortRemotePullRequests(pullRequests []RemotePullRequest) {
	sort.SliceStable(pullRequests, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339, pullRequests[i].UpdatedAt)
		right, rightErr := time.Parse(time.RFC3339, pullRequests[j].UpdatedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.After(right)
		}
		return pullRequests[i].Number > pullRequests[j].Number
	})
}

// BuildPullRequestChatPrompt gives an agent a question together with what is
// known about a pull request, cut to a size a prompt can carry.
func BuildPullRequestChatPrompt(pr RemotePullRequest, question string) string {
	var lines []string
	lines = append(lines,
		"You are helping review a GitHub pull request.",
		"Answer using only the PR context below. Call out uncertainty when context is missing.",
		"",
		fmt.Sprintf("Question: %s", strings.TrimSpace(question)),
		"",
		fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title),
		fmt.Sprintf("URL: %s", pr.URL),
		fmt.Sprintf("State: %s Draft: %t Review: %s", pr.State, pr.IsDraft, emptyDash(pr.ReviewDecision)),
		fmt.Sprintf("Author: %s", emptyDash(pr.Author.Login)),
		fmt.Sprintf("Branch: %s -> %s", emptyDash(pr.HeadRefName), emptyDash(pr.BaseRefName)),
		fmt.Sprintf("Size: %d files, +%d/-%d", pr.ChangedFiles, pr.Additions, pr.Deletions),
	)

	if len(pr.Labels) > 0 {
		labels := make([]string, 0, len(pr.Labels))
		for _, label := range pr.Labels {
			labels = append(labels, label.Name)
		}
		lines = append(lines, fmt.Sprintf("Labels: %s", strings.Join(labels, ", ")))
	}

	lines = append(lines, "", "Description:", truncateRunes(strings.TrimSpace(pr.Body), 3000))
	lines = append(lines, "", "Files:")
	if len(pr.Files) == 0 {
		lines = append(lines, "- not loaded")
	} else {
		for idx, file := range pr.Files {
			if idx == 60 {
				lines = append(lines, fmt.Sprintf("- ... and %d more", len(pr.Files)-idx))
				break
			}
			lines = append(lines, fmt.Sprintf("- %s (+%d/-%d)", file.Path, file.Additions, file.Deletions))
		}
	}

	if len(pr.Comments) > 0 {
		lines = append(lines, "", "Recent comments:")
		start := max(0, len(pr.Comments)-8)
		for _, comment := range pr.Comments[start:] {
			lines = append(lines, fmt.Sprintf("- %s: %s", emptyDash(comment.Author.Login), truncateRunes(strings.TrimSpace(comment.Body), 600)))
		}
	}

	if strings.TrimSpace(pr.Diff) != "" {
		lines = append(lines, "", "Diff:", truncateRunes(pr.Diff, 12000))
	}

	return strings.Join(lines, "\n")
}

// GitHubSlugFromRemoteURL returns the owner/name of a GitHub remote URL, or
// "" for a remote hosted elsewhere.
func GitHubSlugFromRemoteURL(remoteURL string) string {
	remoteURL = strings.TrimSpace(remoteURL)
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	switch {
	case strings.HasPrefix(remoteURL, "git@github.com:"):
		return strings.TrimPrefix(remoteURL, "git@github.com:")
	case strings.HasPrefix(remoteURL, "ssh://git@github.com/"):
		return strings.TrimPrefix(remoteURL, "ssh://git@github.com/")
	case strings.HasPrefix(remoteURL, "https://github.com/"):
		return strings.TrimPrefix(remoteURL, "https://github.com/")
	case strings.HasPrefix(remoteURL, "http://github.com/"):
		return strings.TrimPrefix(remoteURL, "http://github.com/")
	}
	return ""
}

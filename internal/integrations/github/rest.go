package github

import (
	"strings"

	"github.com/VashingMachine/wt-manager/internal/core"
)

type restPullRequest struct {
	Number             int         `json:"number"`
	Title              string      `json:"title"`
	HTMLURL            string      `json:"html_url"`
	State              string      `json:"state"`
	Body               string      `json:"body"`
	Draft              bool        `json:"draft"`
	User               restActor   `json:"user"`
	Assignees          []restActor `json:"assignees"`
	RequestedReviewers []restActor `json:"requested_reviewers"`
	Labels             []restLabel `json:"labels"`
	Head               restRef     `json:"head"`
	Base               restRef     `json:"base"`
	ChangedFiles       int         `json:"changed_files"`
	Additions          int         `json:"additions"`
	Deletions          int         `json:"deletions"`
	CreatedAt          string      `json:"created_at"`
	UpdatedAt          string      `json:"updated_at"`
	MergedAt           *string     `json:"merged_at"`
}

type restActor struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
}

type restLabel struct {
	Name string `json:"name"`
}

type restRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type restPullFile struct {
	Filename  string `json:"filename"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type restIssueComment struct {
	User      restActor `json:"user"`
	Body      string    `json:"body"`
	CreatedAt string    `json:"created_at"`
	HTMLURL   string    `json:"html_url"`
}

type restPullCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

type restPullReview struct {
	User        restActor `json:"user"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt string    `json:"submitted_at"`
	HTMLURL     string    `json:"html_url"`
}

type restCheckRuns struct {
	CheckRuns []restCheckRun `json:"check_runs"`
}

type restCheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	DetailsURL string `json:"details_url"`
	Output     struct {
		Summary string `json:"summary"`
	} `json:"output"`
}

func remotePullRequestFromREST(pr restPullRequest) core.RemotePullRequest {
	state := strings.ToUpper(pr.State)
	if pr.MergedAt != nil && strings.TrimSpace(*pr.MergedAt) != "" {
		state = "MERGED"
	}

	assignees := make([]core.GitHubActor, 0, len(pr.Assignees))
	for _, assignee := range pr.Assignees {
		assignees = append(assignees, githubActorFromREST(assignee))
	}
	reviewRequests := make([]core.ReviewRequest, 0, len(pr.RequestedReviewers))
	for _, reviewer := range pr.RequestedReviewers {
		reviewRequests = append(reviewRequests, core.ReviewRequest{RequestedReviewer: githubActorFromREST(reviewer)})
	}
	labels := make([]core.PullLabel, 0, len(pr.Labels))
	for _, label := range pr.Labels {
		labels = append(labels, core.PullLabel{Name: label.Name})
	}

	return core.RemotePullRequest{
		Number:         pr.Number,
		Title:          pr.Title,
		URL:            pr.HTMLURL,
		State:          state,
		Body:           pr.Body,
		HeadRefName:    pr.Head.Ref,
		HeadSHA:        pr.Head.SHA,
		BaseRefName:    pr.Base.Ref,
		IsDraft:        pr.Draft,
		Author:         githubActorFromREST(pr.User),
		Assignees:      assignees,
		ReviewRequests: reviewRequests,
		Labels:         labels,
		ChangedFiles:   pr.ChangedFiles,
		Additions:      pr.Additions,
		Deletions:      pr.Deletions,
		CreatedAt:      pr.CreatedAt,
		UpdatedAt:      pr.UpdatedAt,
	}
}

func githubActorFromREST(actor restActor) core.GitHubActor {
	return core.GitHubActor{Login: actor.Login, Name: actor.Name, Slug: actor.Slug}
}

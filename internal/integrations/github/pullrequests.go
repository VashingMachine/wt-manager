package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func loadRemotePullRequests(ctx context.Context, cfg core.Config) ([]core.RemotePullRequest, error) {
	if cfg.RepoSlug == "" {
		return nil, errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("gh not found; run setup after installing GitHub CLI")
	}

	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls?state=all&per_page=100", cfg.RepoSlug))
	if err != nil {
		return nil, fmt.Errorf("remote PR lookup failed: %w", err)
	}

	var restPullRequests []restPullRequest
	if err := json.Unmarshal([]byte(output), &restPullRequests); err != nil {
		return nil, fmt.Errorf("gh returned invalid PR JSON: %w", err)
	}
	// Check runs cost one request per PR, so they are not awaited here: the
	// list paints first and LoadRemotePullRequestChecks fills rows in.
	pullRequests := make([]core.RemotePullRequest, 0, len(restPullRequests))
	for _, pr := range restPullRequests {
		pullRequest := remotePullRequestFromREST(pr)
		pullRequests = append(pullRequests, pullRequest)
	}
	core.SortRemotePullRequests(pullRequests)
	return pullRequests, nil
}

type checksLoader func(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error)

func loadRemotePullRequestDetail(ctx context.Context, cfg core.Config, number int) (core.RemotePullRequest, error) {
	return loadRemotePullRequestDetailWith(ctx, cfg, number, loadRemotePullRequestChecks)
}

// loadRemotePullRequestDetailWith issues the detail's independent requests
// concurrently, so a detail costs about two round trips instead of seven: only
// the check runs wait for the head SHA from the PR itself.
func loadRemotePullRequestDetailWith(ctx context.Context, cfg core.Config, number int, loadChecks checksLoader) (core.RemotePullRequest, error) {
	if cfg.RepoSlug == "" {
		return core.RemotePullRequest{}, errors.New("GitHub repository is not configured")
	}
	if number <= 0 {
		return core.RemotePullRequest{}, errors.New("pull request number is required")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return core.RemotePullRequest{}, errors.New("gh not found; run setup after installing GitHub CLI")
	}

	var (
		wg          sync.WaitGroup
		restPR      restPullRequest
		detailErr   error
		checks      []core.StatusCheck
		checksErr   error
		files       []core.PullFile
		filesErr    error
		comments    []core.PullComment
		commentsErr error
		commits     []core.PullCommit
		commitsErr  error
		reviews     []core.PullReview
		reviewsErr  error
		diff        string
		diffErr     error
	)
	wg.Go(func() {
		output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d", cfg.RepoSlug, number))
		if err != nil {
			detailErr = fmt.Errorf("PR detail lookup failed: %w", err)
			return
		}
		if err := json.Unmarshal([]byte(output), &restPR); err != nil {
			detailErr = fmt.Errorf("gh returned invalid PR detail JSON: %w", err)
			return
		}
		if restPR.Head.SHA != "" {
			checks, checksErr = loadChecks(ctx, cfg, restPR.Head.SHA)
		}
	})
	wg.Go(func() { files, filesErr = loadRemotePullRequestFiles(ctx, cfg, number) })
	wg.Go(func() { comments, commentsErr = loadRemotePullRequestComments(ctx, cfg, number) })
	wg.Go(func() { commits, commitsErr = loadRemotePullRequestCommits(ctx, cfg, number) })
	wg.Go(func() { reviews, reviewsErr = loadRemotePullRequestReviews(ctx, cfg, number) })
	wg.Go(func() {
		diff, diffErr = shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d", cfg.RepoSlug, number), "-H", "Accept: application/vnd.github.v3.diff")
	})
	wg.Wait()

	if detailErr != nil {
		return core.RemotePullRequest{}, detailErr
	}
	pullRequest := remotePullRequestFromREST(restPR)
	if filesErr == nil {
		pullRequest.Files = files
	}
	if commentsErr == nil {
		pullRequest.Comments = comments
	}
	if commitsErr == nil {
		pullRequest.Commits = commits
	}
	if reviewsErr == nil {
		pullRequest.Reviews = reviews
		pullRequest.ReviewDecision = core.DeriveReviewDecision(reviews)
	}
	if restPR.Head.SHA != "" && checksErr == nil {
		pullRequest.StatusCheckRollup = checks
		pullRequest.ChecksKnown = true
	}
	if diffErr == nil {
		pullRequest.Diff = diff
	}
	return pullRequest, nil
}

func approveRemotePullRequest(ctx context.Context, cfg core.Config, number int) error {
	if cfg.RepoSlug == "" {
		return errors.New("GitHub repository is not configured")
	}
	if number <= 0 {
		return errors.New("pull request number is required")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh not found; run setup after installing GitHub CLI")
	}
	if _, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "pr", "review", strconv.Itoa(number), "--approve", "--repo", cfg.RepoSlug); err != nil {
		return fmt.Errorf("approve PR #%d failed: %w", number, err)
	}
	return nil
}

func loadRemotePullRequestFiles(ctx context.Context, cfg core.Config, number int) ([]core.PullFile, error) {
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restFiles []restPullFile
	if err := json.Unmarshal([]byte(output), &restFiles); err != nil {
		return nil, err
	}
	files := make([]core.PullFile, 0, len(restFiles))
	for _, file := range restFiles {
		files = append(files, core.PullFile{Path: file.Filename, Additions: file.Additions, Deletions: file.Deletions})
	}
	return files, nil
}

func loadRemotePullRequestComments(ctx context.Context, cfg core.Config, number int) ([]core.PullComment, error) {
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/issues/%d/comments?per_page=30", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restComments []restIssueComment
	if err := json.Unmarshal([]byte(output), &restComments); err != nil {
		return nil, err
	}
	comments := make([]core.PullComment, 0, len(restComments))
	for _, comment := range restComments {
		comments = append(comments, core.PullComment{Author: githubActorFromREST(comment.User), Body: comment.Body, CreatedAt: comment.CreatedAt, URL: comment.HTMLURL})
	}
	return comments, nil
}

func loadRemotePullRequestCommits(ctx context.Context, cfg core.Config, number int) ([]core.PullCommit, error) {
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d/commits?per_page=30", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restCommits []restPullCommit
	if err := json.Unmarshal([]byte(output), &restCommits); err != nil {
		return nil, err
	}
	commits := make([]core.PullCommit, 0, len(restCommits))
	for _, commit := range restCommits {
		commits = append(commits, core.PullCommit{Oid: commit.SHA, MessageHeadline: firstLine(commit.Commit.Message), CommittedDate: commit.Commit.Author.Date})
	}
	return commits, nil
}

func loadRemotePullRequestReviews(ctx context.Context, cfg core.Config, number int) ([]core.PullReview, error) {
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restReviews []restPullReview
	if err := json.Unmarshal([]byte(output), &restReviews); err != nil {
		return nil, err
	}
	reviews := make([]core.PullReview, 0, len(restReviews))
	for _, review := range restReviews {
		reviews = append(reviews, core.PullReview{
			Author:      githubActorFromREST(review.User),
			State:       review.State,
			Body:        review.Body,
			SubmittedAt: review.SubmittedAt,
			URL:         review.HTMLURL,
		})
	}
	return reviews, nil
}

func loadRemotePullRequestChecks(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error) {
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/commits/%s/check-runs?per_page=100", cfg.RepoSlug, sha), "-H", "Accept: application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	var runs restCheckRuns
	if err := json.Unmarshal([]byte(output), &runs); err != nil {
		return nil, err
	}
	checks := make([]core.StatusCheck, 0, len(runs.CheckRuns))
	for _, run := range runs.CheckRuns {
		url := run.HTMLURL
		if url == "" {
			url = run.DetailsURL
		}
		checks = append(checks, core.StatusCheck{Name: run.Name, Status: run.Status, Conclusion: run.Conclusion, URL: url, Summary: run.Output.Summary})
	}
	return checks, nil
}

func loadGitHubCurrentUser(ctx context.Context, cfg core.Config) (string, error) {
	if cfg.RepoSlug == "" {
		return "", errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return "", errors.New("gh not found")
	}
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", "user")
	if err != nil {
		return "", err
	}
	var actor restActor
	if err := json.Unmarshal([]byte(output), &actor); err != nil {
		return "", err
	}
	return strings.TrimSpace(actor.Login), nil
}

func fetchPullRequestIndex(ctx context.Context, cfg core.Config) ([]core.PullRequest, error) {
	if cfg.RepoSlug == "" {
		return nil, errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("gh not found; run setup after installing GitHub CLI")
	}
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "pr", "list", "--repo", cfg.RepoSlug, "--state", "all", "--limit", "200", "--json", "headRefName,number,title,url,state,body,author,isCrossRepository")
	if err != nil {
		return nil, err
	}
	var prs []core.PullRequest
	if err := json.Unmarshal([]byte(output), &prs); err != nil {
		return nil, fmt.Errorf("gh returned invalid PR JSON: %w", err)
	}
	return prs, nil
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func branchPullRequests(ctx context.Context, cfg core.Config, branch string) ([]core.PullRequest, error) {
	output, err := shell.Run(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "pr", "list", "--repo", cfg.RepoSlug, "--head", branch, "--state", "all", "--limit", "20", "--json", "headRefName,number,state,isCrossRepository")
	if err != nil {
		return nil, err
	}
	var prs []core.PullRequest
	if err := json.Unmarshal([]byte(output), &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

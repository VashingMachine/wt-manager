// Package github is the GitHub integration. It talks to GitHub through the gh
// command line, so it uses whatever account and host gh is logged in to.
//
// Client is the whole surface the rest of the app sees. pullrequests.go holds
// the calls, rest.go the JSON shapes of GitHub's REST API and how they become
// core types.
package github

import (
	"context"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// Client runs gh.
type Client struct{}

func New() *Client {
	return &Client{}
}

// CheckCLI returns the problems that would keep pull requests from loading:
// gh missing, not logged in, or without access to the repository.
func (c *Client) CheckCLI(ctx context.Context, repoSlug string) []string {
	return checkGitHubCLI(ctx, repoSlug)
}

// Available reports why pull requests cannot be read for cfg at all: no
// GitHub repository configured, or gh not installed.
func (c *Client) Available(cfg core.Config) error {
	return requireGitHub(cfg)
}

// PullRequests lists the repository's recent pull requests, newest first,
// with one request. Check runs are not included; see Checks.
func (c *Client) PullRequests(ctx context.Context, cfg core.Config) ([]core.RemotePullRequest, error) {
	return loadRemotePullRequests(ctx, cfg)
}

// PullRequestDetail reads one pull request with its files, comments, commits,
// reviews and diff. checks reads the check runs of its head commit, which lets
// the caller serve them from a cache.
func (c *Client) PullRequestDetail(ctx context.Context, cfg core.Config, number int, checks func(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error)) (core.RemotePullRequest, error) {
	return loadRemotePullRequestDetailWith(ctx, cfg, number, checks)
}

// Checks reads the check runs of a commit.
func (c *Client) Checks(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error) {
	return loadRemotePullRequestChecks(ctx, cfg, sha)
}

// Approve approves a pull request as the logged-in user.
func (c *Client) Approve(ctx context.Context, cfg core.Config, number int) error {
	return approveRemotePullRequest(ctx, cfg, number)
}

// CurrentUser returns the login gh is authenticated as.
func (c *Client) CurrentUser(ctx context.Context, cfg core.Config) (string, error) {
	return loadGitHubCurrentUser(ctx, cfg)
}

// PullRequestIndex lists the pull requests used to label branches.
func (c *Client) PullRequestIndex(ctx context.Context, cfg core.Config) ([]core.PullRequest, error) {
	return fetchPullRequestIndex(ctx, cfg)
}

// BranchPullRequests lists the pull requests opened from one branch, whatever
// their state. The index covers only the latest pull requests of a
// repository, and in a busy one a branch merged a few days ago is no longer
// among them.
func (c *Client) BranchPullRequests(ctx context.Context, cfg core.Config, branch string) ([]core.PullRequest, error) {
	return branchPullRequests(ctx, cfg, branch)
}

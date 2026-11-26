// Package git is the git integration: everything wt-manager asks of the git
// command line. It lists, creates and removes worktrees, reads their status,
// keeps stacked branches (stack.go) and finds worktrees nobody uses any more
// (sweep.go).
//
// Client is the whole surface the rest of the app sees; the functions behind
// it are unexported and tested here against throwaway repositories.
package git

import (
	"context"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// Client runs git. It remembers recent fetches so worktree creations queued
// behind each other share one.
type Client struct {
	fetches fetchCoalescer
}

func New() *Client {
	return &Client{}
}

// RepoRootFromCWD returns the repository the process was started in.
func (c *Client) RepoRootFromCWD() (string, bool) {
	return inferRepoFromCWD()
}

// RepoRoot returns the top-level folder of the repository containing dir.
func (c *Client) RepoRoot(ctx context.Context, dir string) (string, bool) {
	return repoRootFromDir(ctx, dir)
}

// Remotes lists the remotes configured in a repository.
func (c *Client) Remotes(ctx context.Context, repoPath string) []string {
	return gitRemotes(ctx, repoPath)
}

// GitHubRepo returns the owner/name slug the profile's remote points at, or
// "" when the remote is not on GitHub.
func (c *Client) GitHubRepo(ctx context.Context, profile core.RepositoryProfile) string {
	return inferGitHubRepo(ctx, profile)
}

// DiscoverWorktrees lists every worktree of the repository cfg is scoped to,
// labelled with its repository and stack link. It reads no status.
func (c *Client) DiscoverWorktrees(ctx context.Context, cfg core.Config) ([]core.Worktree, error) {
	return discoverWorktrees(ctx, cfg)
}

// ReadWorktreeStatus fills in the status, last commit and stack parent state
// of one worktree, and returns a warning for each part that could not be read.
func (c *Client) ReadWorktreeStatus(ctx context.Context, wt *core.Worktree) []string {
	return readWorktreeStatus(ctx, wt)
}

// CreateWorktree creates a worktree for branch at path: the local branch when
// it exists, else the remote one, else a new branch off the remote's default.
// It reports true when path already was a worktree of the repository.
func (c *Client) CreateWorktree(ctx context.Context, cfg core.Config, branch string, path string) (bool, error) {
	return createWorktreeWith(ctx, cfg, branch, path, c.fetches.fetch)
}

// CreateStackedWorktree creates a worktree for a new branch started from the
// tip of the local branch parent, and records the stack link.
func (c *Client) CreateStackedWorktree(ctx context.Context, cfg core.Config, branch string, path string, parent string) error {
	return createStackedWorktree(ctx, cfg, branch, path, parent)
}

// SetStackParent records the branch a worktree's branch is stacked on; an
// empty parent takes it out of its stack.
func (c *Client) SetStackParent(ctx context.Context, cfg core.Config, wt core.Worktree, parent string) error {
	return setStackParent(ctx, cfg, wt, parent)
}

// RestackWorktree rebases a stacked worktree onto its parent, or past parents
// that are merged or gone. merged answers for branches the caller's options do
// not list, such as by asking the forge.
func (c *Client) RestackWorktree(ctx context.Context, cfg core.Config, wt core.Worktree, options core.RestackOptions, merged func(ctx context.Context, branch string) bool) (core.RestackResult, error) {
	return restackWorktree(ctx, cfg, wt, options, c.fetches.fetch, merged)
}

// SweepCandidates lists the clean managed worktrees untouched for olderThan,
// oldest first, as of now.
func (c *Client) SweepCandidates(ctx context.Context, cfg core.Config, olderThan time.Duration, now time.Time) ([]core.SweepCandidate, error) {
	return sweepCandidates(ctx, cfg, olderThan, now)
}

// RemoveWorktree removes a worktree, or prunes its entry when the folder is gone.
func (c *Client) RemoveWorktree(ctx context.Context, cfg core.Config, wt core.Worktree) error {
	return removeWorktree(ctx, cfg, wt)
}

// EnsurePullRequestWorktree returns a worktree holding the pull request's
// head, creating one when none exists, and reports whether it existed.
func (c *Client) EnsurePullRequestWorktree(ctx context.Context, cfg core.Config, pullRequest core.RemotePullRequest) (core.Worktree, bool, error) {
	return ensurePullRequestWorktreeWith(ctx, cfg, pullRequest, c.fetches.fetch)
}

package services

import (
	"context"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// Integrations are the outside systems the application drives: its driven
// ports. Each interface lists exactly what the application needs from one
// system, and is implemented by the package of the same name under
// internal/integrations.
type Integrations struct {
	Config   ConfigStore
	Git      Git
	GitHub   GitHub
	Jira     Jira
	Terminal Terminal
	Editor   Editor
	Browser  Browser
	Agents   AgentRunner
}

// ConfigStore keeps the user's settings.
type ConfigStore interface {
	// Load reads the settings kept for homeDir and reports where they are
	// stored and whether they existed. Missing settings give the defaults.
	Load(homeDir string) (appConfig core.AppConfig, configPath string, existed bool, err error)
	// Read reads and normalizes settings without writing them back.
	Read(configPath string) (core.AppConfig, error)
	Write(configPath string, appConfig core.AppConfig) error
}

// Git is a git repository and its worktrees on disk.
type Git interface {
	// RepoRootFromCWD returns the repository the process was started in.
	RepoRootFromCWD() (string, bool)
	RepoRoot(ctx context.Context, dir string) (string, bool)
	Remotes(ctx context.Context, repoPath string) []string
	// GitHubRepo returns the owner/name slug the profile's remote points at,
	// or "" when it is not on GitHub.
	GitHubRepo(ctx context.Context, profile core.RepositoryProfile) string
	// DiscoverWorktrees lists every worktree of the repository cfg is scoped
	// to, with its stack link and without status.
	DiscoverWorktrees(ctx context.Context, cfg core.Config) ([]core.Worktree, error)
	// ReadWorktreeStatus fills in status and last commit of one worktree and
	// returns a warning for each part that could not be read.
	ReadWorktreeStatus(ctx context.Context, wt *core.Worktree) []string
	// CreateWorktree reports true when path already was a worktree.
	CreateWorktree(ctx context.Context, cfg core.Config, branch string, path string) (bool, error)
	CreateStackedWorktree(ctx context.Context, cfg core.Config, branch string, path string, parent string) error
	SetStackParent(ctx context.Context, cfg core.Config, wt core.Worktree, parent string) error
	// RestackWorktree asks merged about branches options does not list.
	RestackWorktree(ctx context.Context, cfg core.Config, wt core.Worktree, options core.RestackOptions, merged func(ctx context.Context, branch string) bool) (core.RestackResult, error)
	SweepCandidates(ctx context.Context, cfg core.Config, olderThan time.Duration, now time.Time) ([]core.SweepCandidate, error)
	RemoveWorktree(ctx context.Context, cfg core.Config, wt core.Worktree) error
	// EnsurePullRequestWorktree reports true when the worktree already existed.
	EnsurePullRequestWorktree(ctx context.Context, cfg core.Config, pullRequest core.RemotePullRequest) (core.Worktree, bool, error)
}

// GitHub is the forge holding the pull requests of a repository.
type GitHub interface {
	// CheckCLI returns the problems that would keep pull requests from loading.
	CheckCLI(ctx context.Context, repoSlug string) []string
	// Available reports why pull requests cannot be read for cfg at all.
	Available(cfg core.Config) error
	// PullRequests lists recent pull requests without their check runs.
	PullRequests(ctx context.Context, cfg core.Config) ([]core.RemotePullRequest, error)
	// PullRequestDetail reads one pull request in full; checks supplies the
	// check runs of its head commit.
	PullRequestDetail(ctx context.Context, cfg core.Config, number int, checks func(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error)) (core.RemotePullRequest, error)
	Checks(ctx context.Context, cfg core.Config, sha string) ([]core.StatusCheck, error)
	Approve(ctx context.Context, cfg core.Config, number int) error
	CurrentUser(ctx context.Context, cfg core.Config) (string, error)
	// PullRequestIndex lists the pull requests used to label branches.
	PullRequestIndex(ctx context.Context, cfg core.Config) ([]core.PullRequest, error)
	// BranchPullRequests lists the pull requests opened from one branch.
	BranchPullRequests(ctx context.Context, cfg core.Config, branch string) ([]core.PullRequest, error)
}

// Jira is the ticket tracker.
type Jira interface {
	// CheckCLI returns the problems that would keep tickets from loading.
	CheckCLI(ctx context.Context) []string
	Site(ctx context.Context) (string, error)
	ResolveBoard(ctx context.Context, link string) (core.JiraBoard, error)
	SearchBoards(ctx context.Context, name string) ([]core.JiraBoard, error)
	// Tickets lists the tickets of one board or space.
	Tickets(ctx context.Context, cfg core.Config, board core.JiraBoard, mineOnly bool, sprintOnly bool) ([]core.Ticket, error)
	TicketDetail(ctx context.Context, cfg core.Config, key string) (core.Ticket, error)
	Transition(ctx context.Context, key string, status string) error
	Comment(ctx context.Context, key string, body string) error
	Assign(ctx context.Context, key string, assignee string) error
}

// Terminal is the terminal app wt-manager runs in.
type Terminal interface {
	OpenAgent(ctx context.Context, wt core.Worktree, agent core.AgentTool) error
	OpenTerminal(ctx context.Context, wt core.Worktree) error
	// Notify posts a notification where the terminal app has them.
	Notify(ctx context.Context, title string, body string) error
	// OpenURL shows a page inside the terminal app and reports whether it
	// did; false means the system browser should be used.
	OpenURL(ctx context.Context, url string) bool
}

// Editor opens worktrees and files in the user's editor.
type Editor interface {
	OpenWorktree(ctx context.Context, wt core.Worktree, editor core.EditorTool) error
	OpenFile(ctx context.Context, path string, editor core.EditorTool) error
	OpenVSCodeWorktree(ctx context.Context, wt core.Worktree) error
}

// Browser is the system browser.
type Browser interface {
	Open(ctx context.Context, url string) error
}

// AgentRunner runs a coding agent for a one-off question.
type AgentRunner interface {
	RunWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error)
}

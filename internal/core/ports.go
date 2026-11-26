package core

// This file lists the driving ports: everything a UI (the terminal UI in
// internal/tui, the commands in internal/cli) can ask the application to do
// that reaches outside the process. internal/services implements them.
//
// Rules that need no outside world are plain functions of this package
// instead (FilterTickets, NormalizeAppConfig, CreateWorktreePath, ...), so a
// UI calls them directly and a test needs no fake.

import (
	"context"
	"time"
)

type ConfigService interface {
	DefaultConfig(forceSetup bool) (Config, error)
	// ResolveProject resolves a project for use: paths expanded, every
	// repository listed with its GitHub slug (inferred from its remote when
	// not configured), and the flat fields scoped to the first repository.
	ResolveProject(ctx context.Context, profile RepositoryProfile, homeDir string) RepositoryProfile
	// ReadAppConfig reads and normalizes the config file without rewriting
	// it, so edits made outside the app can be picked up or reported.
	ReadAppConfig(configPath string) (AppConfig, error)
	InferRepoFromCWD() (string, bool)
	RepoRootFromDir(ctx context.Context, dir string) (string, bool)
	ProfileFromRepo(ctx context.Context, homeDir string, repoPath string) RepositoryProfile
	InferGitHubRepo(ctx context.Context, profile RepositoryProfile) string
	// GitRemotes lists the remotes configured in a repository.
	GitRemotes(ctx context.Context, repoPath string) []string
	WriteAppConfig(configPath string, appConfig AppConfig) error
	CheckGitHubCLI(ctx context.Context, repoSlug string) []string
}

type WorktreeService interface {
	CollectWorktrees(ctx context.Context, cfg Config, showAll bool) ([]Worktree, string, error)
	CollectTicketWorktrees(ctx context.Context, cfg Config) ([]Worktree, string, error)
	// PeekWorktrees returns the last discovered worktrees with their cached
	// status, without running git, so the first frame is not empty.
	PeekWorktrees(cfg Config, showAll bool) ([]Worktree, bool)
	// DiscoverWorktrees runs only `git worktree list`. Status and commit come
	// from the cache; worktrees without cached status have StatusPending set.
	DiscoverWorktrees(ctx context.Context, cfg Config, showAll bool) ([]Worktree, error)
	// LoadWorktreeStatus reads git status and the last commit of one worktree
	// and caches them.
	LoadWorktreeStatus(ctx context.Context, cfg Config, wt Worktree) (Worktree, error)
	CreateWorktree(ctx context.Context, cfg Config, branch string, path string) (bool, error)
	// CreateStackedWorktree creates a worktree for a new branch started from
	// the tip of the local branch parent, and records parent as the branch it
	// is stacked on.
	CreateStackedWorktree(ctx context.Context, cfg Config, branch string, path string, parent string) error
	// SetStackParent records the branch a worktree's branch is stacked on. An
	// empty parent takes the worktree out of its stack.
	SetStackParent(ctx context.Context, cfg Config, wt Worktree, parent string) error
	// RestackWorktree rebases the commits of a stacked worktree's own branch
	// onto its parent, or, once the parent is merged or gone, onto the nearest
	// surviving branch below it in the stack and finally the default branch.
	RestackWorktree(ctx context.Context, cfg Config, wt Worktree, options RestackOptions) (RestackResult, error)
	// SweepCandidates lists the repository's managed worktrees that have no
	// uncommitted work and saw no activity for olderThan, oldest first.
	SweepCandidates(ctx context.Context, cfg Config, olderThan time.Duration) ([]SweepCandidate, error)
	RemoveWorktree(ctx context.Context, cfg Config, wt Worktree) error
	EnsurePullRequestWorktree(ctx context.Context, cfg Config, pullRequest RemotePullRequest) (Worktree, bool, error)
}

type PullRequestService interface {
	// LoadRemotePullRequests lists PRs with a single API call and overlays
	// cached check runs; LoadRemotePullRequestChecks fills in the rest.
	LoadRemotePullRequests(ctx context.Context, cfg Config) ([]RemotePullRequest, error)
	PeekRemotePullRequests(cfg Config) ([]RemotePullRequest, time.Time, bool)
	// LoadRemotePullRequestChecks returns check runs for a head commit, reusing
	// cached runs while they are fresh (longer once every run has completed).
	LoadRemotePullRequestChecks(ctx context.Context, cfg Config, sha string) ([]StatusCheck, error)
	// LoadPullRequestIndex returns the PRs used to label worktrees and tickets,
	// from the cache while it is younger than its TTL unless refresh is set. A
	// failed refetch falls back to cached PRs with a warning.
	LoadPullRequestIndex(ctx context.Context, cfg Config, refresh bool) ([]PullRequest, string, error)
	PeekPullRequestIndex(cfg Config) ([]PullRequest, time.Time, bool)
	LoadGitHubCurrentUser(ctx context.Context, cfg Config) (string, error)
	LoadRemotePullRequestDetail(ctx context.Context, cfg Config, number int) (RemotePullRequest, error)
	PeekRemotePullRequestDetail(cfg Config, number int) (RemotePullRequest, time.Time, bool)
	ApproveRemotePullRequest(ctx context.Context, cfg Config, number int) error
}

type AgentService interface {
	RunAgentWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error)
}

type OpenerService interface {
	OpenBrowser(ctx context.Context, url string) error
	OpenVSCodeWorktree(ctx context.Context, wt Worktree) error
	OpenEditorWorktree(ctx context.Context, wt Worktree, editor EditorTool) error
	// OpenEditorFile opens a single file, such as the config, in the editor.
	OpenEditorFile(ctx context.Context, path string, editor EditorTool) error
	OpenAgent(ctx context.Context, wt Worktree, agent AgentTool) error
	OpenTerminal(ctx context.Context, wt Worktree) error
	// Notify posts a notification in the terminal host; it is a no-op outside cmux.
	Notify(ctx context.Context, title string, body string) error
}

type JiraService interface {
	CheckAtlassianCLI(ctx context.Context) []string
	JiraSite(ctx context.Context) (string, error)
	// ResolveJiraBoard turns a board link or id into a board, and any other
	// link or key inside a space into that whole space.
	ResolveJiraBoard(ctx context.Context, link string) (JiraBoard, error)
	SearchJiraBoards(ctx context.Context, name string) ([]JiraBoard, error)
	PeekTicketCache(cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) ([]Ticket, time.Time, bool)
	LoadTickets(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool, refresh bool) ([]Ticket, string, error)
	LoadTicketDetail(ctx context.Context, cfg Config, key string) (Ticket, error)
	PeekTicketDetail(cfg Config, key string) (Ticket, bool)
	TransitionTicket(ctx context.Context, key string, status string) error
	CommentTicket(ctx context.Context, key string, body string) error
	AssignTicket(ctx context.Context, key string, assignee string) error
	// InvalidateTicketCache marks every cached scope of a board stale: rows
	// still paint instantly, but the next load revalidates them.
	InvalidateTicketCache(cfg Config, board JiraBoard) error
	// ApplyTicketPatch writes a confirmed mutation into every cached copy of
	// the ticket and marks the board's lists stale.
	ApplyTicketPatch(cfg Config, board JiraBoard, patch TicketPatch)
}

type Services interface {
	ConfigService
	WorktreeService
	PullRequestService
	AgentService
	OpenerService
	JiraService
}

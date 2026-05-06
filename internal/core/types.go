package core

import "context"

type Config struct {
	HomeDir       string
	ConfigPath    string
	App           AppConfig
	ActiveProfile RepositoryProfile
	RepoSlug      string
	ConfigExists  bool
	SetupNeeded   bool
	SetupReason   string
	SetupRepo     string
}

type AppConfig struct {
	DefaultProfile string              `json:"defaultProfile"`
	Profiles       []RepositoryProfile `json:"profiles"`
	DefaultAgent   string              `json:"defaultAgent"`
	Agents         []AgentTool         `json:"agents"`
}

type RepositoryProfile struct {
	Name           string `json:"name"`
	RepositoryPath string `json:"repositoryPath"`
	WorktreesDir   string `json:"worktreesDir"`
	RemoteName     string `json:"remoteName"`
	GitHubRepo     string `json:"githubRepo"`
}

type AgentTool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

type Worktree struct {
	Name      string
	Path      string
	Branch    string
	Head      string
	Missing   bool
	Detached  bool
	Prunable  bool
	IsMain    bool
	Managed   bool
	Status    RepoStatus
	Commit    CommitInfo
	PR        *PullRequest
	LoadError string
}

type RepoStatus struct {
	Dirty     bool
	Ahead     int
	Behind    int
	Staged    int
	Unstaged  int
	Untracked int
	Conflicts int
	Files     []string
}

type CommitInfo struct {
	Hash     string
	Relative string
	Subject  string
}

type PullRequest struct {
	HeadRefName string `json:"headRefName"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	State       string `json:"state"`
	Body        string `json:"body"`
	Author      struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"author"`
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
	Diff              string          `json:"-"`
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

type ConfigService interface {
	DefaultConfig(forceSetup bool) (Config, error)
	DefaultAppConfig() AppConfig
	NormalizeAppConfig(appConfig AppConfig) (AppConfig, bool, error)
	ActiveProfile(appConfig AppConfig, homeDir string) (RepositoryProfile, error)
	ResolveProfilePaths(profile RepositoryProfile, homeDir string) RepositoryProfile
	FindAgent(agents []AgentTool, name string) *AgentTool
	FindProfile(profiles []RepositoryProfile, name string) *RepositoryProfile
	ExpandPath(path string, homeDir string) string
	CompactPath(path string, homeDir string) string
	CanonicalPath(path string) string
	InferRepoFromCWD() (string, bool)
	RepoRootFromDir(ctx context.Context, dir string) (string, bool)
	ProfileFromRepo(ctx context.Context, homeDir string, repoPath string) RepositoryProfile
	InferGitHubRepo(ctx context.Context, profile RepositoryProfile) string
	WriteAppConfig(configPath string, appConfig AppConfig) error
	CheckGitHubCLI(ctx context.Context, repoSlug string) []string
}

type WorktreeService interface {
	CollectWorktrees(ctx context.Context, cfg Config, showAll bool) ([]Worktree, string, error)
	CreateWorktree(ctx context.Context, cfg Config, branch string, path string) (bool, error)
	RemoveWorktree(ctx context.Context, cfg Config, wt Worktree) error
	EnsurePullRequestWorktree(ctx context.Context, cfg Config, pullRequest RemotePullRequest) (Worktree, bool, error)
	CreateWorktreePath(cfg Config, name string) string
}

type PullRequestService interface {
	LoadRemotePullRequests(ctx context.Context, cfg Config) ([]RemotePullRequest, error)
	LoadGitHubCurrentUser(ctx context.Context, cfg Config) (string, error)
	LoadRemotePullRequestDetail(ctx context.Context, cfg Config, number int) (RemotePullRequest, error)
	FilterRemotePullRequestsWithAuthor(pullRequests []RemotePullRequest, query string, state string, user string, author string) []RemotePullRequest
	RemotePRUsers(pullRequests []RemotePullRequest) []string
}

type AgentService interface {
	BuildPullRequestChatPrompt(pr RemotePullRequest, question string) string
	RunAgentWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error)
}

type OpenerService interface {
	OpenBrowser(ctx context.Context, url string) error
	OpenVSCodeWorktree(ctx context.Context, wt Worktree) error
	OpenAgent(ctx context.Context, wt Worktree, agent AgentTool) error
}

type Services interface {
	ConfigService
	WorktreeService
	PullRequestService
	AgentService
	OpenerService
}

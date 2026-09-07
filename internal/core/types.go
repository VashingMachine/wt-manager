package core

import (
	"context"
	"time"
)

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
	DefaultProfile     string              `json:"defaultProfile"`
	Profiles           []RepositoryProfile `json:"profiles"`
	DefaultAgent       string              `json:"defaultAgent"`
	Agents             []AgentTool         `json:"agents"`
	DefaultEnterAction string              `json:"defaultEnterAction"`
}

const (
	EnterActionVSCode   = "vscode"
	EnterActionAgent    = "agent"
	EnterActionTerminal = "terminal"
)

type RepositoryProfile struct {
	Name           string      `json:"name"`
	RepositoryPath string      `json:"repositoryPath"`
	WorktreesDir   string      `json:"worktreesDir"`
	RemoteName     string      `json:"remoteName"`
	GitHubRepo     string      `json:"githubRepo"`
	Jira           *JiraConfig `json:"jira,omitempty"`
}

// JiraBoard is a board resolved from a pasted board URL. ID/Name/Projects are
// filled once by ResolveJiraBoard and then persisted, so the URL only has to be
// parsed and looked up during setup.
type JiraBoard struct {
	URL      string   `json:"url"`
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Projects []string `json:"projects"`
}

// JiraConfig is the per-repository JIRA setup written under a profile.
type JiraConfig struct {
	Site              string      `json:"site"`
	Boards            []JiraBoard `json:"boards"`
	DefaultBoard      string      `json:"defaultBoard"`
	MineOnly          bool        `json:"mineOnly"`
	SprintOnly        bool        `json:"sprintOnly"`
	OpenOnly          bool        `json:"openOnly"`
	FiltersConfigured bool        `json:"filtersConfigured,omitempty"`
	Limit             int         `json:"limit"`
}

// Board returns the configured board with the given name, or nil.
func (c *JiraConfig) Board(name string) *JiraBoard {
	if c == nil {
		return nil
	}
	for i := range c.Boards {
		if c.Boards[i].Name == name {
			return &c.Boards[i]
		}
	}
	return nil
}

// ActiveBoard returns the board named by DefaultBoard, falling back to the
// first configured board.
func (c *JiraConfig) ActiveBoard() *JiraBoard {
	if c == nil || len(c.Boards) == 0 {
		return nil
	}
	if board := c.Board(c.DefaultBoard); board != nil {
		return board
	}
	return &c.Boards[0]
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
	Reviews           []PullReview    `json:"reviews"`
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

type PullReview struct {
	Author      GitHubActor `json:"author"`
	State       string      `json:"state"`
	Body        string      `json:"body"`
	SubmittedAt string      `json:"submittedAt"`
	URL         string      `json:"url"`
}

// JiraUser identifies a JIRA account. Email is often absent depending on the
// site's privacy settings, so DisplayName is the only reliably present field.
type JiraUser struct {
	AccountID   string
	DisplayName string
	Email       string
}

// Name is the best available human label for the user.
func (u JiraUser) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Email != "" {
		return u.Email
	}
	return u.AccountID
}

type TicketComment struct {
	Author  string
	Body    string
	Created string
}

// Ticket is a JIRA work item. Description/Comments are only populated once the
// detail fetch has run (Detail == true); Worktree/PR are local joins computed by
// AttachTicketContext rather than anything JIRA returns.
type Ticket struct {
	Key            string
	Summary        string
	Type           string
	Priority       string
	Status         string
	StatusCategory string
	Assignee       JiraUser
	Reporter       JiraUser
	Labels         []string
	URL            string
	Description    string
	Comments       []TicketComment
	Detail         bool
	Worktree       *Worktree
	PR             *PullRequest
	LoadError      string
}

// JIRA status category keys, used for colouring instead of status names so that
// arbitrary project workflows render correctly.
const (
	StatusCategoryNew        = "new"
	StatusCategoryInProgress = "indeterminate"
	StatusCategoryDone       = "done"
	TicketAssigneeSelf       = "@me"
)

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
	ApproveRemotePullRequest(ctx context.Context, cfg Config, number int) error
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
	OpenTerminal(ctx context.Context, wt Worktree) error
}

type JiraService interface {
	CheckAtlassianCLI(ctx context.Context) []string
	JiraSite(ctx context.Context) (string, error)
	ResolveJiraBoard(ctx context.Context, boardURLOrID string) (JiraBoard, error)
	SearchJiraBoards(ctx context.Context, name string) ([]JiraBoard, error)
	PeekTicketCache(cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) ([]Ticket, time.Time, bool)
	LoadTickets(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool, refresh bool) ([]Ticket, string, error)
	LoadTicketDetail(ctx context.Context, cfg Config, key string) (Ticket, error)
	TransitionTicket(ctx context.Context, key string, status string) error
	CommentTicket(ctx context.Context, key string, body string) error
	AssignTicket(ctx context.Context, key string, assignee string) error
	AttachTicketContext(tickets []Ticket, worktrees []Worktree, pullRequests []PullRequest) []Ticket
	FilterTickets(tickets []Ticket, query string, status string, openOnly bool) []Ticket
	TicketStatuses(tickets []Ticket) []string
	TicketAssignees(tickets []Ticket) []JiraUser
	TicketBranchName(ticket Ticket) string
	TicketKeyFromBranch(branch string) string
	BuildTicketChatPrompt(ticket Ticket, question string) string
	InvalidateTicketCache(cfg Config, board JiraBoard) error
}

type Services interface {
	ConfigService
	WorktreeService
	PullRequestService
	AgentService
	OpenerService
	JiraService
}

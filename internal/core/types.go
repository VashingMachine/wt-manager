package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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
	Editor             EditorTool          `json:"editor"`
}

const (
	EnterActionVSCode   = "vscode"
	EnterActionAgent    = "agent"
	EnterActionTerminal = "terminal"
)

// RepositoryProfile is a project: one or more repositories managed together,
// sharing a JIRA setup. A single-repository project keeps the flat
// repositoryPath/worktreesDir/remoteName/githubRepo fields; a project with
// several repositories lists them under repositories instead.
//
// In a resolved Config.ActiveProfile, Repositories always lists every
// repository and the flat fields describe the repository the Config is scoped
// to (see Config.ForRepository), so per-repository services keep reading them.
type RepositoryProfile struct {
	Name           string       `json:"name"`
	RepositoryPath string       `json:"repositoryPath,omitempty"`
	WorktreesDir   string       `json:"worktreesDir,omitempty"`
	RemoteName     string       `json:"remoteName,omitempty"`
	GitHubRepo     string       `json:"githubRepo,omitempty"`
	Repositories   []Repository `json:"repositories,omitempty"`
	// LastRepository names the repository the last worktree was created in,
	// offered first the next time the app asks for one.
	LastRepository string      `json:"lastRepository,omitempty"`
	Jira           *JiraConfig `json:"jira,omitempty"`
}

// Repository is one git repository of a project.
type Repository struct {
	Name           string `json:"name"`
	RepositoryPath string `json:"repositoryPath"`
	WorktreesDir   string `json:"worktreesDir"`
	RemoteName     string `json:"remoteName,omitempty"`
	GitHubRepo     string `json:"githubRepo,omitempty"`
}

// RepositoryList returns the project's repositories in configured order: the
// repositories list, or the flat fields as the only repository.
func (p RepositoryProfile) RepositoryList() []Repository {
	if len(p.Repositories) > 0 {
		return p.Repositories
	}
	if strings.TrimSpace(p.RepositoryPath) == "" {
		return nil
	}
	return []Repository{{
		Name:           RepositoryNameFromPath(p.RepositoryPath),
		RepositoryPath: p.RepositoryPath,
		WorktreesDir:   p.WorktreesDir,
		RemoteName:     p.RemoteName,
		GitHubRepo:     p.GitHubRepo,
	}}
}

// Repository returns the repository with the given name, or nil.
func (p RepositoryProfile) Repository(name string) *Repository {
	repositories := p.RepositoryList()
	for i := range repositories {
		if repositories[i].Name == name {
			return &repositories[i]
		}
	}
	return nil
}

// WithRepositories returns the profile listing repositories in place of its
// flat single-repository fields.
func (p RepositoryProfile) WithRepositories(repositories []Repository) RepositoryProfile {
	p.RepositoryPath, p.WorktreesDir, p.RemoteName, p.GitHubRepo = "", "", "", ""
	p.Repositories = repositories
	if p.LastRepository != "" && p.Repository(p.LastRepository) == nil {
		p.LastRepository = ""
	}
	return p
}

// UniqueRepositoryName returns name, with a numeric suffix when the project
// already has a repository of that name.
func (p RepositoryProfile) UniqueRepositoryName(name string) string {
	if name == "" {
		name = "repository"
	}
	candidate := name
	for suffix := 2; p.Repository(candidate) != nil; suffix++ {
		candidate = fmt.Sprintf("%s-%d", name, suffix)
	}
	return candidate
}

// SuggestWorktreesDir proposes the worktrees folder of a repository joining a
// project, following the project's layout: repositories keeping worktrees in
// a folder named after themselves (~/worktrees/api) suggest a sibling named
// after the new one; otherwise the first repository's folder is shared, where
// worktree folders get the repository name as a prefix. A project without
// repositories keeps worktrees next to the repository, as setup does.
func SuggestWorktreesDir(repositories []Repository, name string, repoPath string) string {
	for _, repository := range repositories {
		base := filepath.Base(repository.WorktreesDir)
		if base == repository.Name || base == filepath.Base(repository.RepositoryPath) {
			return filepath.Join(filepath.Dir(repository.WorktreesDir), name)
		}
	}
	if len(repositories) > 0 && repositories[0].WorktreesDir != "" {
		return repositories[0].WorktreesDir
	}
	return filepath.Dir(repoPath)
}

// RepositoryNameFromPath is the default repository name: its folder name.
func RepositoryNameFromPath(path string) string {
	path = strings.TrimRight(strings.TrimSpace(path), `/\`)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// Repositories lists the active project's repositories.
func (c Config) Repositories() []Repository {
	repositories := c.ActiveProfile.RepositoryList()
	if len(c.ActiveProfile.Repositories) == 0 && len(repositories) == 1 && repositories[0].GitHubRepo == "" {
		repositories[0].GitHubRepo = c.RepoSlug
	}
	return repositories
}

// MultiRepository reports whether the active project has more than one
// repository, which is when the UI shows and asks for repositories.
func (c Config) MultiRepository() bool {
	return len(c.ActiveProfile.Repositories) > 1
}

// RepositoryName names the repository the Config is scoped to.
func (c Config) RepositoryName() string {
	for _, repository := range c.Repositories() {
		if repository.RepositoryPath == c.ActiveProfile.RepositoryPath {
			return repository.Name
		}
	}
	return RepositoryNameFromPath(c.ActiveProfile.RepositoryPath)
}

// ForRepository scopes the Config to one repository of the active project: the
// profile's flat fields and RepoSlug describe that repository afterwards. An
// unknown name leaves the Config unchanged.
func (c Config) ForRepository(name string) Config {
	for _, repository := range c.ActiveProfile.Repositories {
		if repository.Name != name {
			continue
		}
		c.ActiveProfile.RepositoryPath = repository.RepositoryPath
		c.ActiveProfile.WorktreesDir = repository.WorktreesDir
		c.ActiveProfile.RemoteName = repository.RemoteName
		c.ActiveProfile.GitHubRepo = repository.GitHubRepo
		c.RepoSlug = repository.GitHubRepo
		return c
	}
	// A project without a repositories list has one repository, which the
	// Config already describes.
	return c
}

// SharesWorktreesDir reports whether another repository of the project puts
// its worktrees in the same folder as the scoped one, so folder names need the
// repository name to stay apart.
func (c Config) SharesWorktreesDir() bool {
	if !c.MultiRepository() {
		return false
	}
	scoped := filepath.Clean(c.ActiveProfile.WorktreesDir)
	for _, repository := range c.ActiveProfile.Repositories {
		if repository.RepositoryPath != c.ActiveProfile.RepositoryPath && filepath.Clean(repository.WorktreesDir) == scoped {
			return true
		}
	}
	return false
}

// JiraBoard is a ticket source resolved from a pasted JIRA link. A board link
// gives a board (ID set), which can also be scoped to its active sprints. Any
// other link inside a space (a work item, a project page, a bare key) gives the
// whole space: no ID and the space's key as its only project. ID/Name/Projects
// are filled once by ResolveJiraBoard and then persisted, so the link only has
// to be parsed and looked up during setup.
type JiraBoard struct {
	URL      string   `json:"url"`
	ID       int      `json:"id,omitempty"`
	Name     string   `json:"name"`
	Projects []string `json:"projects"`
	// Sources holds the boards and spaces of the all-sources selection built
	// by JiraConfig.AllSources. It is never persisted.
	Sources []JiraBoard `json:"-"`
}

// JiraAllSources names the selection that lists every configured board and
// space together. DefaultBoard stores it like any board name.
const JiraAllSources = "All boards and spaces"

// Configured reports whether the source can list tickets at all.
func (b JiraBoard) Configured() bool {
	return b.ID != 0 || len(b.Projects) > 0 || len(b.Sources) > 0
}

// IsCombined reports whether the source is the all-sources selection.
func (b JiraBoard) IsCombined() bool {
	return len(b.Sources) > 0
}

// Members lists the boards and spaces the source loads: its sources when
// combined, otherwise itself.
func (b JiraBoard) Members() []JiraBoard {
	if b.IsCombined() {
		return b.Sources
	}
	return []JiraBoard{b}
}

// IsSpace reports whether the source is a whole JIRA space rather than a board.
func (b JiraBoard) IsSpace() bool {
	return b.ID == 0 && len(b.Projects) > 0
}

// SourceID identifies the source in cache and selection keys.
func (b JiraBoard) SourceID() string {
	if b.IsCombined() {
		ids := make([]string, 0, len(b.Sources))
		for _, source := range b.Sources {
			ids = append(ids, source.SourceID())
		}
		return "all:" + strings.Join(ids, "|")
	}
	if b.ID != 0 {
		return fmt.Sprint(b.ID)
	}
	return "space:" + strings.Join(b.Projects, ",")
}

// SprintScoped reports whether the sprint filter narrows this source. Sprints
// belong to boards, so a space always lists every ticket in it, and the
// all-sources selection is narrowed only through its boards.
func (b JiraBoard) SprintScoped(sprintOnly bool) bool {
	if !sprintOnly {
		return false
	}
	for _, member := range b.Members() {
		if member.ID != 0 {
			return true
		}
	}
	return false
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

// AllSources is the selection that lists every configured board and space
// together.
func (c *JiraConfig) AllSources() JiraBoard {
	return JiraBoard{Name: JiraAllSources, Sources: append([]JiraBoard(nil), c.Boards...)}
}

// ActiveBoard returns the board or space named by DefaultBoard. Otherwise it
// returns every board and space together, or the only one when there is just
// one.
func (c *JiraConfig) ActiveBoard() *JiraBoard {
	if c == nil || len(c.Boards) == 0 {
		return nil
	}
	if board := c.Board(c.DefaultBoard); board != nil {
		return board
	}
	if len(c.Boards) == 1 {
		return &c.Boards[0]
	}
	all := c.AllSources()
	return &all
}

type AgentTool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

type EditorTool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}

type Worktree struct {
	Name     string
	Path     string
	Branch   string
	Head     string
	Missing  bool
	Detached bool
	Prunable bool
	IsMain   bool
	Managed  bool
	// Repository names the project repository the worktree belongs to.
	Repository string
	Status     RepoStatus
	Commit     CommitInfo
	PR         *PullRequest
	LoadError  string
	// StatusPending is true while git status and the last commit have not been
	// read in this session or an earlier, cached one. Rows render a placeholder
	// instead of claiming such a worktree is clean.
	StatusPending bool
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

// TicketPatch is a local edit that is shown before JIRA confirms it, and is
// written into the cache once it does. Empty fields are left untouched.
type TicketPatch struct {
	Key            string
	Status         string
	StatusCategory string
	Assignee       *JiraUser
	Comment        *TicketComment
}

// Apply writes the patch into ticket when it is the patched ticket.
func (p TicketPatch) Apply(ticket *Ticket) {
	if ticket == nil || ticket.Key != p.Key {
		return
	}
	if p.Status != "" {
		ticket.Status = p.Status
		if p.StatusCategory != "" {
			ticket.StatusCategory = p.StatusCategory
		}
	}
	if p.Assignee != nil {
		ticket.Assignee = *p.Assignee
	}
	if p.Comment != nil {
		ticket.Comments = append(append([]TicketComment(nil), ticket.Comments...), *p.Comment)
	}
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
	// ResolveProject resolves a project for use: paths expanded, every
	// repository listed with its GitHub slug (inferred from its remote when
	// not configured), and the flat fields scoped to the first repository.
	ResolveProject(ctx context.Context, profile RepositoryProfile, homeDir string) RepositoryProfile
	// ReadAppConfig reads and normalizes the config file without rewriting
	// it, so edits made outside the app can be picked up or reported.
	ReadAppConfig(configPath string) (AppConfig, error)
	FindAgent(agents []AgentTool, name string) *AgentTool
	FindProfile(profiles []RepositoryProfile, name string) *RepositoryProfile
	ExpandPath(path string, homeDir string) string
	CompactPath(path string, homeDir string) string
	CanonicalPath(path string) string
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
	RemoveWorktree(ctx context.Context, cfg Config, wt Worktree) error
	EnsurePullRequestWorktree(ctx context.Context, cfg Config, pullRequest RemotePullRequest) (Worktree, bool, error)
	CreateWorktreePath(cfg Config, name string) string
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
	AttachTicketContext(tickets []Ticket, worktrees []Worktree, pullRequests []PullRequest) []Ticket
	FilterTickets(tickets []Ticket, query string, status string, openOnly bool) []Ticket
	TicketStatuses(tickets []Ticket) []string
	TicketAssignees(tickets []Ticket) []JiraUser
	TicketBranchName(ticket Ticket) string
	TicketKeyFromBranch(branch string) string
	BuildTicketChatPrompt(ticket Ticket, question string) string
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

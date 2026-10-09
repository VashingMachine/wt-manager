package services

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) DefaultConfig(forceSetup bool) (Config, error) {
	return defaultConfig(forceSetup)
}

func (s *Service) DefaultAppConfig() AppConfig {
	return defaultAppConfig()
}

func (s *Service) NormalizeAppConfig(appConfig AppConfig) (AppConfig, bool, error) {
	return normalizeAppConfig(appConfig)
}

func (s *Service) ActiveProfile(appConfig AppConfig, homeDir string) (RepositoryProfile, error) {
	return activeProfile(appConfig, homeDir)
}

func (s *Service) ResolveProfilePaths(profile RepositoryProfile, homeDir string) RepositoryProfile {
	return resolveProfilePaths(profile, homeDir)
}

func (s *Service) FindAgent(agents []AgentTool, name string) *AgentTool {
	return findAgent(agents, name)
}

func (s *Service) FindProfile(profiles []RepositoryProfile, name string) *RepositoryProfile {
	return findProfile(profiles, name)
}

func (s *Service) ExpandPath(path string, homeDir string) string {
	return expandPath(path, homeDir)
}

func (s *Service) CompactPath(path string, homeDir string) string {
	return compactPath(path, homeDir)
}

func (s *Service) CanonicalPath(path string) string {
	return canonicalPath(path)
}

func (s *Service) InferRepoFromCWD() (string, bool) {
	return inferRepoFromCWD()
}

func (s *Service) RepoRootFromDir(ctx context.Context, dir string) (string, bool) {
	return repoRootFromDir(ctx, dir)
}

func (s *Service) ProfileFromRepo(ctx context.Context, homeDir string, repoPath string) RepositoryProfile {
	return profileFromRepo(ctx, homeDir, repoPath)
}

func (s *Service) InferGitHubRepo(ctx context.Context, profile RepositoryProfile) string {
	return inferGitHubRepo(ctx, profile)
}

func (s *Service) WriteAppConfig(configPath string, appConfig AppConfig) error {
	return writeAppConfig(configPath, appConfig)
}

func (s *Service) CheckGitHubCLI(ctx context.Context, repoSlug string) []string {
	return checkGitHubCLI(ctx, repoSlug)
}

func checkGitHubCLI(ctx context.Context, repoSlug string) []string {
	if _, err := exec.LookPath("gh"); err != nil {
		return []string{"gh not found; PR lookup will be disabled until gh is installed"}
	}
	if _, err := runCommand(ctx, "", "gh", "--version"); err != nil {
		return []string{"gh is installed but not runnable; PR lookup may be unavailable"}
	}
	if _, err := runCommand(ctx, "", "gh", "auth", "status"); err != nil {
		return []string{"gh is not authenticated; PR lookup will be unavailable until gh auth login"}
	}
	if repoSlug != "" {
		if _, err := runCommand(ctx, "", "gh", "repo", "view", repoSlug); err != nil {
			return []string{fmt.Sprintf("gh cannot access %s; PR lookup may be unavailable", repoSlug)}
		}
	}
	return nil
}

func (s *Service) CollectWorktrees(ctx context.Context, cfg Config, showAll bool) ([]Worktree, string, error) {
	return collectWorktrees(ctx, cfg, showAll)
}

func (s *Service) CollectTicketWorktrees(ctx context.Context, cfg Config) ([]Worktree, string, error) {
	return collectTicketWorktrees(ctx, cfg)
}

func (s *Service) CreateWorktree(ctx context.Context, cfg Config, branch string, path string) (bool, error) {
	return createWorktree(ctx, cfg, branch, path)
}

func (s *Service) RemoveWorktree(ctx context.Context, cfg Config, wt Worktree) error {
	return removeWorktree(ctx, cfg, wt)
}

func (s *Service) EnsurePullRequestWorktree(ctx context.Context, cfg Config, pullRequest RemotePullRequest) (Worktree, bool, error) {
	return ensurePullRequestWorktree(ctx, cfg, pullRequest)
}

func (s *Service) CreateWorktreePath(cfg Config, name string) string {
	return createWorktreePath(cfg, name)
}

func (s *Service) LoadRemotePullRequests(ctx context.Context, cfg Config) ([]RemotePullRequest, error) {
	return loadRemotePullRequests(ctx, cfg)
}

func (s *Service) LoadPullRequestsForTickets(ctx context.Context, cfg Config) ([]PullRequest, error) {
	return loadPullRequestsForTickets(ctx, cfg)
}

func (s *Service) LoadGitHubCurrentUser(ctx context.Context, cfg Config) (string, error) {
	return loadGitHubCurrentUser(ctx, cfg)
}

func (s *Service) LoadRemotePullRequestDetail(ctx context.Context, cfg Config, number int) (RemotePullRequest, error) {
	return loadRemotePullRequestDetail(ctx, cfg, number)
}

func (s *Service) ApproveRemotePullRequest(ctx context.Context, cfg Config, number int) error {
	return approveRemotePullRequest(ctx, cfg, number)
}

func (s *Service) FilterRemotePullRequestsWithAuthor(pullRequests []RemotePullRequest, query string, state string, user string, author string) []RemotePullRequest {
	return filterRemotePullRequestsWithAuthor(pullRequests, query, state, user, author)
}

func (s *Service) RemotePRUsers(pullRequests []RemotePullRequest) []string {
	return remotePRUsers(pullRequests)
}

func (s *Service) BuildPullRequestChatPrompt(pr RemotePullRequest, question string) string {
	return buildPullRequestChatPrompt(pr, question)
}

func (s *Service) RunAgentWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error) {
	return runAgentWithPrompt(ctx, dir, command, prompt)
}

func (s *Service) OpenBrowser(ctx context.Context, url string) error {
	return openBrowser(ctx, url)
}

func (s *Service) OpenVSCodeWorktree(ctx context.Context, wt Worktree) error {
	return openVSCodeWorktree(ctx, wt)
}

func (s *Service) OpenAgent(ctx context.Context, wt Worktree, agent AgentTool) error {
	return openWorktreeAgent(ctx, wt, agent)
}

func (s *Service) OpenTerminal(ctx context.Context, wt Worktree) error {
	return openWorktreeTerminal(ctx, wt)
}

func (s *Service) Notify(ctx context.Context, title string, body string) error {
	return notifyCmux(ctx, title, body)
}

func (s *Service) CheckAtlassianCLI(ctx context.Context) []string {
	return checkAtlassianCLI(ctx)
}

func (s *Service) JiraSite(ctx context.Context) (string, error) {
	return jiraSite(ctx)
}

func (s *Service) ResolveJiraBoard(ctx context.Context, boardURLOrID string) (JiraBoard, error) {
	return resolveJiraBoard(ctx, boardURLOrID)
}

func (s *Service) SearchJiraBoards(ctx context.Context, name string) ([]JiraBoard, error) {
	return searchJiraBoards(ctx, name)
}

func (s *Service) PeekTicketCache(cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) ([]Ticket, time.Time, bool) {
	return peekTicketCache(cfg, board, mineOnly, sprintOnly)
}

func (s *Service) LoadTickets(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool, refresh bool) ([]Ticket, string, error) {
	return loadTickets(ctx, cfg, board, mineOnly, sprintOnly, refresh)
}

func (s *Service) LoadTicketDetail(ctx context.Context, cfg Config, key string) (Ticket, error) {
	return loadTicketDetail(ctx, cfg, key)
}

func (s *Service) TransitionTicket(ctx context.Context, key string, status string) error {
	return transitionTicket(ctx, key, status)
}

func (s *Service) CommentTicket(ctx context.Context, key string, body string) error {
	return commentTicket(ctx, key, body)
}

func (s *Service) AssignTicket(ctx context.Context, key string, assignee string) error {
	return assignTicket(ctx, key, assignee)
}

func (s *Service) AttachTicketContext(tickets []Ticket, worktrees []Worktree, pullRequests []PullRequest) []Ticket {
	return attachTicketContext(tickets, worktrees, pullRequests)
}

func (s *Service) FilterTickets(tickets []Ticket, query string, status string, openOnly bool) []Ticket {
	return filterTickets(tickets, query, status, openOnly)
}

func (s *Service) TicketStatuses(tickets []Ticket) []string {
	return ticketStatuses(tickets)
}

func (s *Service) TicketAssignees(tickets []Ticket) []JiraUser {
	return ticketAssignees(tickets)
}

func (s *Service) TicketBranchName(ticket Ticket) string {
	return ticketBranchName(ticket)
}

func (s *Service) TicketKeyFromBranch(branch string) string {
	return ticketKeyFromBranch(branch)
}

func (s *Service) BuildTicketChatPrompt(ticket Ticket, question string) string {
	return buildTicketChatPrompt(ticket, question)
}

func (s *Service) InvalidateTicketCache(cfg Config, board JiraBoard) error {
	return invalidateTicketCache(cfg, board)
}

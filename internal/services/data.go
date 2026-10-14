package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

const refreshTimeout = 45 * time.Second

type Config = core.Config
type AppConfig = core.AppConfig
type RepositoryProfile = core.RepositoryProfile
type AgentTool = core.AgentTool
type EditorTool = core.EditorTool
type Worktree = core.Worktree
type RepoStatus = core.RepoStatus
type CommitInfo = core.CommitInfo
type PullRequest = core.PullRequest
type RemotePullRequest = core.RemotePullRequest
type GitHubActor = core.GitHubActor
type ReviewRequest = core.ReviewRequest
type PullLabel = core.PullLabel
type StatusCheck = core.StatusCheck
type PullFile = core.PullFile
type PullComment = core.PullComment
type PullCommit = core.PullCommit
type PullReview = core.PullReview

func defaultConfig(forceSetup bool) (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	appConfig, configPath, existed, err := loadAppConfig(homeDir)
	if err != nil {
		return Config{}, err
	}

	currentRepo, hasCurrentRepo := inferRepoFromCWD()
	cfg := Config{HomeDir: homeDir, ConfigPath: configPath, App: appConfig, ConfigExists: existed}
	if forceSetup {
		cfg.SetupNeeded = true
		cfg.SetupReason = "Manual setup requested"
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	if len(appConfig.Profiles) == 0 {
		cfg.SetupNeeded = true
		if existed {
			cfg.SetupReason = "No repository profiles configured"
		} else {
			cfg.SetupReason = "No config found"
		}
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	if hasCurrentRepo && !repoConfigured(appConfig, homeDir, currentRepo) {
		cfg.SetupNeeded = true
		cfg.SetupReason = "Current repository is not configured"
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	activeProfile, err := activeProfile(appConfig, homeDir)
	if err != nil {
		cfg.SetupNeeded = true
		cfg.SetupReason = err.Error()
		cfg.SetupRepo = currentRepo
		return cfg, nil
	}

	cfg.ActiveProfile = inferRepositorySlugs(context.Background(), activeProfile)
	cfg.RepoSlug = cfg.ActiveProfile.GitHubRepo
	cfg.ConfigExists = existed
	return cfg, nil
}

func defaultAppConfig() AppConfig {
	return AppConfig{
		DefaultAgent:       "codex",
		DefaultEnterAction: core.EnterActionVSCode,
		Editor:             EditorTool{Name: "VS Code", Command: "code"},
		Agents: []AgentTool{
			{Name: "codex", Command: "codex"},
			{Name: "opencode", Command: "opencode"},
			{Name: "claude", Command: "claude"},
			{Name: "copilot", Command: "copilot"},
		},
	}
}

// DefaultConfigPath is where the config file lives.
func DefaultConfigPath(homeDir string) string {
	return filepath.Join(homeDir, ".wt-manager.json")
}

func loadAppConfig(homeDir string) (AppConfig, string, bool, error) {
	configPath := DefaultConfigPath(homeDir)
	data, err := os.ReadFile(configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return AppConfig{}, "", false, err
		}
		return defaultAppConfig(), configPath, false, nil
	}

	var appConfig AppConfig
	if err := json.Unmarshal(data, &appConfig); err != nil {
		return AppConfig{}, "", true, fmt.Errorf("read %s: %w", configPath, err)
	}

	appConfig, changed, err := normalizeAppConfig(appConfig)
	if err != nil {
		return AppConfig{}, "", true, err
	}
	if changed {
		if err := writeAppConfig(configPath, appConfig); err != nil {
			return AppConfig{}, "", true, err
		}
	}

	return appConfig, configPath, true, nil
}

// readAppConfig reads and normalizes a config file without writing it back.
func readAppConfig(configPath string) (AppConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return AppConfig{}, err
	}
	var appConfig AppConfig
	if err := json.Unmarshal(data, &appConfig); err != nil {
		return AppConfig{}, fmt.Errorf("read %s: %w", configPath, err)
	}
	appConfig, _, err = normalizeAppConfig(appConfig)
	return appConfig, err
}

func loadOrCreateAppConfig(homeDir string) (AppConfig, string, error) {
	appConfig, configPath, _, err := loadAppConfig(homeDir)
	return appConfig, configPath, err
}

func normalizeAppConfig(appConfig AppConfig) (AppConfig, bool, error) {
	changed := false
	defaults := defaultAppConfig()
	editor, err := normalizeEditorTool(appConfig.Editor)
	if err != nil {
		return AppConfig{}, false, err
	}
	if editor != appConfig.Editor {
		appConfig.Editor = editor
		changed = true
	}
	if normalized := normalizeEnterAction(appConfig.DefaultEnterAction); normalized != appConfig.DefaultEnterAction {
		appConfig.DefaultEnterAction = normalized
		changed = true
	}
	if len(appConfig.Agents) == 0 {
		appConfig.Agents = defaults.Agents
		changed = true
	}

	if findAgent(appConfig.Agents, appConfig.DefaultAgent) == nil {
		appConfig.DefaultAgent = defaults.DefaultAgent
		if findAgent(appConfig.Agents, appConfig.DefaultAgent) == nil && len(appConfig.Agents) > 0 {
			appConfig.DefaultAgent = appConfig.Agents[0].Name
		}
		changed = true
	}

	seenProfiles := map[string]struct{}{}
	for i := range appConfig.Profiles {
		profile := &appConfig.Profiles[i]
		profile.Name = strings.TrimSpace(profile.Name)
		profile.RepositoryPath = strings.TrimSpace(profile.RepositoryPath)
		profile.WorktreesDir = strings.TrimSpace(profile.WorktreesDir)
		profile.RemoteName = strings.TrimSpace(profile.RemoteName)
		profile.GitHubRepo = strings.TrimSpace(profile.GitHubRepo)
		repositoriesChanged, err := normalizeProfileRepositories(profile)
		if err != nil {
			return AppConfig{}, false, err
		}
		if repositoriesChanged {
			changed = true
		}
		if normalizeJiraConfig(profile) {
			changed = true
		}
		if err := validateProfile(*profile); err != nil {
			return AppConfig{}, false, err
		}
		if _, ok := seenProfiles[profile.Name]; ok {
			return AppConfig{}, false, fmt.Errorf("duplicate profile %q", profile.Name)
		}
		seenProfiles[profile.Name] = struct{}{}
	}

	if appConfig.DefaultProfile != "" {
		appConfig.DefaultProfile = strings.TrimSpace(appConfig.DefaultProfile)
		if findProfile(appConfig.Profiles, appConfig.DefaultProfile) == nil {
			return AppConfig{}, false, fmt.Errorf("defaultProfile %q does not match any profile", appConfig.DefaultProfile)
		}
	}
	if appConfig.DefaultProfile == "" && len(appConfig.Profiles) > 0 {
		appConfig.DefaultProfile = appConfig.Profiles[0].Name
		changed = true
	}

	return appConfig, changed, nil
}

// normalizeProfileRepositories fills in repository defaults. A project keeps
// its single repository in the flat fields until it lists repositories; flat
// fields next to a list describe one more repository and are moved into it.
func normalizeProfileRepositories(profile *RepositoryProfile) (bool, error) {
	changed := false
	if len(profile.Repositories) == 0 {
		if profile.RemoteName == "" {
			profile.RemoteName = "origin"
			changed = true
		}
	} else {
		if profile.RepositoryPath != "" {
			flat := core.Repository{
				Name:           core.RepositoryNameFromPath(profile.RepositoryPath),
				RepositoryPath: profile.RepositoryPath,
				WorktreesDir:   profile.WorktreesDir,
				RemoteName:     profile.RemoteName,
				GitHubRepo:     profile.GitHubRepo,
			}
			listed := false
			for _, repository := range profile.Repositories {
				if filepath.Clean(strings.TrimSpace(repository.RepositoryPath)) == filepath.Clean(flat.RepositoryPath) {
					listed = true
					break
				}
			}
			if !listed {
				profile.Repositories = append([]core.Repository{flat}, profile.Repositories...)
			}
		}
		if profile.RepositoryPath != "" || profile.WorktreesDir != "" || profile.RemoteName != "" || profile.GitHubRepo != "" {
			profile.RepositoryPath, profile.WorktreesDir, profile.RemoteName, profile.GitHubRepo = "", "", "", ""
			changed = true
		}
		seen := map[string]struct{}{}
		for i := range profile.Repositories {
			repository := &profile.Repositories[i]
			before := *repository
			repository.Name = strings.TrimSpace(repository.Name)
			repository.RepositoryPath = strings.TrimSpace(repository.RepositoryPath)
			repository.WorktreesDir = strings.TrimSpace(repository.WorktreesDir)
			repository.RemoteName = strings.TrimSpace(repository.RemoteName)
			repository.GitHubRepo = strings.TrimSpace(repository.GitHubRepo)
			if repository.Name == "" {
				repository.Name = core.RepositoryNameFromPath(repository.RepositoryPath)
			}
			if repository.RemoteName == "" {
				repository.RemoteName = "origin"
			}
			if *repository != before {
				changed = true
			}
			if repository.RepositoryPath == "" {
				return false, fmt.Errorf("profile %q repository %d repositoryPath is required", profile.Name, i+1)
			}
			if repository.WorktreesDir == "" {
				return false, fmt.Errorf("profile %q repository %q worktreesDir is required", profile.Name, repository.Name)
			}
			if _, ok := seen[repository.Name]; ok {
				return false, fmt.Errorf("profile %q has two repositories named %q; give each a unique name", profile.Name, repository.Name)
			}
			seen[repository.Name] = struct{}{}
		}
	}
	profile.LastRepository = strings.TrimSpace(profile.LastRepository)
	if profile.LastRepository != "" && profile.Repository(profile.LastRepository) == nil {
		profile.LastRepository = ""
		changed = true
	}
	return changed, nil
}

// normalizeJiraConfig fills in defaults for an opted-in JIRA block. It never
// creates one: profiles without JIRA configured stay byte-identical on rewrite.
func normalizeJiraConfig(profile *RepositoryProfile) bool {
	jira := profile.Jira
	if jira == nil {
		return false
	}

	changed := false
	if trimmed := strings.TrimSpace(jira.Site); trimmed != jira.Site {
		jira.Site = trimmed
		changed = true
	}

	boards := make([]core.JiraBoard, 0, len(jira.Boards))
	seen := map[string]struct{}{}
	for _, board := range jira.Boards {
		board.Name = strings.TrimSpace(board.Name)
		board.URL = strings.TrimSpace(board.URL)
		if board.ID == 0 {
			changed = true
			continue
		}
		if board.Name == "" {
			board.Name = fmt.Sprintf("board %d", board.ID)
			changed = true
		}
		if _, ok := seen[board.Name]; ok {
			changed = true
			continue
		}
		seen[board.Name] = struct{}{}
		boards = append(boards, board)
	}
	if len(boards) != len(jira.Boards) {
		changed = true
	}
	jira.Boards = boards

	jira.DefaultBoard = strings.TrimSpace(jira.DefaultBoard)
	if jira.DefaultBoard != "" && jira.Board(jira.DefaultBoard) == nil {
		jira.DefaultBoard = ""
		changed = true
	}
	if jira.DefaultBoard == "" && len(jira.Boards) > 0 {
		jira.DefaultBoard = jira.Boards[0].Name
		changed = true
	}
	if jira.Limit <= 0 {
		jira.Limit = jiraDefaultLimit
		changed = true
	}
	if !jira.FiltersConfigured {
		jira.MineOnly = true
		jira.SprintOnly = true
		jira.OpenOnly = true
		jira.FiltersConfigured = true
		changed = true
	}

	// Tickets default to "assigned to me"; without boards there is nothing to show.
	if len(jira.Boards) == 0 {
		profile.Jira = nil
		return true
	}
	return changed
}

func normalizeEnterAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case core.EnterActionVSCode:
		return core.EnterActionVSCode
	case core.EnterActionAgent:
		return core.EnterActionAgent
	case core.EnterActionTerminal:
		return core.EnterActionTerminal
	default:
		return core.EnterActionVSCode
	}
}

func findAgent(agents []AgentTool, name string) *AgentTool {
	for i := range agents {
		if agents[i].Name == name {
			return &agents[i]
		}
	}
	return nil
}

func findProfile(profiles []RepositoryProfile, name string) *RepositoryProfile {
	for i := range profiles {
		if profiles[i].Name == name {
			return &profiles[i]
		}
	}
	return nil
}

func validateProfile(profile RepositoryProfile) error {
	if profile.Name == "" {
		return errors.New("profile name is required")
	}
	if len(profile.Repositories) > 0 {
		// Repositories were validated while being normalized.
		return nil
	}
	if profile.RepositoryPath == "" {
		return fmt.Errorf("profile %q repositoryPath is required", profile.Name)
	}
	if profile.WorktreesDir == "" {
		return fmt.Errorf("profile %q worktreesDir is required", profile.Name)
	}
	if profile.RemoteName == "" {
		return fmt.Errorf("profile %q remoteName is required", profile.Name)
	}
	return nil
}

func activeProfile(appConfig AppConfig, homeDir string) (RepositoryProfile, error) {
	if len(appConfig.Profiles) == 0 {
		return RepositoryProfile{}, errors.New("no repository profiles configured")
	}
	profile := findProfile(appConfig.Profiles, appConfig.DefaultProfile)
	if profile == nil {
		return RepositoryProfile{}, fmt.Errorf("default profile %q not found", appConfig.DefaultProfile)
	}
	return resolveProfilePaths(*profile, homeDir), nil
}

// resolveProfilePaths expands every path of a project, lists all of its
// repositories under Repositories and scopes the flat fields to the first.
func resolveProfilePaths(profile RepositoryProfile, homeDir string) RepositoryProfile {
	listed := profile.RepositoryList()
	repositories := make([]core.Repository, len(listed))
	for i, repository := range listed {
		repository.RepositoryPath = expandPath(repository.RepositoryPath, homeDir)
		repository.WorktreesDir = expandPath(repository.WorktreesDir, homeDir)
		if repository.RemoteName == "" {
			repository.RemoteName = "origin"
		}
		repositories[i] = repository
	}
	profile.Repositories = repositories
	if len(repositories) > 0 {
		primary := repositories[0]
		profile.RepositoryPath = primary.RepositoryPath
		profile.WorktreesDir = primary.WorktreesDir
		profile.RemoteName = primary.RemoteName
		profile.GitHubRepo = primary.GitHubRepo
	}
	return profile
}

// inferRepositorySlugs fills in the GitHub slug of every repository that does
// not configure one, from its remote.
func inferRepositorySlugs(ctx context.Context, profile RepositoryProfile) RepositoryProfile {
	repositories := append([]core.Repository(nil), profile.Repositories...)
	for i := range repositories {
		if repositories[i].GitHubRepo == "" {
			repositories[i].GitHubRepo = inferGitHubRepo(ctx, RepositoryProfile{RepositoryPath: repositories[i].RepositoryPath, RemoteName: repositories[i].RemoteName})
		}
	}
	profile.Repositories = repositories
	if len(repositories) > 0 {
		profile.GitHubRepo = repositories[0].GitHubRepo
	}
	return profile
}

func resolveProject(ctx context.Context, profile RepositoryProfile, homeDir string) RepositoryProfile {
	return inferRepositorySlugs(ctx, resolveProfilePaths(profile, homeDir))
}

func expandPath(path string, homeDir string) string {
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func compactPath(path string, homeDir string) string {
	cleanPath := filepath.Clean(path)
	cleanHome := filepath.Clean(homeDir)
	relPath := cleanPath
	relHome := cleanHome
	if resolved, err := filepath.EvalSymlinks(cleanPath); err == nil {
		relPath = filepath.Clean(resolved)
	}
	if resolved, err := filepath.EvalSymlinks(cleanHome); err == nil {
		relHome = filepath.Clean(resolved)
	}

	if relPath == relHome || cleanPath == cleanHome {
		return "~"
	}
	if rel, err := filepath.Rel(relHome, relPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(filepath.Join("~", rel))
	}
	if rel, err := filepath.Rel(cleanHome, cleanPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(filepath.Join("~", rel))
	}
	return cleanPath
}

func inferRepoFromCWD() (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	return repoRootFromDir(ctx, cwd)
}

func repoRootFromDir(ctx context.Context, dir string) (string, bool) {
	output, err := runCommand(ctx, dir, "git", "-C", dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	repoPath := filepath.Clean(strings.TrimSpace(output))
	if repoPath == "" {
		return "", false
	}
	return repoPath, true
}

func profileFromRepo(ctx context.Context, homeDir string, repoPath string) RepositoryProfile {
	profile := RepositoryProfile{
		Name:           filepath.Base(repoPath),
		RepositoryPath: repoPath,
		WorktreesDir:   filepath.Dir(repoPath),
		RemoteName:     "origin",
	}
	profile.GitHubRepo = inferGitHubRepo(ctx, resolveProfilePaths(profile, homeDir))
	return profile
}

func inferProfileFromCurrentRepo(homeDir string) (RepositoryProfile, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	repoPath, ok := inferRepoFromCWD()
	if !ok {
		return RepositoryProfile{}, false
	}
	return profileFromRepo(ctx, homeDir, repoPath), true
}

func repoConfigured(appConfig AppConfig, homeDir string, repoPath string) bool {
	want := canonicalPath(repoPath)
	for _, profile := range appConfig.Profiles {
		for _, repository := range resolveProfilePaths(profile, homeDir).Repositories {
			if canonicalPath(repository.RepositoryPath) == want {
				return true
			}
		}
	}
	return false
}

func writeAppConfig(configPath string, appConfig AppConfig) error {
	data, err := json.MarshalIndent(appConfig, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(configPath, data, 0o600)
}

func loadRemotePullRequests(ctx context.Context, cfg Config) ([]RemotePullRequest, error) {
	if cfg.RepoSlug == "" {
		return nil, errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("gh not found; run setup after installing GitHub CLI")
	}

	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls?state=all&per_page=100", cfg.RepoSlug))
	if err != nil {
		return nil, fmt.Errorf("remote PR lookup failed: %w", err)
	}

	var restPullRequests []restPullRequest
	if err := json.Unmarshal([]byte(output), &restPullRequests); err != nil {
		return nil, fmt.Errorf("gh returned invalid PR JSON: %w", err)
	}
	// Check runs cost one request per PR, so they are not awaited here: the
	// list paints first and LoadRemotePullRequestChecks fills rows in.
	pullRequests := make([]RemotePullRequest, 0, len(restPullRequests))
	for _, pr := range restPullRequests {
		pullRequest := remotePullRequestFromREST(pr)
		pullRequests = append(pullRequests, pullRequest)
	}
	sortRemotePullRequests(pullRequests)
	return pullRequests, nil
}

type checksLoader func(ctx context.Context, cfg Config, sha string) ([]StatusCheck, error)

func loadRemotePullRequestDetail(ctx context.Context, cfg Config, number int) (RemotePullRequest, error) {
	return loadRemotePullRequestDetailWith(ctx, cfg, number, loadRemotePullRequestChecks)
}

// loadRemotePullRequestDetailWith issues the detail's independent requests
// concurrently, so a detail costs about two round trips instead of seven: only
// the check runs wait for the head SHA from the PR itself.
func loadRemotePullRequestDetailWith(ctx context.Context, cfg Config, number int, loadChecks checksLoader) (RemotePullRequest, error) {
	if cfg.RepoSlug == "" {
		return RemotePullRequest{}, errors.New("GitHub repository is not configured")
	}
	if number <= 0 {
		return RemotePullRequest{}, errors.New("pull request number is required")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return RemotePullRequest{}, errors.New("gh not found; run setup after installing GitHub CLI")
	}

	var (
		wg          sync.WaitGroup
		restPR      restPullRequest
		detailErr   error
		checks      []StatusCheck
		checksErr   error
		files       []PullFile
		filesErr    error
		comments    []PullComment
		commentsErr error
		commits     []PullCommit
		commitsErr  error
		reviews     []PullReview
		reviewsErr  error
		diff        string
		diffErr     error
	)
	wg.Go(func() {
		output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d", cfg.RepoSlug, number))
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
		diff, diffErr = runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d", cfg.RepoSlug, number), "-H", "Accept: application/vnd.github.v3.diff")
	})
	wg.Wait()

	if detailErr != nil {
		return RemotePullRequest{}, detailErr
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
		pullRequest.ReviewDecision = deriveReviewDecision(reviews)
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

func approveRemotePullRequest(ctx context.Context, cfg Config, number int) error {
	if cfg.RepoSlug == "" {
		return errors.New("GitHub repository is not configured")
	}
	if number <= 0 {
		return errors.New("pull request number is required")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh not found; run setup after installing GitHub CLI")
	}
	if _, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "pr", "review", strconv.Itoa(number), "--approve", "--repo", cfg.RepoSlug); err != nil {
		return fmt.Errorf("approve PR #%d failed: %w", number, err)
	}
	return nil
}

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

func remotePullRequestFromREST(pr restPullRequest) RemotePullRequest {
	state := strings.ToUpper(pr.State)
	if pr.MergedAt != nil && strings.TrimSpace(*pr.MergedAt) != "" {
		state = "MERGED"
	}

	assignees := make([]GitHubActor, 0, len(pr.Assignees))
	for _, assignee := range pr.Assignees {
		assignees = append(assignees, githubActorFromREST(assignee))
	}
	reviewRequests := make([]ReviewRequest, 0, len(pr.RequestedReviewers))
	for _, reviewer := range pr.RequestedReviewers {
		reviewRequests = append(reviewRequests, ReviewRequest{RequestedReviewer: githubActorFromREST(reviewer)})
	}
	labels := make([]PullLabel, 0, len(pr.Labels))
	for _, label := range pr.Labels {
		labels = append(labels, PullLabel{Name: label.Name})
	}

	return RemotePullRequest{
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

func githubActorFromREST(actor restActor) GitHubActor {
	return GitHubActor{Login: actor.Login, Name: actor.Name, Slug: actor.Slug}
}

func loadRemotePullRequestFiles(ctx context.Context, cfg Config, number int) ([]PullFile, error) {
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restFiles []restPullFile
	if err := json.Unmarshal([]byte(output), &restFiles); err != nil {
		return nil, err
	}
	files := make([]PullFile, 0, len(restFiles))
	for _, file := range restFiles {
		files = append(files, PullFile{Path: file.Filename, Additions: file.Additions, Deletions: file.Deletions})
	}
	return files, nil
}

func loadRemotePullRequestComments(ctx context.Context, cfg Config, number int) ([]PullComment, error) {
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/issues/%d/comments?per_page=30", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restComments []restIssueComment
	if err := json.Unmarshal([]byte(output), &restComments); err != nil {
		return nil, err
	}
	comments := make([]PullComment, 0, len(restComments))
	for _, comment := range restComments {
		comments = append(comments, PullComment{Author: githubActorFromREST(comment.User), Body: comment.Body, CreatedAt: comment.CreatedAt, URL: comment.HTMLURL})
	}
	return comments, nil
}

func loadRemotePullRequestCommits(ctx context.Context, cfg Config, number int) ([]PullCommit, error) {
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d/commits?per_page=30", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restCommits []restPullCommit
	if err := json.Unmarshal([]byte(output), &restCommits); err != nil {
		return nil, err
	}
	commits := make([]PullCommit, 0, len(restCommits))
	for _, commit := range restCommits {
		commits = append(commits, PullCommit{Oid: commit.SHA, MessageHeadline: firstLine(commit.Commit.Message), CommittedDate: commit.Commit.Author.Date})
	}
	return commits, nil
}

func loadRemotePullRequestReviews(ctx context.Context, cfg Config, number int) ([]PullReview, error) {
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100", cfg.RepoSlug, number))
	if err != nil {
		return nil, err
	}
	var restReviews []restPullReview
	if err := json.Unmarshal([]byte(output), &restReviews); err != nil {
		return nil, err
	}
	reviews := make([]PullReview, 0, len(restReviews))
	for _, review := range restReviews {
		reviews = append(reviews, PullReview{
			Author:      githubActorFromREST(review.User),
			State:       review.State,
			Body:        review.Body,
			SubmittedAt: review.SubmittedAt,
			URL:         review.HTMLURL,
		})
	}
	return reviews, nil
}

func deriveReviewDecision(reviews []PullReview) string {
	latestByUser := map[string]PullReview{}
	for _, review := range reviews {
		user := strings.TrimSpace(review.Author.Login)
		if user == "" {
			user = strings.TrimSpace(review.Author.Slug)
		}
		if user == "" {
			continue
		}
		state := strings.ToUpper(strings.TrimSpace(review.State))
		if state == "" || state == "COMMENTED" {
			continue
		}
		latestByUser[strings.ToLower(user)] = review
	}

	approved := false
	for _, review := range latestByUser {
		switch strings.ToUpper(strings.TrimSpace(review.State)) {
		case "CHANGES_REQUESTED":
			return "CHANGES_REQUESTED"
		case "APPROVED":
			approved = true
		}
	}
	if approved {
		return "APPROVED"
	}
	return ""
}

func loadRemotePullRequestChecks(ctx context.Context, cfg Config, sha string) ([]StatusCheck, error) {
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", fmt.Sprintf("repos/%s/commits/%s/check-runs?per_page=100", cfg.RepoSlug, sha), "-H", "Accept: application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	var runs restCheckRuns
	if err := json.Unmarshal([]byte(output), &runs); err != nil {
		return nil, err
	}
	checks := make([]StatusCheck, 0, len(runs.CheckRuns))
	for _, run := range runs.CheckRuns {
		url := run.HTMLURL
		if url == "" {
			url = run.DetailsURL
		}
		checks = append(checks, StatusCheck{Name: run.Name, Status: run.Status, Conclusion: run.Conclusion, URL: url, Summary: run.Output.Summary})
	}
	return checks, nil
}

func loadGitHubCurrentUser(ctx context.Context, cfg Config) (string, error) {
	if cfg.RepoSlug == "" {
		return "", errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return "", errors.New("gh not found")
	}
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "api", "user")
	if err != nil {
		return "", err
	}
	var actor restActor
	if err := json.Unmarshal([]byte(output), &actor); err != nil {
		return "", err
	}
	return strings.TrimSpace(actor.Login), nil
}

func filterRemotePullRequests(pullRequests []RemotePullRequest, query string, state string, user string) []RemotePullRequest {
	return filterRemotePullRequestsWithAuthor(pullRequests, query, state, user, "")
}

func filterRemotePullRequestsWithAuthor(pullRequests []RemotePullRequest, query string, state string, user string, author string) []RemotePullRequest {
	query = strings.ToLower(strings.TrimSpace(query))
	state = strings.ToLower(strings.TrimSpace(state))
	user = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(user), "@"))
	author = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(author), "@"))

	filtered := make([]RemotePullRequest, 0, len(pullRequests))
	for _, pr := range pullRequests {
		if state != "" && state != "all" && strings.ToLower(pr.State) != state {
			continue
		}
		if author != "" && !strings.EqualFold(pr.Author.Login, author) {
			continue
		}
		if user != "" && !remotePullRequestHasUser(pr, user) {
			continue
		}
		if query != "" && !remotePullRequestMatchesQuery(pr, query) {
			continue
		}
		filtered = append(filtered, pr)
	}
	return filtered
}

func remotePRUsers(pullRequests []RemotePullRequest) []string {
	seen := map[string]struct{}{}
	var users []string
	add := func(actor GitHubActor) {
		login := strings.TrimSpace(actor.Login)
		if login == "" {
			login = strings.TrimSpace(actor.Slug)
		}
		if login == "" {
			return
		}
		key := strings.ToLower(login)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		users = append(users, login)
	}

	for _, pr := range pullRequests {
		add(pr.Author)
		for _, assignee := range pr.Assignees {
			add(assignee)
		}
		for _, request := range pr.ReviewRequests {
			add(request.RequestedReviewer)
		}
	}
	sort.Strings(users)
	return users
}

func remotePullRequestHasUser(pr RemotePullRequest, user string) bool {
	matches := func(actor GitHubActor) bool {
		return strings.EqualFold(actor.Login, user) || strings.EqualFold(actor.Slug, user)
	}
	if matches(pr.Author) {
		return true
	}
	for _, assignee := range pr.Assignees {
		if matches(assignee) {
			return true
		}
	}
	for _, request := range pr.ReviewRequests {
		if matches(request.RequestedReviewer) {
			return true
		}
	}
	return false
}

func remotePullRequestMatchesQuery(pr RemotePullRequest, query string) bool {
	fields := []string{strconv.Itoa(pr.Number), pr.Title, pr.HeadRefName, pr.BaseRefName, pr.Author.Login, pr.ReviewDecision, strings.ToLower(pr.State)}
	for _, label := range pr.Labels {
		fields = append(fields, label.Name)
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func sortRemotePullRequests(pullRequests []RemotePullRequest) {
	sort.SliceStable(pullRequests, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339, pullRequests[i].UpdatedAt)
		right, rightErr := time.Parse(time.RFC3339, pullRequests[j].UpdatedAt)
		if leftErr == nil && rightErr == nil && !left.Equal(right) {
			return left.After(right)
		}
		return pullRequests[i].Number > pullRequests[j].Number
	})
}

func buildPullRequestChatPrompt(pr RemotePullRequest, question string) string {
	var lines []string
	lines = append(lines,
		"You are helping review a GitHub pull request.",
		"Answer using only the PR context below. Call out uncertainty when context is missing.",
		"",
		fmt.Sprintf("Question: %s", strings.TrimSpace(question)),
		"",
		fmt.Sprintf("PR #%d: %s", pr.Number, pr.Title),
		fmt.Sprintf("URL: %s", pr.URL),
		fmt.Sprintf("State: %s Draft: %t Review: %s", pr.State, pr.IsDraft, emptyDash(pr.ReviewDecision)),
		fmt.Sprintf("Author: %s", emptyDash(pr.Author.Login)),
		fmt.Sprintf("Branch: %s -> %s", emptyDash(pr.HeadRefName), emptyDash(pr.BaseRefName)),
		fmt.Sprintf("Size: %d files, +%d/-%d", pr.ChangedFiles, pr.Additions, pr.Deletions),
	)

	if len(pr.Labels) > 0 {
		labels := make([]string, 0, len(pr.Labels))
		for _, label := range pr.Labels {
			labels = append(labels, label.Name)
		}
		lines = append(lines, fmt.Sprintf("Labels: %s", strings.Join(labels, ", ")))
	}

	lines = append(lines, "", "Description:", truncateRunes(strings.TrimSpace(pr.Body), 3000))
	lines = append(lines, "", "Files:")
	if len(pr.Files) == 0 {
		lines = append(lines, "- not loaded")
	} else {
		for idx, file := range pr.Files {
			if idx == 60 {
				lines = append(lines, fmt.Sprintf("- ... and %d more", len(pr.Files)-idx))
				break
			}
			lines = append(lines, fmt.Sprintf("- %s (+%d/-%d)", file.Path, file.Additions, file.Deletions))
		}
	}

	if len(pr.Comments) > 0 {
		lines = append(lines, "", "Recent comments:")
		start := max(0, len(pr.Comments)-8)
		for _, comment := range pr.Comments[start:] {
			lines = append(lines, fmt.Sprintf("- %s: %s", emptyDash(comment.Author.Login), truncateRunes(strings.TrimSpace(comment.Body), 600)))
		}
	}

	if strings.TrimSpace(pr.Diff) != "" {
		lines = append(lines, "", "Diff:", truncateRunes(pr.Diff, 12000))
	}

	return strings.Join(lines, "\n")
}

func runAgentWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", errors.New("agent command is empty")
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		return "", fmt.Errorf("agent %s not found", fields[0])
	}

	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = strings.NewReader(prompt)
	output, err := loggedCombinedOutput(ctx, cmd)
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, trimmed)
	}
	return string(output), nil
}

func filterManaged(worktrees []Worktree, showAll bool) []Worktree {
	filtered := make([]Worktree, 0, len(worktrees))
	for _, wt := range worktrees {
		if showAll || wt.Managed {
			filtered = append(filtered, wt)
		}
	}
	return filtered
}

func discoverWorktrees(ctx context.Context, cfg Config) ([]Worktree, error) {
	repoPath := cfg.ActiveProfile.RepositoryPath
	output, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	// Symlink resolution walks every path component, so resolve the profile's
	// directories once rather than per worktree.
	canonicalRepo := canonicalPath(repoPath)
	canonicalWorktrees := canonicalPath(cfg.ActiveProfile.WorktreesDir)
	entries := make([]Worktree, 0, 8)
	current := Worktree{}
	flush := func() {
		if current.Path == "" {
			return
		}
		current.Path = filepath.Clean(current.Path)
		canonical := canonicalPath(current.Path)
		current.IsMain = canonical == canonicalRepo
		current.Managed = current.IsMain || canonicalInsideDir(canonical, canonicalWorktrees)
		current.Name = filepath.Base(current.Path)
		if _, err := os.Stat(current.Path); err != nil {
			current.Missing = errors.Is(err, os.ErrNotExist)
		}
		entries = append(entries, current)
		current = Worktree{}
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.Detached = true
			current.Branch = "detached"
		case strings.HasPrefix(line, "prunable"):
			current.Prunable = true
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	labelWorktrees(cfg, entries)
	return entries, nil
}

// labelWorktrees records which repository the worktrees belong to and names
// the main checkout after the project, or after its repository when the
// project has several.
func labelWorktrees(cfg Config, worktrees []Worktree) {
	repository := cfg.RepositoryName()
	mainName := cfg.ActiveProfile.Name
	if cfg.MultiRepository() || mainName == "" {
		mainName = repository
	}
	for i := range worktrees {
		worktrees[i].Repository = repository
		if worktrees[i].IsMain {
			worktrees[i].Name = mainName
		}
	}
}

// missingWorktreeNote explains why a worktree has no status to read, or
// returns "" when its directory exists.
func missingWorktreeNote(wt Worktree) string {
	if !wt.Missing {
		return ""
	}
	if wt.Prunable {
		return "prunable worktree entry"
	}
	return "path missing on disk"
}

// readWorktreeStatus runs git status and git log for one worktree
// concurrently and records what they report on wt.
func readWorktreeStatus(ctx context.Context, wt *Worktree) []string {
	wt.StatusPending = false
	if note := missingWorktreeNote(*wt); note != "" {
		wt.LoadError = note
		return nil
	}

	var (
		wg        sync.WaitGroup
		status    RepoStatus
		statusErr error
		commit    CommitInfo
		commitErr error
	)
	wg.Go(func() { status, statusErr = loadRepoStatus(ctx, wt.Path) })
	wg.Go(func() { commit, commitErr = loadCommitInfo(ctx, wt.Path) })
	wg.Wait()

	var warnings []string
	wt.LoadError = ""
	if statusErr != nil {
		wt.LoadError = statusErr.Error()
		warnings = append(warnings, fmt.Sprintf("status failed for %s", wt.Name))
	} else {
		wt.Status = status
	}
	if commitErr != nil {
		warnings = append(warnings, fmt.Sprintf("commit failed for %s", wt.Name))
	} else {
		wt.Commit = commit
	}
	return warnings
}

// attachPullRequests labels each branch worktree with its PR, if any.
func attachPullRequests(worktrees []Worktree, prs []PullRequest) {
	index := make(map[string]*PullRequest, len(prs))
	for i := range prs {
		if prs[i].HeadRefName == "" {
			continue
		}
		pr := prs[i]
		index[pr.HeadRefName] = &pr
	}
	for i := range worktrees {
		wt := &worktrees[i]
		wt.PR = nil
		if wt.Detached || wt.Branch == "" || wt.Branch == "detached" {
			continue
		}
		wt.PR = index[wt.Branch]
	}
}

func loadRepoStatus(ctx context.Context, repoPath string) (RepoStatus, error) {
	output, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return RepoStatus{}, err
	}

	status := RepoStatus{}
	files := make([]string, 0, 16)
	seen := map[string]struct{}{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "# branch.ab "):
			fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &status.Ahead, &status.Behind)
		case strings.HasPrefix(line, "? "):
			status.Untracked++
			appendUnique(&files, seen, strings.TrimPrefix(line, "? "))
		case strings.HasPrefix(line, "1 "):
			xy, file := parseOrdinaryStatus(line)
			applyXY(&status, xy)
			appendUnique(&files, seen, file)
		case strings.HasPrefix(line, "2 "):
			xy, file := parseRenameStatus(line)
			applyXY(&status, xy)
			appendUnique(&files, seen, file)
		case strings.HasPrefix(line, "u "):
			status.Conflicts++
			appendUnique(&files, seen, parseConflictPath(line))
		}
	}
	if err := scanner.Err(); err != nil {
		return RepoStatus{}, err
	}

	status.Files = files
	status.Dirty = status.Staged+status.Unstaged+status.Untracked+status.Conflicts > 0
	return status, nil
}

func parseOrdinaryStatus(line string) (string, string) {
	parts := strings.SplitN(line, " ", 9)
	if len(parts) < 9 {
		return "..", strings.TrimSpace(line)
	}
	return parts[1], parts[8]
}

func parseRenameStatus(line string) (string, string) {
	parts := strings.SplitN(line, "\t", 2)
	prefix := parts[0]
	fields := strings.Fields(prefix)
	if len(fields) < 9 {
		return "..", strings.TrimSpace(line)
	}
	return fields[1], fields[len(fields)-1]
}

func parseConflictPath(line string) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return strings.TrimSpace(line)
	}
	return parts[len(parts)-1]
}

func applyXY(status *RepoStatus, xy string) {
	if len(xy) < 2 {
		return
	}
	if xy[0] != '.' {
		status.Staged++
	}
	if xy[1] != '.' {
		status.Unstaged++
	}
}

func loadCommitInfo(ctx context.Context, repoPath string) (CommitInfo, error) {
	output, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "log", "-1", "--date=relative", "--format=%h%x09%cr%x09%s")
	if err != nil {
		return CommitInfo{}, err
	}

	parts := strings.SplitN(strings.TrimSpace(output), "\t", 3)
	commit := CommitInfo{}
	if len(parts) > 0 {
		commit.Hash = parts[0]
	}
	if len(parts) > 1 {
		commit.Relative = parts[1]
	}
	if len(parts) > 2 {
		commit.Subject = parts[2]
	}
	return commit, nil
}

func fetchPullRequestIndex(ctx context.Context, cfg Config) ([]PullRequest, error) {
	if cfg.RepoSlug == "" {
		return nil, errors.New("GitHub repository is not configured")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, errors.New("gh not found; run setup after installing GitHub CLI")
	}
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "pr", "list", "--repo", cfg.RepoSlug, "--state", "all", "--limit", "200", "--json", "headRefName,number,title,url,state,body,author")
	if err != nil {
		return nil, err
	}
	var prs []PullRequest
	if err := json.Unmarshal([]byte(output), &prs); err != nil {
		return nil, fmt.Errorf("gh returned invalid PR JSON: %w", err)
	}
	return prs, nil
}

func removeWorktree(ctx context.Context, cfg Config, wt Worktree) error {
	repoPath := cfg.ActiveProfile.RepositoryPath
	var err error
	if wt.Missing || wt.Prunable {
		_, err = runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "prune")
	} else if wt.Status.Dirty {
		_, err = runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "remove", "--force", wt.Path)
	} else {
		_, err = runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "remove", wt.Path)
	}
	if err == nil {
		_, _ = runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "prune")
	}
	return err
}

// fetchFunc updates remote-tracking refs. It reports whether it actually ran
// git fetch; force bypasses any reuse of a recent fetch.
type fetchFunc func(ctx context.Context, repoPath string, remoteName string, force bool) bool

func fetchRemote(ctx context.Context, repoPath string, remoteName string, _ bool) bool {
	_, _ = runCommand(ctx, repoPath, "git", "-C", repoPath, "fetch", "--quiet", remoteName, "--prune")
	return true
}

func createWorktree(ctx context.Context, cfg Config, branch string, path string) (bool, error) {
	return createWorktreeWith(ctx, cfg, branch, path, fetchRemote)
}

func createWorktreeWith(ctx context.Context, cfg Config, branch string, path string, fetch fetchFunc) (bool, error) {
	if strings.TrimSpace(branch) == "" {
		return false, errors.New("branch name is required")
	}
	repoPath := cfg.ActiveProfile.RepositoryPath
	remoteName := cfg.ActiveProfile.RemoteName
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return false, fmt.Errorf("repository not found: %s", repoPath)
	}

	if knownWorktreePath(ctx, repoPath, path) {
		return true, nil
	}
	if _, err := os.Stat(path); err == nil {
		return false, fmt.Errorf("%s already exists but is not a worktree for profile %s", path, cfg.ActiveProfile.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}

	// A local branch is checked out as-is, so the network round trip of a fetch
	// would not change the result.
	if gitRefExists(ctx, repoPath, "refs/heads/"+branch) {
		_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", path, branch)
		return false, err
	}

	remoteRef := "refs/remotes/" + remoteName + "/" + branch
	fetched := fetch(ctx, repoPath, remoteName, false)
	if !fetched && !gitRefExists(ctx, repoPath, remoteRef) {
		// A reused fetch may predate a branch pushed moments ago; check again
		// before creating a new branch instead of tracking the remote one.
		fetch(ctx, repoPath, remoteName, true)
	}
	if gitRefExists(ctx, repoPath, remoteRef) {
		_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", "--track", "-b", branch, path, remoteName+"/"+branch)
		return false, err
	}

	defaultRef := defaultRemoteRef(ctx, repoPath, remoteName)
	_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", "-b", branch, path, defaultRef)
	return false, err
}

func ensurePullRequestWorktree(ctx context.Context, cfg Config, pullRequest RemotePullRequest) (Worktree, bool, error) {
	return ensurePullRequestWorktreeWith(ctx, cfg, pullRequest, fetchRemote)
}

func ensurePullRequestWorktreeWith(ctx context.Context, cfg Config, pullRequest RemotePullRequest, fetch fetchFunc) (Worktree, bool, error) {
	if pullRequest.Number <= 0 {
		return Worktree{}, false, errors.New("pull request number is required")
	}
	branch := pullRequest.HeadRefName
	if strings.TrimSpace(branch) == "" {
		branch = fmt.Sprintf("pr-%d", pullRequest.Number)
	}
	if strings.TrimSpace(pullRequest.HeadRefName) != "" {
		if existing, ok := existingWorktreeForBranch(ctx, cfg, pullRequest.HeadRefName); ok {
			return existing, true, nil
		}
	}
	localBranch := fmt.Sprintf("pr-%d-%s", pullRequest.Number, branch)
	path := createWorktreePath(cfg, localBranch)
	if knownWorktreePath(ctx, cfg.ActiveProfile.RepositoryPath, path) {
		return Worktree{Name: filepath.Base(path), Path: path, Branch: localBranch}, true, nil
	}
	if _, err := os.Stat(path); err == nil {
		return Worktree{}, false, fmt.Errorf("%s already exists but is not a worktree for profile %s", path, cfg.ActiveProfile.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Worktree{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Worktree{}, false, err
	}

	repoPath := cfg.ActiveProfile.RepositoryPath
	remoteName := cfg.ActiveProfile.RemoteName
	if remoteName == "" {
		remoteName = "origin"
	}
	prRef := fmt.Sprintf("pull/%d/head:%s", pullRequest.Number, localBranch)
	_, _ = runCommand(ctx, repoPath, "git", "-C", repoPath, "fetch", "--quiet", remoteName, prRef)
	if gitRefExists(ctx, repoPath, "refs/heads/"+localBranch) {
		_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", path, localBranch)
		return Worktree{Name: filepath.Base(path), Path: path, Branch: localBranch}, false, err
	}

	if strings.TrimSpace(pullRequest.HeadRefName) != "" {
		existing, err := createWorktreeWith(ctx, cfg, pullRequest.HeadRefName, path, fetch)
		return Worktree{Name: filepath.Base(path), Path: path, Branch: pullRequest.HeadRefName}, existing, err
	}
	return Worktree{}, false, fmt.Errorf("cannot create worktree for PR #%d", pullRequest.Number)
}

func existingWorktreeForBranch(ctx context.Context, cfg Config, branch string) (Worktree, bool) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return Worktree{}, false
	}
	worktrees, err := discoverWorktrees(ctx, cfg)
	if err != nil {
		return Worktree{}, false
	}
	for _, wt := range worktrees {
		if wt.Missing || wt.Prunable || wt.Detached {
			continue
		}
		if wt.Branch == branch {
			return wt, true
		}
	}
	return Worktree{}, false
}

func gitRefExists(ctx context.Context, repoPath string, ref string) bool {
	_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

func defaultRemoteRef(ctx context.Context, repoPath string, remoteName string) string {
	output, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+remoteName+"/HEAD")
	if err != nil {
		return "HEAD"
	}
	ref := strings.TrimSpace(output)
	if ref == "" {
		return "HEAD"
	}
	return ref
}

func createWorktreePath(cfg Config, name string) string {
	worktreeName := strings.ReplaceAll(name, "/", "-")
	if cfg.SharesWorktreesDir() {
		// The same branch (say, one JIRA ticket) is often created in several
		// repositories of a project; a shared folder keeps them apart by name.
		worktreeName = cfg.RepositoryName() + "-" + worktreeName
	}
	return filepath.Join(cfg.ActiveProfile.WorktreesDir, worktreeName)
}

func openBrowser(ctx context.Context, url string) error {
	if inCmux(os.Getenv) {
		if err := openURLInCmux(ctx, url); err == nil {
			return nil
		}
	}
	cmdName := "open"
	args := []string{url}
	if runtime.GOOS == "linux" {
		cmdName = "xdg-open"
	}
	if runtime.GOOS == "windows" {
		cmdName = "cmd"
		args = []string{"/c", "start", url}
	}
	_, err := runCommand(ctx, "", cmdName, args...)
	return err
}

func openVSCodeWorktree(ctx context.Context, wt Worktree) error {
	_, err := runCommand(ctx, "", "code", "--new-window", wt.Path)
	if err == nil {
		return nil
	}
	if runtime.GOOS != "darwin" {
		return err
	}
	_, fallbackErr := runCommand(ctx, "", "open", "-a", "Visual Studio Code", wt.Path)
	return fallbackErr
}

func appleScriptString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r")
	return "\"" + replacer.Replace(value) + "\""
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func runCommand(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := loggedCombinedOutput(ctx, cmd)
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, trimmed)
	}
	return string(output), nil
}

func knownWorktreePath(ctx context.Context, repoPath string, path string) bool {
	output, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	want := canonicalPath(path)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "worktree ") && canonicalPath(strings.TrimPrefix(line, "worktree ")) == want {
			return true
		}
	}
	return false
}

func pathInsideDir(path string, dir string) bool {
	return canonicalInsideDir(canonicalPath(path), canonicalPath(dir))
}

func canonicalInsideDir(cleanPath string, cleanDir string) bool {
	if cleanPath == cleanDir {
		return true
	}
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

// gitRemotes lists the remotes configured in a repository.
func gitRemotes(ctx context.Context, repoPath string) []string {
	output, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "remote")
	if err != nil {
		return nil
	}
	return strings.Fields(output)
}

func inferGitHubRepo(ctx context.Context, profile RepositoryProfile) string {
	remoteName := profile.RemoteName
	if remoteName == "" {
		remoteName = "origin"
	}
	output, err := runCommand(ctx, profile.RepositoryPath, "git", "-C", profile.RepositoryPath, "config", "--get", "remote."+remoteName+".url")
	if err != nil {
		return ""
	}
	return gitHubSlugFromRemoteURL(strings.TrimSpace(output))
}

func gitHubSlugFromRemoteURL(remoteURL string) string {
	remoteURL = strings.TrimSpace(remoteURL)
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	switch {
	case strings.HasPrefix(remoteURL, "git@github.com:"):
		return strings.TrimPrefix(remoteURL, "git@github.com:")
	case strings.HasPrefix(remoteURL, "ssh://git@github.com/"):
		return strings.TrimPrefix(remoteURL, "ssh://git@github.com/")
	case strings.HasPrefix(remoteURL, "https://github.com/"):
		return strings.TrimPrefix(remoteURL, "https://github.com/")
	case strings.HasPrefix(remoteURL, "http://github.com/"):
		return strings.TrimPrefix(remoteURL, "http://github.com/")
	}
	return ""
}

func sortWorktrees(worktrees []Worktree) {
	sort.SliceStable(worktrees, func(i, j int) bool {
		left := worktrees[i]
		right := worktrees[j]

		if left.IsMain != right.IsMain {
			return left.IsMain
		}

		leftBranch := strings.ToLower(left.Branch)
		rightBranch := strings.ToLower(right.Branch)
		if leftBranch != rightBranch {
			return leftBranch < rightBranch
		}

		leftName := strings.ToLower(left.Name)
		rightName := strings.ToLower(right.Name)
		if leftName != rightName {
			return leftName < rightName
		}

		return strings.ToLower(left.Path) < strings.ToLower(right.Path)
	})
}

func appendUnique(files *[]string, seen map[string]struct{}, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if _, ok := seen[path]; ok {
		return
	}
	seen[path] = struct{}{}
	*files = append(*files, path)
}

func shortPR(pr *PullRequest) string {
	if pr == nil {
		return "-"
	}
	return fmt.Sprintf("#%d %s", pr.Number, strings.ToLower(pr.State))
}

func stateLabel(wt Worktree) string {
	switch {
	case wt.IsMain:
		return "main"
	case wt.Prunable:
		return "prunable"
	case wt.Missing:
		return "missing"
	case wt.Detached:
		return "detached"
	case wt.Status.Conflicts > 0:
		return "conflict"
	case wt.Status.Dirty:
		return "dirty"
	default:
		return "clean"
	}
}

func changeSummary(status RepoStatus) string {
	return fmt.Sprintf("%d/%d/?%d", status.Staged, status.Unstaged, status.Untracked)
}

func trackingSummary(status RepoStatus) string {
	if status.Ahead == 0 && status.Behind == 0 {
		return "up to date"
	}
	return fmt.Sprintf("+%d/-%d", status.Ahead, status.Behind)
}

func truncateText(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return text[:max-1] + "…"
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

func envFlag(name string) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

package main

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

	tea "github.com/charmbracelet/bubbletea"
)

const refreshTimeout = 45 * time.Second

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

type loadResult struct {
	Worktrees []Worktree
	Warning   string
	Err       error
}

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

	cfg.ActiveProfile = activeProfile
	cfg.RepoSlug = activeProfile.GitHubRepo
	if cfg.RepoSlug == "" {
		cfg.RepoSlug = inferGitHubRepo(context.Background(), activeProfile)
	}
	cfg.ConfigExists = existed
	return cfg, nil
}

func defaultAppConfig() AppConfig {
	return AppConfig{
		DefaultAgent: "codex",
		Agents: []AgentTool{
			{Name: "codex", Command: "codex"},
			{Name: "opencode", Command: "opencode"},
			{Name: "claude", Command: "claude"},
			{Name: "copilot", Command: "copilot"},
		},
	}
}

func loadAppConfig(homeDir string) (AppConfig, string, bool, error) {
	configPath := filepath.Join(homeDir, ".wt-manager.json")
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

func loadOrCreateAppConfig(homeDir string) (AppConfig, string, error) {
	appConfig, configPath, _, err := loadAppConfig(homeDir)
	return appConfig, configPath, err
}

func normalizeAppConfig(appConfig AppConfig) (AppConfig, bool, error) {
	changed := false
	defaults := defaultAppConfig()
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
		if profile.RemoteName == "" {
			profile.RemoteName = "origin"
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

func resolveProfilePaths(profile RepositoryProfile, homeDir string) RepositoryProfile {
	profile.RepositoryPath = expandPath(profile.RepositoryPath, homeDir)
	profile.WorktreesDir = expandPath(profile.WorktreesDir, homeDir)
	return profile
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
		resolved := resolveProfilePaths(profile, homeDir)
		if canonicalPath(resolved.RepositoryPath) == want {
			return true
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

func saveAppConfigCmd(cfg Config) tea.Cmd {
	return func() tea.Msg {
		if err := writeAppConfig(cfg.ConfigPath, cfg.App); err != nil {
			return actionResult{Err: fmt.Errorf("save config failed: %w", err)}
		}
		return actionResult{Message: fmt.Sprintf("Saved config %s", cfg.ConfigPath)}
	}
}

func saveAgentConfigCmd(cfg Config) tea.Cmd {
	return func() tea.Msg {
		if err := writeAppConfig(cfg.ConfigPath, cfg.App); err != nil {
			return actionResult{Err: fmt.Errorf("save config failed: %w", err)}
		}
		return actionResult{Message: fmt.Sprintf("Default AI agent set to %s", cfg.App.DefaultAgent)}
	}
}

func loadWorktreesCmd(cfg Config, showAll bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		worktrees, warning, err := collectWorktrees(ctx, cfg, showAll)
		return loadResult{Worktrees: worktrees, Warning: warning, Err: err}
	}
}

func collectWorktrees(ctx context.Context, cfg Config, showAll bool) ([]Worktree, string, error) {
	entries, err := discoverWorktrees(ctx, cfg)
	if err != nil {
		return nil, "", err
	}

	filtered := make([]Worktree, 0, len(entries))
	for _, wt := range entries {
		if showAll || wt.Managed {
			filtered = append(filtered, wt)
		}
	}

	prIndex, prWarning := loadPullRequestIndex(ctx, cfg)

	var warnings []string
	if prWarning != "" {
		warnings = append(warnings, prWarning)
	}
	enrichWorktrees(ctx, filtered, prIndex, &warnings)
	sortWorktrees(filtered)

	return filtered, strings.Join(warnings, " | "), nil
}

func discoverWorktrees(ctx context.Context, cfg Config) ([]Worktree, error) {
	repoPath := cfg.ActiveProfile.RepositoryPath
	output, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	entries := make([]Worktree, 0, 8)
	current := Worktree{}
	flush := func() {
		if current.Path == "" {
			return
		}
		current.Path = filepath.Clean(current.Path)
		current.IsMain = canonicalPath(current.Path) == canonicalPath(cfg.ActiveProfile.RepositoryPath)
		current.Managed = current.IsMain || pathInsideDir(current.Path, cfg.ActiveProfile.WorktreesDir)
		current.Name = worktreeName(current, cfg)
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

	return entries, nil
}

func enrichWorktrees(ctx context.Context, worktrees []Worktree, prIndex map[string]*PullRequest, warnings *[]string) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	var mu sync.Mutex

	for i := range worktrees {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			wt := &worktrees[idx]
			localWarnings := enrichWorktree(ctx, wt, prIndex)

			if len(localWarnings) == 0 {
				return
			}

			mu.Lock()
			*warnings = append(*warnings, localWarnings...)
			mu.Unlock()
		}(i)
	}

	wg.Wait()
}

func enrichWorktree(ctx context.Context, wt *Worktree, prIndex map[string]*PullRequest) []string {
	var warnings []string

	if wt.Missing {
		wt.LoadError = "path missing on disk"
		if wt.Prunable {
			wt.LoadError = "prunable worktree entry"
		}
		return warnings
	}

	status, err := loadRepoStatus(ctx, wt.Path)
	if err != nil {
		wt.LoadError = err.Error()
		warnings = append(warnings, fmt.Sprintf("status failed for %s", wt.Name))
	} else {
		wt.Status = status
	}

	commit, err := loadCommitInfo(ctx, wt.Path)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("commit failed for %s", wt.Name))
	} else {
		wt.Commit = commit
	}

	if wt.Detached || wt.Branch == "" || wt.Branch == "detached" {
		return warnings
	}

	if prIndex != nil {
		wt.PR = prIndex[wt.Branch]
	}

	return warnings
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

func loadPullRequestIndex(ctx context.Context, cfg Config) (map[string]*PullRequest, string) {
	if cfg.RepoSlug == "" {
		return nil, ""
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, "PR lookup unavailable: gh not found"
	}
	output, err := runCommand(ctx, cfg.ActiveProfile.RepositoryPath, "gh", "pr", "list", "--repo", cfg.RepoSlug, "--state", "all", "--limit", "200", "--json", "headRefName,number,title,url,state,body,author")
	if err != nil {
		return nil, "PR lookup unavailable: gh auth or repo access failed"
	}

	var prs []PullRequest
	if err := json.Unmarshal([]byte(output), &prs); err != nil {
		return nil, "PR lookup unavailable: gh returned invalid JSON"
	}

	index := make(map[string]*PullRequest, len(prs))
	for i := range prs {
		pr := prs[i]
		if pr.HeadRefName == "" {
			continue
		}
		prCopy := pr
		index[pr.HeadRefName] = &prCopy
	}

	return index, ""
}

func removeWorktreeCmd(cfg Config, wt Worktree) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

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

		return deleteResult{Path: wt.Path, Name: wt.Name, Err: err}
	}
}

func createWorktreeCmd(cfg Config, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()

		path := createWorktreePath(cfg, name)
		existing, err := createWorktree(ctx, cfg, name, path)
		return createResult{Path: path, Name: name, Existing: existing, Err: err}
	}
}

func createWorktree(ctx context.Context, cfg Config, branch string, path string) (bool, error) {
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

	_, _ = runCommand(ctx, repoPath, "git", "-C", repoPath, "fetch", "--quiet", remoteName, "--prune")

	if gitRefExists(ctx, repoPath, "refs/heads/"+branch) {
		_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", path, branch)
		return false, err
	}

	remoteRef := "refs/remotes/" + remoteName + "/" + branch
	if gitRefExists(ctx, repoPath, remoteRef) {
		_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", "--track", "-b", branch, path, remoteName+"/"+branch)
		return false, err
	}

	defaultRef := defaultRemoteRef(ctx, repoPath, remoteName)
	_, err := runCommand(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", "-b", branch, path, defaultRef)
	return false, err
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

func expireDeleteArmCmd(path string, armedAt time.Time) tea.Cmd {
	return tea.Tick(deleteConfirmWindow, func(time.Time) tea.Msg {
		return deleteArmExpired{Path: path, ArmedAt: armedAt}
	})
}

func createWorktreePath(cfg Config, name string) string {
	worktreeName := strings.ReplaceAll(name, "/", "-")
	return filepath.Join(cfg.ActiveProfile.WorktreesDir, worktreeName)
}

func openBrowserCmd(url string) tea.Cmd {
	return func() tea.Msg {
		cmdName := "open"
		args := []string{url}
		if runtime.GOOS == "linux" {
			cmdName = "xdg-open"
		}
		if runtime.GOOS == "windows" {
			cmdName = "cmd"
			args = []string{"/c", "start", url}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := runCommand(ctx, "", cmdName, args...)
		return actionResult{Message: "Opened PR in browser", Err: err}
	}
}

func openVSCodeWorktreeCmd(wt Worktree) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := runCommand(ctx, "", "code", "--new-window", wt.Path)
		if err == nil {
			return actionResult{Message: fmt.Sprintf("Opened %s in VS Code", wt.Name)}
		}

		if runtime.GOOS != "darwin" {
			return actionResult{Err: err}
		}

		fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer fallbackCancel()
		_, fallbackErr := runCommand(fallbackCtx, "", "open", "-a", "Visual Studio Code", wt.Path)
		return actionResult{Message: fmt.Sprintf("Opened %s in VS Code", wt.Name), Err: fallbackErr}
	}
}

func openAgentInGhosttyCmd(wt Worktree, agent AgentTool) tea.Cmd {
	return func() tea.Msg {
		if runtime.GOOS != "darwin" {
			return actionResult{Err: errors.New("opening Ghostty tabs is only supported on macOS")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		command := fmt.Sprintf("cd %s && %s", shellQuote(wt.Path), agent.Command)
		_, err := runCommand(ctx, "", "osascript", "-e", ghosttyTabScript(command))
		if err == nil {
			return actionResult{Message: fmt.Sprintf("Opened %s for %s", agent.Name, wt.Name)}
		}

		fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer fallbackCancel()
		_, fallbackErr := runCommand(fallbackCtx, "", "open", "-na", "Ghostty.app", "--args", "--working-directory="+wt.Path, "-e", "/bin/zsh", "-lc", agent.Command)
		if fallbackErr != nil {
			return actionResult{Err: fmt.Errorf("open Ghostty tab failed: %w. Grant Accessibility permission to wt-manager or your terminal", err)}
		}

		return actionResult{Message: fmt.Sprintf("Opened %s for %s in a new Ghostty window; tab automation was blocked", agent.Name, wt.Name)}
	}
}

func ghosttyTabScript(command string) string {
	commandLiteral := appleScriptString(command)
	return fmt.Sprintf(`set commandText to %s
tell application "Ghostty" to activate
delay 0.1
tell application "System Events"
	tell process "Ghostty"
		keystroke "t" using command down
		delay 0.2
		keystroke commandText
		key code 36
	end tell
end tell`, commandLiteral)
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
	output, err := cmd.CombinedOutput()
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
	cleanPath := canonicalPath(path)
	cleanDir := canonicalPath(dir)
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

func worktreeName(wt Worktree, cfg Config) string {
	if canonicalPath(wt.Path) == canonicalPath(cfg.ActiveProfile.RepositoryPath) {
		return cfg.ActiveProfile.Name
	}
	return filepath.Base(wt.Path)
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

func envFlag(name string) bool {
	value, ok := os.LookupEnv(name)
	if !ok {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

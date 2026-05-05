package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type setupStep int

const (
	setupStepRepo setupStep = iota
	setupStepProfile
	setupStepWorktrees
	setupStepAgents
	setupStepCustomName
	setupStepCustomCommand
	setupStepGitHub
)

type setupResult struct {
	Config    Config
	Message   string
	Startup   bool
	Cancelled bool
	Err       error
}

type setupModel struct {
	cfg            Config
	startup        bool
	step           setupStep
	repoPicker     directoryPicker
	worktreePicker directoryPicker
	profileInput   textinput.Model
	customName     textinput.Model
	customCommand  textinput.Model
	agentOptions   []setupAgentOption
	agentCursor    int
	repoPath       string
	worktreesDir   string
	warnings       []string
	status         string
}

type setupAgentOption struct {
	Name      string
	Command   string
	Installed bool
	Selected  bool
	Custom    bool
}

type directoryPicker struct {
	input    textinput.Model
	dirs     []string
	filtered []string
	cursor   int
	homeDir  string
}

func newSetupModel(cfg Config, startup bool) setupModel {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	repoPath := cfg.SetupRepo
	if repoPath == "" {
		if found, ok := inferRepoFromCWD(); ok {
			repoPath = found
		}
	}

	defaultRepo := repoPath
	if defaultRepo == "" {
		defaultRepo = cfg.HomeDir
	}

	profileName := ""
	worktreesDir := ""
	if repoPath != "" {
		profile := profileFromRepo(ctx, cfg.HomeDir, repoPath)
		profileName = profile.Name
		worktreesDir = profile.WorktreesDir
	}
	if profileName == "" {
		profileName = "default"
	}
	if worktreesDir == "" {
		worktreesDir = cfg.HomeDir
	}

	profileInput := textinput.New()
	profileInput.Prompt = "profile: "
	profileInput.Placeholder = "repo-name"
	profileInput.CharLimit = 80
	profileInput.Width = 48
	profileInput.SetValue(profileName)

	customName := textinput.New()
	customName.Prompt = "name: "
	customName.Placeholder = "my-agent"
	customName.CharLimit = 80
	customName.Width = 48

	customCommand := textinput.New()
	customCommand.Prompt = "command: "
	customCommand.Placeholder = "my-agent --flag"
	customCommand.CharLimit = 180
	customCommand.Width = 64

	step := setupStepProfile
	if repoPath == "" {
		step = setupStepRepo
	}
	focusSetupInput(step, &profileInput, &customName, &customCommand)

	seeds := setupDirectorySeeds(cfg.HomeDir, repoPath)
	repoPicker := newDirectoryPicker(cfg.HomeDir, seeds, defaultRepo)
	worktreePicker := newDirectoryPicker(cfg.HomeDir, setupDirectorySeeds(cfg.HomeDir, worktreesDir), worktreesDir)

	existingAgents := []AgentTool(nil)
	if cfg.ConfigExists {
		existingAgents = cfg.App.Agents
	}

	return setupModel{
		cfg:            cfg,
		startup:        startup,
		step:           step,
		repoPicker:     repoPicker,
		worktreePicker: worktreePicker,
		profileInput:   profileInput,
		customName:     customName,
		customCommand:  customCommand,
		agentOptions:   detectSetupAgents(existingAgents, cfg.App.DefaultAgent, exec.LookPath),
		repoPath:       repoPath,
		worktreesDir:   worktreesDir,
		status:         cfg.SetupReason,
	}
}

func focusSetupInput(step setupStep, profileInput, customName, customCommand *textinput.Model) {
	profileInput.Blur()
	customName.Blur()
	customCommand.Blur()
	switch step {
	case setupStepProfile:
		profileInput.Focus()
	case setupStepCustomName:
		customName.Focus()
	case setupStepCustomCommand:
		customCommand.Focus()
	}
}

func (s setupModel) Update(msg tea.Msg) (setupModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}

	switch key.String() {
	case "ctrl+c":
		return s, func() tea.Msg { return setupResult{Startup: s.startup, Cancelled: true} }
	case "esc":
		if s.startup {
			s.status = "Setup is required before wt-manager can start"
			return s, nil
		}
		return s, func() tea.Msg { return setupResult{Startup: s.startup, Cancelled: true} }
	}

	switch s.step {
	case setupStepRepo:
		return s.updateRepoStep(key)
	case setupStepProfile:
		return s.updateProfileStep(key)
	case setupStepWorktrees:
		return s.updateWorktreesStep(key)
	case setupStepAgents:
		return s.updateAgentsStep(key)
	case setupStepCustomName:
		return s.updateCustomNameStep(key)
	case setupStepCustomCommand:
		return s.updateCustomCommandStep(key)
	case setupStepGitHub:
		return s.updateGitHubStep(key)
	}
	return s, nil
}

func (s setupModel) updateRepoStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "enter":
		selected := s.repoPicker.SelectedPath()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		repoPath, ok := repoRootFromDir(ctx, expandPath(selected, s.cfg.HomeDir))
		if !ok {
			s.status = fmt.Sprintf("%s is not inside a git repository", selected)
			return s, nil
		}
		s.repoPath = repoPath
		if strings.TrimSpace(s.profileInput.Value()) == "" || s.profileInput.Value() == "default" {
			s.profileInput.SetValue(filepath.Base(repoPath))
		}
		if s.worktreesDir == "" || s.worktreesDir == s.cfg.HomeDir {
			s.worktreesDir = filepath.Dir(repoPath)
			s.worktreePicker = newDirectoryPicker(s.cfg.HomeDir, setupDirectorySeeds(s.cfg.HomeDir, s.worktreesDir), s.worktreesDir)
		}
		s.step = setupStepProfile
		s.profileInput.Focus()
		s.status = "Name this repository profile"
		return s, nil
	default:
		var cmd tea.Cmd
		s.repoPicker, cmd = s.repoPicker.Update(key)
		return s, cmd
	}
}

func (s setupModel) updateProfileStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "enter":
		name := strings.TrimSpace(s.profileInput.Value())
		if name == "" {
			s.status = "Profile name is required"
			return s, nil
		}
		s.step = setupStepWorktrees
		s.profileInput.Blur()
		s.status = "Choose a folder for managed worktrees"
		return s, nil
	default:
		var cmd tea.Cmd
		s.profileInput, cmd = s.profileInput.Update(key)
		return s, cmd
	}
}

func (s setupModel) updateWorktreesStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "enter":
		s.worktreesDir = expandPath(s.worktreePicker.SelectedPath(), s.cfg.HomeDir)
		if strings.TrimSpace(s.worktreesDir) == "" {
			s.status = "Worktrees folder is required"
			return s, nil
		}
		s.step = setupStepAgents
		s.status = "Select agent tools"
		return s, nil
	default:
		var cmd tea.Cmd
		s.worktreePicker, cmd = s.worktreePicker.Update(key)
		return s, cmd
	}
}

func (s setupModel) updateAgentsStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if s.agentCursor > 0 {
			s.agentCursor--
		}
		return s, nil
	case "down", "j":
		if s.agentCursor < len(s.agentOptions) {
			s.agentCursor++
		}
		return s, nil
	case " ":
		if s.agentCursor < len(s.agentOptions) {
			s.agentOptions[s.agentCursor].Selected = !s.agentOptions[s.agentCursor].Selected
		} else {
			s.step = setupStepCustomName
			s.customName.SetValue("")
			s.customCommand.SetValue("")
			s.customName.Focus()
			s.status = "Enter custom agent name"
		}
		return s, nil
	case "a":
		s.step = setupStepCustomName
		s.customName.SetValue("")
		s.customCommand.SetValue("")
		s.customName.Focus()
		s.status = "Enter custom agent name"
		return s, nil
	case "enter":
		if s.agentCursor == len(s.agentOptions) {
			s.step = setupStepCustomName
			s.customName.SetValue("")
			s.customCommand.SetValue("")
			s.customName.Focus()
			s.status = "Enter custom agent name"
			return s, nil
		}
		if len(selectedSetupAgents(s.agentOptions)) == 0 {
			s.status = "Select at least one agent tool"
			return s, nil
		}
		s.step = setupStepGitHub
		s.status = "Review GitHub CLI status"
		return s, nil
	}
	return s, nil
}

func (s setupModel) updateCustomNameStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "enter":
		if strings.TrimSpace(s.customName.Value()) == "" {
			s.status = "Custom agent name is required"
			return s, nil
		}
		s.step = setupStepCustomCommand
		s.customName.Blur()
		s.customCommand.Focus()
		s.status = "Enter custom agent command"
		return s, nil
	default:
		var cmd tea.Cmd
		s.customName, cmd = s.customName.Update(key)
		return s, cmd
	}
}

func (s setupModel) updateCustomCommandStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "enter":
		name := strings.TrimSpace(s.customName.Value())
		command := strings.TrimSpace(s.customCommand.Value())
		if command == "" {
			s.status = "Custom agent command is required"
			return s, nil
		}
		s.agentOptions = upsertSetupAgent(s.agentOptions, setupAgentOption{Name: name, Command: command, Installed: true, Selected: true, Custom: true})
		s.agentCursor = len(s.agentOptions) - 1
		s.step = setupStepAgents
		s.customCommand.Blur()
		s.status = fmt.Sprintf("Added %s", name)
		return s, nil
	default:
		var cmd tea.Cmd
		s.customCommand, cmd = s.customCommand.Update(key)
		return s, cmd
	}
}

func (s setupModel) updateGitHubStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "enter":
		cfg, warnings, err := completeSetupConfig(s)
		if err != nil {
			s.status = err.Error()
			return s, nil
		}
		if err := os.MkdirAll(expandPath(s.worktreesDir, s.cfg.HomeDir), 0o755); err != nil {
			s.status = fmt.Sprintf("Create worktrees folder failed: %v", err)
			return s, nil
		}
		if err := writeAppConfig(cfg.ConfigPath, cfg.App); err != nil {
			s.status = fmt.Sprintf("Save config failed: %v", err)
			return s, nil
		}
		cfg.SetupNeeded = false
		cfg.SetupReason = ""
		message := fmt.Sprintf("Configured profile %s", cfg.ActiveProfile.Name)
		if len(warnings) > 0 {
			message += ": " + strings.Join(warnings, " | ")
		}
		return s, func() tea.Msg { return setupResult{Config: cfg, Message: message, Startup: s.startup} }
	}
	return s, nil
}

func (s setupModel) View(width int) string {
	lines := []string{titleStyle.Render("Setup wt-manager"), ""}
	if s.status != "" {
		lines = append(lines, subtleStyle.Render(s.status), "")
	}

	switch s.step {
	case setupStepRepo:
		lines = append(lines, infoStyle.Render("Repository"), "Choose a git repository folder.", "", s.repoPicker.View(width))
	case setupStepProfile:
		lines = append(lines, infoStyle.Render("Profile"), fmt.Sprintf("Repository: %s", s.repoPath), "", s.profileInput.View())
	case setupStepWorktrees:
		lines = append(lines, infoStyle.Render("Worktrees Folder"), fmt.Sprintf("Repository: %s", s.repoPath), "", s.worktreePicker.View(width))
	case setupStepAgents:
		lines = append(lines, infoStyle.Render("Agent Tools"), "space toggles, a adds custom, enter continues", "")
		for idx, agent := range s.agentOptions {
			cursor := "  "
			style := subtleStyle
			if idx == s.agentCursor {
				cursor = "> "
				style = infoStyle.Copy().Bold(true)
			}
			checked := "[ ]"
			if agent.Selected {
				checked = "[x]"
			}
			installed := "missing"
			if agent.Installed {
				installed = "found"
			}
			if agent.Custom {
				installed = "custom"
			}
			lines = append(lines, style.Render(fmt.Sprintf("%s%s %-10s %-24s %s", cursor, checked, agent.Name, agent.Command, installed)))
		}
		addCursor := "  "
		addStyle := subtleStyle
		if s.agentCursor == len(s.agentOptions) {
			addCursor = "> "
			addStyle = infoStyle.Copy().Bold(true)
		}
		lines = append(lines, addStyle.Render(addCursor+"+ add custom agent"))
	case setupStepCustomName:
		lines = append(lines, infoStyle.Render("Custom Agent"), "", s.customName.View())
	case setupStepCustomCommand:
		lines = append(lines, infoStyle.Render("Custom Agent"), fmt.Sprintf("Name: %s", s.customName.Value()), "", s.customCommand.View())
	case setupStepGitHub:
		cfg, err := buildSetupConfig(s)
		lines = append(lines, infoStyle.Render("Review"), "")
		if err != nil {
			lines = append(lines, errorStyle.Render(err.Error()))
		} else {
			lines = append(lines,
				fmt.Sprintf("Profile: %s", cfg.ActiveProfile.Name),
				fmt.Sprintf("Repository: %s", cfg.ActiveProfile.RepositoryPath),
				fmt.Sprintf("Worktrees: %s", cfg.ActiveProfile.WorktreesDir),
				fmt.Sprintf("Agents: %s", strings.Join(agentNames(cfg.App.Agents), ", ")),
				fmt.Sprintf("Default agent: %s", cfg.App.DefaultAgent),
			)
			if cfg.RepoSlug != "" {
				lines = append(lines, fmt.Sprintf("GitHub: %s", cfg.RepoSlug))
			} else {
				lines = append(lines, "GitHub: disabled")
			}
			if _, err := os.Stat(cfg.ActiveProfile.WorktreesDir); errors.Is(err, os.ErrNotExist) {
				lines = append(lines, warnStyle.Render("Worktrees folder will be created on save."))
			}
			lines = append(lines, subtleStyle.Render("GitHub CLI will be checked on save; failures are warnings only."))
			lines = append(lines, "", subtleStyle.Render("enter saves, esc cancels"))
		}
	}

	return setupCardStyle(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func setupCardStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(max(64, min(96, width-6)))
}

func newDirectoryPicker(homeDir string, seeds []string, initial string) directoryPicker {
	input := textinput.New()
	input.Prompt = "search/path: "
	input.Placeholder = "~/projects"
	input.CharLimit = 240
	input.Width = 72
	input.SetValue(compactPath(initial, homeDir))
	input.CursorEnd()
	input.Focus()

	picker := directoryPicker{input: input, dirs: discoverDirectories(seeds, 3, 700), homeDir: homeDir}
	picker.refilter()
	return picker
}

func (p directoryPicker) Update(key tea.KeyMsg) (directoryPicker, tea.Cmd) {
	switch key.Type {
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil
	case tea.KeyDown:
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
		return p, nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(key)
	p.refilter()
	return p, cmd
}

func (p *directoryPicker) refilter() {
	query := strings.TrimSpace(p.input.Value())
	p.filtered = filterDirectories(p.dirs, query, p.homeDir)
	if p.cursor >= len(p.filtered) {
		p.cursor = max(0, len(p.filtered)-1)
	}
}

func (p directoryPicker) SelectedPath() string {
	query := strings.TrimSpace(p.input.Value())
	if query != "" && (strings.HasPrefix(query, "~") || strings.HasPrefix(query, "/") || strings.HasPrefix(query, ".")) {
		typed := expandPath(query, p.homeDir)
		if _, err := os.Stat(typed); err == nil {
			return typed
		}
		if len(p.filtered) == 0 {
			return typed
		}
	}
	if len(p.filtered) == 0 {
		return expandPath(query, p.homeDir)
	}
	return p.filtered[min(max(p.cursor, 0), len(p.filtered)-1)]
}

func (p directoryPicker) View(width int) string {
	lines := []string{p.input.View()}
	limit := min(10, len(p.filtered))
	for i := 0; i < limit; i++ {
		cursor := "  "
		style := subtleStyle
		if i == p.cursor {
			cursor = "> "
			style = infoStyle.Copy().Bold(true)
		}
		lines = append(lines, style.Render(cursor+compactPath(p.filtered[i], p.homeDir)))
	}
	if len(p.filtered) == 0 {
		lines = append(lines, mutedStyle.Render("  no matching folders; enter uses typed path"))
	}
	return strings.Join(lines, "\n")
}

func setupDirectorySeeds(homeDir string, paths ...string) []string {
	seeds := []string{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		expanded := expandPath(path, homeDir)
		seeds = append(seeds, expanded, filepath.Dir(expanded))
	}
	seeds = append(seeds,
		filepath.Join(homeDir, "projects"),
		filepath.Join(homeDir, "worktrees"),
		filepath.Join(homeDir, "src"),
		homeDir,
	)
	return uniqueCleanPaths(seeds)
}

func discoverDirectories(seeds []string, maxDepth int, maxDirs int) []string {
	seen := map[string]struct{}{}
	var dirs []string
	for _, seed := range uniqueCleanPaths(seeds) {
		walkDirectories(seed, 0, maxDepth, maxDirs, seen, &dirs)
		if len(dirs) >= maxDirs {
			break
		}
	}
	sort.Strings(dirs)
	return dirs
}

func walkDirectories(path string, depth int, maxDepth int, maxDirs int, seen map[string]struct{}, dirs *[]string) {
	if len(*dirs) >= maxDirs || depth > maxDepth {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return
	}
	clean := canonicalPath(path)
	if _, ok := seen[clean]; ok {
		return
	}
	seen[clean] = struct{}{}
	*dirs = append(*dirs, clean)

	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if len(*dirs) >= maxDirs || !entry.IsDir() || skipSetupDir(entry) {
			continue
		}
		walkDirectories(filepath.Join(path, entry.Name()), depth+1, maxDepth, maxDirs, seen, dirs)
	}
}

func skipSetupDir(entry fs.DirEntry) bool {
	name := entry.Name()
	if strings.HasPrefix(name, ".") {
		return true
	}
	return name == "node_modules" || name == "vendor" || name == "Library"
}

func uniqueCleanPaths(paths []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		result = append(result, clean)
	}
	return result
}

func filterDirectories(dirs []string, query string, homeDir string) []string {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return append([]string(nil), dirs...)
	}
	type scoredDir struct {
		path  string
		score int
	}
	var scored []scoredDir
	for _, dir := range dirs {
		candidate := strings.ToLower(compactPath(dir, homeDir))
		base := strings.ToLower(filepath.Base(dir))
		score := -1
		switch {
		case candidate == query:
			score = 0
		case strings.HasPrefix(candidate, query):
			score = 1
		case base == query:
			score = 2
		case strings.HasPrefix(base, query):
			score = 3
		case strings.Contains(candidate, query):
			score = 4
		case fuzzyMatch(candidate, query):
			score = 5
		case fuzzyMatch(base, query):
			score = 6
		}
		if score >= 0 {
			scored = append(scored, scoredDir{path: dir, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score < scored[j].score
		}
		left := strings.ToLower(compactPath(scored[i].path, homeDir))
		right := strings.ToLower(compactPath(scored[j].path, homeDir))
		if len(left) != len(right) {
			return len(left) < len(right)
		}
		return left < right
	})
	result := make([]string, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.path)
	}
	return result
}

func fuzzyMatch(candidate string, query string) bool {
	if query == "" {
		return true
	}
	idx := 0
	for _, r := range candidate {
		if idx < len(query) && byte(r) == query[idx] {
			idx++
		}
	}
	return idx == len(query)
}

func detectSetupAgents(existing []AgentTool, defaultAgent string, lookup func(string) (string, error)) []setupAgentOption {
	defaults := defaultAppConfig().Agents
	options := make([]setupAgentOption, 0, len(defaults)+len(existing))
	seen := map[string]struct{}{}
	for _, agent := range defaults {
		_, err := lookup(agent.Command)
		installed := err == nil
		options = append(options, setupAgentOption{Name: agent.Name, Command: agent.Command, Installed: installed, Selected: installed})
		seen[agent.Name] = struct{}{}
	}
	for _, agent := range existing {
		if _, ok := seen[agent.Name]; ok {
			for i := range options {
				if options[i].Name == agent.Name {
					options[i].Command = agent.Command
					options[i].Selected = true
					break
				}
			}
			continue
		}
		options = append(options, setupAgentOption{Name: agent.Name, Command: agent.Command, Installed: true, Selected: true, Custom: true})
		seen[agent.Name] = struct{}{}
	}
	return options
}

func selectedSetupAgents(options []setupAgentOption) []AgentTool {
	var agents []AgentTool
	for _, option := range options {
		if option.Selected {
			agents = append(agents, AgentTool{Name: option.Name, Command: option.Command})
		}
	}
	return agents
}

func upsertSetupAgent(options []setupAgentOption, next setupAgentOption) []setupAgentOption {
	for i := range options {
		if options[i].Name == next.Name {
			options[i] = next
			return options
		}
	}
	return append(options, next)
}

func completeSetupConfig(s setupModel) (Config, []string, error) {
	cfg, err := buildSetupConfig(s)
	if err != nil {
		return Config{}, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return cfg, checkGitHubCLI(ctx, cfg.RepoSlug), nil
}

func buildSetupConfig(s setupModel) (Config, error) {
	profileName := strings.TrimSpace(s.profileInput.Value())
	if profileName == "" {
		return Config{}, errors.New("profile name is required")
	}
	if strings.TrimSpace(s.repoPath) == "" {
		return Config{}, errors.New("repository path is required")
	}
	worktreesDir := expandPath(s.worktreesDir, s.cfg.HomeDir)
	if strings.TrimSpace(worktreesDir) == "" {
		return Config{}, errors.New("worktrees folder is required")
	}
	agents := selectedSetupAgents(s.agentOptions)
	if len(agents) == 0 {
		return Config{}, errors.New("select at least one agent tool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	profile := RepositoryProfile{
		Name:           profileName,
		RepositoryPath: compactPath(s.repoPath, s.cfg.HomeDir),
		WorktreesDir:   compactPath(worktreesDir, s.cfg.HomeDir),
		RemoteName:     "origin",
	}
	profile.GitHubRepo = inferGitHubRepo(ctx, resolveProfilePaths(profile, s.cfg.HomeDir))

	appConfig := s.cfg.App
	appConfig.Agents = agents
	if findAgent(agents, appConfig.DefaultAgent) == nil {
		appConfig.DefaultAgent = agents[0].Name
	}
	appConfig.Profiles = upsertProfile(appConfig.Profiles, profile, s.cfg.HomeDir)
	appConfig.DefaultProfile = profile.Name
	normalized, _, err := normalizeAppConfig(appConfig)
	if err != nil {
		return Config{}, err
	}

	active, err := activeProfile(normalized, s.cfg.HomeDir)
	if err != nil {
		return Config{}, err
	}
	repoSlug := active.GitHubRepo
	if repoSlug == "" {
		repoSlug = inferGitHubRepo(ctx, active)
	}

	return Config{
		HomeDir:       s.cfg.HomeDir,
		ConfigPath:    s.cfg.ConfigPath,
		App:           normalized,
		ActiveProfile: active,
		RepoSlug:      repoSlug,
		ConfigExists:  true,
	}, nil
}

func upsertProfile(profiles []RepositoryProfile, next RepositoryProfile, homeDir string) []RepositoryProfile {
	nextPath := canonicalPath(expandPath(next.RepositoryPath, homeDir))
	for i := range profiles {
		if profiles[i].Name == next.Name || canonicalPath(expandPath(profiles[i].RepositoryPath, homeDir)) == nextPath {
			profiles[i] = next
			return profiles
		}
	}
	return append(profiles, next)
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

func agentNames(agents []AgentTool) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	return names
}

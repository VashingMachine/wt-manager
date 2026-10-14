package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Setup is a wizard for one project: its name, its repositories, the agent
// tools, JIRA, and a review before anything is saved. esc steps back.

type setupStep int

const (
	setupStepProject setupStep = iota
	setupStepRepositories
	setupStepAgents
	setupStepCustomName
	setupStepCustomCommand
	setupStepJira
	setupStepReview
)

// setupStages are the steps shown in the wizard's progress line; the custom
// agent steps belong to Agents.
var setupStages = []struct {
	step  setupStep
	title string
}{
	{setupStepProject, "Project"},
	{setupStepRepositories, "Repositories"},
	{setupStepAgents, "Agents"},
	{setupStepJira, "JIRA"},
	{setupStepReview, "Review"},
}

type setupResult struct {
	Config    Config
	Message   string
	Startup   bool
	Cancelled bool
	Err       error
}

type setupModel struct {
	services     core.Services
	cfg          Config
	startup      bool
	step         setupStep
	projectInput textinput.Model
	// editing names the configured project being changed; it is empty while
	// setting up a new project.
	editing string
	// seededFor is the project name the repository list was built for.
	seededFor string
	// setupRepo is the repository setup was opened for. It joins whichever
	// project is chosen.
	setupRepo      string
	repositories   []core.Repository
	repoCursor     int
	editor         *repositoryEditor
	editIndex      int
	customName     textinput.Model
	customCommand  textinput.Model
	jiraBoardInput textinput.Model
	agentOptions   []setupAgentOption
	agentCursor    int
	jiraConfig     *JiraConfig
	review         *Config
	reviewErr      error
	status         string
	statusErr      bool
}

type setupAgentOption struct {
	Name      string
	Command   string
	Installed bool
	Selected  bool
	Custom    bool
}

// newSetupModel sets up the project of the repository setup was opened for
// (cfg.SetupRepo, else the current directory's): the project containing it,
// or a new project named after it.
func newSetupModel(cfg Config, startup bool, services core.Services) setupModel {
	repoPath := cfg.SetupRepo
	if repoPath == "" {
		if found, ok := services.InferRepoFromCWD(); ok {
			repoPath = found
		}
	}
	projectName := ""
	if repoPath != "" {
		projectName = core.RepositoryNameFromPath(repoPath)
		if idx, _, ok := findRepositoryOwner(services, cfg.App.Profiles, cfg.HomeDir, repoPath); ok {
			projectName = cfg.App.Profiles[idx].Name
		}
	}
	s := newSetupModelFor(cfg, startup, services, repoPath, projectName)
	if s.jiraConfig == nil && cfg.ActiveProfile.Jira != nil && repoPath != "" &&
		services.CanonicalPath(services.ExpandPath(cfg.ActiveProfile.RepositoryPath, cfg.HomeDir)) == services.CanonicalPath(repoPath) {
		s.jiraConfig = copyJiraConfig(cfg.ActiveProfile.Jira)
	}
	return s
}

// newProjectSetupModel sets up another project. The current directory's
// repository is offered when no project has it yet.
func newProjectSetupModel(cfg Config, services core.Services) setupModel {
	repoPath := ""
	if found, ok := services.InferRepoFromCWD(); ok {
		if _, _, owned := findRepositoryOwner(services, cfg.App.Profiles, cfg.HomeDir, found); !owned {
			repoPath = found
		}
	}
	projectName := core.RepositoryNameFromPath(repoPath)
	if services.FindProfile(cfg.App.Profiles, projectName) != nil {
		projectName = ""
	}
	cfg.SetupReason = "Set up a new project"
	return newSetupModelFor(cfg, false, services, repoPath, projectName)
}

func newSetupModelFor(cfg Config, startup bool, services core.Services, repoPath string, projectName string) setupModel {
	projectInput := textinput.New()
	projectInput.Prompt = ""
	projectInput.Placeholder = "my-project"
	projectInput.CharLimit = 80
	projectInput.Width = 48
	projectInput.SetValue(projectName)
	projectInput.CursorEnd()

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

	jiraBoardInput := textinput.New()
	jiraBoardInput.Prompt = "JIRA link: "
	jiraBoardInput.Placeholder = "https://site.atlassian.net/browse/KEY-123 or a board link"
	jiraBoardInput.CharLimit = 300
	jiraBoardInput.Width = 76

	existingAgents := []AgentTool(nil)
	if cfg.ConfigExists {
		existingAgents = cfg.App.Agents
	}

	s := setupModel{
		services:       services,
		cfg:            cfg,
		startup:        startup,
		step:           setupStepProject,
		projectInput:   projectInput,
		setupRepo:      repoPath,
		editIndex:      -1,
		customName:     customName,
		customCommand:  customCommand,
		jiraBoardInput: jiraBoardInput,
		agentOptions:   detectSetupAgents(services, existingAgents, cfg.App.DefaultAgent, exec.LookPath),
	}
	s.seedRepositories(projectName)
	s.focusStep()
	return s
}

// seedRepositories lists the repositories of the project called name, plus
// the repository setup was opened for.
func (s *setupModel) seedRepositories(name string) {
	s.seededFor = name
	s.editing = ""
	s.repositories = nil
	s.jiraConfig = nil
	if profile := s.services.FindProfile(s.cfg.App.Profiles, name); name != "" && profile != nil {
		s.editing = profile.Name
		s.repositories = slices.Clone(s.services.ResolveProfilePaths(*profile, s.cfg.HomeDir).Repositories)
		s.jiraConfig = copyJiraConfig(profile.Jira)
	}
	if s.setupRepo != "" && s.repositoryIndex(s.setupRepo) < 0 {
		s.repositories = append(s.repositories, s.defaultRepository(s.setupRepo))
	}
	s.repoCursor = 0
}

func (s setupModel) repositoryIndex(path string) int {
	want := s.services.CanonicalPath(s.services.ExpandPath(path, s.cfg.HomeDir))
	for idx, repository := range s.repositories {
		if s.services.CanonicalPath(repository.RepositoryPath) == want {
			return idx
		}
	}
	return -1
}

// defaultRepository describes a repository joining the list with the same
// defaults the repository editor offers.
func (s setupModel) defaultRepository(repoPath string) core.Repository {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	remote := "origin"
	if remotes := s.services.GitRemotes(ctx, repoPath); len(remotes) > 0 && !slices.Contains(remotes, "origin") {
		remote = remotes[0]
	}
	name := (RepositoryProfile{Repositories: s.repositories}).UniqueRepositoryName(core.RepositoryNameFromPath(repoPath))
	return core.Repository{
		Name:           name,
		RepositoryPath: repoPath,
		WorktreesDir:   core.SuggestWorktreesDir(s.repositories, name, repoPath),
		RemoteName:     remote,
	}
}

func (s *setupModel) focusStep() {
	s.projectInput.Blur()
	s.customName.Blur()
	s.customCommand.Blur()
	s.jiraBoardInput.Blur()
	switch s.step {
	case setupStepProject:
		s.projectInput.Focus()
	case setupStepCustomName:
		s.customName.Focus()
	case setupStepCustomCommand:
		s.customCommand.Focus()
	case setupStepJira:
		s.jiraBoardInput.Focus()
	}
}

func (s *setupModel) goTo(step setupStep, status string) {
	s.step = step
	s.status = status
	s.statusErr = false
	s.focusStep()
}

func (s *setupModel) fail(status string) {
	s.status = status
	s.statusErr = true
}

func (s setupModel) Update(msg tea.Msg) (setupModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	if key.String() == "ctrl+c" {
		return s, func() tea.Msg { return setupResult{Startup: s.startup, Cancelled: true} }
	}
	if s.editor != nil {
		return s.updateEditor(key)
	}
	if key.String() == "esc" {
		return s.back()
	}

	switch s.step {
	case setupStepProject:
		return s.updateProjectStep(key)
	case setupStepRepositories:
		return s.updateRepositoriesStep(key)
	case setupStepAgents:
		return s.updateAgentsStep(key)
	case setupStepCustomName:
		return s.updateCustomNameStep(key)
	case setupStepCustomCommand:
		return s.updateCustomCommandStep(key)
	case setupStepJira:
		return s.updateJiraStep(key)
	case setupStepReview:
		return s.updateReviewStep(key)
	}
	return s, nil
}

// back returns to the previous step; leaving the first step cancels setup
// unless the app cannot start without it.
func (s setupModel) back() (setupModel, tea.Cmd) {
	switch s.step {
	case setupStepProject:
		if s.startup {
			s.fail("Setup is required before wt-manager can start; ctrl+c quits")
			return s, nil
		}
		return s, func() tea.Msg { return setupResult{Startup: s.startup, Cancelled: true} }
	case setupStepRepositories:
		s.goTo(setupStepProject, "")
	case setupStepAgents:
		s.goTo(setupStepRepositories, "")
	case setupStepCustomName, setupStepCustomCommand:
		s.goTo(setupStepAgents, "")
	case setupStepJira:
		s.goTo(setupStepAgents, "")
	case setupStepReview:
		s.goTo(setupStepJira, "")
	}
	return s, nil
}

// --- project ------------------------------------------------------------

func (s setupModel) updateProjectStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	if key.String() != "enter" {
		var cmd tea.Cmd
		s.projectInput, cmd = s.projectInput.Update(key)
		return s, cmd
	}
	name := strings.TrimSpace(s.projectInput.Value())
	if name == "" {
		s.fail("Name the project")
		return s, nil
	}
	s.applyProjectName(name)
	s.enterRepositoriesStep()
	return s, nil
}

// applyProjectName follows a change of project name: naming another
// configured project switches to editing it, while a new name renames the
// edited project or names a new one.
func (s *setupModel) applyProjectName(name string) {
	if name == s.seededFor || name == s.editing {
		return
	}
	if s.services.FindProfile(s.cfg.App.Profiles, name) != nil {
		s.seedRepositories(name)
		return
	}
	if s.editing == "" {
		s.seededFor = name
	}
}

// projectHint says what saving under the typed name does.
func (s setupModel) projectHint() string {
	name := strings.TrimSpace(s.projectInput.Value())
	existing := s.services.FindProfile(s.cfg.App.Profiles, name)
	switch {
	case name == "":
		return "Name the project its repositories are managed under."
	case existing != nil:
		hint := fmt.Sprintf("Edits project %s (%s)", name, strings.Join(repositoryNames(existing.RepositoryList()), ", "))
		if s.setupRepo != "" {
			if _, _, owned := findRepositoryOwner(s.services, []RepositoryProfile{*existing}, s.cfg.HomeDir, s.setupRepo); !owned {
				hint += " and adds " + core.RepositoryNameFromPath(s.setupRepo)
			}
		}
		return hint
	case s.editing != "":
		return fmt.Sprintf("Renames project %s to %s", s.editing, name)
	default:
		return "Creates a new project"
	}
}

// --- repositories -----------------------------------------------------------

// Rows of the repositories step: one per repository, then two actions.
func (s setupModel) addRow() int      { return len(s.repositories) }
func (s setupModel) continueRow() int { return len(s.repositories) + 1 }

func (s *setupModel) enterRepositoriesStep() {
	s.goTo(setupStepRepositories, "")
	if len(s.repositories) == 0 {
		s.repoCursor = s.addRow()
		s.openAddEditor()
		return
	}
	s.repoCursor = s.continueRow()
}

func (s *setupModel) openAddEditor() {
	initial := s.cfg.HomeDir
	if len(s.repositories) > 0 {
		initial = filepath.Dir(s.repositories[0].RepositoryPath)
	}
	editor := newRepositoryEditor(s.services, s.cfg.HomeDir, s.repositories, initial)
	s.editor = &editor
	s.editIndex = -1
}

func (s *setupModel) openEditEditor(idx int) {
	siblings := slices.Delete(slices.Clone(s.repositories), idx, idx+1)
	editor := editRepositoryEditor(s.services, s.cfg.HomeDir, siblings, s.repositories[idx])
	s.editor = &editor
	s.editIndex = idx
}

func (s setupModel) updateEditor(key tea.KeyMsg) (setupModel, tea.Cmd) {
	editor, cmd := s.editor.Update(key)
	switch {
	case editor.cancelled:
		s.editor = nil
		s.status, s.statusErr = "", false
	case editor.done:
		s.editor = nil
		if s.editIndex >= 0 && s.editIndex < len(s.repositories) {
			s.repositories[s.editIndex] = editor.result
			s.status = "Updated " + editor.result.Name
		} else {
			s.repositories = append(s.repositories, editor.result)
			s.status = "Added " + editor.result.Name
		}
		s.statusErr = false
		s.repoCursor = s.continueRow()
	default:
		s.editor = &editor
	}
	return s, cmd
}

func (s setupModel) updateRepositoriesStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		s.repoCursor = max(0, s.repoCursor-1)
	case "down", "j":
		s.repoCursor = min(s.continueRow(), s.repoCursor+1)
	case "a":
		s.openAddEditor()
	case "d", "x", "backspace", "delete":
		if s.repoCursor < len(s.repositories) {
			removed := s.repositories[s.repoCursor].Name
			s.repositories = slices.Delete(s.repositories, s.repoCursor, s.repoCursor+1)
			s.repoCursor = min(s.repoCursor, s.continueRow())
			s.status, s.statusErr = "Removed "+removed+" from the list; nothing on disk changes", false
		}
	case "tab":
		return s.continueFromRepositories()
	case "enter":
		switch {
		case s.repoCursor < len(s.repositories):
			s.openEditEditor(s.repoCursor)
		case s.repoCursor == s.addRow():
			s.openAddEditor()
		default:
			return s.continueFromRepositories()
		}
	}
	return s, nil
}

func (s setupModel) continueFromRepositories() (setupModel, tea.Cmd) {
	if len(s.repositories) == 0 {
		s.fail("Add at least one repository")
		s.repoCursor = s.addRow()
		return s, nil
	}
	s.goTo(setupStepAgents, "")
	return s, nil
}

// --- agents -----------------------------------------------------------------

func (s *setupModel) openCustomAgent() {
	s.customName.SetValue("")
	s.customCommand.SetValue("")
	s.goTo(setupStepCustomName, "")
}

func (s setupModel) updateAgentsStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if s.agentCursor > 0 {
			s.agentCursor--
		}
	case "down", "j":
		if s.agentCursor < len(s.agentOptions) {
			s.agentCursor++
		}
	case " ":
		if s.agentCursor < len(s.agentOptions) {
			s.agentOptions[s.agentCursor].Selected = !s.agentOptions[s.agentCursor].Selected
		} else {
			s.openCustomAgent()
		}
	case "a":
		s.openCustomAgent()
	case "enter", "tab":
		if key.String() == "enter" && s.agentCursor == len(s.agentOptions) {
			s.openCustomAgent()
			return s, nil
		}
		if len(selectedSetupAgents(s.agentOptions)) == 0 {
			s.fail("Select at least one agent tool")
			return s, nil
		}
		s.jiraBoardInput.SetValue("")
		s.goTo(setupStepJira, "")
	}
	return s, nil
}

func (s setupModel) updateCustomNameStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	if key.String() != "enter" {
		var cmd tea.Cmd
		s.customName, cmd = s.customName.Update(key)
		return s, cmd
	}
	if strings.TrimSpace(s.customName.Value()) == "" {
		s.fail("Name the agent")
		return s, nil
	}
	s.goTo(setupStepCustomCommand, "")
	return s, nil
}

func (s setupModel) updateCustomCommandStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	if key.String() != "enter" {
		var cmd tea.Cmd
		s.customCommand, cmd = s.customCommand.Update(key)
		return s, cmd
	}
	name := strings.TrimSpace(s.customName.Value())
	command := strings.TrimSpace(s.customCommand.Value())
	if command == "" {
		s.fail("Enter the command that starts the agent")
		return s, nil
	}
	s.agentOptions = upsertSetupAgent(s.agentOptions, setupAgentOption{Name: name, Command: command, Installed: true, Selected: true, Custom: true})
	s.agentCursor = len(s.agentOptions) - 1
	s.goTo(setupStepAgents, "Added "+name)
	return s, nil
}

// --- JIRA -------------------------------------------------------------------

func (s setupModel) updateJiraStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	switch key.String() {
	case "ctrl+x":
		s.jiraConfig = nil
		s.jiraBoardInput.SetValue("")
		s.status, s.statusErr = "JIRA turned off for this project", false
		return s, nil
	case "enter":
		input := strings.TrimSpace(s.jiraBoardInput.Value())
		if input == "" {
			s.enterReviewStep()
			return s, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		board, err := s.services.ResolveJiraBoard(ctx, input)
		if err != nil {
			s.fail(fmt.Sprintf("Resolve JIRA link failed: %v", err))
			return s, nil
		}
		if s.jiraConfig == nil {
			site, err := s.services.JiraSite(ctx)
			if err != nil {
				s.fail(fmt.Sprintf("Determine JIRA site failed: %v", err))
				return s, nil
			}
			s.jiraConfig = &JiraConfig{
				Site:              site,
				MineOnly:          true,
				SprintOnly:        true,
				OpenOnly:          true,
				FiltersConfigured: true,
				Limit:             100,
			}
		}
		replaced := false
		for idx := range s.jiraConfig.Boards {
			if s.jiraConfig.Boards[idx].SourceID() == board.SourceID() {
				s.jiraConfig.Boards[idx] = board
				replaced = true
				break
			}
		}
		if !replaced {
			s.jiraConfig.Boards = append(s.jiraConfig.Boards, board)
			// Open tickets on everything configured, so a newly added board or
			// space shows up rather than staying behind the previous default.
			s.jiraConfig.DefaultBoard = core.JiraAllSources
		}
		s.jiraBoardInput.SetValue("")
		added := "board " + board.Name
		if board.IsSpace() {
			added = "space " + board.Name + ", listing all of its tickets"
		}
		s.status, s.statusErr = fmt.Sprintf("Added %s; paste another or press enter to review", added), false
		return s, nil
	}
	var cmd tea.Cmd
	s.jiraBoardInput, cmd = s.jiraBoardInput.Update(key)
	return s, cmd
}

// --- review -----------------------------------------------------------------

// enterReviewStep builds the config once, so rendering never runs git.
func (s *setupModel) enterReviewStep() {
	s.goTo(setupStepReview, "")
	cfg, err := buildSetupConfig(*s)
	s.review, s.reviewErr = nil, err
	if err == nil {
		s.review = &cfg
	}
}

func (s setupModel) updateReviewStep(key tea.KeyMsg) (setupModel, tea.Cmd) {
	if key.String() != "enter" {
		return s, nil
	}
	if s.reviewErr != nil {
		s.fail(s.reviewErr.Error())
		return s, nil
	}
	return s.finishSetup()
}

func (s setupModel) finishSetup() (setupModel, tea.Cmd) {
	cfg, warnings, err := completeSetupConfig(s)
	if err != nil {
		s.fail(err.Error())
		return s, nil
	}
	for _, repository := range cfg.Repositories() {
		if err := os.MkdirAll(repository.WorktreesDir, 0o755); err != nil {
			s.fail(fmt.Sprintf("Create worktrees folder failed: %v", err))
			return s, nil
		}
	}
	if err := s.services.WriteAppConfig(cfg.ConfigPath, cfg.App); err != nil {
		s.fail(fmt.Sprintf("Save config failed: %v", err))
		return s, nil
	}
	cfg.SetupNeeded = false
	cfg.SetupReason = ""
	message := fmt.Sprintf("Saved project %s", cfg.ActiveProfile.Name)
	if cfg.MultiRepository() {
		message += " (" + strings.Join(repositoryNames(cfg.ActiveProfile.Repositories), ", ") + ")"
	}
	if len(warnings) > 0 {
		message += ": " + strings.Join(warnings, " | ")
	}
	return s, func() tea.Msg { return setupResult{Config: cfg, Message: message, Startup: s.startup} }
}

// --- view ---------------------------------------------------------------------

func (s setupModel) View(width int) string {
	title := "Set up wt-manager"
	if s.editing != "" {
		title = "Edit project " + s.editing
	}
	lines := []string{titleStyle.Render(title)}
	if s.cfg.SetupReason != "" && s.step == setupStepProject {
		lines = append(lines, mutedStyle.Render(s.cfg.SetupReason))
	}
	lines = append(lines, "", s.stagesLine(), "")

	heading, intro, body, hints := s.stepContent(width)
	lines = append(lines, infoStyle.Bold(true).Render(heading))
	if intro != "" {
		lines = append(lines, subtleStyle.Render(intro))
	}
	lines = append(lines, "")
	lines = append(lines, body...)
	if s.status != "" {
		style, prefix := subtleStyle, ""
		if s.statusErr {
			style, prefix = errorStyle, icons.Err+" "
		}
		lines = append(lines, "", style.Render(prefix+s.status))
	}
	lines = append(lines, "", renderKeyHints(hints))
	return setupCardStyle(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// stagesLine shows the wizard's progress: done stages, the current one, and
// those still ahead.
func (s setupModel) stagesLine() string {
	current := s.step
	if current == setupStepCustomName || current == setupStepCustomCommand {
		current = setupStepAgents
	}
	parts := make([]string, 0, len(setupStages))
	for idx, stage := range setupStages {
		switch {
		case stage.step == current:
			parts = append(parts, keyHintKeyStyle.Render(fmt.Sprintf("%d %s", idx+1, stage.title)))
		case stage.step < current:
			parts = append(parts, successStyle.Render(icons.OK+" "+stage.title))
		default:
			parts = append(parts, mutedStyle.Render(fmt.Sprintf("%d %s", idx+1, stage.title)))
		}
	}
	return strings.Join(parts, mutedStyle.Render("  ›  "))
}

func (s setupModel) stepContent(width int) (string, string, []string, []keyHint) {
	switch s.step {
	case setupStepProject:
		body := []string{keyHintKeyStyle.Render("Name  ") + s.projectInput.View(), "", subtleStyle.Render(s.projectHint())}
		if others := s.otherProjects(); len(others) > 0 {
			body = append(body, mutedStyle.Render("Existing projects: "+strings.Join(others, ", ")+" — type one to edit it"))
		}
		return "Project", "A project groups repositories whose worktrees are listed together.", body, s.projectHints()
	case setupStepRepositories:
		if s.editor != nil {
			heading := "Add repository"
			if s.editIndex >= 0 {
				heading = "Edit repository"
			}
			return heading, "", s.editor.View(width), s.editor.hints()
		}
		return "Repositories", "Worktrees of every repository are listed together; new ones ask which repository to use.", s.repositoriesView(),
			[]keyHint{{"enter", "edit / choose"}, {"a", "add"}, {"d", "remove"}, {"tab", "continue"}, {"esc", "back"}}
	case setupStepAgents:
		return "Agents", "Tools that i and the agent Enter action open in a worktree.", s.agentsView(),
			[]keyHint{{"space", "toggle"}, {"a", "add custom"}, {"enter", "continue"}, {"esc", "back"}}
	case setupStepCustomName:
		return "Custom agent", "", []string{s.customName.View()}, []keyHint{{"enter", "next"}, {"esc", "back"}}
	case setupStepCustomCommand:
		return "Custom agent", "Name: " + s.customName.Value(), []string{s.customCommand.View()}, []keyHint{{"enter", "add"}, {"esc", "back"}}
	case setupStepJira:
		return "JIRA (optional)", "Paste a board link, or any link from a space (such as a ticket) to browse all of that space's tickets. Links are resolved through ACLI.", s.jiraView(),
			[]keyHint{{"enter", "add link / review"}, {"ctrl+x", "turn off JIRA"}, {"esc", "back"}}
	case setupStepReview:
		return "Review", "Nothing is saved until you confirm.", s.reviewView(),
			[]keyHint{{"enter", "save"}, {"esc", "back"}}
	}
	return "", "", nil, nil
}

func (s setupModel) projectHints() []keyHint {
	if s.startup {
		return []keyHint{{"enter", "continue"}, {"ctrl+c", "quit"}}
	}
	return []keyHint{{"enter", "continue"}, {"esc", "cancel"}}
}

func (s setupModel) otherProjects() []string {
	var names []string
	for _, profile := range s.cfg.App.Profiles {
		if profile.Name != s.editing {
			names = append(names, profile.Name)
		}
	}
	return names
}

func (s setupModel) repositoriesView() []string {
	nameWidth := 4
	for _, repository := range s.repositories {
		nameWidth = max(nameWidth, len(repository.Name))
	}
	var lines []string
	for idx, repository := range s.repositories {
		github := repository.GitHubRepo
		if github == "" {
			github = "from " + emptyDash(repository.RemoteName)
		}
		label := fmt.Sprintf("%-*s  %s", nameWidth, repository.Name, s.services.CompactPath(repository.RepositoryPath, s.cfg.HomeDir))
		detail := fmt.Sprintf("%*s  worktrees %s · GitHub %s", nameWidth, "", s.services.CompactPath(repository.WorktreesDir, s.cfg.HomeDir), github)
		lines = append(lines, selectorLine(idx == s.repoCursor, label, false), mutedStyle.Render("  "+detail))
	}
	if len(s.repositories) == 0 {
		lines = append(lines, mutedStyle.Render("  No repositories yet."))
	}
	lines = append(lines,
		"",
		selectorLine(s.repoCursor == s.addRow(), "+ Add repository", false),
		selectorLine(s.repoCursor == s.continueRow(), "Continue →", false),
	)
	return lines
}

func (s setupModel) agentsView() []string {
	var lines []string
	for idx, agent := range s.agentOptions {
		checked := "[ ]"
		if agent.Selected {
			checked = "[x]"
		}
		state := mutedStyle.Render("not installed")
		switch {
		case agent.Custom:
			state = purpleStyle.Render("custom")
		case agent.Installed:
			state = successStyle.Render("installed")
		}
		lines = append(lines, selectorLine(idx == s.agentCursor, fmt.Sprintf("%s %-10s %-24s", checked, agent.Name, agent.Command), false)+" "+state)
	}
	return append(lines, selectorLine(s.agentCursor == len(s.agentOptions), "+ Add custom agent", false))
}

func (s setupModel) jiraView() []string {
	var lines []string
	if s.jiraConfig != nil && len(s.jiraConfig.Boards) > 0 {
		for _, board := range s.jiraConfig.Boards {
			lines = append(lines, successStyle.Render(fmt.Sprintf("%s %s", icons.OK, board.Name))+mutedStyle.Render(" "+jiraSourceTag(board)))
		}
	} else {
		lines = append(lines, mutedStyle.Render("No boards or spaces; JIRA tickets stay off for this project."))
	}
	return append(lines, "", s.jiraBoardInput.View())
}

func (s setupModel) reviewView() []string {
	if s.reviewErr != nil {
		return []string{errorStyle.Render(icons.Err + " " + s.reviewErr.Error())}
	}
	if s.review == nil {
		return nil
	}
	cfg := *s.review
	field := func(label string, value string) string {
		return mutedStyle.Render(fmt.Sprintf("%-13s", label)) + value
	}
	project := cfg.ActiveProfile.Name
	switch {
	case s.editing == "":
		project += mutedStyle.Render("  new project")
	case s.editing != project:
		project += mutedStyle.Render("  renamed from " + s.editing)
	default:
		project += mutedStyle.Render("  updates the existing project")
	}
	lines := []string{field("Project", project), field("Repositories", "")}
	var notes []string
	repositories := cfg.Repositories()
	nameWidth := 4
	for _, repository := range repositories {
		nameWidth = max(nameWidth, len(repository.Name))
	}
	for _, repository := range repositories {
		github := repository.GitHubRepo
		if github == "" {
			github = warnStyle.Render("no GitHub")
		}
		lines = append(lines,
			"  "+infoStyle.Render(fmt.Sprintf("%-*s", nameWidth, repository.Name))+"  "+s.services.CompactPath(repository.RepositoryPath, cfg.HomeDir),
			mutedStyle.Render(fmt.Sprintf("  %*s  worktrees %s · GitHub ", nameWidth, "", s.services.CompactPath(repository.WorktreesDir, cfg.HomeDir)))+github,
		)
		if _, err := os.Stat(repository.WorktreesDir); errors.Is(err, os.ErrNotExist) {
			notes = append(notes, fmt.Sprintf("%s will be created", s.services.CompactPath(repository.WorktreesDir, cfg.HomeDir)))
		}
		if cfg.ForRepository(repository.Name).SharesWorktreesDir() {
			notes = append(notes, fmt.Sprintf("%s shares its worktrees folder; its worktree folders start with %q", repository.Name, repository.Name+"-"))
		}
	}
	jira := mutedStyle.Render("off")
	if cfg.ActiveProfile.Jira != nil {
		boards := make([]string, 0, len(cfg.ActiveProfile.Jira.Boards))
		for _, board := range cfg.ActiveProfile.Jira.Boards {
			boards = append(boards, board.Name)
		}
		jira = strings.Join(boards, ", ")
	}
	lines = append(lines,
		field("Agents", strings.Join(agentNames(cfg.App.Agents), ", ")+mutedStyle.Render(" · default "+cfg.App.DefaultAgent)),
		field("JIRA", jira),
	)
	if len(notes) > 0 {
		lines = append(lines, "")
		for _, note := range slices.Compact(notes) {
			lines = append(lines, subtleStyle.Render(icons.Section+" "+note))
		}
	}
	return append(lines, "", mutedStyle.Render("GitHub CLI access is checked on save; problems are only warnings."))
}

func setupCardStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(max(64, min(100, width-6)))
}

// --- agents helpers -----------------------------------------------------------

func detectSetupAgents(services core.Services, existing []AgentTool, defaultAgent string, lookup func(string) (string, error)) []setupAgentOption {
	defaults := services.DefaultAppConfig().Agents
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

// --- building the config -----------------------------------------------------

func completeSetupConfig(s setupModel) (Config, []string, error) {
	cfg, err := buildSetupConfig(s)
	if err != nil {
		return Config{}, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var warnings []string
	checked := map[string]bool{}
	for _, repository := range cfg.Repositories() {
		if checked[repository.GitHubRepo] {
			continue
		}
		checked[repository.GitHubRepo] = true
		for _, warning := range s.services.CheckGitHubCLI(ctx, repository.GitHubRepo) {
			if !slices.Contains(warnings, warning) {
				warnings = append(warnings, warning)
			}
		}
	}
	if cfg.ActiveProfile.Jira != nil {
		warnings = append(warnings, s.services.CheckAtlassianCLI(ctx)...)
	}
	return cfg, warnings, nil
}

// buildSetupConfig applies the wizard to the config: the edited project (or a
// new one) gets exactly the listed repositories, and becomes the default.
func buildSetupConfig(s setupModel) (Config, error) {
	name := strings.TrimSpace(s.projectInput.Value())
	if name == "" {
		return Config{}, errors.New("name the project")
	}
	if len(s.repositories) == 0 {
		return Config{}, errors.New("add at least one repository")
	}
	agents := selectedSetupAgents(s.agentOptions)
	if len(agents) == 0 {
		return Config{}, errors.New("select at least one agent tool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	repositories := make([]core.Repository, 0, len(s.repositories))
	for _, repository := range s.repositories {
		if repository.RemoteName == "" {
			repository.RemoteName = "origin"
		}
		if repository.GitHubRepo == "" {
			repository.GitHubRepo = s.services.InferGitHubRepo(ctx, RepositoryProfile{RepositoryPath: repository.RepositoryPath, RemoteName: repository.RemoteName})
		}
		repository.RepositoryPath = s.services.CompactPath(repository.RepositoryPath, s.cfg.HomeDir)
		repository.WorktreesDir = s.services.CompactPath(s.services.ExpandPath(repository.WorktreesDir, s.cfg.HomeDir), s.cfg.HomeDir)
		repositories = append(repositories, repository)
	}

	app := cloneAppConfig(s.cfg.App)
	target := -1
	for idx, profile := range app.Profiles {
		if s.editing != "" && profile.Name == s.editing {
			target = idx
		}
	}
	for idx, profile := range app.Profiles {
		if profile.Name == name && idx != target {
			return Config{}, fmt.Errorf("a project named %s already exists", name)
		}
	}

	profile := RepositoryProfile{Name: name, Jira: copyJiraConfig(s.jiraConfig)}
	if profile.Jira != nil && len(profile.Jira.Boards) == 0 {
		profile.Jira = nil
	}
	if profile.Jira != nil {
		if profile.Jira.DefaultBoard == "" {
			profile.Jira.DefaultBoard = core.JiraAllSources
		}
		if profile.Jira.Limit <= 0 {
			profile.Jira.Limit = 100
		}
	}
	// A single repository named after its folder keeps the compact form.
	if only := repositories[0]; len(repositories) == 1 && only.Name == core.RepositoryNameFromPath(only.RepositoryPath) {
		profile.RepositoryPath, profile.WorktreesDir, profile.RemoteName, profile.GitHubRepo = only.RepositoryPath, only.WorktreesDir, only.RemoteName, only.GitHubRepo
	} else {
		if target >= 0 {
			profile.LastRepository = app.Profiles[target].LastRepository
		}
		profile = profile.WithRepositories(repositories)
	}
	if target >= 0 {
		app.Profiles[target] = profile
	} else {
		app.Profiles = append(app.Profiles, profile)
	}
	app.Agents = agents
	if s.services.FindAgent(agents, app.DefaultAgent) == nil {
		app.DefaultAgent = agents[0].Name
	}
	app.DefaultProfile = name
	normalized, _, err := s.services.NormalizeAppConfig(app)
	if err != nil {
		return Config{}, err
	}
	selected := s.services.FindProfile(normalized.Profiles, name)
	if selected == nil {
		return Config{}, fmt.Errorf("project %q not found", name)
	}
	active := s.services.ResolveProject(ctx, *selected, s.cfg.HomeDir)
	return Config{
		HomeDir:       s.cfg.HomeDir,
		ConfigPath:    s.cfg.ConfigPath,
		App:           normalized,
		ActiveProfile: active,
		RepoSlug:      active.GitHubRepo,
		ConfigExists:  true,
	}, nil
}

// findRepositoryOwner finds the project a repository belongs to.
func findRepositoryOwner(services core.Services, profiles []RepositoryProfile, homeDir string, repoPath string) (int, core.Repository, bool) {
	want := services.CanonicalPath(services.ExpandPath(repoPath, homeDir))
	for idx, profile := range profiles {
		for _, repository := range profile.RepositoryList() {
			if services.CanonicalPath(services.ExpandPath(repository.RepositoryPath, homeDir)) == want {
				return idx, repository, true
			}
		}
	}
	return -1, core.Repository{}, false
}

func copyJiraConfig(jira *JiraConfig) *JiraConfig {
	if jira == nil {
		return nil
	}
	copyConfig := *jira
	copyConfig.Boards = append([]JiraBoard(nil), jira.Boards...)
	return &copyConfig
}

func repositoryNames(repositories []core.Repository) []string {
	names := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		names = append(names, repository.Name)
	}
	return names
}

func agentNames(agents []AgentTool) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	return names
}

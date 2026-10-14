package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// multiRepoSpy answers per repository, the way services answer for a Config
// scoped with ForRepository.
type multiRepoSpy struct {
	core.Services
	mu          sync.Mutex
	worktrees   map[string][]Worktree
	discoverErr map[string]error
	prIndexes   map[string][]PullRequest
	created     []string
	prLists     []string
	saved       []AppConfig
	openedFile  string
	openedPath  string
	cwdRepo     string
}

func (s *multiRepoSpy) InferRepoFromCWD() (string, bool) {
	return s.cwdRepo, s.cwdRepo != ""
}

func newMultiRepoSpy() *multiRepoSpy {
	return &multiRepoSpy{Services: services.NewService(), worktrees: map[string][]Worktree{}, discoverErr: map[string]error{}, prIndexes: map[string][]PullRequest{}}
}

func (s *multiRepoSpy) DiscoverWorktrees(_ context.Context, cfg Config, _ bool) ([]Worktree, error) {
	repository := cfg.RepositoryName()
	if err := s.discoverErr[repository]; err != nil {
		return nil, err
	}
	worktrees := append([]Worktree(nil), s.worktrees[repository]...)
	for i := range worktrees {
		worktrees[i].Repository = repository
	}
	return worktrees, nil
}

func (s *multiRepoSpy) PeekWorktrees(Config, bool) ([]Worktree, bool) { return nil, false }

func (s *multiRepoSpy) CollectTicketWorktrees(ctx context.Context, cfg Config) ([]Worktree, string, error) {
	worktrees, err := s.DiscoverWorktrees(ctx, cfg, true)
	return worktrees, "", err
}

func (s *multiRepoSpy) LoadTickets(context.Context, Config, JiraBoard, bool, bool, bool) ([]Ticket, string, error) {
	return []Ticket{{Key: "APP-1", Summary: "Fix checkout"}}, "", nil
}

func (s *multiRepoSpy) LoadPullRequestIndex(_ context.Context, cfg Config, _ bool) ([]PullRequest, string, error) {
	return s.prIndexes[cfg.RepoSlug], "", nil
}

func (s *multiRepoSpy) LoadRemotePullRequests(_ context.Context, cfg Config) ([]RemotePullRequest, error) {
	s.mu.Lock()
	s.prLists = append(s.prLists, cfg.RepoSlug)
	s.mu.Unlock()
	return nil, nil
}

func (s *multiRepoSpy) LoadGitHubCurrentUser(context.Context, Config) (string, error) {
	return "me", nil
}

func (s *multiRepoSpy) CreateWorktree(_ context.Context, cfg Config, branch string, path string) (bool, error) {
	s.mu.Lock()
	s.created = append(s.created, cfg.RepositoryName()+":"+path)
	s.mu.Unlock()
	return false, nil
}

func (s *multiRepoSpy) WriteAppConfig(_ string, app AppConfig) error {
	s.mu.Lock()
	s.saved = append(s.saved, app)
	s.mu.Unlock()
	return nil
}

func (s *multiRepoSpy) OpenEditorFile(_ context.Context, path string, _ core.EditorTool) error {
	s.openedFile = path
	return nil
}

func (s *multiRepoSpy) OpenEditorWorktree(_ context.Context, wt Worktree, _ core.EditorTool) error {
	s.openedPath = wt.Path
	return nil
}

func shopProfile() RepositoryProfile {
	return RepositoryProfile{
		Name: "shop",
		Repositories: []core.Repository{
			{Name: "api", RepositoryPath: "/src/api", WorktreesDir: "/wt/api", RemoteName: "origin", GitHubRepo: "owner/api"},
			{Name: "web", RepositoryPath: "/src/web", WorktreesDir: "/wt/web", RemoteName: "origin", GitHubRepo: "owner/web"},
		},
		LastRepository: "web",
		Jira:           &JiraConfig{Site: "example.atlassian.net", Boards: []JiraBoard{{ID: 7, Name: "Board"}}},
	}
}

// multiRepoTestModel is a model for the two-repository "shop" project, with
// the active profile resolved the way the app resolves it.
func multiRepoTestModel(spy *multiRepoSpy) model {
	raw := shopProfile()
	resolved := raw
	resolved.RepositoryPath, resolved.WorktreesDir, resolved.RemoteName, resolved.GitHubRepo = "/src/api", "/wt/api", "origin", "owner/api"
	m := NewModel(Config{
		RepoSlug:      "owner/api",
		ActiveProfile: resolved,
		App: AppConfig{
			DefaultProfile: "shop",
			Profiles:       []RepositoryProfile{raw},
			DefaultAgent:   "codex",
			Agents:         []AgentTool{{Name: "codex", Command: "codex"}},
		},
	}, spy)
	m.ready = true
	m.loading = false
	m.width, m.height = 160, 30
	m.allWorktrees = []Worktree{
		{Name: "api", Path: "/src/api", Branch: "main", IsMain: true, Managed: true, Repository: "api"},
		{Name: "feature", Path: "/wt/api/feature", Branch: "feature", Managed: true, Repository: "api"},
		{Name: "web", Path: "/src/web", Branch: "main", IsMain: true, Managed: true, Repository: "web"},
		{Name: "feature", Path: "/wt/web/feature", Branch: "feature", Managed: true, Repository: "web"},
	}
	m.resize()
	m.applyFilter()
	return m
}

func pressKeys(t *testing.T, m model, keys ...string) (model, []tea.Msg) {
	t.Helper()
	var msgs []tea.Msg
	for _, value := range keys {
		var msg tea.KeyMsg
		switch value {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		default:
			msg = teaKey(value)
		}
		next, cmd := m.Update(msg)
		m = next.(model)
		msgs = append(msgs, runPromptCommands(cmd)...)
	}
	return m, msgs
}

// runPromptCommands runs commands concurrently and keeps the messages of those
// that finish promptly, leaving timers such as prefetching to expire unseen.
func runPromptCommands(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	results := make(chan []tea.Msg, 1)
	go func() { results <- runCommandsConcurrently(cmd) }()
	select {
	case msgs := <-results:
		return msgs
	case <-time.After(time.Second):
		return nil
	}
}

func runCommandsConcurrently(cmd tea.Cmd) []tea.Msg {
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return runCommands(func() tea.Msg { return msg })
	}
	var (
		mu  sync.Mutex
		out []tea.Msg
		wg  sync.WaitGroup
	)
	done := make(chan struct{})
	for _, inner := range batch {
		if inner == nil {
			continue
		}
		wg.Go(func() {
			msgs := runCommandsConcurrently(inner)
			mu.Lock()
			out = append(out, msgs...)
			mu.Unlock()
		})
	}
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]tea.Msg(nil), out...)
}

// Assures that worktrees of every repository are listed with a Repo column only when the project has more than one repository, and that the filter matches repository names.
func TestWorktreeListShowsRepositoryColumnForMultiRepositoryProjects(t *testing.T) {
	m := multiRepoTestModel(newMultiRepoSpy())
	view := ansi.Strip(m.tableView())
	if !strings.Contains(strings.Split(view, "\n")[0], "Repo") {
		t.Fatalf("multi-repository header lacks a Repo column: %q", view)
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[2], "api") || !strings.Contains(lines[4], "web") || !strings.Contains(lines[4], "feature") {
		t.Fatalf("rows do not name their repositories: %q", view)
	}
	if !strings.Contains(ansi.Strip(m.headerView()), "api, web") {
		t.Fatalf("header does not list the project's repositories: %q", ansi.Strip(m.headerView()))
	}

	m.filterQuery = "web"
	m.applyFilter()
	if len(m.visibleWorktrees) != 2 || m.visibleWorktrees[0].Repository != "web" {
		t.Fatalf("filtering by repository = %#v", m.visibleWorktrees)
	}

	single := layoutTestModel(160, 30)
	single.allWorktrees = []Worktree{layoutTestWorktree()}
	single.applyFilter()
	if header := strings.Split(ansi.Strip(single.tableView()), "\n")[0]; strings.Contains(header, "Repo") {
		t.Fatalf("single-repository header shows a Repo column: %q", header)
	}
}

// Assures that loading worktrees lists every repository of the project in configured order, and an unreadable repository becomes a warning.
func TestLoadWorktreesListsEveryRepository(t *testing.T) {
	spy := newMultiRepoSpy()
	spy.worktrees["api"] = []Worktree{{Name: "api", Path: "/src/api", IsMain: true}}
	spy.worktrees["web"] = []Worktree{{Name: "web", Path: "/src/web", IsMain: true}, {Name: "fix", Path: "/wt/web/fix"}}
	m := multiRepoTestModel(spy)

	result := loadWorktreesCmd(spy, m.cfg, false)().(loadResult)
	if result.Err != nil || len(result.Worktrees) != 3 || result.Worktrees[0].Repository != "api" || result.Worktrees[2].Path != "/wt/web/fix" || result.Worktrees[2].Repository != "web" {
		t.Fatalf("project worktrees = %#v, err = %v", result.Worktrees, result.Err)
	}
	next, _ := m.Update(result)
	if got := next.(model); len(got.allWorktrees) != 3 {
		t.Fatalf("rows = %d, want every repository's worktrees", len(got.allWorktrees))
	}

	spy.discoverErr["web"] = errors.New("not a git repository")
	partial := loadWorktreesCmd(spy, m.cfg, false)().(loadResult)
	if partial.Err != nil || len(partial.Worktrees) != 1 || !strings.Contains(partial.Warning, "web: not a git repository") {
		t.Fatalf("partial load = %#v", partial)
	}
}

// Assures that creating a worktree asks for the repository every time, starting at the last used one, and creates it there.
func TestNewWorktreeAsksForRepositoryStartingAtLastUsed(t *testing.T) {
	spy := newMultiRepoSpy()
	m := multiRepoTestModel(spy)

	m, _ = pressKeys(t, m, "n")
	if !m.pickingRepository || m.creatingWorktree {
		t.Fatal("n did not ask for a repository first")
	}
	if m.cfg.Repositories()[m.repoPickerCursor].Name != "web" {
		t.Fatalf("picker starts at %d, want the last used repository", m.repoPickerCursor)
	}
	if modal := ansi.Strip(renderRepositoryPickerModal(m)); !strings.Contains(modal, "web") || !strings.Contains(modal, "last used") {
		t.Fatalf("picker does not highlight the last used repository: %q", modal)
	}

	m, _ = pressKeys(t, m, "k", "enter")
	if m.pickingRepository || !m.creatingWorktree || m.newWorktreeRepository != "api" {
		t.Fatalf("choosing api: picking=%v creating=%v repo=%q", m.pickingRepository, m.creatingWorktree, m.newWorktreeRepository)
	}
	if modal := ansi.Strip(renderNewWorktreeModal(m)); !strings.Contains(modal, "api") {
		t.Fatalf("branch input does not show the chosen repository: %q", modal)
	}
	m.newInput.SetValue("feature/cart")
	m, _ = pressKeys(t, m, "enter")
	if len(spy.created) != 1 || spy.created[0] != "api:/wt/api/feature-cart" {
		t.Fatalf("created = %v, want the worktree in api", spy.created)
	}
	if pending, ok := m.pendingCreations["/wt/api/feature-cart"]; !ok || pending.Worktree.Repository != "api" {
		t.Fatalf("optimistic row = %#v", pending)
	}
	if m.cfg.ActiveProfile.LastRepository != "api" || len(spy.saved) == 0 || spy.saved[len(spy.saved)-1].Profiles[0].LastRepository != "api" {
		t.Fatalf("last used repository was not remembered: %q / %d saves", m.cfg.ActiveProfile.LastRepository, len(spy.saved))
	}

	m, _ = pressKeys(t, m, "n")
	if m.cfg.Repositories()[m.repoPickerCursor].Name != "api" {
		t.Fatal("the next creation did not start at the repository used last")
	}
}

// Assures that a project with one repository never asks which repository to use.
func TestNewWorktreeSkipsRepositoryChoiceForSingleRepository(t *testing.T) {
	m, _ := enterActionTestModel()
	m, _ = pressKeys(t, m, "n")
	if m.pickingRepository || !m.creatingWorktree {
		t.Fatalf("single repository: picking=%v creating=%v", m.pickingRepository, m.creatingWorktree)
	}
}

func ticketModeModel(t *testing.T, spy *multiRepoSpy) model {
	t.Helper()
	m := multiRepoTestModel(spy)
	m.mode = modeTickets
	m.tickets = []Ticket{{Key: "APP-1", Summary: "Fix checkout"}}
	m.ticketsSource = m.ticketSource()
	m.table.SetRows(nil)
	m.table.SetColumns(m.activeColumns())
	m.applyTicketFilter()
	return m
}

// Assures that creating a worktree from a JIRA ticket asks for the repository and creates the ticket branch there.
func TestTicketWorktreeAsksForRepository(t *testing.T) {
	spy := newMultiRepoSpy()
	m := ticketModeModel(t, spy)

	m, _ = pressKeys(t, m, "n")
	if !m.pickingRepository || m.repoPickerPurpose != pickRepoForTicket {
		t.Fatal("n on a ticket did not ask for a repository")
	}
	if modal := ansi.Strip(renderRepositoryPickerModal(m)); !strings.Contains(modal, "APP-1") {
		t.Fatalf("picker does not name the ticket: %q", modal)
	}
	m, _ = pressKeys(t, m, "1")
	if len(spy.created) != 1 || spy.created[0] != "api:/wt/api/APP-1-fix-checkout" {
		t.Fatalf("created = %v, want the ticket worktree in api", spy.created)
	}
	if m.cfg.ActiveProfile.LastRepository != "api" {
		t.Fatal("the ticket's repository was not remembered")
	}
}

// Assures that choosing a repository where the ticket already has a worktree reuses it, and opening a ticket's worktree never asks.
func TestTicketWorktreeReusesWorktreeInChosenRepository(t *testing.T) {
	spy := newMultiRepoSpy()
	m := ticketModeModel(t, spy)
	existing := Worktree{Name: "web-APP-1-fix", Path: "/wt/web/APP-1-fix", Branch: "APP-1-fix", Managed: true, Repository: "web"}
	m.allWorktrees = append(m.allWorktrees, existing)

	m, _ = pressKeys(t, m, "n", "2")
	if len(spy.created) != 0 || m.pendingWorktreePath != existing.Path {
		t.Fatalf("created = %v, pending = %q; want the existing web worktree selected", spy.created, m.pendingWorktreePath)
	}

	m.tickets[0].Worktree = &existing
	m.applyTicketFilter()
	m, msgs := pressKeys(t, m, "v")
	if m.pickingRepository || spy.openedPath != existing.Path || len(messagesOf[actionResult](msgs)) != 1 {
		t.Fatalf("opening the ticket worktree: picking=%v opened=%q", m.pickingRepository, spy.openedPath)
	}
	if detail := ansi.Strip(m.detail.View()); !strings.Contains(detail, "web") || !strings.Contains(detail, existing.Path) {
		t.Fatalf("ticket details do not list the worktree with its repository: %q", detail)
	}
}

// Assures that ticket rows are matched against worktrees and PRs of every repository in the project.
func TestTicketLoadMatchesEveryRepository(t *testing.T) {
	spy := newMultiRepoSpy()
	spy.worktrees["web"] = []Worktree{{Name: "APP-1-fix", Path: "/wt/web/APP-1-fix", Branch: "APP-1-fix", Managed: true}}
	spy.prIndexes["owner/web"] = []PullRequest{{Number: 9, HeadRefName: "APP-1-fix", State: "OPEN"}}
	m := multiRepoTestModel(spy)

	result := loadTicketsCmd(spy, m.cfg, JiraBoard{ID: 7}, true, true, false)().(ticketListResult)
	if len(result.Tickets) != 1 || result.Tickets[0].Worktree == nil || result.Tickets[0].Worktree.Repository != "web" || result.Tickets[0].PR == nil || result.Tickets[0].PR.Number != 9 {
		t.Fatalf("ticket = %#v, want the web worktree and PR attached", result.Tickets)
	}
	m.mode = modeTickets
	m.tickets = result.Tickets
	m.applyTicketFilter()
	if row := strings.Join(m.ticketRowCells(m.tickets[0], false), " "); !strings.Contains(row, "web/APP-1-fix") {
		t.Fatalf("ticket row does not name the worktree's repository: %q", row)
	}
}

// Assures that worktrees get PR labels from their own repository when the same branch exists in several.
func TestPRLabelsFollowWorktreeRepository(t *testing.T) {
	m := multiRepoTestModel(newMultiRepoSpy())
	for _, result := range []prIndexResult{
		{Repository: "api", RepoSlug: "owner/api", PullRequests: []PullRequest{{Number: 1, HeadRefName: "feature", State: "OPEN"}}},
		{Repository: "web", RepoSlug: "owner/web", PullRequests: []PullRequest{{Number: 2, HeadRefName: "feature", State: "OPEN"}}},
		{Repository: "web", RepoSlug: "owner/api", PullRequests: []PullRequest{{Number: 3, HeadRefName: "feature", State: "OPEN"}}},
	} {
		next, _ := m.Update(result)
		m = next.(model)
	}
	labels := map[string]int{}
	for _, wt := range m.allWorktrees {
		if wt.PR != nil {
			labels[wt.Path] = wt.PR.Number
		}
	}
	if labels["/wt/api/feature"] != 1 || labels["/wt/web/feature"] != 2 {
		t.Fatalf("PR labels = %v, want each repository's own PR", labels)
	}
}

// Assures that PR Radar shows one repository at a time and R switches it, ignoring PR lists of the previous repository.
func TestPRRadarSwitchesRepository(t *testing.T) {
	spy := newMultiRepoSpy()
	m := multiRepoTestModel(spy)
	m, _ = pressKeys(t, m, "P")
	if len(spy.prLists) != 1 || spy.prLists[0] != "owner/api" {
		t.Fatalf("PR lists = %v, want the first repository", spy.prLists)
	}

	m, _ = pressKeys(t, m, "R")
	if !m.pickingRepository || m.repoPickerPurpose != pickRepoForPRRadar || m.repoPickerCursor != 0 {
		t.Fatalf("R: picking=%v purpose=%v cursor=%d", m.pickingRepository, m.repoPickerPurpose, m.repoPickerCursor)
	}
	m, _ = pressKeys(t, m, "2")
	if m.prCfg().RepoSlug != "owner/web" || spy.prLists[len(spy.prLists)-1] != "owner/web" {
		t.Fatalf("PR Radar repository = %q, lists = %v", m.prCfg().RepoSlug, spy.prLists)
	}
	next, _ := m.Update(remotePRListResult{RepoSlug: "owner/api", PullRequests: []RemotePullRequest{remotePRFixture(5, "OPEN", "me", "old", "old")}})
	if got := next.(model); len(got.remotePRs) != 0 {
		t.Fatal("a PR list of the previous repository replaced the current one")
	}
	if title := ansi.Strip(m.View()); !strings.Contains(title, "PR Radar") || !strings.Contains(title, "web") {
		t.Fatalf("PR Radar does not show its repository: %q", title)
	}
}

// Assures that removing a repository from a project needs a second enter and keeps the rest of the project.
func TestRemoveRepositoryRequiresConfirmation(t *testing.T) {
	spy := newMultiRepoSpy()
	m := multiRepoTestModel(spy)
	m, _ = choosePaletteEntry(t, m, "repo-remove")
	if !m.pickingRepository || m.repoPickerPurpose != pickRepoToRemove {
		t.Fatal("remove repository did not open the picker")
	}
	m, _ = pressKeys(t, m, "2")
	if !m.pickingRepository || m.repoRemoveArmed != "web" || len(m.cfg.App.Profiles[0].RepositoryList()) != 2 {
		t.Fatal("the first choice removed the repository without confirmation")
	}
	m, _ = pressKeys(t, m, "enter")
	if m.pickingRepository || m.cfg.MultiRepository() || len(m.cfg.App.Profiles[0].RepositoryList()) != 1 || m.cfg.App.Profiles[0].RepositoryList()[0].Name != "api" {
		t.Fatalf("after confirming, repositories = %#v", m.cfg.App.Profiles[0].RepositoryList())
	}
	if len(spy.saved) == 0 || len(spy.saved[len(spy.saved)-1].Profiles[0].Repositories) != 1 {
		t.Fatal("removal was not saved")
	}
}

// Assures that the palette offers config editing and repository helpers, the multi-repository ones only when they apply, and opens the config file in the editor.
func TestPaletteOffersConfigurationHelpers(t *testing.T) {
	ids := func(m model) map[string]bool {
		found := map[string]bool{}
		for _, entry := range m.paletteEntries() {
			found[entry.ID] = true
		}
		return found
	}
	single, _ := enterActionTestModel()
	entries := ids(single)
	for _, id := range []string{"config-edit", "config-reload", "repo-add"} {
		if !entries[id] {
			t.Fatalf("palette lacks %s", id)
		}
	}
	if entries["repo-remove"] || entries["pr-repo"] {
		t.Fatal("single-repository project offers multi-repository commands")
	}

	spy := newMultiRepoSpy()
	m := multiRepoTestModel(spy)
	m.cfg.ConfigPath = "/home/me/.wt-manager.json"
	if entries := ids(m); !entries["repo-remove"] || !entries["pr-repo"] {
		t.Fatal("multi-repository project lacks remove repository or PR Radar repository")
	}
	_, cmd := choosePaletteEntry(t, m, "config-edit")
	runCommands(cmd)
	if spy.openedFile != "/home/me/.wt-manager.json" {
		t.Fatalf("opened %q, want the config file", spy.openedFile)
	}
}

// Assures that a terminal editor named by $VISUAL or $EDITOR is offered for the config.
func TestTerminalEditorFromEnvironment(t *testing.T) {
	env := map[string]string{"EDITOR": "vim"}
	if got := terminalEditor(func(key string) string { return env[key] }); got != "vim" {
		t.Fatalf("terminalEditor() = %q, want vim", got)
	}
	env["VISUAL"] = "nvim -p"
	if got := terminalEditor(func(key string) string { return env[key] }); got != "nvim -p" {
		t.Fatalf("terminalEditor() = %q, want $VISUAL first", got)
	}
	if got := terminalEditor(func(string) string { return "" }); got != "" {
		t.Fatalf("terminalEditor() = %q, want none", got)
	}
}

// configFileModel is a single-repository model whose config lives in a real file.
func configFileModel(t *testing.T) (model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".wt-manager.json")
	svc := services.NewService()
	app := svc.DefaultAppConfig()
	app.DefaultProfile = "shop"
	app.Profiles = []RepositoryProfile{{Name: "shop", RepositoryPath: "/src/api", WorktreesDir: "/wt/api", RemoteName: "origin", GitHubRepo: "owner/api"}}
	if err := svc.WriteAppConfig(path, app); err != nil {
		t.Fatalf("WriteAppConfig() error = %v", err)
	}
	m := NewModel(Config{HomeDir: dir, ConfigPath: path, App: app, ActiveProfile: svc.ResolveProfilePaths(app.Profiles[0], dir), RepoSlug: "owner/api", ConfigExists: true}, newMultiRepoSpy())
	m.ready = true
	m.loading = false
	m.width, m.height = 140, 30
	m.resize()
	return m, path
}

// writeConfigFile replaces the config file as an editor would, with a new modification time.
func writeConfigFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}
}

const twoRepositoryConfig = `{
  "defaultProfile": "shop",
  "defaultAgent": "codex",
  "agents": [{"name": "codex", "command": "codex"}],
  "profiles": [{
    "name": "shop",
    "repositoryPath": "/src/api",
    "worktreesDir": "/wt/api",
    "githubRepo": "owner/api",
    "repositories": [{"name": "web", "repositoryPath": "/src/web", "worktreesDir": "/wt/web", "githubRepo": "owner/web"}]
  }]
}`

// Assures that config edits saved outside the app are applied while it runs, reloading the project when its repositories change.
func TestConfigFileEditedOutsideAppIsApplied(t *testing.T) {
	m, path := configFileModel(t)
	if result := m.checkConfigFileCmd()(); result != nil {
		t.Fatalf("an unchanged config file reported %#v", result)
	}
	writeConfigFile(t, path, twoRepositoryConfig)

	result, ok := m.checkConfigFileCmd()().(configFileResult)
	if !ok || !result.Changed {
		t.Fatalf("edited config result = %#v", result)
	}
	next, cmd := m.Update(result)
	m = next.(model)
	if !m.cfg.MultiRepository() || m.cfg.ActiveProfile.Repository("web") == nil || cmd == nil || !m.loading {
		t.Fatalf("project after reload = %#v (loading %v)", m.cfg.ActiveProfile.Repositories, m.loading)
	}
	if m.cfg.App.DefaultAgent != "codex" || len(m.cfg.App.Agents) != 1 {
		t.Fatalf("app settings were not reloaded: %#v", m.cfg.App.Agents)
	}
	if again := m.checkConfigFileCmd()(); again != nil {
		t.Fatalf("an applied edit was reported again: %#v", again)
	}
	// The table gains a Repo column once the project has two repositories.
	next, _ = m.Update(loadResult{Worktrees: []Worktree{{Name: "web", Path: "/src/web", IsMain: true, Managed: true, Repository: "web"}}, Project: projectKey(m.cfg)})
	m = next.(model)
	if columns := m.table.Columns(); len(columns) != 7 || columns[0].Title != "Repo" {
		t.Fatalf("columns after reload = %#v", columns)
	}
}

// Assures that a config file with errors is reported and protected from in-app saves until it is fixed.
func TestConfigFileWithErrorsIsProtected(t *testing.T) {
	m, path := configFileModel(t)
	broken := `{"defaultProfile": "shop", "profiles": [`
	writeConfigFile(t, path, broken)

	result := m.checkConfigFileCmd()().(configFileResult)
	next, _ := m.Update(result)
	m = next.(model)
	if result.Err == nil || !m.statusError || !strings.Contains(m.statusMessage, "errors") {
		t.Fatalf("broken config: result=%#v status=%q", result, m.statusMessage)
	}
	m.cfg.App.DefaultAgent = "claude"
	saved, ok := m.persistConfig("Default agent set")().(actionResult)
	if !ok || saved.Err == nil || !strings.Contains(saved.Err.Error(), "fix") {
		t.Fatalf("saving over a broken config = %#v", saved)
	}
	if data, _ := os.ReadFile(path); string(data) != broken {
		t.Fatalf("the broken file was overwritten: %s", data)
	}

	writeConfigFile(t, path, twoRepositoryConfig)
	if fixed := m.checkConfigFileCmd()().(configFileResult); !fixed.Changed {
		t.Fatalf("fixed config = %#v", fixed)
	}
}

// Assures that an in-app setting change never overwrites edits saved to the file since it was read; the edits are applied instead.
func TestPersistConfigKeepsExternalEdits(t *testing.T) {
	m, path := configFileModel(t)
	writeConfigFile(t, path, twoRepositoryConfig)
	m.cfg.App.DefaultAgent = "opencode"

	result, ok := m.persistConfig("Default AI agent set to opencode")().(configFileResult)
	if !ok || !result.Changed || result.Rejected != "Default AI agent set to opencode" {
		t.Fatalf("save after an external edit = %#v", result)
	}
	if data, _ := os.ReadFile(path); string(data) != twoRepositoryConfig {
		t.Fatalf("external edit was overwritten: %s", data)
	}
	next, _ := m.Update(result)
	if got := next.(model); !got.cfg.MultiRepository() || !strings.Contains(got.statusMessage, "was not saved") {
		t.Fatalf("after reload: multi=%v status=%q", got.cfg.MultiRepository(), got.statusMessage)
	}
}

// Assures that the app's own saves are not mistaken for external edits.
func TestOwnConfigSavesAreNotReloaded(t *testing.T) {
	m, path := configFileModel(t)
	m.services = services.NewService()
	m.cfg.App.DefaultAgent = "claude"
	if result, ok := m.persistConfig("Saved")().(actionResult); !ok || result.Err != nil {
		t.Fatalf("save = %#v", result)
	}
	later := time.Now().Add(2 * time.Minute)
	_ = os.Chtimes(path, later, later)
	if result := m.checkConfigFileCmd()(); result != nil {
		t.Fatalf("own save reported as external change: %#v", result)
	}
}

// Assures that the add-repository helper turns a project into a multi-repository one, with defaults following the project's layout.
func TestAddRepositoryHelperAddsRepositoryToProject(t *testing.T) {
	cfg := testGitRepo(t)
	web := filepath.Join(cfg.HomeDir, "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	runGit(t, web, "init")
	svc := services.NewService()
	cfg.ConfigPath = filepath.Join(cfg.HomeDir, ".wt-manager.json")
	cfg.App = svc.DefaultAppConfig()
	cfg.App.DefaultProfile = "test"
	cfg.App.Profiles = []RepositoryProfile{cfg.ActiveProfile}
	if err := svc.WriteAppConfig(cfg.ConfigPath, cfg.App); err != nil {
		t.Fatalf("WriteAppConfig() error = %v", err)
	}
	cfg.ActiveProfile = svc.ResolveProject(context.Background(), cfg.ActiveProfile, cfg.HomeDir)
	m := NewModel(cfg, svc)
	m.ready = true
	m.width, m.height = 140, 30

	m, _ = choosePaletteEntry(t, m, "repo-add")
	if m.addingRepository == nil || !m.addingRepository.choosingFolder {
		t.Fatal("add repository did not ask for a folder")
	}
	m.addingRepository.folder.input.SetValue(cfg.ActiveProfile.RepositoryPath)
	m.addingRepository.folder.refilter()
	m, _ = pressKeys(t, m, "enter")
	if m.addingRepository == nil || !strings.Contains(m.addingRepository.err, "already part") {
		t.Fatalf("adding a repository twice was accepted: %#v", m.addingRepository)
	}

	m.addingRepository.folder.input.SetValue(web)
	m.addingRepository.folder.refilter()
	m, _ = pressKeys(t, m, "enter")
	form := m.addingRepository
	if form == nil || form.choosingFolder || form.nameInput.Value() != "web" || form.worktrees.input.Value() == "" {
		t.Fatalf("details step = %#v", form)
	}
	m, msgs := pressKeys(t, m, "enter", "enter")
	if m.addingRepository != nil || !m.cfg.MultiRepository() || m.cfg.ActiveProfile.Repository("web") == nil {
		t.Fatalf("project after adding = %#v", m.cfg.ActiveProfile.Repositories)
	}
	saved := m.cfg.App.Profiles[0]
	if saved.RepositoryPath != "" || len(saved.Repositories) != 2 || saved.Repositories[0].Name != "app" || saved.Repositories[1].Name != "web" {
		t.Fatalf("saved profile = %#v", saved)
	}
	if results := messagesOf[actionResult](msgs); len(results) == 0 || results[len(results)-1].Err != nil {
		t.Fatalf("save results = %#v", results)
	}
	written, err := svc.ReadAppConfig(cfg.ConfigPath)
	if err != nil || len(written.Profiles[0].Repositories) != 2 {
		t.Fatalf("config file = %#v, err = %v", written.Profiles, err)
	}
}

// Assures that PR labels are requested for every repository, and that a forced refresh is not skipped while earlier label loads run, unlike background revalidation.
func TestPRLabelLoadsCoverEveryRepository(t *testing.T) {
	m := multiRepoTestModel(newMultiRepoSpy())
	m.prIndexLoading = map[string]bool{"api": true, "web": true}
	if cmd := m.loadPRIndexesCmd(false, true); cmd != nil {
		t.Fatal("background revalidation re-requested labels that are still loading")
	}
	results := messagesOf[prIndexResult](runCommands(m.loadPRIndexesCmd(true, false)))
	if len(results) != 2 || results[0].RepoSlug == results[1].RepoSlug {
		t.Fatalf("forced refresh results = %#v, want one per repository", results)
	}
}

// Assures that the Repo column keeps worktree tables within their width and leaves room for full state labels at common sizes.
func TestRepositoryColumnsFitTableWidth(t *testing.T) {
	for _, width := range []int{40, 60, 70, 71, 80, 96, 160} {
		columns := buildRepositoryColumns(width)
		if columns[0].Title != "Repo" {
			t.Fatalf("first column = %q, want Repo", columns[0].Title)
		}
		if rendered := lipgloss.Width(renderTableHeader(columns)); rendered > width {
			t.Fatalf("rendered header width = %d, want <= %d for columns %#v", rendered, width, columns)
		}
	}
	m := multiRepoTestModel(newMultiRepoSpy())
	m.width = 150
	m.resize()
	for _, column := range m.table.Columns() {
		if column.Title == "State" && column.Width < len("+ clean") {
			t.Fatalf("State column width = %d at 150 columns, too narrow for its labels", column.Width)
		}
	}
	for _, line := range strings.Split(m.tableView(), "\n") {
		if width := lipgloss.Width(line); width > m.table.Width() {
			t.Fatalf("table line width = %d, want <= %d: %q", width, m.table.Width(), ansi.Strip(line))
		}
	}
}

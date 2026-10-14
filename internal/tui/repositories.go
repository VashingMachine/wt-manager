package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	tea "github.com/charmbracelet/bubbletea"
)

// A project can span several repositories. Services work on one repository at
// a time through a Config scoped with ForRepository; the TUI lists worktrees
// of every repository together and asks which repository to use whenever it
// creates a worktree, offering the last used one first.

type repoPickerPurpose int

const (
	pickRepoForWorktree repoPickerPurpose = iota
	pickRepoForTicket
	pickRepoForPRRadar
	pickRepoToRemove
)

// worktreeCfg scopes the Config to the repository a worktree belongs to.
func (m model) worktreeCfg(wt Worktree) Config {
	return m.cfg.ForRepository(wt.Repository)
}

// prCfg scopes the Config to the repository PR Radar shows.
func (m model) prCfg() Config {
	return m.cfg.ForRepository(m.prRepository)
}

// worktreeRepository names a worktree's repository; rows from older caches
// or tests without one belong to the primary repository.
func (m model) worktreeRepository(wt Worktree) string {
	if wt.Repository != "" {
		return wt.Repository
	}
	return m.cfg.RepositoryName()
}

// ownsRepositoryPath reports whether path is a repository of the active project.
func (m model) ownsRepositoryPath(path string) bool {
	for _, repository := range m.cfg.Repositories() {
		if repository.RepositoryPath == path {
			return true
		}
	}
	return false
}

// projectKey identifies the active project and its repositories, so results
// requested for a project that has since changed are dropped.
func projectKey(cfg Config) string {
	parts := []string{cfg.ActiveProfile.Name}
	for _, repository := range cfg.Repositories() {
		parts = append(parts, repository.Name+"="+repository.RepositoryPath)
	}
	return strings.Join(parts, "\x00")
}

// discoverProjectWorktrees lists the worktrees of every repository of the
// project at once, grouped in configured repository order. A repository that
// cannot be read becomes a warning unless none can be read.
func discoverProjectWorktrees(ctx context.Context, services core.WorktreeService, cfg Config, showAll bool) ([]Worktree, string, error) {
	repositories := cfg.Repositories()
	if len(repositories) <= 1 {
		worktrees, err := services.DiscoverWorktrees(ctx, cfg, showAll)
		return worktrees, "", err
	}
	lists := make([][]Worktree, len(repositories))
	errs := make([]error, len(repositories))
	var wg sync.WaitGroup
	for i, repository := range repositories {
		wg.Go(func() {
			lists[i], errs[i] = services.DiscoverWorktrees(ctx, cfg.ForRepository(repository.Name), showAll)
		})
	}
	wg.Wait()
	var (
		worktrees []Worktree
		warnings  []string
		firstErr  error
	)
	for i, repository := range repositories {
		if errs[i] != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", repository.Name, errs[i]))
			if firstErr == nil {
				firstErr = errs[i]
			}
			continue
		}
		worktrees = append(worktrees, lists[i]...)
	}
	if len(warnings) == len(repositories) {
		return nil, "", firstErr
	}
	return worktrees, strings.Join(warnings, " | "), nil
}

// peekProjectWorktrees paints the cached worktrees of every repository.
func peekProjectWorktrees(services core.WorktreeService, cfg Config, showAll bool) ([]Worktree, bool) {
	repositories := cfg.Repositories()
	if len(repositories) <= 1 {
		return services.PeekWorktrees(cfg, showAll)
	}
	var worktrees []Worktree
	for _, repository := range repositories {
		if cached, ok := services.PeekWorktrees(cfg.ForRepository(repository.Name), showAll); ok {
			worktrees = append(worktrees, cached...)
		}
	}
	return worktrees, len(worktrees) > 0
}

// defaultRepositoryIndex points at the last used repository, else the first.
func (m model) defaultRepositoryIndex() int {
	last := m.cfg.ActiveProfile.LastRepository
	for idx, repository := range m.cfg.Repositories() {
		if repository.Name == last {
			return idx
		}
	}
	return 0
}

func (m model) repositoryIndex(name string) int {
	for idx, repository := range m.cfg.Repositories() {
		if repository.Name == name {
			return idx
		}
	}
	return 0
}

// openRepositoryPicker asks which repository to use. Creation flows only call
// it for projects with several repositories.
func (m model) openRepositoryPicker(purpose repoPickerPurpose, ticket Ticket, action string) (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	m.pickingRepository = true
	m.repoPickerPurpose = purpose
	m.repoPickerTicket = ticket
	m.repoPickerAction = action
	m.repoRemoveArmed = ""
	m.repoPickerCursor = m.defaultRepositoryIndex()
	switch purpose {
	case pickRepoForTicket:
		m.statusMessage = fmt.Sprintf("Choose the repository for %s", ticket.Key)
	case pickRepoForPRRadar:
		m.repoPickerCursor = m.repositoryIndex(m.prCfg().RepositoryName())
		m.statusMessage = "Choose the repository PR Radar shows"
	case pickRepoToRemove:
		m.statusMessage = "Choose a repository to remove from " + m.cfg.ActiveProfile.Name
	default:
		m.statusMessage = "Choose the repository for the new worktree"
	}
	m.statusError = false
	return m, nil
}

func (m model) updateRepositoryPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	repositories := m.cfg.Repositories()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.pickingRepository = false
		m.repoRemoveArmed = ""
		return m, m.setTransientStatus("Repository selection cancelled")
	case "up", "k":
		m.repoPickerCursor = max(0, m.repoPickerCursor-1)
		m.repoRemoveArmed = ""
		return m, nil
	case "down", "j":
		m.repoPickerCursor = min(max(0, len(repositories)-1), m.repoPickerCursor+1)
		m.repoRemoveArmed = ""
		return m, nil
	case "enter":
		if len(repositories) == 0 {
			m.pickingRepository = false
			return m, nil
		}
		return m.chooseRepository(repositories[min(max(m.repoPickerCursor, 0), len(repositories)-1)].Name)
	}
	// Digits pick a repository directly, the way they are numbered on screen.
	if len(msg.Runes) == 1 && msg.Runes[0] >= '1' && msg.Runes[0] <= '9' {
		if idx := int(msg.Runes[0] - '1'); idx < len(repositories) {
			m.repoPickerCursor = idx
			return m.chooseRepository(repositories[idx].Name)
		}
	}
	return m, nil
}

func (m model) chooseRepository(name string) (tea.Model, tea.Cmd) {
	if m.repoPickerPurpose == pickRepoToRemove && m.repoRemoveArmed != name {
		m.repoRemoveArmed = name
		m.statusMessage = fmt.Sprintf("Press enter again to remove %s from %s", name, m.cfg.ActiveProfile.Name)
		m.statusError = false
		return m, nil
	}
	m.pickingRepository = false
	m.repoRemoveArmed = ""
	switch m.repoPickerPurpose {
	case pickRepoForTicket:
		return m.startTicketWorktreeInRepository(m.repoPickerTicket, name, m.repoPickerAction)
	case pickRepoForPRRadar:
		return m.switchPRRepository(name)
	case pickRepoToRemove:
		return m.removeRepository(name)
	default:
		return m.openNewWorktreeInput(name)
	}
}

// openNewWorktreeInput asks for the branch of a new worktree in repository
// (empty for a single-repository project).
func (m model) openNewWorktreeInput(repository string) (tea.Model, tea.Cmd) {
	m.creatingWorktree = true
	m.newWorktreeRepository = repository
	m.newInput.SetValue("")
	m.newInput.Focus()
	m.statusMessage = "Enter branch/worktree name"
	if repository != "" {
		m.statusMessage = fmt.Sprintf("Enter branch/worktree name for %s", repository)
	}
	m.statusError = false
	return m, nil
}

// rememberRepository makes name the repository offered first next time.
func (m *model) rememberRepository(name string) tea.Cmd {
	if !m.cfg.MultiRepository() || name == "" || m.cfg.ActiveProfile.LastRepository == name {
		return nil
	}
	m.cfg.ActiveProfile.LastRepository = name
	for i := range m.cfg.App.Profiles {
		if m.cfg.App.Profiles[i].Name == m.cfg.ActiveProfile.Name {
			m.cfg.App.Profiles[i].LastRepository = name
		}
	}
	return m.persistConfigQuietly()
}

// switchPRRepository points PR Radar at another repository of the project.
func (m model) switchPRRepository(name string) (tea.Model, tea.Cmd) {
	if name == m.prCfg().RepositoryName() {
		return m, m.setTransientStatus("PR Radar already shows " + name)
	}
	m.prRepository = name
	m.cancelPRDetailLoad()
	m.remotePRs = nil
	m.visiblePRs = nil
	m.prListSlug = ""
	m.prListFetchedAt = time.Time{}
	m.prListLoading = false
	m.prChecksLoading = map[string]bool{}
	m.selectedPRNumber = 0
	m.selectedPRIndex = 0
	m.prUserFilter = ""
	m.prAuthorFilter = ""
	m.prChatQuestion, m.prChatAnswer = "", ""
	m.confirmingPRApproval = false
	m.prApprovalPendingNumber = 0
	m.prApprovalAwaitingDetailNumber = 0
	m.prApprovalWarnings = nil
	m.table.SetCursor(0)
	if !m.isPRMode() {
		m.statusMessage = "PR Radar now shows " + name
		m.statusError = false
		return m, nil
	}
	return m.enterPRMode()
}

// ticketWorktreeIn returns the ticket's present worktree in repository.
func (m model) ticketWorktreeIn(key string, repository string) *Worktree {
	for _, wt := range m.ticketWorktrees(key) {
		if wt.Repository == repository {
			return &wt
		}
	}
	return nil
}

// ticketWorktrees lists the ticket's present worktrees in every repository,
// labelled with their repository.
func (m model) ticketWorktrees(key string) []Worktree {
	key = strings.ToUpper(key)
	var worktrees []Worktree
	for _, wt := range m.allWorktrees {
		if wt.Missing || wt.Prunable {
			continue
		}
		found := m.services.TicketKeyFromBranch(wt.Branch)
		if found == "" {
			found = m.services.TicketKeyFromBranch(wt.Name)
		}
		if found == key {
			wt.Repository = m.worktreeRepository(wt)
			worktrees = append(worktrees, wt)
		}
	}
	return worktrees
}

func renderRepositoryPickerModal(m model) string {
	repositories := m.cfg.Repositories()
	var intro, title string
	switch m.repoPickerPurpose {
	case pickRepoForTicket:
		title = "Ticket Worktree Repository"
		intro = fmt.Sprintf("Create the %s worktree in which repository?", emptyDash(m.repoPickerTicket.Key))
	case pickRepoForPRRadar:
		title = "PR Radar Repository"
		intro = "Show pull requests of which repository?"
	case pickRepoToRemove:
		title = "Remove Repository"
		intro = fmt.Sprintf("Remove a repository from %s. Files and worktrees stay on disk.", m.cfg.ActiveProfile.Name)
	default:
		title = "New Worktree Repository"
		intro = "Create the worktree in which repository?"
	}
	body := []string{intro, ""}
	last := m.cfg.ActiveProfile.LastRepository
	current := ""
	if m.repoPickerPurpose == pickRepoForPRRadar {
		current = m.prCfg().RepositoryName()
	}
	for idx, repository := range repositories {
		label := fmt.Sprintf("%d  %-14s %s", idx+1, repository.Name, m.services.CompactPath(repository.RepositoryPath, m.cfg.HomeDir))
		line := selectorLine(idx == m.repoPickerCursor, label, repository.Name == current)
		creating := m.repoPickerPurpose == pickRepoForWorktree || m.repoPickerPurpose == pickRepoForTicket
		if repository.Name == last && creating {
			line += " " + warnStyle.Render(icons.Star+" last used")
		}
		body = append(body, line)
	}
	hints := []keyHint{{"j/k", "move"}, {"1-9", "pick"}, {"enter", "select"}, {"esc", "cancel"}}
	if m.repoPickerPurpose == pickRepoToRemove && m.repoRemoveArmed != "" {
		body = append(body, "", warnStyle.Render(fmt.Sprintf("%s Press enter again to remove %s from the project.", icons.Warn, m.repoRemoveArmed)))
		hints = []keyHint{{"enter", "remove"}, {"esc", "cancel"}}
	}
	return renderModal(title, body, hints)
}

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type ticketLoadSpyServices struct {
	core.Services
	started chan string
	release <-chan struct{}
}

func (s *ticketLoadSpyServices) wait(ctx context.Context, operation string) {
	s.started <- operation
	select {
	case <-s.release:
	case <-ctx.Done():
	}
}

func (s *ticketLoadSpyServices) LoadTickets(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool, refresh bool) ([]Ticket, string, error) {
	s.wait(ctx, "tickets")
	return []Ticket{{Key: "APP-1"}}, "", nil
}

func (s *ticketLoadSpyServices) CollectTicketWorktrees(ctx context.Context, cfg Config) ([]Worktree, string, error) {
	s.wait(ctx, "worktrees")
	return []Worktree{{Name: "APP-1", Branch: "APP-1", Path: "/tmp/app-1"}}, "", nil
}

func (s *ticketLoadSpyServices) LoadPullRequestIndex(ctx context.Context, cfg Config, refresh bool) ([]PullRequest, string, error) {
	s.wait(ctx, "pull requests")
	return []PullRequest{{Number: 1, HeadRefName: "APP-1"}}, "", nil
}

// Assures that ticket loading does not serialize independent Jira, worktree, and PR requests.
func TestTicketLoadFetchesIndependentContextConcurrently(t *testing.T) {
	release := make(chan struct{})
	spy := &ticketLoadSpyServices{
		Services: services.NewService(),
		started:  make(chan string, 3),
		release:  release,
	}
	result := make(chan tea.Msg, 1)
	go func() {
		result <- loadTicketsCmd(spy, Config{}, JiraBoard{ID: 1}, true, true, true)()
	}()
	defer func() {
		close(release)
		<-result
	}()

	started := map[string]bool{}
	timeout := time.After(time.Second)
	for len(started) < 3 {
		select {
		case operation := <-spy.started:
			started[operation] = true
		case <-timeout:
			t.Fatalf("only %d independent ticket-load operations started before the others completed: %v", len(started), started)
		}
	}
}

// Assures that a worktree created from a ticket remains selected after returning to Worktrees.
func TestTicketWorktreeCreationSelectsWorktreeWhenLeavingTickets(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.mode = modeTickets
	m.tickets = []Ticket{{Key: "APP-1", Summary: "Create worktree"}}
	m.visibleTickets = m.tickets
	m.allWorktrees = []Worktree{{Name: "previous", Path: "/tmp/previous"}}
	m.visibleWorktrees = append([]Worktree(nil), m.allWorktrees...)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)

	next, _ := m.Update(ticketWorktreeResult{
		Key:      "APP-1",
		Worktree: Worktree{Name: "APP-1-create-worktree", Branch: "APP-1-create-worktree", Path: "/tmp/app-1"},
	})
	created := next.(model)
	next, _ = created.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	switched := next.(model)
	if !switched.isWorktreeMode() {
		t.Fatal("J did not switch from tickets to worktrees")
	}

	next, _ = switched.Update(loadResult{Worktrees: []Worktree{
		{Name: "other", Path: "/tmp/other"},
		{Name: "APP-1-create-worktree", Branch: "APP-1-create-worktree", Path: "/tmp/app-1"},
	}})
	got := next.(model)
	if got.selectedPath != "/tmp/app-1" || got.table.Cursor() != 1 {
		t.Fatalf("created worktree was not selected: path=%q cursor=%d", got.selectedPath, got.table.Cursor())
	}
}

// Assures that ticket creation immediately joins its optimistic worktree and selects it in Worktrees.
func TestTicketWorktreeCreationIsOptimisticAcrossViews(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.mode = modeTickets
	m.tickets = []Ticket{{Key: "APP-1", Summary: "Create worktree"}}
	m.visibleTickets = m.tickets
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)

	next, cmd := m.Update(teaKey("n"))
	created := next.(model)
	if cmd == nil {
		t.Fatal("ticket creation did not return a command")
	}
	selected := created.selectedTicket()
	if selected == nil || selected.Worktree == nil {
		t.Fatal("ticket did not show its optimistic worktree")
	}
	path := selected.Worktree.Path
	if view := ansi.Strip(created.tableView()); !strings.Contains(view, "APP-1-create-work") {
		t.Fatalf("ticket table did not render optimistic worktree: %q", view)
	}

	next, _ = created.Update(teaKey("J"))
	switched := next.(model)
	if !switched.isWorktreeMode() || switched.selectedPath != path || !worktreeExists(switched.visibleWorktrees, path) {
		t.Fatalf("worktree switch = mode:%v path:%q rows:%#v", switched.mode, switched.selectedPath, switched.visibleWorktrees)
	}
}

// Assures that switching from a selected ticket immediately selects its existing associated worktree.
func TestTicketSelectionTransfersToExistingWorktree(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.mode = modeTickets
	associated := Worktree{Name: "APP-1-existing", Branch: "APP-1-existing", Path: "/tmp/app-1"}
	m.tickets = []Ticket{{Key: "APP-1", Worktree: &associated}}
	m.visibleTickets = m.tickets
	m.allWorktrees = []Worktree{{Name: "other", Path: "/tmp/other"}, associated}
	m.visibleWorktrees = append([]Worktree(nil), m.allWorktrees...)
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)

	next, _ := m.Update(teaKey("J"))
	got := next.(model)
	if got.selectedPath != associated.Path {
		t.Fatalf("associated worktree selection = path:%q cursor:%d", got.selectedPath, got.table.Cursor())
	}
}

// Assures that a failed ticket creation removes its optimistic ticket association and worktree row.
func TestFailedTicketWorktreeCreationRollsBackTicketAssociation(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.mode = modeTickets
	m.tickets = []Ticket{{Key: "APP-1", Summary: "Create worktree"}}
	m.visibleTickets = m.tickets
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)

	next, _ := m.Update(teaKey("n"))
	creating := next.(model)
	path := creating.selectedTicket().Worktree.Path
	next, _ = creating.Update(ticketWorktreeResult{
		Key:      "APP-1",
		Worktree: Worktree{Name: "APP-1-create-worktree", Path: path},
		Err:      context.DeadlineExceeded,
	})
	got := next.(model)

	if selected := got.selectedTicket(); selected == nil || selected.Worktree != nil {
		t.Fatalf("ticket worktree after rollback = %#v", selected)
	}
	if worktreeExists(got.allWorktrees, path) || !got.statusError {
		t.Fatalf("rollback rows/error = rows:%#v error:%v", got.allWorktrees, got.statusError)
	}
}

func TestTicketModeRendersJoinedContext(t *testing.T) {
	m := layoutTestModel(150, 32)
	m.services = services.NewService()
	m.mode = modeTickets
	m.ticketBoard = JiraBoard{ID: 24731, Name: "EDP Blue Steel Board"}
	m.tickets = []Ticket{{
		Key:            "EDPSL-4618",
		Summary:        "Persisted deactivation failures",
		Type:           "Bug",
		Status:         "In Progress",
		StatusCategory: "indeterminate",
		Worktree:       &Worktree{Name: "EDPSL-4618-deactivation-failures"},
		PR:             &PullRequest{Number: 2998, State: "OPEN"},
	}}
	m.visibleTickets = m.tickets
	m.selectedTicketKey = "EDPSL-4618"
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)
	if selected := m.selectedTicket(); selected == nil {
		t.Fatalf("ticket selection missing: cursor=%d tickets=%d", m.table.Cursor(), len(m.visibleTickets))
	}
	m.updateDetailContent()

	view := ansi.Strip(m.View())
	for _, want := range []string{"JIRA TICKETS", "EDPSL-4618", "#2998"} {
		if !strings.Contains(view, want) {
			t.Fatalf("ticket view missing %q", want)
		}
	}
	if cells := m.ticketRowCells(m.tickets[0], false); !strings.Contains(cells[2], "In Progress") {
		t.Fatalf("ticket status cell = %q", cells[2])
	}
	if cells := m.ticketRowCells(m.tickets[0], false); !strings.Contains(cells[4], "EDPSL-4618-deactivation-failures") {
		t.Fatalf("ticket worktree cell = %q", cells[4])
	}
}

func TestTicketEnterOpensReader(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.services = services.NewService()
	m.mode = modeTickets
	m.tickets = []Ticket{{Key: "EDPSL-4618", Summary: "Failure", Detail: true, Description: "Readable description"}}
	m.visibleTickets = m.tickets
	m.selectedTicketKey = "EDPSL-4618"
	m.ticketDetail = &m.tickets[0]

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(model)
	if !got.ticketReadMode {
		t.Fatal("enter did not open ticket reader")
	}
	if view := ansi.Strip(got.View()); !strings.Contains(view, "Readable description") {
		t.Fatalf("ticket reader missing description: %q", view)
	}
}

func TestTicketStatusAllowsDirectEntry(t *testing.T) {
	m := layoutTestModel(120, 30)
	m.services = services.NewService()
	m.mode = modeTickets
	m.tickets = []Ticket{{Key: "EDPSL-4618", Status: "In Progress"}}
	m.visibleTickets = m.tickets
	m.selectedTicketKey = "EDPSL-4618"
	m.table.SetColumns(m.activeColumns())
	m.rebuildRows()
	m.table.SetCursor(0)
	if selected := m.selectedTicket(); selected == nil {
		t.Fatalf("ticket selection missing: cursor=%d tickets=%d", m.table.Cursor(), len(m.visibleTickets))
	}

	next, _, handled := m.updateTicketMode(teaKey("t"))
	if !handled {
		t.Fatal("ticket status key was not handled")
	}
	got := next.(model)
	if !got.selectingTicketStatus {
		t.Fatal("t did not open status selector")
	}
	next, _ = got.updateTicketStatusSelector(teaKey("e"))
	got = next.(model)
	if !got.enteringTicketStatus {
		t.Fatal("e did not open direct status input")
	}
	for _, r := range "Resolved" {
		next, _ = got.Update(teaKey(string(r)))
		got = next.(model)
	}
	next, cmd := got.Update(teaKey("enter"))
	got = next.(model)
	if cmd == nil || got.ticketMutationsInFlight() != 1 {
		t.Fatalf("status submission did not start mutation: in flight=%d cmd=%v", got.ticketMutationsInFlight(), cmd)
	}
	if ticket, ok := got.ticketByKey("EDPSL-4618"); !ok || ticket.Status != "Resolved" {
		t.Fatalf("status submission was not applied optimistically: %#v", ticket)
	}
}

func TestTicketFiltersDefaultEnabledAndApplyOpenOnly(t *testing.T) {
	m := NewModel(Config{
		ActiveProfile: RepositoryProfile{
			Name: "app",
			Jira: &JiraConfig{
				Boards: []JiraBoard{{ID: 123, Name: "App Board"}},
			},
		},
	}, services.NewService())
	m.ready = true
	m.loading = false
	m.mode = modeTickets
	m.tickets = []Ticket{
		{Key: "APP-1", Status: "In Progress", StatusCategory: "indeterminate"},
		{Key: "APP-2", Status: "Done", StatusCategory: "done"},
	}
	m.applyTicketFilter()
	if !m.ticketMineOnly || !m.ticketSprintOnly || !m.ticketOpenOnly {
		t.Fatalf("default filters = mine:%v sprint:%v open:%v", m.ticketMineOnly, m.ticketSprintOnly, m.ticketOpenOnly)
	}
	if len(m.visibleTickets) != 1 || m.visibleTickets[0].Key != "APP-1" {
		t.Fatalf("visible tickets = %#v", m.visibleTickets)
	}

	next, _, handled := m.updateTicketMode(teaKey("f"))
	if !handled {
		t.Fatal("f did not open ticket filters")
	}
	got := next.(model)
	modal := ansi.Strip(renderTicketFiltersModal(got))
	for _, want := range []string{"Only me", "Current sprint", "Open (not closed)"} {
		if !strings.Contains(modal, "[x] "+want) {
			t.Fatalf("filter modal missing checked %q: %q", want, modal)
		}
	}

	got.ticketFilterCursor = 1
	next, _ = got.updateTicketFilters(teaKey(" "))
	got = next.(model)
	next, cmd := got.updateTicketFilters(teaKey("enter"))
	got = next.(model)
	if got.selectingTicketFilters || got.ticketSprintOnly || cmd == nil {
		t.Fatalf("applied filters = selecting:%v sprint:%v cmd:%v", got.selectingTicketFilters, got.ticketSprintOnly, cmd)
	}
}

func TestBuildSetupConfigPersistsJira(t *testing.T) {
	svc := services.NewService()
	cfg := testGitRepo(t)
	setup := newSetupModel(Config{
		HomeDir:    cfg.HomeDir,
		ConfigPath: cfg.HomeDir + "/.wt-manager.json",
		App:        svc.DefaultAppConfig(),
		SetupRepo:  cfg.ActiveProfile.RepositoryPath,
	}, true, svc)
	setup.repositories[0].WorktreesDir = cfg.ActiveProfile.WorktreesDir
	setup.projectInput.SetValue("bluesteel")
	for idx := range setup.agentOptions {
		setup.agentOptions[idx].Selected = setup.agentOptions[idx].Name == "codex"
	}
	setup.jiraConfig = &JiraConfig{
		Site:         "example.atlassian.net",
		Boards:       []JiraBoard{{ID: 24731, Name: "EDP Blue Steel Board", Projects: []string{"EDPSL"}}},
		DefaultBoard: "EDP Blue Steel Board",
		MineOnly:     true,
		Limit:        100,
	}

	got, err := buildSetupConfig(setup)
	if err != nil {
		t.Fatalf("buildSetupConfig() error = %v", err)
	}
	if got.ActiveProfile.Jira == nil || got.ActiveProfile.Jira.DefaultBoard != "EDP Blue Steel Board" || !got.ActiveProfile.Jira.MineOnly {
		t.Fatalf("JIRA config = %#v", got.ActiveProfile.Jira)
	}
}

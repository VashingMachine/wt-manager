package tui

import (
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/services"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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
	if cmd == nil || !got.loading || got.loadingKind != loadingMutation {
		t.Fatalf("status submission did not start mutation: loading=%v kind=%q cmd=%v", got.loading, got.loadingKind, cmd)
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
	setup.repoPath = cfg.ActiveProfile.RepositoryPath
	setup.worktreesDir = cfg.ActiveProfile.WorktreesDir
	setup.profileInput.SetValue("bluesteel")
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

package services

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseJiraBoardRef(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int
	}{
		{input: "24731", want: 24731},
		{input: "https://site.atlassian.net/jira/software/c/projects/EDPSL/boards/24731", want: 24731},
		{input: "https://site.atlassian.net/secure/RapidBoard.jspa?rapidView=24731", want: 24731},
	} {
		got, err := parseJiraBoardRef(test.input)
		if err != nil {
			t.Fatalf("parseJiraBoardRef(%q) error = %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parseJiraBoardRef(%q) = %d, want %d", test.input, got, test.want)
		}
	}
}

func TestTicketBranchAndKey(t *testing.T) {
	ticket := Ticket{Key: "EDPSL-4618", Summary: "Persisted deactivation failures in service orders"}
	branch := ticketBranchName(ticket)
	if branch != "EDPSL-4618-persisted-deactivation-failures-in" {
		t.Fatalf("ticketBranchName() = %q", branch)
	}
	for _, candidate := range []string{branch, "feature/edpsl-4618-fix", "EDPSL-4618/changes"} {
		if got := ticketKeyFromBranch(candidate); got != "EDPSL-4618" {
			t.Fatalf("ticketKeyFromBranch(%q) = %q", candidate, got)
		}
	}
}

func TestAttachTicketContextMatchesWorktreeAndPR(t *testing.T) {
	tickets := []Ticket{{Key: "EDPSL-4618"}}
	worktrees := []Worktree{{Name: "EDPSL-4618-fix", Branch: "EDPSL-4618-fix", Managed: true}}
	pullRequests := []PullRequest{{Number: 2998, HeadRefName: "EDPSL-4618-fix", State: "OPEN"}}

	got := attachTicketContext(tickets, worktrees, pullRequests)
	if got[0].Worktree == nil || got[0].Worktree.Name != worktrees[0].Name {
		t.Fatalf("worktree join = %#v", got[0].Worktree)
	}
	if got[0].PR == nil || got[0].PR.Number != 2998 {
		t.Fatalf("PR join = %#v", got[0].PR)
	}
}

func TestFilterTicketsOpenOnlyUsesStatusCategory(t *testing.T) {
	tickets := []Ticket{
		{Key: "APP-1", Status: "In Progress", StatusCategory: "indeterminate"},
		{Key: "APP-2", Status: "Resolved", StatusCategory: "done"},
		{Key: "APP-3", Status: "Closed"},
	}
	got := filterTickets(tickets, "", "", true)
	if len(got) != 1 || got[0].Key != "APP-1" {
		t.Fatalf("filterTickets(openOnly) = %#v", got)
	}
	if got := filterTickets(tickets, "", "", false); len(got) != 3 {
		t.Fatalf("filterTickets(all) returned %d tickets", len(got))
	}
}

func TestRenderADF(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","version":1,"content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Summary"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"first"}]}]}]}]}`)
	got := renderADF(raw)
	for _, want := range []string{"## Summary", "- first"} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderADF() = %q, missing %q", got, want)
		}
	}
}

func TestDecodeJiraIssuesAcceptsJSONStream(t *testing.T) {
	output := `[{"key":"APP-1","fields":{"summary":"first"}}]{"issues":[{"key":"APP-2","fields":{"summary":"second"}}]}`
	issues, err := decodeJiraIssues(output)
	if err != nil {
		t.Fatalf("decodeJiraIssues() error = %v", err)
	}
	if len(issues) != 2 || issues[0].Key != "APP-1" || issues[1].Key != "APP-2" {
		t.Fatalf("decodeJiraIssues() = %#v", issues)
	}
}

func TestTicketCacheScopesAndInvalidation(t *testing.T) {
	home := t.TempDir()
	cfg := Config{
		HomeDir: home,
		ActiveProfile: RepositoryProfile{
			Name: "bluesteel",
			Jira: &JiraConfig{Site: "example.atlassian.net"},
		},
	}
	board := JiraBoard{ID: 24731, Name: "EDP Blue Steel Board"}
	path := ticketCachePath(cfg, board, true, false)
	entry := ticketCacheEntry{
		Version:   ticketCacheSchemaVersion,
		FetchedAt: time.Now(),
		Site:      "example.atlassian.net",
		BoardID:   board.ID,
		BoardName: board.Name,
		Scope:     ticketCacheScope(true, false),
		Tickets:   []Ticket{{Key: "EDPSL-4618"}},
	}
	if err := writeTicketCache(path, entry); err != nil {
		t.Fatalf("writeTicketCache() error = %v", err)
	}
	if filepath.Dir(path) != jiraCacheDir(cfg) {
		t.Fatalf("cache path = %q", path)
	}
	tickets, _, ok := peekTicketCache(cfg, board, true, false)
	if !ok || len(tickets) != 1 || tickets[0].Key != "EDPSL-4618" {
		t.Fatalf("peekTicketCache() = %#v, ok=%v", tickets, ok)
	}
	if _, _, ok := peekTicketCache(cfg, board, false, false); ok {
		t.Fatal("all-ticket scope unexpectedly reused mine-only cache")
	}
	if err := invalidateTicketCache(cfg, board); err != nil {
		t.Fatalf("invalidateTicketCache() error = %v", err)
	}
	if _, _, ok := peekTicketCache(cfg, board, true, false); ok {
		t.Fatal("cache remained after invalidation")
	}
}

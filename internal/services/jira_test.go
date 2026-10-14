package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Assures that board links and ids resolve to a board, while any other link or
// key inside a space (a ticket, a project page, a bare key) resolves to that
// whole space.
func TestParseJiraLink(t *testing.T) {
	for _, test := range []struct {
		input string
		want  jiraLinkRef
	}{
		{input: "456", want: jiraLinkRef{BoardID: 456}},
		{input: "https://site.atlassian.net/jira/software/c/projects/APP/boards/456", want: jiraLinkRef{BoardID: 456}},
		{input: "https://site.atlassian.net/secure/RapidBoard.jspa?rapidView=456", want: jiraLinkRef{BoardID: 456}},
		{input: "https://site.atlassian.net/jira/software/c/projects/OPS/boards/123?selectedIssue=OPS-42", want: jiraLinkRef{BoardID: 123}},
		{input: "https://example.atlassian.net/browse/OPS-42", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/browse/OPS-42?focusedCommentId=1", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/browse/OPS", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/issues/?jql=assignee%3DcurrentUser()&selectedIssue=OPS-42", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/jira/software/c/projects/OPS/list", want: jiraLinkRef{Space: "OPS"}},
		{input: "https://site.atlassian.net/jira/software/projects/AB2/issues/AB2-7", want: jiraLinkRef{Space: "AB2"}},
		{input: "https://site.atlassian.net/jira/servicedesk/projects/HELP/queues/custom/1", want: jiraLinkRef{Space: "HELP"}},
		{input: "ops-42", want: jiraLinkRef{Space: "OPS"}},
		{input: " OPS ", want: jiraLinkRef{Space: "OPS"}},
	} {
		got, err := parseJiraLink(test.input)
		if err != nil {
			t.Fatalf("parseJiraLink(%q) error = %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("parseJiraLink(%q) = %+v, want %+v", test.input, got, test.want)
		}
	}
	for _, input := range []string{"", "https://site.atlassian.net/jira/your-work", "not a link"} {
		if got, err := parseJiraLink(input); err == nil {
			t.Fatalf("parseJiraLink(%q) = %+v, want an error", input, got)
		}
	}
}

func TestTicketBranchAndKey(t *testing.T) {
	ticket := Ticket{Key: "APP-123", Summary: "Retry failed exports when the upstream API times out"}
	branch := ticketBranchName(ticket)
	if branch != "APP-123-retry-failed-exports-when-the-upstream" {
		t.Fatalf("ticketBranchName() = %q", branch)
	}
	for _, candidate := range []string{branch, "feature/app-123-fix", "APP-123/changes"} {
		if got := ticketKeyFromBranch(candidate); got != "APP-123" {
			t.Fatalf("ticketKeyFromBranch(%q) = %q", candidate, got)
		}
	}
}

func TestAttachTicketContextMatchesWorktreeAndPR(t *testing.T) {
	tickets := []Ticket{{Key: "APP-123"}}
	worktrees := []Worktree{{Name: "APP-123-fix", Branch: "APP-123-fix", Managed: true}}
	pullRequests := []PullRequest{{Number: 321, HeadRefName: "APP-123-fix", State: "OPEN"}}

	got := attachTicketContext(tickets, worktrees, pullRequests)
	if got[0].Worktree == nil || got[0].Worktree.Name != worktrees[0].Name {
		t.Fatalf("worktree join = %#v", got[0].Worktree)
	}
	if got[0].PR == nil || got[0].PR.Number != 321 {
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

// Assures that ticket scopes are cached separately and that invalidation keeps rows paintable but forces revalidation.
func TestTicketCacheScopesAndInvalidation(t *testing.T) {
	svc := NewService()
	cfg := Config{
		ActiveProfile: RepositoryProfile{
			Name: "app",
			Jira: &JiraConfig{Site: "example.atlassian.net"},
		},
	}
	board := JiraBoard{ID: 456, Name: "App Board"}
	if err := svc.cache.Put(bucketTickets, ticketCacheKey(cfg, board, true, false), []Ticket{{Key: "APP-123"}}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	tickets, fetchedAt, ok := svc.PeekTicketCache(cfg, board, true, false)
	if !ok || len(tickets) != 1 || tickets[0].Key != "APP-123" || fetchedAt.IsZero() {
		t.Fatalf("PeekTicketCache() = %#v at=%v ok=%v", tickets, fetchedAt, ok)
	}
	if _, _, ok := svc.PeekTicketCache(cfg, board, false, false); ok {
		t.Fatal("all-ticket scope unexpectedly reused mine-only cache")
	}
	if err := svc.InvalidateTicketCache(cfg, board); err != nil {
		t.Fatalf("InvalidateTicketCache() error = %v", err)
	}
	tickets, fetchedAt, ok = svc.PeekTicketCache(cfg, board, true, false)
	if !ok || len(tickets) != 1 || !fetchedAt.IsZero() {
		t.Fatalf("invalidated cache = %#v at=%v ok=%v, want stale rows kept", tickets, fetchedAt, ok)
	}
}

// fakeACLIScript answers the space lookup, an APP board without an active
// sprint, and work item searches, and logs every call with its arguments
// joined by spaces.
const fakeACLIScript = `#!/bin/sh
echo "$*" >> "$WT_FAKE_ACLI_LOG"
case "$*" in
  "jira project view --key OPS --json") echo '{"key":"OPS","name":"Operations"}' ;;
  "jira board list-sprints --id 123 --state active --json") echo '{"sprints":[]}' ;;
  "jira workitem search --jql project IN (\"APP\") "*) echo '[{"key":"APP-7","fields":{"summary":"Board ticket"}},{"key":"OPS-43","fields":{"summary":"Also on the board"}}]' ;;
  "jira workitem search "*) echo '[{"key":"OPS-43","fields":{"summary":"Second","status":{"name":"To Do","statusCategory":{"key":"new"}}}},{"key":"OPS-41","fields":{"summary":"First"}}]' ;;
  *) echo "✗ Error: unexpected acli call: $*"; exit 2 ;;
esac
`

// installFakeACLI puts the scripted acli first on PATH and returns its call log.
func installFakeACLI(t *testing.T) func() []string {
	t.Helper()
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "acli"), fakeACLIScript)
	log := filepath.Join(bin, "calls.log")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WT_FAKE_ACLI_LOG", log)
	return func() []string {
		data, _ := os.ReadFile(log)
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}
}

// Assures that pasting a ticket link resolves to the whole space the ticket
// belongs to, named after the space, rather than requiring a board link.
func TestResolveJiraBoardFromTicketLinkResolvesWholeSpace(t *testing.T) {
	installFakeACLI(t)
	link := "https://example.atlassian.net/browse/OPS-42"

	got, err := resolveJiraBoard(context.Background(), link)
	if err != nil {
		t.Fatalf("resolveJiraBoard() error = %v", err)
	}
	if !got.IsSpace() || got.ID != 0 || got.Name != "Operations (OPS)" || got.URL != link || len(got.Projects) != 1 || got.Projects[0] != "OPS" {
		t.Fatalf("resolveJiraBoard() = %+v, want the OPS space", got)
	}
}

// Assures that a space lists the tickets assigned to the current user across
// the entire space, even while the sprint filter is on, and caches them under
// one scope for both sprint settings.
func TestLoadTicketsForSpaceListsAssignedTicketsAcrossSpace(t *testing.T) {
	calls := installFakeACLI(t)
	svc := NewService()
	cfg := Config{ActiveProfile: RepositoryProfile{Name: "ops", Jira: &JiraConfig{Site: "example.atlassian.net"}}}
	space := JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}

	tickets, warning, err := svc.LoadTickets(context.Background(), cfg, space, true, true, false)
	if err != nil || warning != "" {
		t.Fatalf("LoadTickets() error = %v, warning = %q", err, warning)
	}
	if len(tickets) != 2 || tickets[0].Key != "OPS-43" || tickets[0].URL != "https://example.atlassian.net/browse/OPS-43" {
		t.Fatalf("LoadTickets() = %+v", tickets)
	}
	log := calls()
	if len(log) != 1 || !strings.Contains(log[0], `--jql project IN ("OPS") AND assignee = currentUser() ORDER BY updated DESC `) {
		t.Fatalf("acli calls = %q, want one whole-space search assigned to me", log)
	}
	if cached, _, ok := svc.PeekTicketCache(cfg, space, true, false); !ok || len(cached) != 2 {
		t.Fatalf("PeekTicketCache(sprint off) = %+v ok=%v, want the rows loaded with sprint on", cached, ok)
	}
}

// Assures that spaces are cached apart from each other and from boards, while
// board cache keys keep their existing shape so earlier caches stay valid.
func TestTicketCacheKeysSeparateSpacesFromBoards(t *testing.T) {
	cfg := Config{ActiveProfile: RepositoryProfile{Name: "app", Jira: &JiraConfig{Site: "example.atlassian.net"}}}
	ops := JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}
	web := JiraBoard{Name: "Website (WEB)", Projects: []string{"WEB"}}
	board := JiraBoard{ID: 123, Name: "App Board", Projects: []string{"APP"}}

	if got := ticketCacheKey(cfg, board, true, true); got != "example.atlassian.net/app/123/mine-sprint" {
		t.Fatalf("board cache key = %q", got)
	}
	if ticketCacheKey(cfg, ops, true, false) == ticketCacheKey(cfg, web, true, false) {
		t.Fatal("two spaces share a cache key")
	}
	if keys := ticketCacheKeys(cfg, ops); len(keys) != 2 {
		t.Fatalf("space cache keys = %q, want one per assignee scope", keys)
	}
	if keys := ticketCacheKeys(cfg, board); len(keys) != 4 {
		t.Fatalf("board cache keys = %q, want every assignee and sprint scope", keys)
	}
}

// Assures that all boards and spaces together list every source's tickets once,
// and that a source that fails (a board without an active sprint) is reported
// as a warning while the other sources still show.
func TestLoadTicketsForAllSourcesMergesEverySource(t *testing.T) {
	installFakeACLI(t)
	svc := NewService()
	cfg := Config{ActiveProfile: RepositoryProfile{Name: "app", Jira: &JiraConfig{Site: "example.atlassian.net"}}}
	all := (&JiraConfig{Boards: []JiraBoard{
		{ID: 123, Name: "App Board", Projects: []string{"APP"}},
		{Name: "Operations (OPS)", Projects: []string{"OPS"}},
	}}).AllSources()

	tickets, warning, err := svc.LoadTickets(context.Background(), cfg, all, true, true, false)
	if err != nil {
		t.Fatalf("LoadTickets(sprint) error = %v", err)
	}
	if got := ticketKeys(tickets); got != "OPS-43,OPS-41" || !strings.Contains(warning, "App Board") || !strings.Contains(warning, "no active sprint") {
		t.Fatalf("LoadTickets(sprint) = %s warning %q, want the space's tickets and a board warning", got, warning)
	}

	tickets, warning, err = svc.LoadTickets(context.Background(), cfg, all, true, false, false)
	if err != nil || warning != "" {
		t.Fatalf("LoadTickets(no sprint) error = %v, warning = %q", err, warning)
	}
	if got := ticketKeys(tickets); got != "APP-7,OPS-43,OPS-41" {
		t.Fatalf("LoadTickets(no sprint) = %s, want board then space tickets, each once", got)
	}
}

// Assures that cached rows of all boards and spaces together paint at once,
// dated by the oldest source and undated while any source is stale, and that
// invalidating them marks every source for revalidation.
func TestPeekTicketCacheForAllSourcesMergesSources(t *testing.T) {
	svc := NewService()
	cfg := Config{ActiveProfile: RepositoryProfile{Name: "app", Jira: &JiraConfig{Site: "example.atlassian.net"}}}
	board := JiraBoard{ID: 123, Name: "App Board", Projects: []string{"APP"}}
	space := JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}
	all := (&JiraConfig{Boards: []JiraBoard{board, space}}).AllSources()
	older := time.Now().Add(-3 * time.Minute)
	if err := svc.cache.PutAt(bucketTickets, ticketCacheKey(cfg, board, true, false), []Ticket{{Key: "APP-7"}}, older); err != nil {
		t.Fatalf("PutAt() error = %v", err)
	}
	if err := svc.cache.Put(bucketTickets, ticketCacheKey(cfg, space, true, false), []Ticket{{Key: "OPS-41"}, {Key: "APP-7"}}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	tickets, fetchedAt, ok := svc.PeekTicketCache(cfg, all, true, false)
	if !ok || ticketKeys(tickets) != "APP-7,OPS-41" || !fetchedAt.Equal(older) {
		t.Fatalf("PeekTicketCache() = %s at %v ok=%v, want both sources dated by the older", ticketKeys(tickets), fetchedAt, ok)
	}
	if err := svc.InvalidateTicketCache(cfg, all); err != nil {
		t.Fatalf("InvalidateTicketCache() error = %v", err)
	}
	for _, source := range []JiraBoard{board, space, all} {
		if _, fetchedAt, ok := svc.PeekTicketCache(cfg, source, true, false); !ok || !fetchedAt.IsZero() {
			t.Fatalf("%s after invalidation: at %v ok=%v, want stale rows kept", source.Name, fetchedAt, ok)
		}
	}
}

func ticketKeys(tickets []Ticket) string {
	keys := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		keys = append(keys, ticket.Key)
	}
	return strings.Join(keys, ",")
}

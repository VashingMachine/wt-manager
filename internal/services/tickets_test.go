package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

// Assures that ticket scopes are cached separately and that invalidation keeps rows paintable but forces revalidation.
func TestTicketCacheScopesAndInvalidation(t *testing.T) {
	svc := NewService()
	cfg := core.Config{
		ActiveProfile: core.RepositoryProfile{
			Name: "app",
			Jira: &core.JiraConfig{Site: "example.atlassian.net"},
		},
	}
	board := core.JiraBoard{ID: 456, Name: "App Board"}
	if err := svc.cache.Put(bucketTickets, ticketCacheKey(cfg, board, true, false), []core.Ticket{{Key: "APP-123"}}); err != nil {
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

// Assures that a space lists the tickets assigned to the current user across
// the entire space, even while the sprint filter is on, and caches them under
// one scope for both sprint settings.
func TestLoadTicketsForSpaceListsAssignedTicketsAcrossSpace(t *testing.T) {
	calls := testkit.InstallFakeACLI(t)
	svc := NewService()
	cfg := core.Config{ActiveProfile: core.RepositoryProfile{Name: "ops", Jira: &core.JiraConfig{Site: "example.atlassian.net"}}}
	space := core.JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}

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
	cfg := core.Config{ActiveProfile: core.RepositoryProfile{Name: "app", Jira: &core.JiraConfig{Site: "example.atlassian.net"}}}
	ops := core.JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}
	web := core.JiraBoard{Name: "Website (WEB)", Projects: []string{"WEB"}}
	board := core.JiraBoard{ID: 123, Name: "App Board", Projects: []string{"APP"}}

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
	testkit.InstallFakeACLI(t)
	svc := NewService()
	cfg := core.Config{ActiveProfile: core.RepositoryProfile{Name: "app", Jira: &core.JiraConfig{Site: "example.atlassian.net"}}}
	all := (&core.JiraConfig{Boards: []core.JiraBoard{
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
	cfg := core.Config{ActiveProfile: core.RepositoryProfile{Name: "app", Jira: &core.JiraConfig{Site: "example.atlassian.net"}}}
	board := core.JiraBoard{ID: 123, Name: "App Board", Projects: []string{"APP"}}
	space := core.JiraBoard{Name: "Operations (OPS)", Projects: []string{"OPS"}}
	all := (&core.JiraConfig{Boards: []core.JiraBoard{board, space}}).AllSources()
	older := time.Now().Add(-3 * time.Minute)
	if err := svc.cache.PutAt(bucketTickets, ticketCacheKey(cfg, board, true, false), []core.Ticket{{Key: "APP-7"}}, older); err != nil {
		t.Fatalf("PutAt() error = %v", err)
	}
	if err := svc.cache.Put(bucketTickets, ticketCacheKey(cfg, space, true, false), []core.Ticket{{Key: "OPS-41"}, {Key: "APP-7"}}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	tickets, fetchedAt, ok := svc.PeekTicketCache(cfg, all, true, false)
	if !ok || ticketKeys(tickets) != "APP-7,OPS-41" || !fetchedAt.Equal(older) {
		t.Fatalf("PeekTicketCache() = %s at %v ok=%v, want both sources dated by the older", ticketKeys(tickets), fetchedAt, ok)
	}
	if err := svc.InvalidateTicketCache(cfg, all); err != nil {
		t.Fatalf("InvalidateTicketCache() error = %v", err)
	}
	for _, source := range []core.JiraBoard{board, space, all} {
		if _, fetchedAt, ok := svc.PeekTicketCache(cfg, source, true, false); !ok || !fetchedAt.IsZero() {
			t.Fatalf("%s after invalidation: at %v ok=%v, want stale rows kept", source.Name, fetchedAt, ok)
		}
	}
}

func ticketKeys(tickets []core.Ticket) string {
	keys := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		keys = append(keys, ticket.Key)
	}
	return strings.Join(keys, ",")
}

// Assures that a confirmed ticket mutation is written into every cached list and detail, and lists are marked for revalidation.
func TestApplyTicketPatchUpdatesCachedCopies(t *testing.T) {
	svc := NewService()
	cfg := core.Config{ActiveProfile: core.RepositoryProfile{Name: "app", Jira: &core.JiraConfig{Site: "example.atlassian.net"}}}
	board := core.JiraBoard{ID: 9, Name: "Board"}
	for _, mine := range []bool{true, false} {
		_ = svc.cache.Put(bucketTickets, ticketCacheKey(cfg, board, mine, true), []core.Ticket{{Key: "APP-1", Status: "To Do"}, {Key: "APP-2", Status: "To Do"}})
	}
	_ = svc.cache.Put(bucketTicketDetail, ticketDetailCacheKey(cfg, "APP-1"), core.Ticket{Key: "APP-1", Status: "To Do", Detail: true})

	svc.ApplyTicketPatch(cfg, board, core.TicketPatch{Key: "APP-1", Status: "Done", StatusCategory: core.StatusCategoryDone})

	for _, mine := range []bool{true, false} {
		tickets, fetchedAt, ok := svc.PeekTicketCache(cfg, board, mine, true)
		if !ok || !fetchedAt.IsZero() || tickets[0].Status != "Done" || tickets[1].Status != "To Do" {
			t.Fatalf("scope mine=%v = %#v at=%v ok=%v", mine, tickets, fetchedAt, ok)
		}
	}
	detail, ok := svc.PeekTicketDetail(cfg, "APP-1")
	if !ok || detail.Status != "Done" || detail.StatusCategory != core.StatusCategoryDone || !detail.Detail {
		t.Fatalf("PeekTicketDetail() = %#v ok=%v", detail, ok)
	}
}

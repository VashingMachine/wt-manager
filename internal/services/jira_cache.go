package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// JIRA is the slowest data source in the app: a board listing costs 1-3s
// through acli. Strategy is stale-while-revalidate. PeekTicketCache is a
// synchronous cache read the TUI uses to paint rows immediately; LoadTickets then
// either short-circuits (cache younger than ticketCacheTTL) or refetches and
// rewrites the cache. When a refetch fails but a cache exists, the cached tickets
// are returned with a warning rather than an error, so the view keeps working
// offline. Mutations patch every cached copy of the ticket and mark the lists
// stale rather than dropping them, so the next visit still paints instantly.

const ticketCacheTTL = 5 * time.Minute

const (
	bucketTickets      = "jira-tickets"
	bucketTicketDetail = "jira-ticket"
)

func ticketCacheScope(mineOnly bool, sprintOnly bool) string {
	parts := []string{}
	if mineOnly {
		parts = append(parts, "mine")
	} else {
		parts = append(parts, "all")
	}
	if sprintOnly {
		parts = append(parts, "sprint")
	} else {
		parts = append(parts, "board")
	}
	return strings.Join(parts, "-")
}

func ticketCacheSite(cfg Config) string {
	if cfg.ActiveProfile.Jira != nil && cfg.ActiveProfile.Jira.Site != "" {
		return cfg.ActiveProfile.Jira.Site
	}
	return "site"
}

func ticketCacheKey(cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) string {
	return fmt.Sprintf("%s/%s/%d/%s", ticketCacheSite(cfg), cacheProfileName(cfg), board.ID, ticketCacheScope(mineOnly, sprintOnly))
}

// ticketCacheKeys lists every scope a board can be cached under.
func ticketCacheKeys(cfg Config, board JiraBoard) []string {
	keys := make([]string, 0, 4)
	for _, mineOnly := range []bool{true, false} {
		for _, sprintOnly := range []bool{true, false} {
			keys = append(keys, ticketCacheKey(cfg, board, mineOnly, sprintOnly))
		}
	}
	return keys
}

func ticketDetailCacheKey(cfg Config, key string) string {
	return ticketCacheSite(cfg) + "/" + strings.ToUpper(strings.TrimSpace(key))
}

// cacheableTickets drops the local worktree/PR joins, which are recomputed on
// every load and would otherwise go stale on disk.
func cacheableTickets(tickets []Ticket) []Ticket {
	out := make([]Ticket, len(tickets))
	for i, ticket := range tickets {
		ticket.Worktree = nil
		ticket.PR = nil
		out[i] = ticket
	}
	return out
}

// PeekTicketCache returns cached tickets without touching the network, along
// with the time they were fetched (zero once a mutation made them stale).
func (s *Service) PeekTicketCache(cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) ([]Ticket, time.Time, bool) {
	var tickets []Ticket
	fetchedAt, ok := s.cache.Get(bucketTickets, ticketCacheKey(cfg, board, mineOnly, sprintOnly), &tickets)
	return tickets, fetchedAt, ok
}

func (s *Service) LoadTickets(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool, refresh bool) ([]Ticket, string, error) {
	ctx = s.commandContext(ctx)
	if board.ID == 0 {
		return nil, "", errors.New("no JIRA board configured for this profile; press I to run setup")
	}

	key := ticketCacheKey(cfg, board, mineOnly, sprintOnly)
	cached, fetchedAt, cachedOK := s.PeekTicketCache(cfg, board, mineOnly, sprintOnly)
	if !refresh && cachedOK && isFresh(fetchedAt, ticketCacheTTL) {
		return cached, "", nil
	}

	tickets, err := s.flights.do(bucketTickets+"\x00"+key, func() (any, error) {
		return fetchTickets(ctx, cfg, board, mineOnly, sprintOnly)
	})
	if err != nil {
		if cachedOK {
			return cached, fmt.Sprintf("JIRA refresh failed, showing %s: %v", cachedLabel("tickets", fetchedAt), err), nil
		}
		return nil, "", err
	}
	fresh := tickets.([]Ticket)
	if err := s.cache.Put(bucketTickets, key, cacheableTickets(fresh)); err != nil {
		return fresh, fmt.Sprintf("ticket cache write failed: %v", err), nil
	}
	return fresh, "", nil
}

func (s *Service) LoadTicketDetail(ctx context.Context, cfg Config, key string) (Ticket, error) {
	ctx = s.commandContext(ctx)
	ticket, err := loadTicketDetail(ctx, cfg, key)
	if err != nil {
		return Ticket{}, err
	}
	_ = s.cache.Put(bucketTicketDetail, ticketDetailCacheKey(cfg, ticket.Key), cacheableTickets([]Ticket{ticket})[0])
	return ticket, nil
}

// PeekTicketDetail returns a ticket's description and comments from an earlier
// detail load, so revisiting a ticket does not show an empty reader.
func (s *Service) PeekTicketDetail(cfg Config, key string) (Ticket, bool) {
	var ticket Ticket
	if _, ok := s.cache.Get(bucketTicketDetail, ticketDetailCacheKey(cfg, key), &ticket); !ok {
		return Ticket{}, false
	}
	return ticket, true
}

func (s *Service) InvalidateTicketCache(cfg Config, board JiraBoard) error {
	if board.ID == 0 {
		return nil
	}
	for _, key := range ticketCacheKeys(cfg, board) {
		s.cache.Expire(bucketTickets, key)
	}
	return nil
}

func (s *Service) ApplyTicketPatch(cfg Config, board JiraBoard, patch core.TicketPatch) {
	if patch.Key == "" {
		return
	}
	if board.ID != 0 {
		for _, key := range ticketCacheKeys(cfg, board) {
			var tickets []Ticket
			if _, ok := s.cache.Get(bucketTickets, key, &tickets); !ok {
				continue
			}
			for i := range tickets {
				patch.Apply(&tickets[i])
			}
			// A mutation can move the ticket in or out of a scope (assignee,
			// open status), so keep the patched rows but revalidate them.
			_ = s.cache.PutAt(bucketTickets, key, tickets, time.Time{})
		}
	}
	detailKey := ticketDetailCacheKey(cfg, patch.Key)
	var detail Ticket
	if fetchedAt, ok := s.cache.Get(bucketTicketDetail, detailKey, &detail); ok {
		patch.Apply(&detail)
		_ = s.cache.PutAt(bucketTicketDetail, detailKey, detail, fetchedAt)
	}
}

func cacheProfileName(cfg Config) string {
	if cfg.ActiveProfile.Name != "" {
		return cfg.ActiveProfile.Name
	}
	return "default"
}

// isFresh reports whether an entry fetched at fetchedAt is still within ttl. A
// zero time marks an entry that was explicitly expired.
func isFresh(fetchedAt time.Time, ttl time.Duration) bool {
	return !fetchedAt.IsZero() && time.Since(fetchedAt) < ttl
}

// cachedLabel describes cached data for a fallback warning.
func cachedLabel(what string, fetchedAt time.Time) string {
	if fetchedAt.IsZero() {
		return "cached " + what
	}
	return fmt.Sprintf("%s cached %s ago", what, cacheAgeLabel(time.Since(fetchedAt)))
}

// cacheAgeLabel renders a duration the way the status bar wants to read it.
func cacheAgeLabel(age time.Duration) string {
	switch {
	case age < time.Minute:
		return strconv.Itoa(int(age.Seconds())) + "s"
	case age < time.Hour:
		return strconv.Itoa(int(age.Minutes())) + "m"
	case age < 24*time.Hour:
		return strconv.Itoa(int(age.Hours())) + "h"
	default:
		return strconv.Itoa(int(age.Hours()/24)) + "d"
	}
}

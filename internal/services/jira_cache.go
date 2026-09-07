package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// JIRA is the first data source in the app slow enough to need caching: a board
// listing costs 1-3s through acli. The cache exists so that entering tickets mode
// paints instantly and so that moving between modes does not re-hit the network.
//
// Strategy is stale-while-revalidate. PeekTicketCache is a synchronous disk read
// the TUI uses to paint rows immediately; loadTickets then either short-circuits
// (cache younger than ticketCacheTTL) or refetches and rewrites the cache. When a
// refetch fails but a cache exists, the cached tickets are returned with a
// warning rather than an error, so the view keeps working offline.

const ticketCacheTTL = 5 * time.Minute
const ticketCacheSchemaVersion = 1

type ticketCacheEntry struct {
	Version   int       `json:"version"`
	FetchedAt time.Time `json:"fetchedAt"`
	Site      string    `json:"site"`
	BoardID   int       `json:"boardId"`
	BoardName string    `json:"boardName"`
	Scope     string    `json:"scope"`
	Tickets   []Ticket  `json:"tickets"`
}

func jiraCacheDir(cfg Config) string {
	home := cfg.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".wt-manager", "cache")
}

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

func ticketCachePath(cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) string {
	profile := cfg.ActiveProfile.Name
	if profile == "" {
		profile = "default"
	}
	site := "site"
	if cfg.ActiveProfile.Jira != nil && cfg.ActiveProfile.Jira.Site != "" {
		site = cfg.ActiveProfile.Jira.Site
	}
	name := fmt.Sprintf("jira-%s-%s-%d-%s.json", sanitizeCacheSegment(site), sanitizeCacheSegment(profile), board.ID, ticketCacheScope(mineOnly, sprintOnly))
	return filepath.Join(jiraCacheDir(cfg), name)
}

func sanitizeCacheSegment(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

func readTicketCache(path string) (ticketCacheEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ticketCacheEntry{}, false
	}
	var entry ticketCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return ticketCacheEntry{}, false
	}
	if entry.FetchedAt.IsZero() {
		return ticketCacheEntry{}, false
	}
	if entry.Version != ticketCacheSchemaVersion {
		return ticketCacheEntry{}, false
	}
	return entry, true
}

func writeTicketCache(path string, entry ticketCacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".jira-cache-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// peekTicketCache returns cached tickets without touching the network, along with
// the time they were fetched.
func peekTicketCache(cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool) ([]Ticket, time.Time, bool) {
	entry, ok := readTicketCache(ticketCachePath(cfg, board, mineOnly, sprintOnly))
	if !ok {
		return nil, time.Time{}, false
	}
	if entry.BoardID != board.ID || entry.Scope != ticketCacheScope(mineOnly, sprintOnly) {
		return nil, time.Time{}, false
	}
	return entry.Tickets, entry.FetchedAt, true
}

func loadTickets(ctx context.Context, cfg Config, board JiraBoard, mineOnly bool, sprintOnly bool, refresh bool) ([]Ticket, string, error) {
	if board.ID == 0 {
		return nil, "", errors.New("no JIRA board configured for this profile; press I to run setup")
	}

	path := ticketCachePath(cfg, board, mineOnly, sprintOnly)
	cached, cachedOK := readTicketCache(path)
	if !refresh && cachedOK && time.Since(cached.FetchedAt) < ticketCacheTTL {
		return cached.Tickets, "", nil
	}

	tickets, err := fetchTickets(ctx, cfg, board, mineOnly, sprintOnly)
	if err != nil {
		if cachedOK {
			return cached.Tickets, fmt.Sprintf("JIRA refresh failed, showing tickets cached %s ago: %v", cacheAgeLabel(time.Since(cached.FetchedAt)), err), nil
		}
		return nil, "", err
	}

	entry := ticketCacheEntry{
		Version:   ticketCacheSchemaVersion,
		FetchedAt: time.Now(),
		Site:      jiraSiteForConfig(ctx, cfg),
		BoardID:   board.ID,
		BoardName: board.Name,
		Scope:     ticketCacheScope(mineOnly, sprintOnly),
		Tickets:   tickets,
	}
	warning := ""
	if err := writeTicketCache(path, entry); err != nil {
		warning = fmt.Sprintf("ticket cache write failed: %v", err)
	}
	return tickets, warning, nil
}

// invalidateTicketCache drops every scope cached for a board. Called after a
// mutation so the next load cannot serve a stale status.
func invalidateTicketCache(cfg Config, board JiraBoard) error {
	if board.ID == 0 {
		return nil
	}
	site := "site"
	if cfg.ActiveProfile.Jira != nil && cfg.ActiveProfile.Jira.Site != "" {
		site = cfg.ActiveProfile.Jira.Site
	}
	prefix := fmt.Sprintf("jira-%s-%s-%d-", sanitizeCacheSegment(site), sanitizeCacheSegment(cacheProfileName(cfg)), board.ID)
	entries, err := os.ReadDir(jiraCacheDir(cfg))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		if err := os.Remove(filepath.Join(jiraCacheDir(cfg), entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func cacheProfileName(cfg Config) string {
	if cfg.ActiveProfile.Name != "" {
		return cfg.ActiveProfile.Name
	}
	return "default"
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

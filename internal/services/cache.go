package services

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

// Cache layout. Buckets group one kind of data; keys identify the repository
// (or JIRA site) and the item, so profiles sharing a cache file never collide.
const (
	bucketWorktreeList   = "worktree-list"   // repo path -> discovered worktrees
	bucketWorktreeStatus = "worktree-status" // repo path + worktree path -> status, commit
	bucketPRIndex        = "pr-index"        // repo slug -> PRs used to label branches
	bucketPRList         = "pr-list"         // repo slug -> PR Radar list
	bucketPRChecks       = "pr-checks"       // repo slug + head SHA -> check runs
	bucketPRDetail       = "pr-detail"       // repo slug + number -> PR detail and diff
	bucketGitHubUser     = "github-user"     // "login" -> authenticated user
)

const (
	// prIndexTTL bounds how long branch labels reuse a `gh pr list`; refreshes
	// within it (after creating a worktree, entering tickets) cost nothing.
	prIndexTTL    = 2 * time.Minute
	githubUserTTL = 24 * time.Hour
	// Completed check runs for a commit rarely change, while queued or
	// in-progress ones do, so they are revisited much sooner.
	checksCompleteTTL = 15 * time.Minute
	checksPendingTTL  = 30 * time.Second
	checksEmptyTTL    = 2 * time.Minute
)

// flightGroup collapses concurrent identical fetches into one: a background
// prefetch and the view the user just opened share the same request.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flight
}

type flight struct {
	done  chan struct{}
	value any
	err   error
}

func (g *flightGroup) do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flight{}
	}
	if call, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-call.done
		return call.value, call.err
	}
	call := &flight{done: make(chan struct{})}
	g.calls[key] = call
	g.mu.Unlock()

	call.value, call.err = fn()
	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	close(call.done)
	return call.value, call.err
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

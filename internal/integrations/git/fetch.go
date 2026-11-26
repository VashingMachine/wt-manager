package git

import (
	"context"
	"sync"
	"time"
)

// fetchReuseWindow lets worktree creations queued behind each other share
// one `git fetch` instead of each paying the network round trip.
const fetchReuseWindow = 15 * time.Second

// fetchCoalescer skips a `git fetch` when the same remote was fetched within
// fetchReuseWindow, which is what queued worktree creations look like.
type fetchCoalescer struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func (c *fetchCoalescer) fetch(ctx context.Context, repoPath string, remoteName string, force bool) bool {
	key := repoPath + "\x00" + remoteName
	c.mu.Lock()
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	if at, ok := c.last[key]; ok && !force && now().Sub(at) < fetchReuseWindow {
		c.mu.Unlock()
		return false
	}
	c.mu.Unlock()

	fetchRemote(ctx, repoPath, remoteName, force)

	c.mu.Lock()
	if c.last == nil {
		c.last = map[string]time.Time{}
	}
	c.last[key] = now()
	c.mu.Unlock()
	return true
}

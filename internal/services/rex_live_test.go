package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in live check against a running Rex: set WT_REX_LIVE=1 and run from a Rex
// terminal. It creates a real session for a temporary worktree, checks that the
// agent ran in the worktree directory and that a second open finds the same
// session, then kills the session. It does not switch the app to it.
func TestRexLiveIntegration(t *testing.T) {
	if os.Getenv("WT_REX_LIVE") != "1" || !inRex(os.Getenv) {
		t.Skip("set WT_REX_LIVE=1 and run inside a Rex terminal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := canonicalPath(t.TempDir())
	wt := Worktree{Name: "wt-live-rex", Path: dir}
	agent := AgentTool{Name: "agent", Command: "echo agent-started-in-$(pwd -P)"}

	session, created, err := ensureRexSession(ctx, wt, &agent)
	if session.ID != "" {
		t.Cleanup(func() { _, _ = runRex(context.Background(), "kill", session.ID) })
	}
	if err != nil {
		t.Fatalf("ensureRexSession: %v", err)
	}
	if !created || session.BlockID == "" {
		t.Fatalf("session = %+v, created = %v, want a new session with a pane", session, created)
	}

	want := "agent-started-in-" + dir
	var screen string
	for range 50 {
		screen, err = runRex(ctx, "capture", "-s", session.ID, "-b", session.BlockID, "--trim", "--unwrap")
		// The typed command is echoed with $(pwd -P) unexpanded, so only its
		// output contains the expanded directory.
		if err == nil && strings.Contains(screen, want) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(screen, want) {
		t.Fatalf("pane never showed %q; capture:\n%s", want, screen)
	}

	again, createdAgain, err := ensureRexSession(ctx, wt, &agent)
	if err != nil {
		t.Fatalf("second ensureRexSession: %v", err)
	}
	if createdAgain || again.ID != session.ID {
		t.Fatalf("second open = %+v (created %v), want session %s reused", again, createdAgain, session.ID)
	}
}

// Opt-in live check of switching the Rex app: turn on Remote Control, set
// WT_REX_LIVE_SWITCH=1 and run from a Rex terminal. The app showing this
// terminal switches to a new worktree session, back, and to it again, so the
// screen flickers. It ends back here and kills the session.
func TestRexLiveSwitchesApp(t *testing.T) {
	if os.Getenv("WT_REX_LIVE_SWITCH") != "1" || !inRex(os.Getenv) || rexOwnPane() == "" {
		t.Skip("set WT_REX_LIVE_SWITCH=1 and run inside a Rex terminal")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	app := rexOwnPaneOwner(ctx)
	if app == "" {
		t.Skip("no Rex app owns this terminal")
	}
	whoami, err := runRex(ctx, "whoami", "--json")
	var me struct {
		SessionID string `json:"session_id"`
	}
	if err != nil || decodeRexJSON(whoami, &me) != nil || me.SessionID == "" {
		t.Fatalf("whoami = %q, %v", whoami, err)
	}

	wt := Worktree{Name: fmt.Sprintf("wt-live-switch-%d", time.Now().UnixNano()), Path: canonicalPath(t.TempDir())}
	// Cleanups run last-registered first: return the app here, then kill.
	t.Cleanup(func() { killRexSessionsNamed(wt.Name) })
	t.Cleanup(func() { _ = showRexSession(context.Background(), me.SessionID) })

	if err := openAgentInRex(ctx, wt, AgentTool{Name: "agent", Command: "echo agent-started"}); err != nil {
		t.Fatalf("first open: %v", err)
	}
	sessionID := findRexSession(ctx, wt)
	if sessionID == "" {
		t.Fatal("first open left no session for the worktree")
	}
	// A pane gains an owner only once an app displays it, so this shows the
	// switch really happened in the app the user is looking at.
	shownBy := ""
	for range 30 {
		if blocks, err := listRexBlocks(ctx, sessionID); err == nil && len(blocks) > 0 && blocks[0].Owner != "" {
			shownBy = blocks[0].Owner
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if shownBy != app {
		t.Fatalf("worktree session shown by %q, want %q", shownBy, app)
	}

	// Return to wt-manager's session as the user would, then open again.
	if err := showRexSession(ctx, me.SessionID); err != nil {
		t.Fatalf("switch back: %v", err)
	}
	if err := openTerminalInRex(ctx, wt); err != nil {
		t.Fatalf("second open: %v", err)
	}
	if n := len(rexSessionsNamed(wt.Name)); n != 1 {
		t.Fatalf("%d sessions named %s, want the first one reused", n, wt.Name)
	}
}

func rexSessionsNamed(name string) []string {
	output, err := runRex(context.Background(), "ls", "--json")
	if err != nil {
		return nil
	}
	var listing struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Label     string `json:"label"`
		} `json:"sessions"`
	}
	if decodeRexJSON(output, &listing) != nil {
		return nil
	}
	var ids []string
	for _, session := range listing.Sessions {
		if session.Label == name {
			ids = append(ids, session.SessionID)
		}
	}
	return ids
}

func killRexSessionsNamed(name string) {
	for _, id := range rexSessionsNamed(name) {
		_, _ = runRex(context.Background(), "kill", id)
	}
}

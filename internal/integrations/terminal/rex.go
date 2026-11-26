package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/fspath"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

const rexTerminalCreator = "com.superlogical.terminal"

// inRex reports whether the process runs inside a Rex terminal. REX_BLOCK names
// the pane and, unlike TERM_PROGRAM, survives tmux. REX_SERVER alone only says
// which server to talk to, not that the process sits in one of its panes.
func inRex(getenv func(string) string) bool {
	return getenv("REX_BLOCK") != "" || getenv("TERM_PROGRAM") == "rex"
}

// rexBinary prefers rex on PATH, then the copy in the directory Rex exports to
// its terminals.
var rexBinary = sync.OnceValue(func() string {
	if path, err := exec.LookPath("rex"); err == nil {
		return path
	}
	if dir := os.Getenv("REX_BIN_DIR"); dir != "" {
		candidate := filepath.Join(dir, "rex")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "rex"
})

// runRex and rexOwnPane are seams so tests can avoid a real Rex server.
var (
	runRex = func(ctx context.Context, args ...string) (string, error) {
		return shell.Run(ctx, "", rexBinary(), args...)
	}
	rexOwnPane = func() string { return os.Getenv("REX_BLOCK") }
)

// rexSessionMu serialises opening so a quick double press cannot list the
// sessions twice before either has created the worktree's session.
var rexSessionMu sync.Mutex

type rexSession struct {
	ID      string
	BlockID string
}

func openAgentInRex(ctx context.Context, wt core.Worktree, agent core.AgentTool) error {
	return openRexSession(ctx, wt, &agent)
}

func openTerminalInRex(ctx context.Context, wt core.Worktree) error {
	return openRexSession(ctx, wt, nil)
}

// openRexSession shows the worktree's own Rex session in the app, creating it
// first when the worktree has none.
func openRexSession(ctx context.Context, wt core.Worktree, agent *core.AgentTool) error {
	rexSessionMu.Lock()
	defer rexSessionMu.Unlock()

	session, created, err := ensureRexSession(ctx, wt, agent)
	if err != nil {
		return err
	}
	if err := showRexSession(ctx, session.ID); err != nil {
		if created {
			return fmt.Errorf("opened Rex session %s but could not switch to it: %w", wt.Name, err)
		}
		return fmt.Errorf("found Rex session %s but could not switch to it: %w", wt.Name, err)
	}
	return nil
}

// ensureRexSession returns the worktree's existing session, or creates one and
// reports created=true.
func ensureRexSession(ctx context.Context, wt core.Worktree, agent *core.AgentTool) (rexSession, bool, error) {
	if id := findRexSession(ctx, wt); id != "" {
		return rexSession{ID: id}, false, nil
	}
	session, err := createRexSession(ctx, wt, agent)
	return session, true, err
}

// createRexSession creates a session named after the worktree, holding one
// shell rooted at it. The agent is typed into that shell instead of being its
// program, so quitting the agent leaves a prompt in the worktree.
func createRexSession(ctx context.Context, wt core.Worktree, agent *core.AgentTool) (rexSession, error) {
	window := "shell"
	if agent != nil {
		window = agent.Name
	}
	output, err := runRex(ctx, "new", wt.Name, "--cwd", wt.Path, "--window", window, "--json")
	if err != nil {
		return rexSession{}, fmt.Errorf("open Rex session failed: %w", err)
	}
	var created struct {
		SessionID      string `json:"session_id"`
		InitialWindows []struct {
			BlockIDs []string `json:"block_ids"`
		} `json:"initial_windows"`
	}
	if err := decodeRexJSON(output, &created); err != nil || created.SessionID == "" {
		return rexSession{}, fmt.Errorf("open Rex session failed: unexpected reply %q", strings.TrimSpace(output))
	}
	session := rexSession{ID: created.SessionID}
	if len(created.InitialWindows) > 0 && len(created.InitialWindows[0].BlockIDs) > 0 {
		session.BlockID = created.InitialWindows[0].BlockIDs[0]
	}
	if agent == nil {
		return session, nil
	}
	if session.BlockID == "" {
		return session, fmt.Errorf("opened Rex session %s but could not find its pane to start %s", wt.Name, agent.Name)
	}
	if _, err := runRex(ctx, "send", "-s", session.ID, "-b", session.BlockID, agent.Command); err != nil {
		return session, fmt.Errorf("opened Rex session %s but could not start %s: %w", wt.Name, agent.Name, err)
	}
	if _, err := runRex(ctx, "send-key", "-s", session.ID, "-b", session.BlockID, "enter"); err != nil {
		return session, fmt.Errorf("opened Rex session %s but could not start %s: %w", wt.Name, agent.Name, err)
	}
	return session, nil
}

// findRexSession returns the ID of a session named after the worktree whose
// terminals are in it, or "" when there is none or the listing is unreadable.
func findRexSession(ctx context.Context, wt core.Worktree) string {
	output, err := runRex(ctx, "ls", "--json")
	if err != nil {
		return ""
	}
	var listing struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Label     string `json:"label"`
		} `json:"sessions"`
	}
	if decodeRexJSON(output, &listing) != nil {
		return ""
	}
	for _, session := range listing.Sessions {
		if session.Label == wt.Name && rexSessionInWorktree(ctx, session.SessionID, wt.Path) {
			return session.SessionID
		}
	}
	return ""
}

// rexSessionInWorktree tells a worktree's session apart from a same-named one
// of another repository by where its terminals' programs run. A terminal still
// in a subdirectory counts. A session whose terminals report no directory, such
// as one whose shell is still starting, is trusted by its name.
func rexSessionInWorktree(ctx context.Context, sessionID string, path string) bool {
	blocks, err := listRexBlocks(ctx, sessionID)
	if err != nil {
		return true
	}
	reported := false
	for _, block := range blocks {
		if block.CreatorName != rexTerminalCreator {
			continue
		}
		cwd := rexTerminalDirectory(ctx, sessionID, block.BlockID)
		if cwd == "" {
			continue
		}
		if fspath.Inside(cwd, path) {
			return true
		}
		reported = true
	}
	return !reported
}

// rexTerminalDirectory returns the working directory of the program in the
// foreground of a terminal, as the operating system reports it.
func rexTerminalDirectory(ctx context.Context, sessionID string, blockID string) string {
	output, err := runRex(ctx, "block", "call", "-s", sessionID, "-b", blockID, "process")
	if err != nil {
		return ""
	}
	var process struct {
		Foreground struct {
			Cwd string `json:"cwd"`
		} `json:"foreground"`
	}
	if decodeRexJSON(output, &process) != nil {
		return ""
	}
	return process.Foreground.Cwd
}

// showRexSession switches the Rex app the user sees wt-manager in to another
// session. Rex refuses unless Remote Control is on in the app's settings, and
// its message says so.
func showRexSession(ctx context.Context, sessionID string) error {
	args := []string{"do", "session.select", "session_id=" + sessionID}
	if app := rexOwnPaneOwner(ctx); app != "" {
		args = append([]string{"-C", app}, args...)
	}
	// Without an owner to target, Rex picks the app itself when only one is connected.
	_, err := runRex(ctx, args...)
	return err
}

// rexOwnPaneOwner returns the app that owns wt-manager's own pane. A pane is
// owned by the app that last displayed it, so with several apps connected this
// is the one the user is looking at wt-manager through. Every app attaches to
// every session, so attachment would not tell them apart.
func rexOwnPaneOwner(ctx context.Context) string {
	pane := rexOwnPane()
	if pane == "" {
		return ""
	}
	// A pane's ID also names the session that holds it.
	blocks, err := listRexBlocks(ctx, pane)
	if err != nil {
		return ""
	}
	for _, block := range blocks {
		if block.BlockID == pane {
			return block.Owner
		}
	}
	return ""
}

type rexBlock struct {
	BlockID     string `json:"block_id"`
	CreatorName string `json:"creator_name"`
	Owner       string `json:"owner"`
}

func listRexBlocks(ctx context.Context, session string) ([]rexBlock, error) {
	output, err := runRex(ctx, "block", "ls", "-s", session, "--json")
	if err != nil {
		return nil, err
	}
	var listing struct {
		Blocks []rexBlock `json:"blocks"`
	}
	if err := decodeRexJSON(output, &listing); err != nil {
		return nil, err
	}
	return listing.Blocks, nil
}

// decodeRexJSON decodes the JSON reply of a rex command. The reply is read with
// stderr mixed in, so a stray message before the JSON is skipped.
func decodeRexJSON(output string, v any) error {
	start := strings.IndexByte(output, '{')
	if start < 0 {
		return errors.New("no JSON object in rex output")
	}
	return json.NewDecoder(strings.NewReader(output[start:])).Decode(v)
}

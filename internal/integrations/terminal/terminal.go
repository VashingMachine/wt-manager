// Package terminal opens worktrees in the terminal app wt-manager runs in. It
// detects that app from the environment and drives it the way it supports:
// Rex and cmux through their own command lines (rex.go, cmux.go), Ghostty,
// Warp and Apple Terminal through AppleScript and `open` (this file).
//
// Host is the whole surface the rest of the app sees. To support another
// terminal app, add its open functions in a file of its own, then a
// terminalApp constant, its detection in detectTerminalApp and its row in
// hosts.
package terminal

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"

	"github.com/VashingMachine/wt-manager/internal/core"
)

// Host is the terminal app wt-manager runs in.
type Host struct{}

func New() *Host {
	return &Host{}
}

// OpenAgent opens the worktree in the terminal app with the agent started.
func (h *Host) OpenAgent(ctx context.Context, wt core.Worktree, agent core.AgentTool) error {
	return openWorktreeAgent(ctx, wt, agent)
}

// OpenTerminal opens a shell in the worktree.
func (h *Host) OpenTerminal(ctx context.Context, wt core.Worktree) error {
	return openWorktreeTerminal(ctx, wt)
}

// Notify posts a notification in the terminal app. It does nothing outside
// cmux, the only one that has them.
func (h *Host) Notify(ctx context.Context, title string, body string) error {
	return notifyCmux(ctx, title, body)
}

// OpenURL shows a page inside the terminal app when it has a browser of its
// own, which only cmux does. It reports false when the page was not shown, so
// the caller can fall back to the system browser.
func (h *Host) OpenURL(ctx context.Context, url string) bool {
	return openURLInHost(ctx, url)
}

func openURLInHost(ctx context.Context, url string) bool {
	return inCmux(os.Getenv) && openURLInCmux(ctx, url) == nil
}

type terminalApp int

const (
	terminalGhostty terminalApp = iota
	terminalCmux
	terminalWarp
	terminalAppleTerminal
	terminalRex
	// terminalAppCount stays last: it counts the apps above.
	terminalAppCount
)

func detectTerminalApp(getenv func(string) string) terminalApp {
	// REX_BLOCK is set per pane, so it wins over cmux variables that a Rex server
	// started from a cmux terminal may pass on to its panes.
	if inRex(getenv) {
		return terminalRex
	}
	// cmux reports TERM_PROGRAM=ghostty, so it has to be recognised before Ghostty.
	if inCmux(getenv) {
		return terminalCmux
	}
	// Ghostty-specific vars survive tmux, which rewrites TERM_PROGRAM.
	if getenv("GHOSTTY_RESOURCES_DIR") != "" || getenv("GHOSTTY_BIN_DIR") != "" {
		return terminalGhostty
	}
	switch getenv("TERM_PROGRAM") {
	case "ghostty":
		return terminalGhostty
	case "WarpTerminal":
		return terminalWarp
	case "Apple_Terminal":
		return terminalAppleTerminal
	}
	return terminalGhostty
}

var currentTerminalApp = sync.OnceValue(func() terminalApp {
	return detectTerminalApp(os.Getenv)
})

// host is one terminal app: how to open an agent and a plain shell in it.
type host struct {
	openAgent    func(ctx context.Context, wt core.Worktree, agent core.AgentTool) error
	openTerminal func(ctx context.Context, wt core.Worktree) error
	// anyOS is set for an app that runs anywhere and is driven through its own
	// command line, as Rex is. The others are macOS apps.
	anyOS bool
}

// hosts has one entry per terminalApp.
var hosts = map[terminalApp]host{
	terminalRex:           {openAgent: openAgentInRex, openTerminal: openTerminalInRex, anyOS: true},
	terminalCmux:          {openAgent: openAgentInCmux, openTerminal: openTerminalInCmux},
	terminalWarp:          {openAgent: openAgentInWarp, openTerminal: openTerminalInWarp},
	terminalAppleTerminal: {openAgent: openAgentInAppleTerminal, openTerminal: openTerminalInAppleTerminal},
	terminalGhostty:       {openAgent: openAgentInGhostty, openTerminal: openTerminalInGhostty},
}

// currentHost returns the app to open tabs in, or an error on a system where
// that app cannot be driven.
func currentHost() (host, error) {
	current := hosts[currentTerminalApp()]
	if !current.anyOS && runtime.GOOS != "darwin" {
		return host{}, errors.New("opening terminal tabs is only supported on macOS")
	}
	return current, nil
}

func openWorktreeAgent(ctx context.Context, wt core.Worktree, agent core.AgentTool) error {
	current, err := currentHost()
	if err != nil {
		return err
	}
	return current.openAgent(ctx, wt, agent)
}

func openWorktreeTerminal(ctx context.Context, wt core.Worktree) error {
	current, err := currentHost()
	if err != nil {
		return err
	}
	return current.openTerminal(ctx, wt)
}

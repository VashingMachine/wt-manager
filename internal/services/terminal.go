package services

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"sync"
)

type terminalApp int

const (
	terminalGhostty terminalApp = iota
	terminalWarp
	terminalAppleTerminal
)

func detectTerminalApp(getenv func(string) string) terminalApp {
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

func openWorktreeAgent(ctx context.Context, wt Worktree, agent AgentTool) error {
	if runtime.GOOS != "darwin" {
		return errors.New("opening terminal tabs is only supported on macOS")
	}
	switch currentTerminalApp() {
	case terminalWarp:
		return openAgentInWarp(ctx, wt, agent)
	case terminalAppleTerminal:
		return openAgentInAppleTerminal(ctx, wt, agent)
	default:
		return openAgentInGhostty(ctx, wt, agent)
	}
}

func openWorktreeTerminal(ctx context.Context, wt Worktree) error {
	if runtime.GOOS != "darwin" {
		return errors.New("opening terminal tabs is only supported on macOS")
	}
	switch currentTerminalApp() {
	case terminalWarp:
		return openTerminalInWarp(ctx, wt)
	case terminalAppleTerminal:
		return openTerminalInAppleTerminal(ctx, wt)
	default:
		return openTerminalInGhostty(ctx, wt)
	}
}

func openAgentInGhostty(ctx context.Context, wt Worktree, agent AgentTool) error {
	command := fmt.Sprintf("cd %s && %s", shellQuote(wt.Path), agent.Command)
	_, err := runCommand(ctx, "", "osascript", "-e", terminalTabScript("Ghostty", command))
	if err == nil {
		return nil
	}
	_, fallbackErr := runCommand(ctx, "", "open", "-na", "Ghostty.app", "--args", "--working-directory="+wt.Path, "-e", "/bin/zsh", "-lc", agent.Command)
	if fallbackErr != nil {
		return fmt.Errorf("open Ghostty tab failed: %w. Grant Accessibility permission to wt-manager or your terminal", err)
	}
	return nil
}

func openTerminalInGhostty(ctx context.Context, wt Worktree) error {
	command := "cd " + shellQuote(wt.Path)
	_, err := runCommand(ctx, "", "osascript", "-e", terminalTabScript("Ghostty", command))
	if err == nil {
		return nil
	}
	_, fallbackErr := runCommand(ctx, "", "open", ghosttyTerminalArgs(wt)...)
	if fallbackErr != nil {
		return fmt.Errorf("open Ghostty tab failed: %w. Grant Accessibility permission to wt-manager or your terminal", err)
	}
	return nil
}

func ghosttyTerminalArgs(wt Worktree) []string {
	return []string{"-na", "Ghostty.app", "--args", "--working-directory=" + wt.Path}
}

func openAgentInWarp(ctx context.Context, wt Worktree, agent AgentTool) error {
	command := fmt.Sprintf("cd %s && %s", shellQuote(wt.Path), agent.Command)
	_, err := runCommand(ctx, "", "osascript", "-e", terminalTabScript("Warp", command))
	if err == nil {
		return nil
	}
	// The warp:// URI cannot run a command, only open a tab at a directory.
	_, fallbackErr := runCommand(ctx, "", "open", warpNewTabURL(wt.Path))
	if fallbackErr != nil {
		return fmt.Errorf("open Warp tab failed: %w. Grant Accessibility permission to wt-manager or your terminal", err)
	}
	return fmt.Errorf("opened Warp tab but could not run agent command: %w. Grant Accessibility permission to wt-manager or your terminal", err)
}

func openTerminalInWarp(ctx context.Context, wt Worktree) error {
	_, err := runCommand(ctx, "", "open", warpNewTabURL(wt.Path))
	return err
}

func warpNewTabURL(path string) string {
	return "warp://action/new_tab?path=" + url.QueryEscape(path)
}

func openAgentInAppleTerminal(ctx context.Context, wt Worktree, agent AgentTool) error {
	command := fmt.Sprintf("cd %s && %s", shellQuote(wt.Path), agent.Command)
	return runAppleTerminalCommand(ctx, command)
}

func openTerminalInAppleTerminal(ctx context.Context, wt Worktree) error {
	return runAppleTerminalCommand(ctx, "cd "+shellQuote(wt.Path))
}

func runAppleTerminalCommand(ctx context.Context, command string) error {
	_, err := runCommand(ctx, "", "osascript", "-e", appleTerminalTabScript(command))
	if err == nil {
		return nil
	}
	_, fallbackErr := runCommand(ctx, "", "osascript", "-e", appleTerminalWindowScript(command))
	if fallbackErr != nil {
		return fmt.Errorf("open Terminal tab failed: %w. Grant Accessibility/Automation permission to wt-manager or your terminal", err)
	}
	return nil
}

func terminalTabScript(appName string, command string) string {
	commandLiteral := appleScriptString(command)
	appLiteral := appleScriptString(appName)
	return fmt.Sprintf(`set commandText to %s
tell application %s to activate
delay 0.1
tell application "System Events"
	tell process %s
		keystroke "t" using command down
		delay 0.2
		keystroke commandText
		key code 36
	end tell
end tell`, commandLiteral, appLiteral, appLiteral)
}

func appleTerminalTabScript(command string) string {
	commandLiteral := appleScriptString(command)
	return fmt.Sprintf(`set commandText to %s
tell application "Terminal" to activate
delay 0.1
tell application "Terminal"
	if (count of windows) is 0 then
		do script commandText
	else
		tell application "System Events" to tell process "Terminal" to keystroke "t" using command down
		delay 0.2
		do script commandText in selected tab of front window
	end if
end tell`, commandLiteral)
}

func appleTerminalWindowScript(command string) string {
	return fmt.Sprintf(`tell application "Terminal"
	activate
	do script %s
end tell`, appleScriptString(command))
}

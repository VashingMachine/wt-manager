package terminal

import (
	"context"
	"fmt"
	"net/url"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func openAgentInWarp(ctx context.Context, wt core.Worktree, agent core.AgentTool) error {
	command := fmt.Sprintf("cd %s && %s", shell.Quote(wt.Path), agent.Command)
	_, err := shell.Run(ctx, "", "osascript", "-e", terminalTabScript("Warp", command))
	if err == nil {
		return nil
	}
	// The warp:// URI cannot run a command, only open a tab at a directory.
	_, fallbackErr := shell.Run(ctx, "", "open", warpNewTabURL(wt.Path))
	if fallbackErr != nil {
		return fmt.Errorf("open Warp tab failed: %w. Grant Accessibility permission to wt-manager or your terminal", err)
	}
	return fmt.Errorf("opened Warp tab but could not run agent command: %w. Grant Accessibility permission to wt-manager or your terminal", err)
}

func openTerminalInWarp(ctx context.Context, wt core.Worktree) error {
	_, err := shell.Run(ctx, "", "open", warpNewTabURL(wt.Path))
	return err
}

func warpNewTabURL(path string) string {
	return "warp://action/new_tab?path=" + url.QueryEscape(path)
}

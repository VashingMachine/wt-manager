package terminal

import (
	"context"
	"fmt"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func openAgentInGhostty(ctx context.Context, wt core.Worktree, agent core.AgentTool) error {
	command := fmt.Sprintf("cd %s && %s", shell.Quote(wt.Path), agent.Command)
	_, err := shell.Run(ctx, "", "osascript", "-e", terminalTabScript("Ghostty", command))
	if err == nil {
		return nil
	}
	_, fallbackErr := shell.Run(ctx, "", "open", "-na", "Ghostty.app", "--args", "--working-directory="+wt.Path, "-e", "/bin/zsh", "-lc", agent.Command)
	if fallbackErr != nil {
		return fmt.Errorf("open Ghostty tab failed: %w. Grant Accessibility permission to wt-manager or your terminal", err)
	}
	return nil
}

func openTerminalInGhostty(ctx context.Context, wt core.Worktree) error {
	command := "cd " + shell.Quote(wt.Path)
	_, err := shell.Run(ctx, "", "osascript", "-e", terminalTabScript("Ghostty", command))
	if err == nil {
		return nil
	}
	_, fallbackErr := shell.Run(ctx, "", "open", ghosttyTerminalArgs(wt)...)
	if fallbackErr != nil {
		return fmt.Errorf("open Ghostty tab failed: %w. Grant Accessibility permission to wt-manager or your terminal", err)
	}
	return nil
}

func ghosttyTerminalArgs(wt core.Worktree) []string {
	return []string{"-na", "Ghostty.app", "--args", "--working-directory=" + wt.Path}
}

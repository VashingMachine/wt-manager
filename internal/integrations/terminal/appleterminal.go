package terminal

import (
	"context"
	"fmt"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func openAgentInAppleTerminal(ctx context.Context, wt core.Worktree, agent core.AgentTool) error {
	command := fmt.Sprintf("cd %s && %s", shell.Quote(wt.Path), agent.Command)
	return runAppleTerminalCommand(ctx, command)
}

func openTerminalInAppleTerminal(ctx context.Context, wt core.Worktree) error {
	return runAppleTerminalCommand(ctx, "cd "+shell.Quote(wt.Path))
}

func runAppleTerminalCommand(ctx context.Context, command string) error {
	_, err := shell.Run(ctx, "", "osascript", "-e", appleTerminalTabScript(command))
	if err == nil {
		return nil
	}
	_, fallbackErr := shell.Run(ctx, "", "osascript", "-e", appleTerminalWindowScript(command))
	if fallbackErr != nil {
		return fmt.Errorf("open Terminal tab failed: %w. Grant Accessibility/Automation permission to wt-manager or your terminal", err)
	}
	return nil
}

func appleTerminalTabScript(command string) string {
	commandLiteral := shell.AppleScriptString(command)
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
end tell`, shell.AppleScriptString(command))
}

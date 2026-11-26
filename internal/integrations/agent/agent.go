// Package agent runs coding agents (codex, claude, opencode, copilot or any
// command the user configured) for a one-off question. Opening an agent in a
// terminal tab is the job of the terminal integration.
package agent

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/shell"
)

// Runner starts agent commands.
type Runner struct{}

func New() *Runner {
	return &Runner{}
}

// RunWithPrompt runs the agent command in dir with prompt on its standard
// input and returns what it printed.
func (r *Runner) RunWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error) {
	return runAgentWithPrompt(ctx, dir, command, prompt)
}

func runAgentWithPrompt(ctx context.Context, dir string, command string, prompt string) (string, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", errors.New("agent command is empty")
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		return "", fmt.Errorf("agent %s not found", fields[0])
	}

	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = strings.NewReader(prompt)
	output, err := shell.CombinedOutput(ctx, cmd)
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, trimmed)
	}
	return string(output), nil
}

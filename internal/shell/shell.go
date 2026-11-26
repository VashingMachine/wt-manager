// Package shell runs external commands for the integrations and keeps the
// session log of what was run. It knows nothing about git, GitHub or any other
// tool: integrations build the command, shell runs and records it.
package shell

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// AppleScriptString quotes value as an AppleScript string literal.
func AppleScriptString(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n", "\r", "\\r")
	return "\"" + replacer.Replace(value) + "\""
}

// Quote quotes value as a single POSIX shell word.
func Quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// Run runs a command in dir (the current directory when empty) and returns its
// combined output. A failure carries the trimmed output in its message.
func Run(ctx context.Context, dir string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := CombinedOutput(ctx, cmd)
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, trimmed)
	}
	return string(output), nil
}

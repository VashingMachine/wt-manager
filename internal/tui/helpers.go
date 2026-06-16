package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// deleteArmTickCmd re-renders the armed-delete countdown roughly ten times a
// second; the Update handler re-issues it until the confirmation window ends.
func deleteArmTickCmd(path string, armedAt time.Time) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg {
		return deleteArmTick{Path: path, ArmedAt: armedAt}
	})
}

func stateLabel(wt Worktree) string {
	switch {
	case wt.IsMain:
		return "main"
	case wt.Prunable:
		return "prunable"
	case wt.Missing:
		return "missing"
	case wt.Detached:
		return "detached"
	case wt.Status.Conflicts > 0:
		return "conflict"
	case wt.Status.Dirty:
		return "dirty"
	default:
		return "clean"
	}
}

func truncateText(text string, max int) string {
	if max <= 0 {
		return ""
	}
	return truncateRunes(text, max)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	if max == 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

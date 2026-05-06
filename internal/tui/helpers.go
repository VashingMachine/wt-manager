package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func expireDeleteArmCmd(path string, armedAt time.Time) tea.Cmd {
	return tea.Tick(deleteConfirmWindow, func(time.Time) tea.Msg {
		return deleteArmExpired{Path: path, ArmedAt: armedAt}
	})
}

func shortPR(pr *PullRequest) string {
	if pr == nil {
		return "-"
	}
	return fmt.Sprintf("#%d %s", pr.Number, strings.ToLower(pr.State))
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

func changeSummary(status RepoStatus) string {
	return fmt.Sprintf("%d/%d/?%d", status.Staged, status.Unstaged, status.Untracked)
}

func trackingSummary(status RepoStatus) string {
	if status.Ahead == 0 && status.Behind == 0 {
		return "up to date"
	}
	return fmt.Sprintf("+%d/-%d", status.Ahead, status.Behind)
}

func truncateText(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return text[:max-1] + "…"
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

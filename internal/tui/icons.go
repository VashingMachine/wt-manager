package tui

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// iconSet holds every glyph the UI renders. All unicode glyphs must measure
// width 1 under go-runewidth so table padding stays aligned; set
// WT_MANAGER_ASCII=1 for terminals that render ambiguous-width glyphs wide.
type iconSet struct {
	WTMain     string
	WTClean    string
	WTDirty    string
	WTConflict string
	WTMissing  string
	WTPrunable string
	WTDetached string

	Ahead  string
	Behind string
	Synced string

	PROpen   string
	PRMerged string
	PRClosed string
	PRDraft  string

	CheckPass    string
	CheckFail    string
	CheckPending string
	CheckNone    string

	ReviewApproved string
	ReviewChanges  string
	ReviewRequired string

	OK            string
	Err           string
	Warn          string
	Section       string
	Pointer       string
	Dot           string
	SkeletonBlock string
}

var unicodeIcons = iconSet{
	WTMain: "⌂", WTClean: "✓", WTDirty: "●", WTConflict: "✗",
	WTMissing: "⚠", WTPrunable: "⊘", WTDetached: "◇",
	Ahead: "↑", Behind: "↓", Synced: "≡",
	PROpen: "●", PRMerged: "◆", PRClosed: "✗", PRDraft: "◌",
	CheckPass: "✓", CheckFail: "✗", CheckPending: "◐", CheckNone: "·",
	ReviewApproved: "✓", ReviewChanges: "±", ReviewRequired: "○",
	OK: "✓", Err: "✗", Warn: "⚠",
	Section: "▸", Pointer: "▶", Dot: "·", SkeletonBlock: "░",
}

var asciiIcons = iconSet{
	WTMain: "M", WTClean: "+", WTDirty: "*", WTConflict: "x",
	WTMissing: "!", WTPrunable: "~", WTDetached: "?",
	Ahead: "^", Behind: "v", Synced: "=",
	PROpen: "o", PRMerged: "m", PRClosed: "x", PRDraft: ".",
	CheckPass: "+", CheckFail: "x", CheckPending: "~", CheckNone: "-",
	ReviewApproved: "+", ReviewChanges: "~", ReviewRequired: "o",
	OK: "+", Err: "x", Warn: "!",
	Section: ">", Pointer: ">", Dot: "-", SkeletonBlock: "#",
}

var icons = pickIconSet(os.Getenv)

func pickIconSet(getenv func(string) string) iconSet {
	if strings.TrimSpace(getenv("WT_MANAGER_ASCII")) != "" {
		return asciiIcons
	}
	return unicodeIcons
}

func stateIcon(wt Worktree) string {
	switch stateLabel(wt) {
	case "main":
		return icons.WTMain
	case "prunable":
		return icons.WTPrunable
	case "missing":
		return icons.WTMissing
	case "detached":
		return icons.WTDetached
	case "conflict":
		return icons.WTConflict
	case "dirty":
		return icons.WTDirty
	default:
		return icons.WTClean
	}
}

func stateDisplay(wt Worktree) string {
	return stateIcon(wt) + " " + stateLabel(wt)
}

func shortDuration(duration time.Duration) string {
	if duration < time.Minute {
		return "now"
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Minutes()))
	}
	if duration < 48*time.Hour {
		return fmt.Sprintf("%dh", int(duration.Hours()))
	}
	return fmt.Sprintf("%dd", int(duration.Hours()/24))
}

func prStateIcon(state string, draft bool) string {
	if draft {
		return icons.PRDraft
	}
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "open":
		return icons.PROpen
	case "merged":
		return icons.PRMerged
	case "closed":
		return icons.PRClosed
	default:
		return icons.Dot
	}
}

func prStateDisplay(pr RemotePullRequest) string {
	return prStateIcon(pr.State, pr.IsDraft) + " " + prStateLabel(pr)
}

func shortPRDisplay(pr *PullRequest) string {
	if pr == nil {
		return "-"
	}
	return fmt.Sprintf("%s #%d %s", prStateIcon(pr.State, false), pr.Number, strings.ToLower(pr.State))
}

func checksDisplay(checks []StatusCheck) string {
	if len(checks) == 0 {
		return icons.CheckNone
	}
	failing, pending, passing := checksCounts(checks)
	if failing > 0 {
		return fmt.Sprintf("%s %d", icons.CheckFail, failing)
	}
	if pending > 0 {
		return fmt.Sprintf("%s %d", icons.CheckPending, pending)
	}
	return fmt.Sprintf("%s %d", icons.CheckPass, passing)
}

func checksDetailDisplay(checks []StatusCheck) string {
	if len(checks) == 0 {
		return icons.CheckNone
	}
	failing, pending, passing := checksCounts(checks)
	parts := make([]string, 0, 3)
	if failing > 0 {
		parts = append(parts, fmt.Sprintf("%s %d fail", icons.CheckFail, failing))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%s %d pending", icons.CheckPending, pending))
	}
	if passing > 0 {
		parts = append(parts, fmt.Sprintf("%s %d pass", icons.CheckPass, passing))
	}
	if len(parts) == 0 {
		return icons.CheckNone
	}
	return strings.Join(parts, ", ")
}

func readinessDisplay(pr RemotePullRequest) string {
	readiness := reviewReadiness(pr)
	switch readiness {
	case "draft":
		return icons.PRDraft + " " + readiness
	case "done":
		return icons.OK + " " + readiness
	case "blocked":
		return icons.Err + " " + readiness
	case "waiting":
		return icons.CheckPending + " " + readiness
	case "approved":
		return icons.ReviewApproved + " " + readiness
	case "changes":
		return icons.ReviewChanges + " " + readiness
	default:
		return icons.PROpen + " " + readiness
	}
}

func reviewDisplay(decision string) string {
	short := shortReviewDecision(decision)
	switch short {
	case "approved":
		return icons.ReviewApproved + " " + short
	case "changes":
		return icons.ReviewChanges + " " + short
	case "needed":
		return icons.ReviewRequired + " " + short
	default:
		return short
	}
}

func trackingDisplay(status RepoStatus) string {
	if status.Ahead == 0 && status.Behind == 0 {
		return icons.Synced + " up to date"
	}
	parts := make([]string, 0, 2)
	if status.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", icons.Ahead, status.Ahead))
	}
	if status.Behind > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", icons.Behind, status.Behind))
	}
	return strings.Join(parts, " ")
}

func changeDisplay(status RepoStatus) string {
	if status.Staged == 0 && status.Unstaged == 0 && status.Untracked == 0 && status.Conflicts == 0 {
		return icons.Dot
	}
	parts := make([]string, 0, 4)
	if status.Conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%s%d", icons.WTConflict, status.Conflicts))
	}
	if status.Staged > 0 {
		parts = append(parts, fmt.Sprintf("+%d", status.Staged))
	}
	if status.Unstaged > 0 {
		parts = append(parts, fmt.Sprintf("~%d", status.Unstaged))
	}
	if status.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("?%d", status.Untracked))
	}
	return strings.Join(parts, " ")
}

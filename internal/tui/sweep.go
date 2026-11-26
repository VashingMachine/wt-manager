package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/fspath"
	tea "github.com/charmbracelet/bubbletea"
)

// Sweep (X) clears out worktrees that are finished in practice: nothing
// uncommitted in them and no commit, checkout, pull or rebase for longer than
// sweep.olderThanDays in the config. It scans every repository of the
// project, shows what it found with everything ticked, and removes what stays
// ticked. Branches are kept, so a swept worktree can be created again.

const sweepDialogRows = 12

type sweepItem struct {
	Candidate core.SweepCandidate
	Selected  bool
}

type sweepDialog struct {
	Items  []sweepItem
	Cursor int
	Days   int
}

func (d sweepDialog) selected() []Worktree {
	var worktrees []Worktree
	for _, item := range d.Items {
		if item.Selected {
			worktrees = append(worktrees, item.Candidate.Worktree)
		}
	}
	return worktrees
}

type sweepScanResult struct {
	Project    string
	Candidates []core.SweepCandidate
	Days       int
	Err        error
}

func (m model) sweepDays() int {
	if days := m.cfg.App.Sweep.OlderThanDays; days > 0 {
		return days
	}
	return core.DefaultSweepOlderThanDays
}

// startSweepScan handles X: it looks for sweepable worktrees in the background.
func (m model) startSweepScan() (tea.Model, tea.Cmd) {
	m.clearDeleteArm()
	if !m.isWorktreeMode() {
		m.statusMessage = "Sweep is available for local worktrees"
		m.statusError = true
		return m, nil
	}
	days := m.sweepDays()
	looking := fmt.Sprintf("Looking for clean worktrees untouched for %d+ days...", days)
	return m, tea.Batch(sweepScanCmd(m.services, m.cfg, days), m.startLoading(loadingMutation, looking))
}

// sweepScanCmd scans every repository of the project at once. A repository
// that cannot be read fails the scan: sweeping half a project silently would
// look like the rest had nothing to remove.
func sweepScanCmd(services core.WorktreeService, cfg Config, days int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
		defer cancel()
		olderThan := time.Duration(days) * 24 * time.Hour
		scopes := []Config{cfg}
		if repositories := cfg.Repositories(); len(repositories) > 1 {
			scopes = scopes[:0]
			for _, repository := range repositories {
				scopes = append(scopes, cfg.ForRepository(repository.Name))
			}
		}
		lists := make([][]core.SweepCandidate, len(scopes))
		errs := make([]error, len(scopes))
		var wg sync.WaitGroup
		for i, scoped := range scopes {
			wg.Go(func() { lists[i], errs[i] = services.SweepCandidates(ctx, scoped, olderThan) })
		}
		wg.Wait()
		result := sweepScanResult{Project: projectKey(cfg), Days: days}
		for i := range scopes {
			if errs[i] != nil {
				result.Err = errs[i]
				return result
			}
			result.Candidates = append(result.Candidates, lists[i]...)
		}
		sort.SliceStable(result.Candidates, func(i, j int) bool {
			return result.Candidates[i].LastActivity.Before(result.Candidates[j].LastActivity)
		})
		return result
	}
}

func (m model) handleSweepScan(msg sweepScanResult) (tea.Model, tea.Cmd) {
	if msg.Project != projectKey(m.cfg) {
		return m, nil
	}
	m.loading = false
	if msg.Err != nil {
		m.statusMessage = fmt.Sprintf("Sweep scan failed: %v", msg.Err)
		m.statusError = true
		return m, nil
	}
	dialog := sweepDialog{Days: msg.Days}
	for _, candidate := range msg.Candidates {
		path := candidate.Worktree.Path
		if _, deleting := m.pendingDeletes[path]; deleting || m.isPendingCreation(path) {
			continue
		}
		candidate.Worktree.PR = m.pullRequestFor(candidate.Worktree)
		dialog.Items = append(dialog.Items, sweepItem{Candidate: candidate, Selected: true})
	}
	if len(dialog.Items) == 0 {
		return m, m.setTransientStatus(fmt.Sprintf("Nothing to sweep: no clean worktree has been untouched for %d days", msg.Days))
	}
	m.sweeping = true
	m.sweep = dialog
	m.statusMessage = fmt.Sprintf("%d worktrees can be swept", len(dialog.Items))
	if len(dialog.Items) == 1 {
		m.statusMessage = "1 worktree can be swept"
	}
	m.statusError = false
	return m, nil
}

// pullRequestFor labels a worktree with the PR of its branch, as the table does.
func (m model) pullRequestFor(wt Worktree) *PullRequest {
	prs := m.prIndexes[m.worktreeRepository(wt)]
	var found *PullRequest
	for i := range prs {
		if prs[i].HeadRefName == wt.Branch {
			found = &prs[i]
		}
	}
	return found
}

func (m model) updateSweepDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dialog := &m.sweep
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.sweeping = false
		return m, m.setTransientStatus("Sweep cancelled")
	case "up", "k":
		dialog.Cursor = max(0, dialog.Cursor-1)
		return m, nil
	case "down", "j":
		dialog.Cursor = min(len(dialog.Items)-1, dialog.Cursor+1)
		return m, nil
	case " ", "x":
		if dialog.Cursor >= 0 && dialog.Cursor < len(dialog.Items) {
			// The dialog is copied with the model; never edit a shared slice.
			items := append([]sweepItem(nil), dialog.Items...)
			items[dialog.Cursor].Selected = !items[dialog.Cursor].Selected
			dialog.Items = items
		}
		return m, nil
	case "a":
		all := len(dialog.selected()) == len(dialog.Items)
		items := append([]sweepItem(nil), dialog.Items...)
		for i := range items {
			items[i].Selected = !all
		}
		dialog.Items = items
		return m, nil
	case "enter":
		return m.sweepSelected()
	}
	return m, nil
}

// sweepSelected removes the ticked worktrees through the same optimistic,
// queued deletion as d d. Each one was clean when scanned, so git removes it
// without force and refuses if something was changed in the meantime.
func (m model) sweepSelected() (tea.Model, tea.Cmd) {
	worktrees := m.sweep.selected()
	m.sweeping = false
	if len(worktrees) == 0 {
		return m, m.setTransientStatus("Nothing selected to sweep")
	}
	m.rememberSelection()
	if m.pendingDeletes == nil {
		m.pendingDeletes = make(map[string]pendingDelete)
	}
	cmds := make([]tea.Cmd, 0, len(worktrees)+1)
	for _, wt := range worktrees {
		m.pendingDeletes[wt.Path] = pendingDelete{Worktree: wt, Index: m.worktreeIndex(wt.Path)}
		wtCfg := m.worktreeCfg(wt)
		cmds = append(cmds, m.ops.enqueue(gitQueueKey(wtCfg), removeWorktreeCmd(m.services, wtCfg, wt)))
	}
	for _, wt := range worktrees {
		m.allWorktrees = withoutWorktree(m.allWorktrees, wt.Path)
	}
	m.applyFilter()
	sweeping := fmt.Sprintf("Sweeping %d worktrees...", len(worktrees))
	if len(worktrees) == 1 {
		sweeping = fmt.Sprintf("Sweeping %s...", worktrees[0].Name)
	}
	return m, tea.Batch(append(cmds, m.startLoading(loadingMutation, sweeping))...)
}

func renderSweepModal(m model) string {
	dialog := m.sweep
	body := []string{
		fmt.Sprintf("Clean worktrees with no commit, checkout or pull for %d+ days.", dialog.Days),
		mutedStyle.Render("Their branches stay, so any of them can be created again."),
		"",
	}
	nameWidth := 12
	for _, item := range dialog.Items {
		nameWidth = max(nameWidth, len([]rune(item.Candidate.Worktree.Name)))
	}
	nameWidth = min(nameWidth, 44)
	start, end := visibleRowRange(dialog.Cursor, len(dialog.Items), sweepDialogRows)
	if start > 0 {
		body = append(body, mutedStyle.Render(fmt.Sprintf("  %d more above", start)))
	}
	for idx := start; idx < end; idx++ {
		item := dialog.Items[idx]
		wt := item.Candidate.Worktree
		box := "[ ]"
		if item.Selected {
			box = "[x]"
		}
		parts := []string{box}
		if m.cfg.MultiRepository() {
			parts = append(parts, padCell(truncateRunes(m.worktreeRepository(wt), 10), 10))
		}
		parts = append(parts,
			padCell(truncateRunes(wt.Name, nameWidth), nameWidth),
			fmt.Sprintf("%4s", shortDuration(time.Since(item.Candidate.LastActivity))),
			shortPRDisplay(wt.PR),
		)
		body = append(body, selectorLine(idx == dialog.Cursor, strings.Join(parts, "  "), false))
	}
	if end < len(dialog.Items) {
		body = append(body, mutedStyle.Render(fmt.Sprintf("  %d more below", len(dialog.Items)-end)))
	}
	body = append(body, "", fmt.Sprintf("%d of %d selected", len(dialog.selected()), len(dialog.Items)))
	body = append(body, mutedStyle.Render(fmt.Sprintf("Change the age with sweep.olderThanDays in %s", fspath.Compact(m.cfg.ConfigPath, m.cfg.HomeDir))))
	hints := []keyHint{{"j/k", "move"}, {"space", "toggle"}, {"a", "all/none"}, {"enter", "remove"}, {"esc", "cancel"}}
	return renderWideModal("Sweep Worktrees", body, hints, max(72, min(104, m.width-8)))
}

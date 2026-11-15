package tui

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/charmbracelet/x/ansi"
)

// sweepRepoSpy answers sweep scans per repository and records removals with
// the repository they ran in.
type sweepRepoSpy struct {
	*multiRepoSpy
	candidates map[string][]core.SweepCandidate
	lock       sync.Mutex
	removed    []string
}

func (s *sweepRepoSpy) SweepCandidates(_ context.Context, cfg Config, _ time.Duration) ([]core.SweepCandidate, error) {
	return s.candidates[cfg.RepositoryName()], nil
}

func (s *sweepRepoSpy) RemoveWorktree(_ context.Context, cfg Config, wt Worktree) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.removed = append(s.removed, cfg.RepositoryName()+":"+wt.Path)
	return nil
}

func sweepCandidate(name string, days int) core.SweepCandidate {
	return core.SweepCandidate{
		Worktree:     Worktree{Name: name, Path: "/wt/" + name, Branch: name, Managed: true},
		LastActivity: time.Now().Add(-time.Duration(days) * 24 * time.Hour),
	}
}

// Assures that X scans with the configured age and lists every sweepable worktree ticked, with its age and pull request.
func TestSweepScanListsCandidatesTicked(t *testing.T) {
	spy := &stackSpy{candidates: []core.SweepCandidate{sweepCandidate("child", 40), sweepCandidate("other", 9)}}
	m := stackTestModel(spy)
	m, _ = feed(m, prIndexResult{Repository: "app", RepoSlug: "owner/app", PullRequests: []PullRequest{{HeadRefName: "child", Number: 31, State: "MERGED"}}})

	m, msgs := pressKeys(t, m, "X")
	if want := []time.Duration{5 * 24 * time.Hour}; !reflect.DeepEqual(spy.sweepAges, want) {
		t.Fatalf("scan ages = %v, want the configured %v", spy.sweepAges, want)
	}
	m, _ = feed(m, msgs...)
	if !m.sweeping || len(m.sweep.Items) != 2 || len(m.sweep.selected()) != 2 {
		t.Fatalf("sweep dialog = open:%v items:%d selected:%d", m.sweeping, len(m.sweep.Items), len(m.sweep.selected()))
	}
	modal := ansi.Strip(renderSweepModal(m))
	for _, want := range []string{"Sweep Worktrees", "5+ days", "[x]", "child", "40d", "#31", "other", "9d", "2 of 2 selected", "sweep.olderThanDays", "~/.wt-manager.json"} {
		if !strings.Contains(modal, want) {
			t.Fatalf("sweep dialog is missing %q:\n%s", want, modal)
		}
	}
	if len(spy.removed) != 0 {
		t.Fatalf("scanning removed %v", spy.removed)
	}
}

// Assures that sweep removes exactly the worktrees left ticked, taking them out of the list at once.
func TestSweepRemovesOnlyTickedWorktrees(t *testing.T) {
	spy := &stackSpy{candidates: []core.SweepCandidate{sweepCandidate("child", 40), sweepCandidate("other", 9)}}
	m := stackTestModel(spy)
	m, msgs := pressKeys(t, m, "X")
	m, _ = feed(m, msgs...)

	// Untick "other", the second row.
	m, msgs = pressKeys(t, m, "j", " ", "enter")
	if m.sweeping {
		t.Fatal("the dialog stayed open after enter")
	}
	if want := []string{"child"}; !reflect.DeepEqual(spy.removed, want) {
		t.Fatalf("removed = %v, want %v", spy.removed, want)
	}
	if got, want := worktreeNames(m.allWorktrees), []string{"app", "base", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows after sweeping = %v, want %v", got, want)
	}
	if results := messagesOf[deleteResult](msgs); len(results) != 1 || results[0].Err != nil || results[0].Name != "child" {
		t.Fatalf("deletion results = %#v", results)
	}
}

// Assures that a toggles every candidate, that enter with nothing ticked removes nothing, and that esc cancels.
func TestSweepToggleAllAndCancel(t *testing.T) {
	spy := &stackSpy{candidates: []core.SweepCandidate{sweepCandidate("child", 40), sweepCandidate("other", 9)}}
	m := stackTestModel(spy)
	m, msgs := pressKeys(t, m, "X")
	open, _ := feed(m, msgs...)

	none, _ := pressKeys(t, open, "a")
	if len(none.sweep.selected()) != 0 || len(open.sweep.selected()) != 2 {
		t.Fatalf("a: selected=%d, earlier copy selected=%d", len(none.sweep.selected()), len(open.sweep.selected()))
	}
	all, _ := pressKeys(t, none, "a")
	if len(all.sweep.selected()) != 2 {
		t.Fatalf("a again: selected=%d", len(all.sweep.selected()))
	}
	done, _ := pressKeys(t, none, "enter")
	cancelled, _ := pressKeys(t, open, "esc")
	if done.sweeping || cancelled.sweeping || len(spy.removed) != 0 {
		t.Fatalf("nothing should be removed: removed=%v", spy.removed)
	}
	if got := worktreeNames(cancelled.allWorktrees); len(got) != 4 {
		t.Fatalf("rows after cancelling = %v", got)
	}
}

// Assures that finding nothing, or failing to scan, is reported instead of opening an empty dialog.
func TestSweepWithNothingToRemoveOrFailedScan(t *testing.T) {
	spy := &stackSpy{}
	m := stackTestModel(spy)
	m, msgs := pressKeys(t, m, "X")
	m, _ = feed(m, msgs...)
	if m.sweeping || m.statusError || !strings.Contains(m.statusMessage, "Nothing to sweep") || !strings.Contains(m.statusMessage, "5 days") {
		t.Fatalf("empty scan: sweeping=%v status=%q", m.sweeping, m.statusMessage)
	}

	spy.sweepErr = errors.New("git is unhappy")
	m, msgs = pressKeys(t, m, "X")
	m, _ = feed(m, msgs...)
	if m.sweeping || !m.statusError || !strings.Contains(m.statusMessage, "git is unhappy") {
		t.Fatalf("failed scan: sweeping=%v status=%q", m.sweeping, m.statusMessage)
	}
}

// Assures that sweeping a project with several repositories scans each of them and removes worktrees in their own repository.
func TestSweepCoversEveryRepository(t *testing.T) {
	spy := &sweepRepoSpy{multiRepoSpy: newMultiRepoSpy(), candidates: map[string][]core.SweepCandidate{
		"api": {{Worktree: Worktree{Name: "feature", Path: "/wt/api/feature", Branch: "feature", Managed: true, Repository: "api"}, LastActivity: time.Now().Add(-20 * 24 * time.Hour)}},
		"web": {{Worktree: Worktree{Name: "feature", Path: "/wt/web/feature", Branch: "feature", Managed: true, Repository: "web"}, LastActivity: time.Now().Add(-30 * 24 * time.Hour)}},
	}}
	m := multiRepoTestModel(spy.multiRepoSpy)
	m.services = spy

	m, msgs := pressKeys(t, m, "X")
	m, _ = feed(m, msgs...)
	if !m.sweeping || len(m.sweep.Items) != 2 || m.sweep.Items[0].Candidate.Worktree.Repository != "web" {
		t.Fatalf("sweep dialog = open:%v items:%#v; want both repositories, oldest first", m.sweeping, m.sweep.Items)
	}
	if modal := ansi.Strip(renderSweepModal(m)); !strings.Contains(modal, "api") || !strings.Contains(modal, "web") {
		t.Fatalf("sweep dialog does not name the repositories:\n%s", modal)
	}
	pressKeys(t, m, "enter")
	sort.Strings(spy.removed)
	if want := []string{"api:/wt/api/feature", "web:/wt/web/feature"}; !reflect.DeepEqual(spy.removed, want) {
		t.Fatalf("removed = %v, want %v", spy.removed, want)
	}
}

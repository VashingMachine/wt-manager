package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

func noFetch(context.Context, string, string, bool) bool { return false }

// stackFixture is a repository with a worktree "parent" holding one commit
// and a worktree "child" stacked on it holding one more.
type stackFixture struct {
	cfg    core.Config
	repo   string
	parent core.Worktree
	child  core.Worktree
}

func newStackFixture(t *testing.T) stackFixture {
	t.Helper()
	ctx := context.Background()
	cfg := testkit.GitRepo(t)
	parentPath := core.CreateWorktreePath(cfg, "parent")
	if _, err := createWorktreeWith(ctx, cfg, "parent", parentPath, noFetch); err != nil {
		t.Fatalf("create parent worktree: %v", err)
	}
	testkit.CommitFile(t, parentPath, "parent.txt", "parent\n", "parent work")
	childPath := core.CreateWorktreePath(cfg, "child")
	if err := createStackedWorktree(ctx, cfg, "child", childPath, "parent"); err != nil {
		t.Fatalf("createStackedWorktree() error = %v", err)
	}
	testkit.CommitFile(t, childPath, "child.txt", "child\n", "child work")
	return stackFixture{
		cfg:    cfg,
		repo:   cfg.ActiveProfile.RepositoryPath,
		parent: core.Worktree{Name: "parent", Branch: "parent", Path: parentPath},
		child:  core.Worktree{Name: "child", Branch: "child", Path: childPath},
	}
}

func (f stackFixture) restack(t *testing.T, options core.RestackOptions) (core.RestackResult, error) {
	t.Helper()
	return restackWorktree(context.Background(), f.cfg, f.child, options, fetchRemote, nil)
}

func (f stackFixture) link(t *testing.T, branch string) (core.StackLink, bool) {
	t.Helper()
	link, ok := readStackLinks(context.Background(), f.repo)[branch]
	return link, ok
}

// squashMergeParent lands the parent on the remote's default branch as one
// new commit, the way a squash merge of its pull request does.
func (f stackFixture) squashMergeParent(t *testing.T) {
	t.Helper()
	testkit.RunGit(t, f.repo, "merge", "--squash", "parent")
	testkit.RunGit(t, f.repo, "commit", "-m", "parent work (#1)")
	testkit.RunGit(t, f.repo, "push", "origin", "main")
}

// Assures that a stacked worktree starts from its parent's tip, unpushed commits included, and records the parent and the commit it started from.
func TestCreateStackedWorktreeStartsFromParentAndRecordsLink(t *testing.T) {
	f := newStackFixture(t)
	parentTip := strings.TrimSpace(testkit.RunGit(t, f.parent.Path, "rev-parse", "HEAD"))

	testkit.AssertGitBranch(t, f.child.Path, "child")
	if _, err := os.Stat(filepath.Join(f.child.Path, "parent.txt")); err != nil {
		t.Fatalf("child does not contain the parent's unpushed commit: %v", err)
	}
	link, ok := f.link(t, "child")
	if !ok || link.Parent != "parent" || link.Base != parentTip {
		t.Fatalf("stack link = %#v (ok=%v), want parent at %s", link, ok, parentTip)
	}
	if output, err := shell.Run(context.Background(), f.child.Path, "git", "-C", f.child.Path, "rev-parse", "--abbrev-ref", "@{u}"); err == nil {
		t.Fatalf("stacked branch tracks %q; it should have no upstream until it is pushed", strings.TrimSpace(output))
	}
}

// Assures that stacking needs a new branch name and an existing parent, so a typo never leaves an old branch off the stack.
func TestCreateStackedWorktreeRejectsExistingBranchAndMissingParent(t *testing.T) {
	f := newStackFixture(t)
	ctx := context.Background()

	err := createStackedWorktree(ctx, f.cfg, "parent", core.CreateWorktreePath(f.cfg, "again"), "child")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing branch error = %v", err)
	}
	err = createStackedWorktree(ctx, f.cfg, "other", core.CreateWorktreePath(f.cfg, "other"), "no-such-branch")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing parent error = %v", err)
	}
	if _, err := os.Stat(core.CreateWorktreePath(f.cfg, "other")); err == nil {
		t.Fatal("a worktree folder was created although the parent is missing")
	}
}

// Assures that a stacked worktree's status says how many parent commits it lacks, and that the parent branch is gone once it is deleted.
func TestWorktreeStatusReportsStackParentState(t *testing.T) {
	f := newStackFixture(t)
	ctx := context.Background()
	child := f.child
	child.Stack = core.StackLink{Parent: "parent"}

	readWorktreeStatus(ctx, &child)
	if child.Status.ParentBehind != 0 || child.Status.ParentGone {
		t.Fatalf("fresh stack status = %#v", child.Status)
	}

	testkit.CommitFile(t, f.parent.Path, "more.txt", "more\n", "more parent work")
	readWorktreeStatus(ctx, &child)
	if child.Status.ParentBehind != 1 || child.Status.ParentGone {
		t.Fatalf("status after the parent moved = behind:%d gone:%v", child.Status.ParentBehind, child.Status.ParentGone)
	}

	testkit.RunGit(t, f.repo, "worktree", "remove", "--force", f.parent.Path)
	testkit.RunGit(t, f.repo, "branch", "-D", "parent")
	readWorktreeStatus(ctx, &child)
	if !child.Status.ParentGone {
		t.Fatal("status does not report the deleted parent branch")
	}
}

// Assures that restacking onto a parent with new commits puts the branch's own commits on top of them and moves the recorded base.
func TestRestackOntoMovedParent(t *testing.T) {
	f := newStackFixture(t)
	parentTip := testkit.CommitFile(t, f.parent.Path, "more.txt", "more\n", "more parent work")

	result, err := f.restack(t, core.RestackOptions{})
	if err != nil {
		t.Fatalf("restackWorktree() error = %v", err)
	}
	if !result.Rebased || result.Commits != 1 || result.Onto != "parent" || result.Parent != "parent" {
		t.Fatalf("result = %#v", result)
	}
	if got := testkit.GitLines(t, f.child.Path, "log", "--format=%s", "parent..HEAD"); !reflect.DeepEqual(got, []string{"child work"}) {
		t.Fatalf("commits on top of parent = %v, want only the child's", got)
	}
	if link, _ := f.link(t, "child"); link.Parent != "parent" || link.Base != parentTip {
		t.Fatalf("stack link after restack = %#v, want base %s", link, parentTip)
	}
}

// Assures that after the parent's history was rewritten, restacking replays only the branch's own commits instead of the parent's old ones.
func TestRestackAfterParentWasAmended(t *testing.T) {
	f := newStackFixture(t)
	testkit.WriteRepoFile(t, f.parent.Path, "parent.txt", "parent, reworded\n")
	testkit.RunGit(t, f.parent.Path, "commit", "-am", "parent work, amended", "--amend")

	result, err := f.restack(t, core.RestackOptions{})
	if err != nil {
		t.Fatalf("restackWorktree() error = %v", err)
	}
	if !result.Rebased || result.Commits != 1 {
		t.Fatalf("result = %#v, want exactly the child's commit replayed", result)
	}
	if got := testkit.GitLines(t, f.child.Path, "log", "--format=%s", "main..HEAD"); !reflect.DeepEqual(got, []string{"child work", "parent work, amended"}) {
		t.Fatalf("history = %v, want the amended parent commit followed by the child's", got)
	}
}

// Assures that once the parent is squash-merged, restacking moves the branch's own commits onto the default branch and takes it out of the stack.
func TestRestackAfterParentWasSquashMerged(t *testing.T) {
	f := newStackFixture(t)
	f.squashMergeParent(t)

	result, err := f.restack(t, core.RestackOptions{MergedBranches: []string{"parent"}})
	if err != nil {
		t.Fatalf("restackWorktree() error = %v", err)
	}
	if !result.Rebased || result.Onto != "origin/main" || result.Parent != "" {
		t.Fatalf("result = %#v, want a rebase onto origin/main out of the stack", result)
	}
	if got := testkit.GitLines(t, f.child.Path, "log", "--format=%s", "origin/main..HEAD"); !reflect.DeepEqual(got, []string{"child work"}) {
		t.Fatalf("commits on top of origin/main = %v, want only the child's", got)
	}
	for _, name := range []string{"parent.txt", "child.txt"} {
		if _, err := os.Stat(filepath.Join(f.child.Path, name)); err != nil {
			t.Fatalf("%s is missing after the restack: %v", name, err)
		}
	}
	if link, ok := f.link(t, "child"); ok {
		t.Fatalf("stack link survived leaving the stack: %#v", link)
	}
}

// Assures that a merged parent is skipped in favour of the branch it was itself stacked on, when that one is still alive.
func TestRestackSkipsMergedParentForSurvivingGrandparent(t *testing.T) {
	f := newStackFixture(t)
	ctx := context.Background()
	grandchild := core.Worktree{Name: "grandchild", Branch: "grandchild", Path: core.CreateWorktreePath(f.cfg, "grandchild")}
	if err := createStackedWorktree(ctx, f.cfg, "grandchild", grandchild.Path, "child"); err != nil {
		t.Fatal(err)
	}
	testkit.CommitFile(t, grandchild.Path, "grandchild.txt", "grandchild\n", "grandchild work")

	// "child" is merged as far as GitHub knows; "parent" is not.
	var asked []string
	lookup := func(_ context.Context, branch string) bool {
		asked = append(asked, branch)
		return branch == "child"
	}
	result, err := restackWorktree(ctx, f.cfg, grandchild, core.RestackOptions{}, noFetch, lookup)
	if err != nil {
		t.Fatalf("restackWorktree() error = %v", err)
	}
	if result.Onto != "parent" || result.Parent != "parent" {
		t.Fatalf("result = %#v, want the grandparent as the new parent", result)
	}
	if !reflect.DeepEqual(asked, []string{"child", "parent"}) {
		t.Fatalf("GitHub was asked about %v", asked)
	}
	if link, _ := f.link(t, "grandchild"); link.Parent != "parent" {
		t.Fatalf("stack link = %#v, want parent", link)
	}
}

// Assures that restacking onto the default branch on request works without knowing whether the parent was merged.
func TestRestackOntoDefaultOnRequest(t *testing.T) {
	f := newStackFixture(t)
	f.squashMergeParent(t)

	result, err := f.restack(t, core.RestackOptions{OntoDefault: true})
	if err != nil || result.Onto != "origin/main" || result.Parent != "" {
		t.Fatalf("restackWorktree() = %#v, %v", result, err)
	}
	if got := testkit.GitLines(t, f.child.Path, "log", "--format=%s", "origin/main..HEAD"); !reflect.DeepEqual(got, []string{"child work"}) {
		t.Fatalf("commits on top of origin/main = %v", got)
	}
}

// Assures that a branch already on top of its parent is left alone and only its recorded base is refreshed.
func TestRestackUpToDateOnlyRefreshesLink(t *testing.T) {
	f := newStackFixture(t)
	before := strings.TrimSpace(testkit.RunGit(t, f.child.Path, "rev-parse", "HEAD"))

	result, err := f.restack(t, core.RestackOptions{})
	if err != nil || result.Rebased {
		t.Fatalf("restackWorktree() = %#v, %v; want no rebase", result, err)
	}
	if after := strings.TrimSpace(testkit.RunGit(t, f.child.Path, "rev-parse", "HEAD")); after != before {
		t.Fatalf("HEAD moved from %s to %s", before, after)
	}
}

// Assures that restacking refuses a worktree with uncommitted changes instead of carrying them through a rebase.
func TestRestackRefusesUncommittedChanges(t *testing.T) {
	f := newStackFixture(t)
	testkit.CommitFile(t, f.parent.Path, "more.txt", "more\n", "more parent work")
	testkit.WriteRepoFile(t, f.child.Path, "wip.txt", "wip\n")

	if _, err := f.restack(t, core.RestackOptions{}); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("restackWorktree() error = %v", err)
	}
}

// Assures that a restack stopped by conflicts says how to continue and keeps the stack link, so restacking again works after the conflicts are resolved.
func TestRestackConflictKeepsLinkAndExplains(t *testing.T) {
	f := newStackFixture(t)
	before, _ := f.link(t, "child")
	testkit.CommitFile(t, f.parent.Path, "child.txt", "the parent wrote this first\n", "conflicting parent work")

	_, err := f.restack(t, core.RestackOptions{})
	if err == nil || !strings.Contains(err.Error(), "git rebase --continue") {
		t.Fatalf("restackWorktree() error = %v", err)
	}
	if after, _ := f.link(t, "child"); after != before {
		t.Fatalf("stack link changed by a failed restack: %#v -> %#v", before, after)
	}
	if _, err := f.restack(t, core.RestackOptions{}); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("restack during a rebase error = %v", err)
	}

	testkit.RunGit(t, f.child.Path, "rebase", "--abort")
}

// Assures that an existing worktree can be put on a stack afterwards, that loops are refused, and that it can be taken out again.
func TestSetStackParentRecordsForkPointAndRejectsLoops(t *testing.T) {
	f := newStackFixture(t)
	ctx := context.Background()
	forkPoint := strings.TrimSpace(testkit.RunGit(t, f.repo, "rev-parse", "main"))
	other := core.Worktree{Name: "other", Branch: "other", Path: core.CreateWorktreePath(f.cfg, "other")}
	if _, err := createWorktreeWith(ctx, f.cfg, "other", other.Path, noFetch); err != nil {
		t.Fatal(err)
	}

	if err := setStackParent(ctx, f.cfg, other, "parent"); err != nil {
		t.Fatalf("setStackParent() error = %v", err)
	}
	if link, _ := f.link(t, "other"); link.Parent != "parent" || link.Base != forkPoint {
		t.Fatalf("stack link = %#v, want parent with base %s", link, forkPoint)
	}

	if err := setStackParent(ctx, f.cfg, f.parent, "child"); err == nil || !strings.Contains(err.Error(), "already stacked") {
		t.Fatalf("loop error = %v", err)
	}
	if err := setStackParent(ctx, f.cfg, f.child, "child"); err == nil {
		t.Fatal("a branch was stacked on itself")
	}

	if err := setStackParent(ctx, f.cfg, other, ""); err != nil {
		t.Fatalf("clearing the parent: %v", err)
	}
	if link, ok := f.link(t, "other"); ok {
		t.Fatalf("stack link survived clearing: %#v", link)
	}
}

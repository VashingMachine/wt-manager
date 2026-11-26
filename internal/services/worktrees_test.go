package services

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

// Assures that listing worktrees shows a stacked one right below its parent, one level deeper, and carries its stack link.
func TestDiscoverWorktreesListsStackedWorktreeBelowParent(t *testing.T) {
	svc, cfg, ctx := NewService(), testkit.GitRepo(t), context.Background()
	// "aaa" sorts before "parent"; "child" would too without stack ordering.
	for _, branch := range []string{"parent", "aaa"} {
		if _, err := svc.CreateWorktree(ctx, cfg, branch, core.CreateWorktreePath(cfg, branch)); err != nil {
			t.Fatal(err)
		}
	}
	for _, stacked := range [][2]string{{"child", "parent"}, {"bbb-grandchild", "child"}} {
		if err := svc.CreateStackedWorktree(ctx, cfg, stacked[0], core.CreateWorktreePath(cfg, stacked[0]), stacked[1]); err != nil {
			t.Fatal(err)
		}
	}

	worktrees, err := svc.DiscoverWorktrees(ctx, cfg, false)
	if err != nil {
		t.Fatalf("DiscoverWorktrees() error = %v", err)
	}
	var got []string
	for _, wt := range worktrees {
		got = append(got, strings.Repeat(">", wt.StackDepth)+wt.Branch)
	}
	want := []string{"main", "aaa", "parent", ">child", ">>bbb-grandchild"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if worktrees[3].Stack.Parent != "parent" || worktrees[2].Stacked() {
		t.Fatalf("stack links = child:%#v parent:%#v", worktrees[3].Stack, worktrees[2].Stack)
	}
}

// Assures that discovery reports unknown statuses as pending, reuses cached ones, and that the cached list paints without git.
func TestDiscoverWorktreesOverlaysCachedStatus(t *testing.T) {
	cfg := testkit.GitRepo(t)
	svc, ctx := NewService(), context.Background()
	testkit.WriteRepoFile(t, cfg.ActiveProfile.RepositoryPath, "dirty.txt", "x\n")

	discovered, err := svc.DiscoverWorktrees(ctx, cfg, true)
	if err != nil || len(discovered) != 1 || !discovered[0].StatusPending {
		t.Fatalf("first DiscoverWorktrees() = %#v, %v; want one pending worktree", discovered, err)
	}
	loaded, err := svc.LoadWorktreeStatus(ctx, cfg, discovered[0])
	if err != nil || loaded.StatusPending || !loaded.Status.Dirty || loaded.Commit.Subject != "initial" {
		t.Fatalf("LoadWorktreeStatus() = %#v, %v", loaded, err)
	}

	again, err := svc.DiscoverWorktrees(ctx, cfg, true)
	if err != nil || again[0].StatusPending || !again[0].Status.Dirty || again[0].Commit.Relative == "" {
		t.Fatalf("second DiscoverWorktrees() = %#v, %v; want cached dirty status", again, err)
	}

	// A relative commit time read long ago is no longer true; keep the commit but drop it.
	_ = svc.cache.PutAt(bucketWorktreeStatus, worktreeStatusKey(cfg, loaded.Path), cachedWorktreeStatus{Status: loaded.Status, Commit: loaded.Commit}, time.Now().Add(-time.Hour))
	if aged, _ := svc.DiscoverWorktrees(ctx, cfg, true); aged[0].Commit.Relative != "" || aged[0].Commit.Subject != "initial" {
		t.Fatalf("aged cached commit = %#v, want subject without stale relative time", aged[0].Commit)
	}

	if err := os.RemoveAll(cfg.ActiveProfile.RepositoryPath); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}
	peeked, ok := svc.PeekWorktrees(cfg, true)
	if !ok || len(peeked) != 1 || !peeked[0].Status.Dirty || peeked[0].Name != "test" {
		t.Fatalf("PeekWorktrees() without git = %#v ok=%v", peeked, ok)
	}
}

// Assures that discovered worktrees record their repository and that main checkouts are named after their repository in multi-repository projects.
func TestDiscoverWorktreesLabelsRepository(t *testing.T) {
	cfg := multiRepositoryConfig(t)
	svc := NewService()
	for _, name := range []string{"app", "web"} {
		worktrees, err := svc.DiscoverWorktrees(context.Background(), cfg.ForRepository(name), false)
		if err != nil {
			t.Fatalf("DiscoverWorktrees(%s) error = %v", name, err)
		}
		if len(worktrees) != 1 || worktrees[0].Repository != name || worktrees[0].Name != name || !worktrees[0].IsMain {
			t.Fatalf("%s worktrees = %#v, want its main checkout labelled %s", name, worktrees, name)
		}
		cached, ok := svc.PeekWorktrees(cfg.ForRepository(name), false)
		if !ok || cached[0].Repository != name {
			t.Fatalf("cached %s worktrees = %#v", name, cached)
		}
	}

	single := testkit.GitRepo(t)
	worktrees, err := svc.DiscoverWorktrees(context.Background(), single, false)
	if err != nil || len(worktrees) != 1 || worktrees[0].Name != "test" || worktrees[0].Repository != "app" {
		t.Fatalf("single-repository worktrees = %#v, err = %v; want main named after the profile", worktrees, err)
	}
}

// Assures that repositories sharing a worktrees folder get the repository name in new worktree folders, so one branch can exist in both.
func TestCreateWorktreePathPrefixesRepositoryInSharedFolder(t *testing.T) {
	cfg := multiRepositoryConfig(t)
	worktrees := cfg.ActiveProfile.WorktreesDir
	if got := core.CreateWorktreePath(cfg.ForRepository("web"), "APP-1/fix"); got != filepath.Join(worktrees, "web-APP-1-fix") {
		t.Fatalf("web path = %q", got)
	}
	appPath := core.CreateWorktreePath(cfg.ForRepository("app"), "APP-1/fix")
	webPath := core.CreateWorktreePath(cfg.ForRepository("web"), "APP-1/fix")
	for _, name := range []string{"app", "web"} {
		scoped := cfg.ForRepository(name)
		if _, err := NewService().CreateWorktree(context.Background(), scoped, "APP-1/fix", core.CreateWorktreePath(scoped, "APP-1/fix")); err != nil {
			t.Fatalf("CreateWorktree(%s) error = %v", name, err)
		}
	}
	testkit.AssertGitBranch(t, appPath, "APP-1/fix")
	testkit.AssertGitBranch(t, webPath, "APP-1/fix")

	separate := cfg
	separate.ActiveProfile.Repositories = append([]core.Repository(nil), cfg.ActiveProfile.Repositories...)
	separate.ActiveProfile.Repositories[1].WorktreesDir = filepath.Join(cfg.HomeDir, "web-worktrees")
	if got := core.CreateWorktreePath(separate.ForRepository("web"), "feature"); got != filepath.Join(cfg.HomeDir, "web-worktrees", "feature") {
		t.Fatalf("own folder path = %q, want no prefix", got)
	}
}

package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

// Stacked worktrees: a branch started on top of another branch instead of the
// default one, so a follow-up change can be built before the first is merged.
//
// The link between the two lives in the repository's git config:
//
//	branch.<name>.wt-manager-parent  the local branch <name> sits on
//	branch.<name>.wt-manager-base    the parent commit <name> was started from
//	                                 or last restacked onto
//
// Git config is shared by every worktree of a repository, needs no file of
// its own, and git drops the section when the branch is deleted. The base is
// what makes restacking exact: everything after it belongs to the branch, so
// `git rebase --onto <target> <base>` replays those commits and nothing else,
// even after the parent was amended or squash-merged.

const (
	stackParentKey = "wt-manager-parent"
	stackBaseKey   = "wt-manager-base"
)

func stackConfigKey(branch string, name string) string {
	return "branch." + branch + "." + name
}

// readStackLinks returns the stack link of every branch that has one, with a
// single git call.
func readStackLinks(ctx context.Context, repoPath string) map[string]core.StackLink {
	links := map[string]core.StackLink{}
	// git exits with 1 when nothing matches, which is the usual case.
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "config", "--null", "--get-regexp", `^branch\..*\.wt-manager-(parent|base)$`)
	if err != nil {
		return links
	}
	for _, entry := range strings.Split(output, "\x00") {
		key, value, ok := strings.Cut(entry, "\n")
		if !ok {
			continue
		}
		key = strings.TrimPrefix(key, "branch.")
		value = strings.TrimSpace(value)
		if branch, found := strings.CutSuffix(key, "."+stackParentKey); found {
			link := links[branch]
			link.Parent = value
			links[branch] = link
		} else if branch, found := strings.CutSuffix(key, "."+stackBaseKey); found {
			link := links[branch]
			link.Base = value
			links[branch] = link
		}
	}
	for branch, link := range links {
		if link.Parent == "" {
			delete(links, branch)
		}
	}
	return links
}

func writeStackLink(ctx context.Context, repoPath string, branch string, link core.StackLink) error {
	if _, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "config", stackConfigKey(branch, stackParentKey), link.Parent); err != nil {
		return err
	}
	_, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "config", stackConfigKey(branch, stackBaseKey), link.Base)
	return err
}

func clearStackLink(ctx context.Context, repoPath string, branch string) {
	// Unsetting a key that is not set fails, which is fine.
	_, _ = shell.Run(ctx, repoPath, "git", "-C", repoPath, "config", "--unset-all", stackConfigKey(branch, stackParentKey))
	_, _ = shell.Run(ctx, repoPath, "git", "-C", repoPath, "config", "--unset-all", stackConfigKey(branch, stackBaseKey))
}

// stackParentState reports how a stacked worktree relates to its parent
// branch: how many parent commits it lacks, or that the parent is gone.
func stackParentState(ctx context.Context, wt core.Worktree) (behind int, gone bool) {
	if !wt.Stacked() {
		return 0, false
	}
	output, err := shell.Run(ctx, wt.Path, "git", "-C", wt.Path, "rev-list", "--count", "HEAD..refs/heads/"+wt.Stack.Parent)
	if err != nil {
		return 0, !gitRefExists(ctx, wt.Path, "refs/heads/"+wt.Stack.Parent)
	}
	behind, _ = strconv.Atoi(strings.TrimSpace(output))
	return behind, false
}

func createStackedWorktree(ctx context.Context, cfg core.Config, branch string, path string, parent string) error {
	branch, parent = strings.TrimSpace(branch), strings.TrimSpace(parent)
	if branch == "" {
		return errors.New("branch name is required")
	}
	if parent == "" {
		return errors.New("a stacked worktree needs a parent branch")
	}
	repoPath := cfg.ActiveProfile.RepositoryPath
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return fmt.Errorf("repository not found: %s", repoPath)
	}
	if !gitRefExists(ctx, repoPath, "refs/heads/"+parent) {
		return fmt.Errorf("parent branch %s does not exist", parent)
	}
	// Stacking starts a new line of work; reusing a branch would silently
	// leave it where it was instead of on top of the parent.
	if gitRefExists(ctx, repoPath, "refs/heads/"+branch) {
		return fmt.Errorf("branch %s already exists; a stacked worktree needs a new branch name", branch)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	base, err := resolveCommit(ctx, repoPath, "refs/heads/"+parent)
	if err != nil {
		return err
	}
	if _, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", "--no-track", "-b", branch, path, base); err != nil {
		return err
	}
	return writeStackLink(ctx, repoPath, branch, core.StackLink{Parent: parent, Base: base})
}

func setStackParent(ctx context.Context, cfg core.Config, wt core.Worktree, parent string) error {
	repoPath := cfg.ActiveProfile.RepositoryPath
	parent = strings.TrimSpace(parent)
	if wt.Detached || wt.Branch == "" || wt.Branch == "detached" {
		return fmt.Errorf("%s has no branch to stack", wt.Name)
	}
	if parent == "" {
		clearStackLink(ctx, repoPath, wt.Branch)
		return nil
	}
	if parent == wt.Branch {
		return errors.New("a branch cannot be stacked on itself")
	}
	if !gitRefExists(ctx, repoPath, "refs/heads/"+parent) {
		return fmt.Errorf("parent branch %s does not exist", parent)
	}
	links := readStackLinks(ctx, repoPath)
	seen := map[string]bool{}
	for ancestor := parent; ancestor != "" && !seen[ancestor]; ancestor = links[ancestor].Parent {
		if ancestor == wt.Branch {
			return fmt.Errorf("%s is already stacked on %s", parent, wt.Branch)
		}
		seen[ancestor] = true
	}
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "merge-base", "refs/heads/"+wt.Branch, "refs/heads/"+parent)
	base := strings.TrimSpace(output)
	if err != nil || base == "" {
		return fmt.Errorf("%s and %s share no history", wt.Branch, parent)
	}
	return writeStackLink(ctx, repoPath, wt.Branch, core.StackLink{Parent: parent, Base: base})
}

// mergedLookup reports whether a branch is known to be merged by means other
// than the caller's list, such as asking GitHub.
type mergedLookup func(ctx context.Context, branch string) bool

func restackWorktree(ctx context.Context, cfg core.Config, wt core.Worktree, options core.RestackOptions, fetch fetchFunc, mergedElsewhere mergedLookup) (core.RestackResult, error) {
	repoPath := cfg.ActiveProfile.RepositoryPath
	if wt.Detached || wt.Branch == "" || wt.Branch == "detached" {
		return core.RestackResult{}, fmt.Errorf("%s has no branch to restack", wt.Name)
	}
	if wt.Missing || wt.Prunable {
		return core.RestackResult{}, fmt.Errorf("%s is missing on disk", wt.Name)
	}
	links := readStackLinks(ctx, repoPath)
	link, stacked := links[wt.Branch]
	if !stacked {
		return core.RestackResult{}, fmt.Errorf("%s is not stacked on another branch", wt.Branch)
	}
	if rebaseInProgress(wt.Path) {
		return core.RestackResult{}, fmt.Errorf("a rebase is already in progress in %s; finish it with `git rebase --continue` or `git rebase --abort`", wt.Name)
	}
	status, err := loadRepoStatus(ctx, wt.Path)
	if err != nil {
		return core.RestackResult{}, err
	}
	if status.Dirty {
		return core.RestackResult{}, fmt.Errorf("%s has uncommitted changes; commit or stash them before restacking", wt.Name)
	}

	// The target is the nearest branch below this one in the stack that is
	// still alive: the parent, or what the parent itself was stacked on once
	// the parent is merged or deleted. With none left it is the default branch.
	parent := ""
	if !options.OntoDefault {
		merged := map[string]bool{}
		for _, branch := range options.MergedBranches {
			merged[branch] = true
		}
		seen := map[string]bool{wt.Branch: true}
		for candidate := link.Parent; candidate != "" && !seen[candidate]; candidate = links[candidate].Parent {
			seen[candidate] = true
			if !gitRefExists(ctx, repoPath, "refs/heads/"+candidate) || merged[candidate] {
				continue
			}
			if mergedElsewhere != nil && mergedElsewhere(ctx, candidate) {
				continue
			}
			parent = candidate
			break
		}
	}

	result := core.RestackResult{Onto: parent, Parent: parent}
	targetRef := "refs/heads/" + parent
	if parent == "" {
		remoteName := cfg.ActiveProfile.RemoteName
		if remoteName == "" {
			remoteName = "origin"
		}
		// The merged parent only exists on the default branch after a fetch.
		fetch(ctx, repoPath, remoteName, true)
		defaultRef := defaultRemoteRef(ctx, repoPath, remoteName)
		if defaultRef == "HEAD" {
			return core.RestackResult{}, fmt.Errorf("cannot find the default branch of %s; run `git remote set-head %s --auto`", remoteName, remoteName)
		}
		result.Onto = defaultRef
		targetRef = "refs/remotes/" + defaultRef
	}

	target, err := resolveCommit(ctx, repoPath, targetRef)
	if err != nil {
		return core.RestackResult{}, err
	}
	head, err := resolveCommit(ctx, wt.Path, "HEAD")
	if err != nil {
		return core.RestackResult{}, err
	}

	if !isAncestor(ctx, wt.Path, target, head) {
		base := restackBase(ctx, wt.Path, link, head, target)
		if base == "" {
			return core.RestackResult{}, fmt.Errorf("cannot tell where %s left %s; rebase it by hand", wt.Branch, link.Parent)
		}
		if output, err := shell.Run(ctx, wt.Path, "git", "-C", wt.Path, "rev-list", "--count", base+"..HEAD"); err == nil {
			result.Commits, _ = strconv.Atoi(strings.TrimSpace(output))
		}
		if _, err := shell.Run(ctx, wt.Path, "git", "-C", wt.Path, "rebase", "--onto", target, base); err != nil {
			if rebaseInProgress(wt.Path) {
				return core.RestackResult{}, fmt.Errorf("restacking %s onto %s stopped on conflicts: resolve them in %s and run `git rebase --continue` (or `git rebase --abort`), then restack again", wt.Branch, result.Onto, wt.Path)
			}
			return core.RestackResult{}, err
		}
		result.Rebased = true
	}

	if parent == "" {
		clearStackLink(ctx, repoPath, wt.Branch)
		return result, nil
	}
	return result, writeStackLink(ctx, repoPath, wt.Branch, core.StackLink{Parent: parent, Base: target})
}

// restackBase finds the commit after which the branch's own work starts. The
// recorded base is exact as long as the branch still contains it; otherwise
// (the branch was rebased by hand) the fork point is the best answer left.
func restackBase(ctx context.Context, worktreePath string, link core.StackLink, head string, target string) string {
	if link.Base != "" && isAncestor(ctx, worktreePath, link.Base, head) {
		return link.Base
	}
	for _, other := range []string{"refs/heads/" + link.Parent, target} {
		if output, err := shell.Run(ctx, worktreePath, "git", "-C", worktreePath, "merge-base", head, other); err == nil {
			if base := strings.TrimSpace(output); base != "" {
				return base
			}
		}
	}
	return ""
}

func resolveCommit(ctx context.Context, dir string, ref string) (string, error) {
	output, err := shell.Run(ctx, dir, "git", "-C", dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", ref, err)
	}
	return strings.TrimSpace(output), nil
}

func isAncestor(ctx context.Context, dir string, ancestor string, descendant string) bool {
	_, err := shell.Run(ctx, dir, "git", "-C", dir, "merge-base", "--is-ancestor", ancestor, descendant)
	return err == nil
}

// worktreeGitDir is the directory git keeps a worktree's own state in: .git
// itself for the main checkout, the folder its .git file points at otherwise.
func worktreeGitDir(worktreePath string) string {
	dotGit := filepath.Join(worktreePath, ".git")
	info, err := os.Stat(dotGit)
	if err != nil || info.IsDir() {
		return dotGit
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return dotGit
	}
	gitDir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
	if !ok {
		return dotGit
	}
	gitDir = strings.TrimSpace(gitDir)
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	return filepath.Clean(gitDir)
}

func rebaseInProgress(worktreePath string) bool {
	gitDir := worktreeGitDir(worktreePath)
	for _, name := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			return true
		}
	}
	return false
}

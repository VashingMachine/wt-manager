package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/fspath"
	"github.com/VashingMachine/wt-manager/internal/shell"
)

func discoverWorktrees(ctx context.Context, cfg core.Config) ([]core.Worktree, error) {
	repoPath := cfg.ActiveProfile.RepositoryPath
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	// Symlink resolution walks every path component, so resolve the profile's
	// directories once rather than per worktree.
	canonicalRepo := fspath.Canonical(repoPath)
	canonicalWorktrees := fspath.Canonical(cfg.ActiveProfile.WorktreesDir)
	entries := make([]core.Worktree, 0, 8)
	current := core.Worktree{}
	flush := func() {
		if current.Path == "" {
			return
		}
		current.Path = filepath.Clean(current.Path)
		canonical := fspath.Canonical(current.Path)
		current.IsMain = canonical == canonicalRepo
		current.Managed = current.IsMain || fspath.InsideCanonical(canonical, canonicalWorktrees)
		current.Name = filepath.Base(current.Path)
		if _, err := os.Stat(current.Path); err != nil {
			current.Missing = errors.Is(err, os.ErrNotExist)
		}
		entries = append(entries, current)
		current = core.Worktree{}
	}

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "detached":
			current.Detached = true
			current.Branch = "detached"
		case strings.HasPrefix(line, "prunable"):
			current.Prunable = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			current.Locked = true
		}
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	core.LabelWorktrees(cfg, entries)
	core.AttachStackLinks(entries, readStackLinks(ctx, repoPath))
	return entries, nil
}

// readWorktreeStatus runs git status and git log for one worktree
// concurrently and records what they report on wt.
func readWorktreeStatus(ctx context.Context, wt *core.Worktree) []string {
	wt.StatusPending = false
	if note := core.MissingWorktreeNote(*wt); note != "" {
		wt.LoadError = note
		return nil
	}

	var (
		wg        sync.WaitGroup
		status    core.RepoStatus
		statusErr error
		commit    core.CommitInfo
		commitErr error
	)
	var (
		parentBehind int
		parentGone   bool
	)
	wg.Go(func() { status, statusErr = loadRepoStatus(ctx, wt.Path) })
	wg.Go(func() { commit, commitErr = loadCommitInfo(ctx, wt.Path) })
	wg.Go(func() { parentBehind, parentGone = stackParentState(ctx, *wt) })
	wg.Wait()

	var warnings []string
	wt.LoadError = ""
	if statusErr != nil {
		wt.LoadError = statusErr.Error()
		warnings = append(warnings, fmt.Sprintf("status failed for %s", wt.Name))
	} else {
		status.ParentBehind, status.ParentGone = parentBehind, parentGone
		wt.Status = status
	}
	if commitErr != nil {
		warnings = append(warnings, fmt.Sprintf("commit failed for %s", wt.Name))
	} else {
		wt.Commit = commit
	}
	return warnings
}

func loadRepoStatus(ctx context.Context, repoPath string) (core.RepoStatus, error) {
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return core.RepoStatus{}, err
	}

	status := core.RepoStatus{}
	files := make([]string, 0, 16)
	seen := map[string]struct{}{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "# branch.ab "):
			fmt.Sscanf(strings.TrimPrefix(line, "# branch.ab "), "+%d -%d", &status.Ahead, &status.Behind)
		case strings.HasPrefix(line, "? "):
			status.Untracked++
			appendUnique(&files, seen, strings.TrimPrefix(line, "? "))
		case strings.HasPrefix(line, "1 "):
			xy, file := parseOrdinaryStatus(line)
			applyXY(&status, xy)
			appendUnique(&files, seen, file)
		case strings.HasPrefix(line, "2 "):
			xy, file := parseRenameStatus(line)
			applyXY(&status, xy)
			appendUnique(&files, seen, file)
		case strings.HasPrefix(line, "u "):
			status.Conflicts++
			appendUnique(&files, seen, parseConflictPath(line))
		}
	}
	if err := scanner.Err(); err != nil {
		return core.RepoStatus{}, err
	}

	status.Files = files
	status.Dirty = status.Staged+status.Unstaged+status.Untracked+status.Conflicts > 0
	return status, nil
}

func parseOrdinaryStatus(line string) (string, string) {
	parts := strings.SplitN(line, " ", 9)
	if len(parts) < 9 {
		return "..", strings.TrimSpace(line)
	}
	return parts[1], parts[8]
}

func parseRenameStatus(line string) (string, string) {
	parts := strings.SplitN(line, "\t", 2)
	prefix := parts[0]
	fields := strings.Fields(prefix)
	if len(fields) < 9 {
		return "..", strings.TrimSpace(line)
	}
	return fields[1], fields[len(fields)-1]
}

func parseConflictPath(line string) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return strings.TrimSpace(line)
	}
	return parts[len(parts)-1]
}

func applyXY(status *core.RepoStatus, xy string) {
	if len(xy) < 2 {
		return
	}
	if xy[0] != '.' {
		status.Staged++
	}
	if xy[1] != '.' {
		status.Unstaged++
	}
}

func loadCommitInfo(ctx context.Context, repoPath string) (core.CommitInfo, error) {
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "log", "-1", "--date=relative", "--format=%h%x09%cr%x09%s")
	if err != nil {
		return core.CommitInfo{}, err
	}

	parts := strings.SplitN(strings.TrimSpace(output), "\t", 3)
	commit := core.CommitInfo{}
	if len(parts) > 0 {
		commit.Hash = parts[0]
	}
	if len(parts) > 1 {
		commit.Relative = parts[1]
	}
	if len(parts) > 2 {
		commit.Subject = parts[2]
	}
	return commit, nil
}

func removeWorktree(ctx context.Context, cfg core.Config, wt core.Worktree) error {
	repoPath := cfg.ActiveProfile.RepositoryPath
	var err error
	if wt.Missing || wt.Prunable {
		_, err = shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "prune")
	} else if wt.Status.Dirty {
		_, err = shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "remove", "--force", wt.Path)
	} else {
		_, err = shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "remove", wt.Path)
	}
	if err == nil {
		_, _ = shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "prune")
	}
	return err
}

// fetchFunc updates remote-tracking refs. It reports whether it actually ran
// git fetch; force bypasses any reuse of a recent fetch.
type fetchFunc func(ctx context.Context, repoPath string, remoteName string, force bool) bool

func fetchRemote(ctx context.Context, repoPath string, remoteName string, _ bool) bool {
	_, _ = shell.Run(ctx, repoPath, "git", "-C", repoPath, "fetch", "--quiet", remoteName, "--prune")
	return true
}

func createWorktree(ctx context.Context, cfg core.Config, branch string, path string) (bool, error) {
	return createWorktreeWith(ctx, cfg, branch, path, fetchRemote)
}

func createWorktreeWith(ctx context.Context, cfg core.Config, branch string, path string, fetch fetchFunc) (bool, error) {
	if strings.TrimSpace(branch) == "" {
		return false, errors.New("branch name is required")
	}
	repoPath := cfg.ActiveProfile.RepositoryPath
	remoteName := cfg.ActiveProfile.RemoteName
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return false, fmt.Errorf("repository not found: %s", repoPath)
	}

	if knownWorktreePath(ctx, repoPath, path) {
		return true, nil
	}
	if _, err := os.Stat(path); err == nil {
		return false, fmt.Errorf("%s already exists but is not a worktree for profile %s", path, cfg.ActiveProfile.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}

	// A local branch is checked out as-is, so the network round trip of a fetch
	// would not change the result.
	if gitRefExists(ctx, repoPath, "refs/heads/"+branch) {
		_, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", path, branch)
		return false, err
	}

	remoteRef := "refs/remotes/" + remoteName + "/" + branch
	fetched := fetch(ctx, repoPath, remoteName, false)
	if !fetched && !gitRefExists(ctx, repoPath, remoteRef) {
		// A reused fetch may predate a branch pushed moments ago; check again
		// before creating a new branch instead of tracking the remote one.
		fetch(ctx, repoPath, remoteName, true)
	}
	if gitRefExists(ctx, repoPath, remoteRef) {
		_, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", "--track", "-b", branch, path, remoteName+"/"+branch)
		return false, err
	}

	defaultRef := defaultRemoteRef(ctx, repoPath, remoteName)
	_, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", "-b", branch, path, defaultRef)
	return false, err
}

func ensurePullRequestWorktree(ctx context.Context, cfg core.Config, pullRequest core.RemotePullRequest) (core.Worktree, bool, error) {
	return ensurePullRequestWorktreeWith(ctx, cfg, pullRequest, fetchRemote)
}

func ensurePullRequestWorktreeWith(ctx context.Context, cfg core.Config, pullRequest core.RemotePullRequest, fetch fetchFunc) (core.Worktree, bool, error) {
	if pullRequest.Number <= 0 {
		return core.Worktree{}, false, errors.New("pull request number is required")
	}
	branch := pullRequest.HeadRefName
	if strings.TrimSpace(branch) == "" {
		branch = fmt.Sprintf("pr-%d", pullRequest.Number)
	}
	if strings.TrimSpace(pullRequest.HeadRefName) != "" {
		if existing, ok := existingWorktreeForBranch(ctx, cfg, pullRequest.HeadRefName); ok {
			return existing, true, nil
		}
	}
	localBranch := fmt.Sprintf("pr-%d-%s", pullRequest.Number, branch)
	path := core.CreateWorktreePath(cfg, localBranch)
	if knownWorktreePath(ctx, cfg.ActiveProfile.RepositoryPath, path) {
		return core.Worktree{Name: filepath.Base(path), Path: path, Branch: localBranch}, true, nil
	}
	if _, err := os.Stat(path); err == nil {
		return core.Worktree{}, false, fmt.Errorf("%s already exists but is not a worktree for profile %s", path, cfg.ActiveProfile.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return core.Worktree{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return core.Worktree{}, false, err
	}

	repoPath := cfg.ActiveProfile.RepositoryPath
	remoteName := cfg.ActiveProfile.RemoteName
	if remoteName == "" {
		remoteName = "origin"
	}
	prRef := fmt.Sprintf("pull/%d/head:%s", pullRequest.Number, localBranch)
	_, _ = shell.Run(ctx, repoPath, "git", "-C", repoPath, "fetch", "--quiet", remoteName, prRef)
	if gitRefExists(ctx, repoPath, "refs/heads/"+localBranch) {
		_, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "add", path, localBranch)
		return core.Worktree{Name: filepath.Base(path), Path: path, Branch: localBranch}, false, err
	}

	if strings.TrimSpace(pullRequest.HeadRefName) != "" {
		existing, err := createWorktreeWith(ctx, cfg, pullRequest.HeadRefName, path, fetch)
		return core.Worktree{Name: filepath.Base(path), Path: path, Branch: pullRequest.HeadRefName}, existing, err
	}
	return core.Worktree{}, false, fmt.Errorf("cannot create worktree for PR #%d", pullRequest.Number)
}

func existingWorktreeForBranch(ctx context.Context, cfg core.Config, branch string) (core.Worktree, bool) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return core.Worktree{}, false
	}
	worktrees, err := discoverWorktrees(ctx, cfg)
	if err != nil {
		return core.Worktree{}, false
	}
	for _, wt := range worktrees {
		if wt.Missing || wt.Prunable || wt.Detached {
			continue
		}
		if wt.Branch == branch {
			return wt, true
		}
	}
	return core.Worktree{}, false
}

func gitRefExists(ctx context.Context, repoPath string, ref string) bool {
	_, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

func defaultRemoteRef(ctx context.Context, repoPath string, remoteName string) string {
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+remoteName+"/HEAD")
	if err != nil {
		return "HEAD"
	}
	ref := strings.TrimSpace(output)
	if ref == "" {
		return "HEAD"
	}
	return ref
}

func knownWorktreePath(ctx context.Context, repoPath string, path string) bool {
	output, err := shell.Run(ctx, repoPath, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	want := fspath.Canonical(path)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "worktree ") && fspath.Canonical(strings.TrimPrefix(line, "worktree ")) == want {
			return true
		}
	}
	return false
}

func appendUnique(files *[]string, seen map[string]struct{}, path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if _, ok := seen[path]; ok {
		return
	}
	seen[path] = struct{}{}
	*files = append(*files, path)
}

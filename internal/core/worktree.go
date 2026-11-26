package core

import (
	"sort"
	"strings"
	"time"
)

type Worktree struct {
	Name     string
	Path     string
	Branch   string
	Head     string
	Missing  bool
	Detached bool
	Prunable bool
	IsMain   bool
	Managed  bool
	// Locked is set for worktrees protected with `git worktree lock`.
	Locked bool
	// Repository names the project repository the worktree belongs to.
	Repository string
	// Stack names the branch this worktree's branch was started on top of.
	Stack StackLink
	// StackDepth counts the stack parents listed above the worktree; rows are
	// indented by it.
	StackDepth int
	// ParentMerged is set once the pull request of the stack parent is known
	// to be merged. Git cannot tell after a squash merge; whoever holds the
	// pull requests fills it in.
	ParentMerged bool
	Status       RepoStatus
	Commit       CommitInfo
	PR           *PullRequest
	LoadError    string
	// StatusPending is true while git status and the last commit have not been
	// read in this session or an earlier, cached one. Rows render a placeholder
	// instead of claiming such a worktree is clean.
	StatusPending bool
}

type RepoStatus struct {
	Dirty     bool
	Ahead     int
	Behind    int
	Staged    int
	Unstaged  int
	Untracked int
	Conflicts int
	Files     []string
	// ParentBehind counts the commits of the stack parent the branch does not
	// contain yet, and ParentGone reports that the parent branch no longer
	// exists. Both are read only for stacked worktrees.
	ParentBehind int
	ParentGone   bool
}

// SweepCandidate is a worktree the sweep command offers to remove: clean and
// untouched for longer than the configured time.
type SweepCandidate struct {
	Worktree     Worktree
	LastActivity time.Time
}

type CommitInfo struct {
	Hash     string
	Relative string
	Subject  string
}

// FilterManaged keeps the worktrees the app manages (the main checkout and
// those inside the worktrees folder), or all of them when showAll is set.
func FilterManaged(worktrees []Worktree, showAll bool) []Worktree {
	filtered := make([]Worktree, 0, len(worktrees))
	for _, wt := range worktrees {
		if showAll || wt.Managed {
			filtered = append(filtered, wt)
		}
	}
	return filtered
}

// LabelWorktrees records which repository the worktrees belong to and names
// the main checkout after the project, or after its repository when the
// project has several.
func LabelWorktrees(cfg Config, worktrees []Worktree) {
	repository := cfg.RepositoryName()
	mainName := cfg.ActiveProfile.Name
	if cfg.MultiRepository() || mainName == "" {
		mainName = repository
	}
	for i := range worktrees {
		worktrees[i].Repository = repository
		if worktrees[i].IsMain {
			worktrees[i].Name = mainName
		}
	}
}

// MissingWorktreeNote explains why a worktree has no status to read, or
// returns "" when its directory exists.
func MissingWorktreeNote(wt Worktree) string {
	if !wt.Missing {
		return ""
	}
	if wt.Prunable {
		return "prunable worktree entry"
	}
	return "path missing on disk"
}

// AttachPullRequests labels each branch worktree with its PR, if any.
func AttachPullRequests(worktrees []Worktree, prs []PullRequest) {
	index := make(map[string]*PullRequest, len(prs))
	for i := range prs {
		if prs[i].HeadRefName == "" {
			continue
		}
		pr := prs[i]
		index[pr.HeadRefName] = &pr
	}
	for i := range worktrees {
		wt := &worktrees[i]
		wt.PR = nil
		if wt.Detached || wt.Branch == "" || wt.Branch == "detached" {
			continue
		}
		wt.PR = index[wt.Branch]
	}
}

// SortWorktrees puts the main checkout first and the rest in branch order.
func SortWorktrees(worktrees []Worktree) {
	sort.SliceStable(worktrees, func(i, j int) bool {
		left := worktrees[i]
		right := worktrees[j]

		if left.IsMain != right.IsMain {
			return left.IsMain
		}

		leftBranch := strings.ToLower(left.Branch)
		rightBranch := strings.ToLower(right.Branch)
		if leftBranch != rightBranch {
			return leftBranch < rightBranch
		}

		leftName := strings.ToLower(left.Name)
		rightName := strings.ToLower(right.Name)
		if leftName != rightName {
			return leftName < rightName
		}

		return strings.ToLower(left.Path) < strings.ToLower(right.Path)
	})
}

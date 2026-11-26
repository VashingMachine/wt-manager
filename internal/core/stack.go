package core

// StackLink records that a branch was started on top of another branch
// instead of the default one. It lives in the repository's git config
// (branch.<name>.wt-manager-parent and -base), so it is shared by every
// worktree and disappears together with the branch.
type StackLink struct {
	// Parent is the local branch the branch sits on.
	Parent string
	// Base is the parent commit the branch was started from or last restacked
	// onto: everything after it belongs to the branch itself.
	Base string
}

// Stacked reports whether the worktree's branch sits on another branch.
func (w Worktree) Stacked() bool {
	return w.Stack.Parent != ""
}

// RestackOptions steers RestackWorktree.
type RestackOptions struct {
	// MergedBranches names branches known to be merged; they are skipped when
	// looking for the branch to rebase onto.
	MergedBranches []string
	// OntoDefault rebases onto the remote's default branch whatever the state
	// of the parent, which takes the worktree out of its stack.
	OntoDefault bool
}

// RestackResult describes what RestackWorktree did.
type RestackResult struct {
	// Onto is what the branch sits on now: a local branch, or a remote ref
	// such as origin/main.
	Onto string
	// Parent is the new stack parent; empty when the worktree left its stack.
	Parent string
	// Rebased is false when the branch already contained Onto and only the
	// recorded stack link changed.
	Rebased bool
	// Commits is how many commits were replayed.
	Commits int
}

// AttachStackLinks labels branch worktrees with their stack link.
func AttachStackLinks(worktrees []Worktree, links map[string]StackLink) {
	for i := range worktrees {
		wt := &worktrees[i]
		wt.Stack = StackLink{}
		if wt.Detached || wt.Branch == "" {
			continue
		}
		wt.Stack = links[wt.Branch]
	}
}

// OrderByStack moves every stacked worktree right below the worktree of its
// parent branch, keeping the given order otherwise, and records how deep each
// one sits. A worktree whose parent has no row stays where it was.
func OrderByStack(worktrees []Worktree) {
	byBranch := make(map[string]int, len(worktrees))
	for i, wt := range worktrees {
		if !wt.Detached && wt.Branch != "" {
			if _, taken := byBranch[wt.Branch]; !taken {
				byBranch[wt.Branch] = i
			}
		}
	}
	children := make(map[int][]int)
	isChild := make([]bool, len(worktrees))
	for i, wt := range worktrees {
		worktrees[i].StackDepth = 0
		if !wt.Stacked() {
			continue
		}
		if parent, ok := byBranch[wt.Stack.Parent]; ok && parent != i {
			children[parent] = append(children[parent], i)
			isChild[i] = true
		}
	}
	if len(children) == 0 {
		return
	}

	ordered := make([]Worktree, 0, len(worktrees))
	placed := make([]bool, len(worktrees))
	var place func(index int, depth int)
	place = func(index int, depth int) {
		if placed[index] {
			return
		}
		placed[index] = true
		wt := worktrees[index]
		wt.StackDepth = depth
		ordered = append(ordered, wt)
		for _, child := range children[index] {
			place(child, depth+1)
		}
	}
	for i := range worktrees {
		if !isChild[i] {
			place(i, 0)
		}
	}
	// Links that form a loop have no root; list them flat instead of losing them.
	for i := range worktrees {
		place(i, 0)
	}
	copy(worktrees, ordered)
}

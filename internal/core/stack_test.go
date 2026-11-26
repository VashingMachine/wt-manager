package core

import "testing"

// Assures that stack links forming a loop still list every worktree once.
func TestOrderByStackSurvivesLoops(t *testing.T) {
	worktrees := []Worktree{
		{Branch: "a", Path: "/a", Stack: StackLink{Parent: "b"}},
		{Branch: "b", Path: "/b", Stack: StackLink{Parent: "a"}},
		{Branch: "c", Path: "/c"},
	}
	OrderByStack(worktrees)
	seen := map[string]int{}
	for _, wt := range worktrees {
		seen[wt.Branch]++
	}
	if len(worktrees) != 3 || seen["a"] != 1 || seen["b"] != 1 || seen["c"] != 1 {
		t.Fatalf("worktrees after a loop = %#v", worktrees)
	}
}

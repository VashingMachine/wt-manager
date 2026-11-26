package core

import "testing"

func TestTicketBranchAndKey(t *testing.T) {
	ticket := Ticket{Key: "APP-123", Summary: "Retry failed exports when the upstream API times out"}
	branch := TicketBranchName(ticket)
	if branch != "APP-123-retry-failed-exports-when-the-upstream" {
		t.Fatalf("TicketBranchName() = %q", branch)
	}
	for _, candidate := range []string{branch, "feature/app-123-fix", "APP-123/changes"} {
		if got := TicketKeyFromBranch(candidate); got != "APP-123" {
			t.Fatalf("TicketKeyFromBranch(%q) = %q", candidate, got)
		}
	}
}

func TestAttachTicketContextMatchesWorktreeAndPR(t *testing.T) {
	tickets := []Ticket{{Key: "APP-123"}}
	worktrees := []Worktree{{Name: "APP-123-fix", Branch: "APP-123-fix", Managed: true}}
	pullRequests := []PullRequest{{Number: 321, HeadRefName: "APP-123-fix", State: "OPEN"}}

	got := AttachTicketContext(tickets, worktrees, pullRequests)
	if got[0].Worktree == nil || got[0].Worktree.Name != worktrees[0].Name {
		t.Fatalf("worktree join = %#v", got[0].Worktree)
	}
	if got[0].PR == nil || got[0].PR.Number != 321 {
		t.Fatalf("PR join = %#v", got[0].PR)
	}
}

func TestFilterTicketsOpenOnlyUsesStatusCategory(t *testing.T) {
	tickets := []Ticket{
		{Key: "APP-1", Status: "In Progress", StatusCategory: "indeterminate"},
		{Key: "APP-2", Status: "Resolved", StatusCategory: "done"},
		{Key: "APP-3", Status: "Closed"},
	}
	got := FilterTickets(tickets, "", "", true)
	if len(got) != 1 || got[0].Key != "APP-1" {
		t.Fatalf("FilterTickets(openOnly) = %#v", got)
	}
	if got := FilterTickets(tickets, "", "", false); len(got) != 3 {
		t.Fatalf("FilterTickets(all) returned %d tickets", len(got))
	}
}

// Assures that a ticket patch changes only the patched ticket and fields, and appending a comment never aliases the original comments.
func TestTicketPatchApply(t *testing.T) {
	comments := make([]TicketComment, 1, 4)
	comments[0] = TicketComment{Body: "first"}
	original := Ticket{Key: "APP-1", Status: "To Do", StatusCategory: StatusCategoryNew, Comments: comments}
	patched := original

	TicketPatch{Key: "APP-1", Status: "Doing", Comment: &TicketComment{Body: "second"}}.Apply(&patched)
	if patched.Status != "Doing" || patched.StatusCategory != StatusCategoryNew || len(patched.Comments) != 2 {
		t.Fatalf("patched = %#v, want new status, unchanged unknown category, appended comment", patched)
	}
	if extended := original.Comments[:2]; extended[1].Body == "second" {
		t.Fatal("appending a comment wrote into the original ticket's backing array")
	}

	other := Ticket{Key: "APP-2", Status: "To Do"}
	TicketPatch{Key: "APP-1", Status: "Done"}.Apply(&other)
	if other.Status != "To Do" {
		t.Fatal("patch changed a different ticket")
	}
}

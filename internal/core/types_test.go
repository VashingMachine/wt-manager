package core

import "testing"

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

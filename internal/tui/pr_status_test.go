package tui

import "testing"

func TestChecksSummaryAndReviewReadiness(t *testing.T) {
	pr := remotePRFixture(10, "OPEN", "alice", "Ready PR", "feature/ready")
	pr.StatusCheckRollup = []StatusCheck{{Name: "test", Status: "completed", Conclusion: "success"}}
	if got := checksSummary(pr.StatusCheckRollup); got != "1 pass" {
		t.Fatalf("checksSummary(pass) = %q", got)
	}
	if got := reviewReadiness(pr); got != "ready" {
		t.Fatalf("reviewReadiness(pass) = %q", got)
	}

	pr.StatusCheckRollup = []StatusCheck{{Name: "test", Status: "completed", Conclusion: "failure"}}
	if got := checksSummary(pr.StatusCheckRollup); got != "1 fail" {
		t.Fatalf("checksSummary(fail) = %q", got)
	}
	if got := reviewReadiness(pr); got != "blocked" {
		t.Fatalf("reviewReadiness(fail) = %q", got)
	}

	pr.StatusCheckRollup = []StatusCheck{{Name: "test", Status: "queued", Conclusion: ""}}
	if got := reviewReadiness(pr); got != "waiting" {
		t.Fatalf("reviewReadiness(waiting) = %q", got)
	}

	pr.IsDraft = true
	if got := reviewReadiness(pr); got != "draft" {
		t.Fatalf("reviewReadiness(draft) = %q", got)
	}
}

func TestFailedStatusChecks(t *testing.T) {
	checks := []StatusCheck{
		{Name: "pass", Status: "completed", Conclusion: "success"},
		{Name: "fail", Status: "completed", Conclusion: "failure"},
		{Name: "cancel", Status: "completed", Conclusion: "cancelled"},
		{Name: "wait", Status: "queued"},
	}
	failed := failedStatusChecks(checks)
	if len(failed) != 2 || failed[0].Name != "fail" || failed[1].Name != "cancel" {
		t.Fatalf("failedStatusChecks() = %#v", failed)
	}
}

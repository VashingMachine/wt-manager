package tui

import (
	"context"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/services"
)

type notifySpyServices struct {
	core.Services
	titles []string
}

func (s *notifySpyServices) Notify(ctx context.Context, title string, body string) error {
	s.titles = append(s.titles, title+": "+body)
	return nil
}

// Approving a PR raises a terminal-host (cmux) notification so the result is
// visible even when another workspace is focused.
func TestPRApprovalResultSendsNotification(t *testing.T) {
	spy := &notifySpyServices{Services: services.NewService()}

	msg := notifyCmd(spy, "PR approved", "Approved PR #5")()
	if msg != nil {
		t.Fatalf("notify command returned %v, want nil message", msg)
	}
	if len(spy.titles) != 1 || spy.titles[0] != "PR approved: Approved PR #5" {
		t.Fatalf("notifications = %v", spy.titles)
	}
}

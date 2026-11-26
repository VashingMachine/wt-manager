package github

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

func TestCheckGitHubCLI(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		warnings := checkGitHubCLI(context.Background(), "owner/repo")
		if len(warnings) == 0 || !strings.Contains(warnings[0], "gh not found") {
			t.Fatalf("warnings = %#v", warnings)
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		bin := t.TempDir()
		testkit.WriteExecutable(t, filepath.Join(bin, "gh"), "#!/bin/sh\nif [ \"$1\" = auth ]; then exit 1; fi\nexit 0\n")
		t.Setenv("PATH", bin)
		warnings := checkGitHubCLI(context.Background(), "owner/repo")
		if len(warnings) == 0 || !strings.Contains(warnings[0], "not authenticated") {
			t.Fatalf("warnings = %#v", warnings)
		}
	})

	t.Run("authenticated", func(t *testing.T) {
		bin := t.TempDir()
		testkit.WriteExecutable(t, filepath.Join(bin, "gh"), "#!/bin/sh\nexit 0\n")
		t.Setenv("PATH", bin)
		warnings := checkGitHubCLI(context.Background(), "owner/repo")
		if len(warnings) != 0 {
			t.Fatalf("warnings = %#v, want none", warnings)
		}
	})
}

func TestApproveRemotePullRequestUsesGitHubCLI(t *testing.T) {
	bin := t.TempDir()
	argsPath := filepath.Join(bin, "args")
	testkit.WriteExecutable(t, filepath.Join(bin, "gh"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_GH_ARGS\"\nexit 0\n")
	t.Setenv("PATH", bin)
	t.Setenv("WT_MANAGER_GH_ARGS", argsPath)

	cfg := core.Config{RepoSlug: "owner/app", ActiveProfile: core.RepositoryProfile{RepositoryPath: t.TempDir()}}
	if err := approveRemotePullRequest(context.Background(), cfg, 42); err != nil {
		t.Fatalf("approveRemotePullRequest() error = %v", err)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("ReadFile(args) error = %v", err)
	}
	got := strings.Join(strings.Fields(string(args)), " ")
	want := "pr review 42 --approve --repo owner/app"
	if got != want {
		t.Fatalf("gh args = %q, want %q", got, want)
	}
}

func TestApproveRemotePullRequestReportsCommandFailure(t *testing.T) {
	bin := t.TempDir()
	testkit.WriteExecutable(t, filepath.Join(bin, "gh"), "#!/bin/sh\necho nope >&2\nexit 7\n")
	t.Setenv("PATH", bin)

	cfg := core.Config{RepoSlug: "owner/app", ActiveProfile: core.RepositoryProfile{RepositoryPath: t.TempDir()}}
	err := approveRemotePullRequest(context.Background(), cfg, 42)
	if err == nil || !strings.Contains(err.Error(), "approve PR #42 failed") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("approveRemotePullRequest() error = %v, want command failure with output", err)
	}
}

func TestRemotePullRequestFromREST(t *testing.T) {
	mergedAt := "2026-05-05T12:00:00Z"
	got := remotePullRequestFromREST(restPullRequest{
		Number:       42,
		Title:        "Fix auth refresh",
		HTMLURL:      "https://github.com/owner/repo/pull/42",
		State:        "closed",
		Body:         "body",
		Draft:        true,
		User:         restActor{Login: "alice"},
		Assignees:    []restActor{{Login: "bob"}},
		Labels:       []restLabel{{Name: "backend"}},
		Head:         restRef{Ref: "feature/auth", SHA: "abc"},
		Base:         restRef{Ref: "main"},
		ChangedFiles: 3,
		Additions:    100,
		Deletions:    20,
		CreatedAt:    "2026-05-04T12:00:00Z",
		UpdatedAt:    "2026-05-05T12:00:00Z",
		MergedAt:     &mergedAt,
	})

	if got.State != "MERGED" || got.Number != 42 || got.HeadRefName != "feature/auth" || got.Author.Login != "alice" {
		t.Fatalf("remotePullRequestFromREST() = %#v", got)
	}
	if got.HeadSHA != "abc" {
		t.Fatalf("HeadSHA = %q, want abc", got.HeadSHA)
	}
	if len(got.Assignees) != 1 || got.Assignees[0].Login != "bob" {
		t.Fatalf("Assignees = %#v", got.Assignees)
	}
	if len(got.Labels) != 1 || got.Labels[0].Name != "backend" {
		t.Fatalf("Labels = %#v", got.Labels)
	}
}

package services

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

type cmuxCall struct {
	Dir  string
	Args []string
}

type cmuxStub struct {
	calls   []cmuxCall
	respond func(args []string) (string, error)
}

func (s *cmuxStub) install(t *testing.T) {
	t.Helper()
	originalRun, originalBackground := runCmux, cmuxBackground
	runCmux = func(ctx context.Context, dir string, args ...string) (string, error) {
		s.calls = append(s.calls, cmuxCall{Dir: dir, Args: args})
		if s.respond == nil {
			return "", nil
		}
		return s.respond(args)
	}
	cmuxBackground = func(fn func()) { fn() }
	cmuxBrowserSurface.ref = ""
	t.Cleanup(func() {
		runCmux, cmuxBackground = originalRun, originalBackground
		cmuxBrowserSurface.ref = ""
	})
}

func (s *cmuxStub) find(command string) []string {
	for _, call := range s.calls {
		if slices.Contains(call.Args, command) {
			return call.Args
		}
	}
	return nil
}

func argAfter(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// cmux identifies itself through its own per-terminal variables even though it
// reports TERM_PROGRAM=ghostty, so it must win over the Ghostty detection.
func TestDetectTerminalAppRecognizesCmux(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want terminalApp
	}{
		{name: "workspace id beats ghostty term program", env: map[string]string{"TERM_PROGRAM": "ghostty", "CMUX_WORKSPACE_ID": "abc"}, want: terminalCmux},
		{name: "surface id alone", env: map[string]string{"CMUX_SURFACE_ID": "abc"}, want: terminalCmux},
		{name: "ghostty resources do not hide cmux", env: map[string]string{"GHOSTTY_RESOURCES_DIR": "/x", "CMUX_WORKSPACE_ID": "abc"}, want: terminalCmux},
		{name: "socket path alone is not enough", env: map[string]string{"TERM_PROGRAM": "ghostty", "CMUX_SOCKET_PATH": "/tmp/cmux.sock"}, want: terminalGhostty},
		{name: "plain ghostty stays ghostty", env: map[string]string{"TERM_PROGRAM": "ghostty"}, want: terminalGhostty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectTerminalApp(func(key string) string { return tc.env[key] }); got != tc.want {
				t.Fatalf("detectTerminalApp() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The workspace layout holds one pane: a focused agent tab running the agent
// command followed by a shell tab, or only a shell tab when no agent is given.
func TestCmuxWorkspaceLayout(t *testing.T) {
	got, err := cmuxWorkspaceLayout(&AgentTool{Name: "claude", Command: "claude --resume"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"pane":{"surfaces":[{"type":"terminal","name":"claude","command":"claude --resume","focus":true},{"type":"terminal","name":"shell"}]}}`
	if got != want {
		t.Fatalf("agent layout = %s, want %s", got, want)
	}

	got, err = cmuxWorkspaceLayout(nil)
	if err != nil {
		t.Fatal(err)
	}
	want = `{"pane":{"surfaces":[{"type":"terminal","name":"shell","focus":true}]}}`
	if got != want {
		t.Fatalf("terminal layout = %s, want %s", got, want)
	}
}

// Opening a worktree agent inside cmux creates a focused workspace named after
// the worktree and rooted at its path, with the agent and shell tabs.
func TestOpenAgentInCmuxCreatesFocusedWorkspace(t *testing.T) {
	stub := &cmuxStub{respond: func(args []string) (string, error) {
		if slices.Contains(args, "new-workspace") {
			return "OK workspace:7\n", nil
		}
		return "[]", nil
	}}
	stub.install(t)

	wt := Worktree{Name: "APP-1-fix", Path: "/tmp/wt/APP-1-fix", Branch: "APP-1-fix"}
	if err := openAgentInCmux(context.Background(), wt, AgentTool{Name: "claude", Command: "claude"}); err != nil {
		t.Fatalf("openAgentInCmux() error = %v", err)
	}

	create := stub.find("new-workspace")
	if create == nil {
		t.Fatalf("no new-workspace call in %v", stub.calls)
	}
	for flag, want := range map[string]string{"--name": "APP-1-fix", "--cwd": "/tmp/wt/APP-1-fix", "--focus": "true"} {
		if got := argAfter(create, flag); got != want {
			t.Fatalf("%s = %q, want %q", flag, got, want)
		}
	}
	if !strings.Contains(argAfter(create, "--layout"), `"command":"claude"`) {
		t.Fatalf("layout %q lacks agent command", argAfter(create, "--layout"))
	}
}

// Opening a terminal inside cmux creates a workspace whose only tab is a shell.
func TestOpenTerminalInCmuxHasNoAgentTab(t *testing.T) {
	stub := &cmuxStub{respond: func(args []string) (string, error) {
		if slices.Contains(args, "new-workspace") {
			return "OK workspace:2", nil
		}
		return "[]", nil
	}}
	stub.install(t)

	if err := openTerminalInCmux(context.Background(), Worktree{Name: "feature", Path: "/tmp/feature"}); err != nil {
		t.Fatal(err)
	}
	layout := argAfter(stub.find("new-workspace"), "--layout")
	if strings.Contains(layout, "command") || !strings.Contains(layout, "shell") {
		t.Fatalf("terminal layout = %q, want only a shell tab", layout)
	}
}

// A worktree that already has a cmux workspace is selected instead of opening a duplicate.
func TestOpenAgentInCmuxReusesExistingWorkspace(t *testing.T) {
	stub := &cmuxStub{respond: func(args []string) (string, error) {
		if slices.Contains(args, "list") {
			return `{"workspaces":[{"ref":"workspace:1","title":"main"},{"ref":"workspace:4","title":"APP-1-fix","cwd":"/tmp/wt/APP-1-fix"}]}`, nil
		}
		return "OK", nil
	}}
	stub.install(t)
	createdCmuxWorkspaces.refs = map[string]string{}

	wt := Worktree{Name: "APP-1-fix", Path: "/tmp/wt/APP-1-fix"}
	if err := openAgentInCmux(context.Background(), wt, AgentTool{Name: "claude", Command: "claude"}); err != nil {
		t.Fatal(err)
	}
	if stub.find("new-workspace") != nil {
		t.Fatal("created a duplicate workspace")
	}
	if got := argAfter(stub.find("select-workspace"), "--workspace"); got != "workspace:4" {
		t.Fatalf("selected workspace %q, want workspace:4", got)
	}
}

// Pressing Enter twice quickly must not create two workspaces even though the
// first one is not yet in the listing and has not reported its directory.
func TestOpenAgentInCmuxReusesJustCreatedWorkspace(t *testing.T) {
	createdCmuxWorkspaces.refs = map[string]string{}
	creates := 0
	stub := &cmuxStub{respond: func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "new-workspace"):
			creates++
			return "OK workspace:12", nil
		case slices.Contains(args, "list"):
			return `{"workspaces":[]}`, nil
		}
		return "OK", nil
	}}
	stub.install(t)

	wt := Worktree{Name: "quick", Path: "/tmp/quick"}
	for range 2 {
		if err := openTerminalInCmux(context.Background(), wt); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 1 {
		t.Fatalf("created %d workspaces, want 1", creates)
	}
	if got := argAfter(stub.find("select-workspace"), "--workspace"); got != "workspace:12" {
		t.Fatalf("selected %q, want workspace:12", got)
	}
}

// If the remembered workspace was closed, selecting it fails and a fresh one is created.
func TestOpenAgentInCmuxRecreatesClosedWorkspace(t *testing.T) {
	createdCmuxWorkspaces.refs = map[string]string{}
	creates := 0
	stub := &cmuxStub{respond: func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "new-workspace"):
			creates++
			return "OK workspace:2" + string(rune('0'+creates)), nil
		case slices.Contains(args, "select-workspace"):
			return "", errors.New("Workspace ref not found")
		case slices.Contains(args, "list"):
			return `{"workspaces":[]}`, nil
		}
		return "OK", nil
	}}
	stub.install(t)

	wt := Worktree{Name: "gone", Path: "/tmp/gone"}
	for range 2 {
		if err := openTerminalInCmux(context.Background(), wt); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 2 {
		t.Fatalf("created %d workspaces, want 2", creates)
	}
}

// A same-named workspace rooted at a different directory belongs to another
// repository and must not be reused.
func TestMatchCmuxWorkspaceRequiresMatchingDirectory(t *testing.T) {
	output := `[{"ref":"workspace:3","title":"feature","cwd":"/elsewhere/feature"}]`
	if got := matchCmuxWorkspace(output, "feature", "/tmp/feature"); got != "" {
		t.Fatalf("matched %q across directories, want none", got)
	}
	if got := matchCmuxWorkspace(output, "feature", "/elsewhere/feature"); got != "workspace:3" {
		t.Fatalf("matched %q, want workspace:3", got)
	}
	if got := matchCmuxWorkspace(output, "feature", "/elsewhere/feature/../feature"); got != "workspace:3" {
		t.Fatalf("matched %q for an unclean path, want workspace:3", got)
	}
}

// A workspace whose shell moved into a subdirectory of the worktree is still that worktree's workspace.
func TestMatchCmuxWorkspaceAcceptsSubdirectory(t *testing.T) {
	output := `{"workspaces":[{"ref":"workspace:3","title":"feature","current_directory":"/tmp/feature/internal/pkg"}]}`
	if got := matchCmuxWorkspace(output, "feature", "/tmp/feature"); got != "workspace:3" {
		t.Fatalf("matched %q, want workspace:3", got)
	}
}

// When the listing is plain text, only a whole-title match counts so "feat"
// never selects the "feat-2" workspace.
func TestMatchCmuxWorkspaceTextOutput(t *testing.T) {
	output := "* workspace:1  feat-2\n  workspace:2  feat\n"
	if got := matchCmuxWorkspace(output, "feat", "/tmp/feat"); got != "workspace:2" {
		t.Fatalf("matched %q, want workspace:2", got)
	}
	if got := matchCmuxWorkspace(output, "other", "/tmp/other"); got != "" {
		t.Fatalf("matched %q, want none", got)
	}
}

// If cmux rejects the layout, the workspace is still created by running the
// agent directly and adding the shell tab afterwards.
func TestCreateCmuxWorkspaceFallsBackWithoutLayout(t *testing.T) {
	stub := &cmuxStub{respond: func(args []string) (string, error) {
		switch {
		case slices.Contains(args, "--layout"):
			return "", errors.New("layout rejected")
		case slices.Contains(args, "new-workspace"):
			return "OK workspace:9", nil
		}
		return "", nil
	}}
	stub.install(t)

	ref, err := createCmuxWorkspace(context.Background(), Worktree{Name: "w", Path: "/tmp/w"}, &AgentTool{Name: "claude", Command: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "workspace:9" {
		t.Fatalf("ref = %q, want workspace:9", ref)
	}
	if got := argAfter(stub.calls[1].Args, "--command"); got != "claude" {
		t.Fatalf("fallback --command = %q, want claude", got)
	}
	surface := stub.find("new-surface")
	if surface == nil || argAfter(surface, "--workspace") != "workspace:9" {
		t.Fatalf("shell tab not added to the new workspace: %v", stub.calls)
	}
}

// A failure of cmux itself is reported to the user instead of being swallowed.
func TestOpenAgentInCmuxReportsFailure(t *testing.T) {
	stub := &cmuxStub{respond: func(args []string) (string, error) { return "", errors.New("access denied") }}
	stub.install(t)

	err := openAgentInCmux(context.Background(), Worktree{Name: "w", Path: "/tmp/w"}, AgentTool{Name: "a", Command: "a"})
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("error = %v, want access denied", err)
	}
}

// A new workspace gets the linked PR attached (resolved from the worktree
// directory) and a JIRA pill when the branch carries a ticket key.
func TestCmuxWorkspaceDecorationLinksPRAndTicket(t *testing.T) {
	stub := &cmuxStub{}
	stub.install(t)

	wt := Worktree{Name: "w", Path: "/tmp/w", Branch: "feature/app-42-thing", PR: &PullRequest{Number: 17}}
	decorateCmuxWorkspace(context.Background(), "workspace:5", wt)

	status := stub.find("set-status")
	if status == nil || status[2] != "APP-42" || argAfter(status, "--workspace") != "workspace:5" {
		t.Fatalf("jira status call = %v", status)
	}
	var prCall *cmuxCall
	for i := range stub.calls {
		if slices.Contains(stub.calls[i].Args, "pr") {
			prCall = &stub.calls[i]
		}
	}
	if prCall == nil || prCall.Dir != "/tmp/w" || prCall.Args[1] != "17" || argAfter(prCall.Args, "--workspace") != "workspace:5" {
		t.Fatalf("pr call = %+v", prCall)
	}
}

// Worktrees without a PR or ticket key leave the sidebar untouched.
func TestCmuxWorkspaceDecorationSkipsMissingMetadata(t *testing.T) {
	stub := &cmuxStub{}
	stub.install(t)

	decorateCmuxWorkspace(context.Background(), "workspace:5", Worktree{Name: "w", Path: "/tmp/w", Branch: "main"})
	if len(stub.calls) != 0 {
		t.Fatalf("unexpected cmux calls: %v", stub.calls)
	}
}

// The first URL opens a browser split; later URLs navigate that split, and a
// closed split is replaced by a fresh one.
func TestOpenURLInCmuxReusesBrowserSurface(t *testing.T) {
	stub := &cmuxStub{respond: func(args []string) (string, error) {
		if slices.Contains(args, "open") {
			return "OK surface=surface:12 pane=pane:3 placement=split", nil
		}
		return "OK", nil
	}}
	stub.install(t)
	ctx := context.Background()

	if err := openURLInCmux(ctx, "https://example.test/pr/1"); err != nil {
		t.Fatal(err)
	}
	if err := openURLInCmux(ctx, "https://example.test/pr/2"); err != nil {
		t.Fatal(err)
	}
	last := stub.calls[len(stub.calls)-1].Args
	if last[1] != "--surface" || last[2] != "surface:12" || last[3] != "navigate" || last[4] != "https://example.test/pr/2" {
		t.Fatalf("second call = %v, want navigate on surface:12", last)
	}

	stub.respond = func(args []string) (string, error) {
		if slices.Contains(args, "navigate") {
			return "", errors.New("surface gone")
		}
		return "OK surface=surface:13", nil
	}
	if err := openURLInCmux(ctx, "https://example.test/pr/3"); err != nil {
		t.Fatal(err)
	}
	if cmuxBrowserSurface.ref != "surface:13" {
		t.Fatalf("browser surface = %q, want surface:13", cmuxBrowserSurface.ref)
	}
}

// Outside cmux notifications are a silent no-op and never touch the cmux CLI.
func TestNotifyCmuxOutsideCmuxIsNoop(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "")
	t.Setenv("CMUX_SURFACE_ID", "")
	stub := &cmuxStub{}
	stub.install(t)

	if err := notifyCmux(context.Background(), "t", "b"); err != nil {
		t.Fatal(err)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("cmux called outside cmux: %v", stub.calls)
	}
}

// Inside cmux a notification is forwarded with its title and body.
func TestNotifyCmuxSendsNotification(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "abc")
	stub := &cmuxStub{}
	stub.install(t)

	if err := notifyCmux(context.Background(), "PR approved", "Approved PR #3"); err != nil {
		t.Fatal(err)
	}
	args := stub.find("notify")
	if argAfter(args, "--title") != "PR approved" || argAfter(args, "--body") != "Approved PR #3" {
		t.Fatalf("notify args = %v", args)
	}
}

// Inside cmux, opening a PR or ticket URL goes to a cmux browser split rather
// than the system browser.
func TestOpenBrowserInsideCmuxUsesCmuxBrowser(t *testing.T) {
	t.Setenv("CMUX_WORKSPACE_ID", "abc")
	stub := &cmuxStub{respond: func(args []string) (string, error) { return "OK surface=surface:4", nil }}
	stub.install(t)

	if err := openBrowser(context.Background(), "https://example.test/pr/9"); err != nil {
		t.Fatal(err)
	}
	args := stub.find("open")
	if len(args) < 3 || args[0] != "browser" || args[2] != "https://example.test/pr/9" {
		t.Fatalf("browser call = %v", args)
	}
}

package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
)

type fakeRexSession struct {
	ID    string
	Label string
	// Cwd is where the session's terminal runs; "" means it reports no directory.
	Cwd string
}

// fakeRex answers rex CLI calls from in-memory sessions, so tests can check
// both the calls made and the state they leave behind.
type fakeRex struct {
	mu       sync.Mutex
	calls    [][]string
	sessions []fakeRexSession
	// ownOwner is the app owning wt-manager's own pane; "" means none does.
	ownOwner   string
	failNew    error
	failSend   error
	failSelect error
	selected   []string
}

func (f *fakeRex) install(t *testing.T, ownPane string) {
	t.Helper()
	originalRun, originalPane := runRex, rexOwnPane
	runRex = f.run
	rexOwnPane = func() string { return ownPane }
	t.Cleanup(func() { runRex, rexOwnPane = originalRun, originalPane })
}

func (f *fakeRex) run(_ context.Context, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	client := ""
	if len(args) > 1 && args[0] == "-C" {
		client, args = args[1], args[2:]
	}
	if len(args) > 1 && args[0] == "block" && args[1] == "ls" && argAfter(args, "-s") == "block:own" {
		return fmt.Sprintf(`{"blocks":[{"block_id":"block:other","owner":"client:elsewhere"},{"block_id":"block:own","creator_name":"com.superlogical.terminal","owner":%q}]}`, f.ownOwner), nil
	}
	switch args[0] {
	case "ls":
		type entry struct {
			SessionID string `json:"session_id"`
			Label     string `json:"label"`
		}
		listing := struct {
			Sessions []entry `json:"sessions"`
		}{Sessions: []entry{}}
		for _, s := range f.sessions {
			listing.Sessions = append(listing.Sessions, entry{SessionID: s.ID, Label: s.Label})
		}
		encoded, _ := json.Marshal(listing)
		return string(encoded), nil
	case "new":
		if f.failNew != nil {
			return "", f.failNew
		}
		id := fmt.Sprintf("session:%d", len(f.sessions)+1)
		f.sessions = append(f.sessions, fakeRexSession{ID: id, Label: args[1], Cwd: argAfter(args, "--cwd")})
		return fmt.Sprintf(`{"session_id":%q,"initial_windows":[{"window_id":"window:%[1]s","block_ids":["block:%[1]s"]}]}`, id), nil
	case "block":
		session := f.session(argAfter(args, "-s"))
		if session == nil {
			return "", errors.New("session not found")
		}
		if args[1] == "ls" {
			return fmt.Sprintf(`{"blocks":[{"block_id":"block:%s","creator_name":"com.superlogical.terminal"}]}`, session.ID), nil
		}
		if session.Cwd == "" {
			return `{}`, nil
		}
		return fmt.Sprintf(`{"foreground":{"name":"zsh","cwd":%q}}`, session.Cwd), nil
	case "send", "send-key":
		return "", f.failSend
	case "do":
		if f.failSelect != nil {
			return "", f.failSelect
		}
		f.selected = append(f.selected, client+"|"+strings.TrimPrefix(args[2], "session_id="))
		return "", nil
	}
	return "", fmt.Errorf("unexpected rex call %q", args)
}

func (f *fakeRex) session(id string) *fakeRexSession {
	for i := range f.sessions {
		if f.sessions[i].ID == id {
			return &f.sessions[i]
		}
	}
	return nil
}

func (f *fakeRex) callsTo(verb string) [][]string {
	var found [][]string
	for _, call := range f.calls {
		if len(call) > 0 && call[0] == verb {
			found = append(found, call)
		}
	}
	return found
}

// Rex is recognised from its per-pane REX_BLOCK, which survives tmux, or from
// TERM_PROGRAM=rex. It wins over cmux variables a Rex server may have inherited,
// and REX_SERVER alone (just an address) does not count as running inside Rex.
func TestDetectTerminalAppRecognizesRex(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want terminalApp
	}{
		{name: "term program", env: map[string]string{"TERM_PROGRAM": "rex"}, want: terminalRex},
		{name: "pane id under tmux", env: map[string]string{"TERM_PROGRAM": "tmux", "REX_BLOCK": "block:1"}, want: terminalRex},
		{name: "pane id beats inherited cmux vars", env: map[string]string{"REX_BLOCK": "block:1", "CMUX_WORKSPACE_ID": "abc", "TERM_PROGRAM": "ghostty"}, want: terminalRex},
		{name: "pane id beats inherited ghostty vars", env: map[string]string{"REX_BLOCK": "block:1", "GHOSTTY_RESOURCES_DIR": "/x"}, want: terminalRex},
		{name: "server address alone is not enough", env: map[string]string{"TERM_PROGRAM": "ghostty", "REX_SERVER": "unix:///tmp/rex.sock"}, want: terminalGhostty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectTerminalApp(func(key string) string { return tc.env[key] }); got != tc.want {
				t.Fatalf("detectTerminalApp() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Opening an agent for a worktree without a Rex session creates a session named
// after the worktree, rooted at it, with one window named after the agent. The
// agent is typed into that window's shell so the shell remains when it exits,
// and the Rex app showing wt-manager switches to the new session.
func TestOpenAgentInRexCreatesWorktreeSession(t *testing.T) {
	rex := &fakeRex{ownOwner: "client:mac"}
	rex.install(t, "block:own")

	wt := core.Worktree{Name: "APP-1-fix", Path: "/work/wt/APP-1-fix"}
	if err := openAgentInRex(context.Background(), wt, core.AgentTool{Name: "claude", Command: "claude --resume"}); err != nil {
		t.Fatalf("openAgentInRex() error = %v", err)
	}

	want := [][]string{
		{"new", "APP-1-fix", "--cwd", "/work/wt/APP-1-fix", "--window", "claude", "--json"},
		{"send", "-s", "session:1", "-b", "block:session:1", "claude --resume"},
		{"send-key", "-s", "session:1", "-b", "block:session:1", "enter"},
	}
	var made [][]string
	for _, call := range rex.calls {
		if call[0] == "new" || strings.HasPrefix(call[0], "send") {
			made = append(made, call)
		}
	}
	if !slices.EqualFunc(made, want, slices.Equal[[]string]) {
		t.Fatalf("rex calls = %q, want %q", made, want)
	}
	if !slices.Equal(rex.selected, []string{"client:mac|session:1"}) {
		t.Fatalf("selected = %q, want the new session in the app", rex.selected)
	}
}

// Opening a terminal creates the worktree's session with a single shell window
// and types nothing into it.
func TestOpenTerminalInRexCreatesShellSession(t *testing.T) {
	rex := &fakeRex{ownOwner: "client:mac"}
	rex.install(t, "block:own")

	if err := openTerminalInRex(context.Background(), core.Worktree{Name: "feature", Path: "/work/feature"}); err != nil {
		t.Fatal(err)
	}
	if got := argAfter(rex.callsTo("new")[0], "--window"); got != "shell" {
		t.Fatalf("window = %q, want shell", got)
	}
	if sends := rex.callsTo("send"); sends != nil {
		t.Fatalf("typed into a terminal session: %q", sends)
	}
	if !slices.Equal(rex.selected, []string{"client:mac|session:1"}) {
		t.Fatalf("selected = %q", rex.selected)
	}
}

// A worktree that already has its session is switched to, even when its shell
// has moved into a subdirectory; no second session is created and the agent is
// not typed into it again.
func TestOpenAgentInRexReusesExistingSession(t *testing.T) {
	rex := &fakeRex{
		ownOwner: "client:mac",
		sessions: []fakeRexSession{
			{ID: "session:main", Label: "Onboarding", Cwd: "/work/repo"},
			{ID: "session:wt", Label: "APP-1-fix", Cwd: "/work/wt/APP-1-fix/internal"},
		},
	}
	rex.install(t, "block:own")

	wt := core.Worktree{Name: "APP-1-fix", Path: "/work/wt/APP-1-fix"}
	if err := openAgentInRex(context.Background(), wt, core.AgentTool{Name: "claude", Command: "claude"}); err != nil {
		t.Fatal(err)
	}
	if rex.callsTo("new") != nil || rex.callsTo("send") != nil {
		t.Fatalf("created or typed into a session: %q", rex.calls)
	}
	if !slices.Equal(rex.selected, []string{"client:mac|session:wt"}) {
		t.Fatalf("selected = %q, want the existing session", rex.selected)
	}
}

// A same-named session whose terminal runs in another repository's directory
// belongs to that repository, so the worktree gets a session of its own.
func TestOpenAgentInRexIgnoresSameNamedSessionElsewhere(t *testing.T) {
	rex := &fakeRex{
		ownOwner: "client:mac",
		sessions: []fakeRexSession{{ID: "session:other", Label: "feature", Cwd: "/elsewhere/feature"}},
	}
	rex.install(t, "block:own")

	if err := openTerminalInRex(context.Background(), core.Worktree{Name: "feature", Path: "/work/feature"}); err != nil {
		t.Fatal(err)
	}
	if len(rex.callsTo("new")) != 1 || !slices.Equal(rex.selected, []string{"client:mac|session:2"}) {
		t.Fatalf("calls = %q, selected = %q, want a new session", rex.calls, rex.selected)
	}
}

// A session whose terminal reports no directory yet, such as one whose shell is
// still starting, is trusted by its name rather than duplicated.
func TestOpenAgentInRexTrustsNameWhenDirectoryUnknown(t *testing.T) {
	rex := &fakeRex{
		ownOwner: "client:mac",
		sessions: []fakeRexSession{{ID: "session:starting", Label: "feature"}},
	}
	rex.install(t, "block:own")

	if err := openTerminalInRex(context.Background(), core.Worktree{Name: "feature", Path: "/work/feature"}); err != nil {
		t.Fatal(err)
	}
	if rex.callsTo("new") != nil || !slices.Equal(rex.selected, []string{"client:mac|session:starting"}) {
		t.Fatalf("calls = %q, selected = %q, want the existing session", rex.calls, rex.selected)
	}
}

// Pressing the key twice in quick succession opens one session, not two, even
// when both opens run at the same time.
func TestOpenAgentInRexConcurrentOpensCreateOneSession(t *testing.T) {
	rex := &fakeRex{ownOwner: "client:mac"}
	rex.install(t, "block:own")

	wt := core.Worktree{Name: "quick", Path: "/work/quick"}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := openAgentInRex(context.Background(), wt, core.AgentTool{Name: "claude", Command: "claude"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := len(rex.callsTo("new")); got != 1 {
		t.Fatalf("created %d sessions, want 1", got)
	}
	if got := len(rex.callsTo("send")); got != 1 {
		t.Fatalf("typed the agent %d times, want 1", got)
	}
}

// When the Rex app refuses to switch sessions (Remote Control is off), the
// session is still created, and the error passes on Rex's own explanation.
func TestOpenAgentInRexReportsRefusedSwitch(t *testing.T) {
	refusal := errors.New("Rex.app is not accepting actions. Turn on Remote Control in its Settings under General.")
	rex := &fakeRex{ownOwner: "client:mac", failSelect: refusal}
	rex.install(t, "block:own")

	wt := core.Worktree{Name: "feature", Path: "/work/feature"}
	err := openAgentInRex(context.Background(), wt, core.AgentTool{Name: "claude", Command: "claude"})
	if err == nil || !strings.Contains(err.Error(), "opened Rex session feature but could not switch to it") || !strings.Contains(err.Error(), "Turn on Remote Control") {
		t.Fatalf("error = %v, want the refused switch explained", err)
	}
	if len(rex.sessions) != 1 || rex.callsTo("send") == nil {
		t.Fatalf("session not prepared before the switch: %+v", rex.sessions)
	}

	err = openAgentInRex(context.Background(), wt, core.AgentTool{Name: "claude", Command: "claude"})
	if err == nil || !strings.Contains(err.Error(), "found Rex session feature but could not switch to it") {
		t.Fatalf("second error = %v, want the existing session named", err)
	}
	if len(rex.sessions) != 1 {
		t.Fatalf("created %d sessions, want the first one reused", len(rex.sessions))
	}
}

// With several Rex apps connected, only the one that owns wt-manager's pane,
// that is the app the user is looking at wt-manager in, switches sessions.
func TestShowRexSessionSwitchesAppShowingWtManager(t *testing.T) {
	rex := &fakeRex{ownOwner: "client:ipad"}
	rex.install(t, "block:own")

	if err := showRexSession(context.Background(), "session:7"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rex.selected, []string{"client:ipad|session:7"}) {
		t.Fatalf("selected = %q, want only the app owning wt-manager's pane", rex.selected)
	}
}

// When no app owns wt-manager's pane, or wt-manager does not know its pane, it
// lets Rex pick the app, which Rex does when only one is connected.
func TestShowRexSessionWithoutOwnerLetsRexPickApp(t *testing.T) {
	for _, pane := range []string{"block:own", ""} {
		rex := &fakeRex{}
		rex.install(t, pane)

		if err := showRexSession(context.Background(), "session:7"); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(rex.selected, []string{"|session:7"}) {
			t.Fatalf("pane %q: selected = %q, want Rex to pick the app", pane, rex.selected)
		}
	}
}

// A session Rex refuses to create is reported, and nothing is typed or switched.
func TestOpenAgentInRexReportsSessionFailure(t *testing.T) {
	rex := &fakeRex{ownOwner: "client:mac", failNew: errors.New("server unavailable")}
	rex.install(t, "block:own")

	err := openAgentInRex(context.Background(), core.Worktree{Name: "w", Path: "/work/w"}, core.AgentTool{Name: "claude", Command: "claude"})
	if err == nil || !strings.Contains(err.Error(), "open Rex session failed") || !strings.Contains(err.Error(), "server unavailable") {
		t.Fatalf("error = %v, want the Rex failure", err)
	}
	if rex.callsTo("send") != nil || rex.selected != nil {
		t.Fatalf("acted after the session failed: %q", rex.calls)
	}
}

// When the session opens but the agent command cannot be typed into it, the
// user is told which agent did not start instead of finding a bare shell.
func TestOpenAgentInRexReportsAgentStartFailure(t *testing.T) {
	rex := &fakeRex{ownOwner: "client:mac", failSend: errors.New("block not found")}
	rex.install(t, "block:own")

	err := openAgentInRex(context.Background(), core.Worktree{Name: "w", Path: "/work/w"}, core.AgentTool{Name: "claude", Command: "claude"})
	if err == nil || !strings.Contains(err.Error(), "could not start claude") || !strings.Contains(err.Error(), "block not found") {
		t.Fatalf("error = %v, want an agent start failure", err)
	}
}

// Rex replies are read with stderr mixed in, so a stray message before the JSON
// does not hide the reply.
func TestDecodeRexJSONSkipsLeadingNoise(t *testing.T) {
	var reply struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeRexJSON("warning: something\n{\"session_id\":\"session:1\"}\n", &reply); err != nil || reply.SessionID != "session:1" {
		t.Fatalf("decodeRexJSON() = %+v, %v", reply, err)
	}
	if err := decodeRexJSON("OK", &reply); err == nil {
		t.Fatal("decodeRexJSON(no JSON) succeeded")
	}
}

// Inside Rex, the agent and terminal actions go through the Rex CLI instead of
// falling through to Ghostty's keystroke automation.
func TestOpenWorktreeRoutesToRex(t *testing.T) {
	original := currentTerminalApp
	currentTerminalApp = func() terminalApp { return terminalRex }
	t.Cleanup(func() { currentTerminalApp = original })
	rex := &fakeRex{ownOwner: "client:mac"}
	rex.install(t, "block:own")

	if err := openWorktreeAgent(context.Background(), core.Worktree{Name: "a", Path: "/work/a"}, core.AgentTool{Name: "claude", Command: "claude"}); err != nil {
		t.Fatal(err)
	}
	if err := openWorktreeTerminal(context.Background(), core.Worktree{Name: "b", Path: "/work/b"}); err != nil {
		t.Fatal(err)
	}
	if len(rex.callsTo("new")) != 2 || len(rex.selected) != 2 {
		t.Fatalf("calls = %q, want a session opened and shown for each", rex.calls)
	}
}

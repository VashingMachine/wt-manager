package services

import (
	"strings"
	"testing"
)

func TestDetectTerminalApp(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want terminalApp
	}{
		{name: "ghostty term program", env: map[string]string{"TERM_PROGRAM": "ghostty"}, want: terminalGhostty},
		{name: "ghostty under tmux", env: map[string]string{"TERM_PROGRAM": "tmux", "GHOSTTY_RESOURCES_DIR": "/Applications/Ghostty.app/Contents/Resources"}, want: terminalGhostty},
		{name: "ghostty bin dir", env: map[string]string{"GHOSTTY_BIN_DIR": "/opt/ghostty/bin"}, want: terminalGhostty},
		{name: "warp", env: map[string]string{"TERM_PROGRAM": "WarpTerminal"}, want: terminalWarp},
		{name: "apple terminal", env: map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, want: terminalAppleTerminal},
		{name: "iterm falls back", env: map[string]string{"TERM_PROGRAM": "iTerm.app"}, want: terminalGhostty},
		{name: "vscode falls back", env: map[string]string{"TERM_PROGRAM": "vscode"}, want: terminalGhostty},
		{name: "bare tmux falls back", env: map[string]string{"TERM_PROGRAM": "tmux"}, want: terminalGhostty},
		{name: "empty env falls back", env: map[string]string{}, want: terminalGhostty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectTerminalApp(func(key string) string { return tc.env[key] })
			if got != tc.want {
				t.Fatalf("detectTerminalApp() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWarpNewTabURL(t *testing.T) {
	got := warpNewTabURL("/tmp/worktree with space")
	want := "warp://action/new_tab?path=%2Ftmp%2Fworktree+with+space"
	if got != want {
		t.Fatalf("warpNewTabURL() = %q, want %q", got, want)
	}
}

func TestTerminalTabScript(t *testing.T) {
	for _, app := range []string{"Ghostty", "Warp"} {
		script := terminalTabScript(app, "cd '/tmp/worktree' && copilot")
		for _, want := range []string{"tell application \"" + app + "\" to activate", "tell process \"" + app + "\"", "keystroke \"t\" using command down", "keystroke commandText", "key code 36"} {
			if !strings.Contains(script, want) {
				t.Fatalf("terminalTabScript(%q) missing %q in %q", app, want, script)
			}
		}
		if !strings.Contains(script, "copilot") {
			t.Fatalf("terminalTabScript(%q) should include selected agent command in %q", app, script)
		}
	}
}

func TestAppleTerminalTabScript(t *testing.T) {
	script := appleTerminalTabScript("cd '/tmp/worktree' && copilot")
	for _, want := range []string{"tell application \"Terminal\" to activate", "count of windows", "keystroke \"t\" using command down", "do script commandText in selected tab of front window", "copilot"} {
		if !strings.Contains(script, want) {
			t.Fatalf("appleTerminalTabScript() missing %q in %q", want, script)
		}
	}
}

func TestAppleTerminalWindowScript(t *testing.T) {
	script := appleTerminalWindowScript("cd '/tmp/worktree' && copilot")
	for _, want := range []string{"tell application \"Terminal\"", "do script", "copilot"} {
		if !strings.Contains(script, want) {
			t.Fatalf("appleTerminalWindowScript() missing %q in %q", want, script)
		}
	}
	if strings.Contains(script, "System Events") {
		t.Fatalf("appleTerminalWindowScript() must not require System Events: %q", script)
	}
}

func TestGhosttyTerminalArgs(t *testing.T) {
	args := ghosttyTerminalArgs(Worktree{Path: "/tmp/worktree with space"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"-na", "Ghostty.app", "--args", "--working-directory=/tmp/worktree with space"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("ghosttyTerminalArgs() missing %q in %#v", want, args)
		}
	}
	for _, unwanted := range []string{"codex", "opencode", "claude", "copilot", "-e"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("ghosttyTerminalArgs() should not include %q in %#v", unwanted, args)
		}
	}
}

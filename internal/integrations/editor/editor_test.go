package editor

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/VashingMachine/wt-manager/internal/core"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

// IntelliJ still opens on macOS when an installed Toolbox launcher is stale or the CLI is missing.
func TestOpenEditorWorktreeIntelliJFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("IntelliJ application fallback is macOS only")
	}
	for _, brokenCLI := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "stale Toolbox launcher"}[brokenCLI], func(t *testing.T) {
			bin := t.TempDir()
			argsPath := filepath.Join(bin, "arguments")
			if brokenCLI {
				testkit.WriteExecutable(t, filepath.Join(bin, "idea"), "#!/bin/sh\necho 'Unable to find stale Toolbox application' >&2\nexit 1\n")
			}
			testkit.WriteExecutable(t, filepath.Join(bin, "open"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
			t.Setenv("PATH", bin)
			t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
			wt := core.Worktree{Path: "/tmp/project with 'quotes' & spaces"}
			if err := openEditorWorktree(context.Background(), wt, core.EditorTool{Name: "IntelliJ IDEA", Command: "idea"}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(data), "-a\nIntelliJ IDEA\n"+wt.Path+"\n"; got != want {
				t.Fatalf("IntelliJ application arguments = %q, want %q", got, want)
			}
		})
	}
}

// Failed IntelliJ CLI and macOS fallback attempts retain both errors for investigation.
func TestOpenEditorWorktreeIntelliJFallbackErrors(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("IntelliJ application fallback is macOS only")
	}
	bin := t.TempDir()
	testkit.WriteExecutable(t, filepath.Join(bin, "idea"), "#!/bin/sh\necho 'stale launcher path' >&2\nexit 1\n")
	testkit.WriteExecutable(t, filepath.Join(bin, "open"), "#!/bin/sh\necho 'application not installed' >&2\nexit 1\n")
	t.Setenv("PATH", bin)
	err := openEditorWorktree(context.Background(), core.Worktree{Path: "/tmp/project"}, core.EditorTool{Name: "IntelliJ IDEA", Command: "idea"})
	for _, want := range []string{"IntelliJ IDEA", "stale launcher path", "application not installed"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

// The default editor continues opening VS Code in a new window with an unmodified worktree path.
func TestOpenEditorWorktreeDefault(t *testing.T) {
	bin := t.TempDir()
	argsPath := filepath.Join(bin, "arguments")
	testkit.WriteExecutable(t, filepath.Join(bin, "code"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
	t.Setenv("PATH", bin)
	t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
	wt := core.Worktree{Path: "/tmp/my worktree"}
	if err := openEditorWorktree(context.Background(), wt, core.EditorTool{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "--new-window\n"+wt.Path+"\n"; got != want {
		t.Fatalf("VS Code arguments = %q, want %q", got, want)
	}
}

// VS Code still falls back to the macOS application launcher when its CLI is unavailable.
func TestOpenEditorWorktreeVSCodeFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("VS Code application fallback is macOS only")
	}
	bin := t.TempDir()
	argsPath := filepath.Join(bin, "arguments")
	testkit.WriteExecutable(t, filepath.Join(bin, "open"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
	t.Setenv("PATH", bin)
	t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
	wt := core.Worktree{Path: "/tmp/my worktree"}
	if err := openEditorWorktree(context.Background(), wt, core.DefaultAppConfig().Editor); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "-a\nVisual Studio Code\n"+wt.Path+"\n"; got != want {
		t.Fatalf("application launcher arguments = %q, want %q", got, want)
	}
}

// Invalid or failed editor commands return a useful error containing the configured editor name.
func TestOpenEditorWorktreeError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		editor core.EditorTool
		want   string
	}{
		{name: "blank", editor: core.EditorTool{Name: "IntelliJ IDEA", Command: " "}, want: `editor "IntelliJ IDEA" command is required`},
		{name: "failed", editor: core.EditorTool{Name: "Custom IDE", Command: "exit 1;"}, want: "open Custom IDE:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := openEditorWorktree(context.Background(), core.Worktree{Path: "/tmp/worktree"}, tc.editor); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("openEditorWorktree() error = %v, want %q", err, tc.want)
			}
		})
	}
}

// Assures that the config file is opened in the editor as a single file, keeping VS Code in its current window.
func TestOpenEditorFileOpensConfigPath(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(bin, "args.log")
	testkit.WriteExecutable(t, filepath.Join(bin, "code"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+logPath+"\n")
	testkit.WriteExecutable(t, filepath.Join(bin, "custom-editor"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > "+logPath+"\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	config := filepath.Join(t.TempDir(), "my config.json")

	if err := openEditorFile(context.Background(), config, core.EditorTool{Name: "VS Code", Command: "code"}); err != nil {
		t.Fatalf("openEditorFile(code) error = %v", err)
	}
	if data, _ := os.ReadFile(logPath); strings.TrimSpace(string(data)) != config {
		t.Fatalf("code args = %q, want only the config path", data)
	}
	if err := openEditorFile(context.Background(), config, core.EditorTool{Name: "Custom", Command: "custom-editor --wait"}); err != nil {
		t.Fatalf("openEditorFile(custom) error = %v", err)
	}
	if data, _ := os.ReadFile(logPath); strings.TrimSpace(string(data)) != "--wait\n"+config {
		t.Fatalf("custom args = %q, want flags then the config path", data)
	}
}

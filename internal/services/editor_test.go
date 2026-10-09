package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/VashingMachine/wt-manager/internal/core"
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
				writeExecutable(t, filepath.Join(bin, "idea"), "#!/bin/sh\necho 'Unable to find stale Toolbox application' >&2\nexit 1\n")
			}
			writeExecutable(t, filepath.Join(bin, "open"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
			t.Setenv("PATH", bin)
			t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
			wt := Worktree{Path: "/tmp/project with 'quotes' & spaces"}
			if err := openEditorWorktree(context.Background(), wt, EditorTool{Name: "IntelliJ IDEA", Command: "idea"}); err != nil {
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
	writeExecutable(t, filepath.Join(bin, "idea"), "#!/bin/sh\necho 'stale launcher path' >&2\nexit 1\n")
	writeExecutable(t, filepath.Join(bin, "open"), "#!/bin/sh\necho 'application not installed' >&2\nexit 1\n")
	t.Setenv("PATH", bin)
	err := openEditorWorktree(context.Background(), Worktree{Path: "/tmp/project"}, EditorTool{Name: "IntelliJ IDEA", Command: "idea"})
	for _, want := range []string{"IntelliJ IDEA", "stale launcher path", "application not installed"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
}

// The real backend profile can open in the configured IntelliJ installation when live testing is requested.
func TestOpenEditorWorktreeLiveBackendIntelliJ(t *testing.T) {
	if os.Getenv("WT_MANAGER_LIVE_IDEA") != "1" {
		t.Skip("set WT_MANAGER_LIVE_IDEA=1 to open the real backend profile in IntelliJ")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("live IntelliJ check is macOS only")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".wt-manager.json"))
	if err != nil {
		t.Fatal(err)
	}
	var app AppConfig
	if err := json.Unmarshal(data, &app); err != nil {
		t.Fatal(err)
	}
	profile := findProfile(app.Profiles, "backend")
	if profile == nil {
		t.Fatal("backend profile missing")
	}
	path := resolveProfilePaths(*profile, home).RepositoryPath
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := NewService()
	if err := s.OpenEditorWorktree(ctx, Worktree{Name: "backend", Path: path}, EditorTool{Name: "IntelliJ IDEA", Command: "idea"}); err != nil {
		t.Fatal(err)
	}
	logPath, content, err := s.ReadSessionLog()
	if err != nil || !strings.Contains(content, path) || !strings.Contains(content, "SUCCESS") {
		t.Fatalf("live launch diagnostics missing: %v\n%s", err, content)
	}
	t.Logf("Backend launch diagnostics: %s", logPath)
}

// New installations use VS Code and keep the existing Enter action default.
func TestDefaultAppConfigEditor(t *testing.T) {
	app := defaultAppConfig()
	if want := (EditorTool{Name: "VS Code", Command: "code"}); app.Editor != want {
		t.Fatalf("Editor = %#v, want %#v", app.Editor, want)
	}
	if app.DefaultEnterAction != core.EnterActionVSCode {
		t.Fatalf("DefaultEnterAction = %q, want vscode", app.DefaultEnterAction)
	}
}

// Editor settings normalize whitespace, supply missing defaults and labels, and reject a named editor without a command.
func TestNormalizeAppConfigEditor(t *testing.T) {
	for _, tc := range []struct {
		name    string
		editor  EditorTool
		want    EditorTool
		changed bool
		wantErr bool
	}{
		{name: "missing", want: EditorTool{Name: "VS Code", Command: "code"}, changed: true},
		{name: "empty", editor: EditorTool{Name: " ", Command: "\t"}, want: EditorTool{Name: "VS Code", Command: "code"}, changed: true},
		{name: "configured", editor: EditorTool{Name: "IntelliJ IDEA", Command: "idea --new-window"}, want: EditorTool{Name: "IntelliJ IDEA", Command: "idea --new-window"}},
		{name: "trimmed", editor: EditorTool{Name: " Cursor ", Command: " cursor --reuse-window "}, want: EditorTool{Name: "Cursor", Command: "cursor --reuse-window"}, changed: true},
		{name: "command only", editor: EditorTool{Command: "idea"}, want: EditorTool{Name: "IntelliJ IDEA", Command: "idea"}, changed: true},
		{name: "custom command only", editor: EditorTool{Command: "my-ide --new-window"}, want: EditorTool{Name: "my-ide --new-window", Command: "my-ide --new-window"}, changed: true},
		{name: "missing command", editor: EditorTool{Name: "IntelliJ IDEA"}, wantErr: true},
		{name: "blank command", editor: EditorTool{Name: "Custom IDE", Command: " \t "}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := defaultAppConfig()
			app.Editor = tc.editor
			got, changed, err := normalizeAppConfig(app)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "command is required") {
					t.Fatalf("normalizeAppConfig() error = %v, want missing editor command", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeAppConfig() error = %v", err)
			}
			if got.Editor != tc.want || changed != tc.changed {
				t.Fatalf("Editor = %#v, changed = %v; want %#v, %v", got.Editor, changed, tc.want, tc.changed)
			}
			if _, changedAgain, err := normalizeAppConfig(got); err != nil || changedAgain {
				t.Fatalf("second normalization changed = %v, error = %v; want false, nil", changedAgain, err)
			}
		})
	}
}

// Loading legacy configs persists the editor default while preserving the legacy vscode Enter action.
func TestLoadAppConfigMigratesEditor(t *testing.T) {
	homeDir := t.TempDir()
	configPath := filepath.Join(homeDir, ".wt-manager.json")
	if err := os.WriteFile(configPath, []byte(`{"defaultEnterAction":"vscode"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	app, _, existed, err := loadAppConfig(homeDir)
	if err != nil || !existed {
		t.Fatalf("loadAppConfig() existed = %v, error = %v", existed, err)
	}
	if app.Editor != defaultAppConfig().Editor || app.DefaultEnterAction != core.EnterActionVSCode {
		t.Fatalf("migrated config = %#v", app)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	var editor EditorTool
	if err := json.Unmarshal(saved["editor"], &editor); err != nil || editor != app.Editor {
		t.Fatalf("saved editor = %#v, error = %v", editor, err)
	}
}

// Custom IDE names and shell commands survive writing and loading configuration.
func TestAppConfigEditorRoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	app := defaultAppConfig()
	app.Editor = EditorTool{Name: "Custom IDE", Command: `"/Applications/My IDE.app/Contents/MacOS/ide" --new-window`}
	if err := writeAppConfig(filepath.Join(homeDir, ".wt-manager.json"), app); err != nil {
		t.Fatal(err)
	}
	loaded, _, _, err := loadAppConfig(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Editor != app.Editor {
		t.Fatalf("loaded Editor = %#v, want %#v", loaded.Editor, app.Editor)
	}
}

// Configured IDE commands preserve flags, quoted executable paths, and worktree paths as a single literal argument.
func TestOpenEditorWorktreeCustomCommand(t *testing.T) {
	bin := t.TempDir()
	executable := filepath.Join(bin, "custom IDE")
	argsPath := filepath.Join(bin, "arguments")
	writeExecutable(t, executable, "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
	t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
	worktreePath := filepath.Join(bin, "worktree with 'quotes'; $(touch unexpected) & spaces")
	editor := EditorTool{Name: "Custom IDE", Command: shellQuote(executable) + ` --new-window --title 'my worktree'`}
	if err := NewService().OpenEditorWorktree(context.Background(), Worktree{Path: worktreePath}, editor); err != nil {
		t.Fatalf("OpenEditorWorktree() error = %v", err)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	want := []string{"--new-window", "--title", "my worktree", worktreePath}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDE arguments = %#v, want %#v", got, want)
	}
}

// The default editor continues opening VS Code in a new window with an unmodified worktree path.
func TestOpenEditorWorktreeDefault(t *testing.T) {
	bin := t.TempDir()
	argsPath := filepath.Join(bin, "arguments")
	writeExecutable(t, filepath.Join(bin, "code"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
	t.Setenv("PATH", bin)
	t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
	wt := Worktree{Path: "/tmp/my worktree"}
	if err := openEditorWorktree(context.Background(), wt, EditorTool{}); err != nil {
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
	writeExecutable(t, filepath.Join(bin, "open"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
	t.Setenv("PATH", bin)
	t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
	wt := Worktree{Path: "/tmp/my worktree"}
	if err := openEditorWorktree(context.Background(), wt, defaultAppConfig().Editor); err != nil {
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
		editor EditorTool
		want   string
	}{
		{name: "blank", editor: EditorTool{Name: "IntelliJ IDEA", Command: " "}, want: `editor "IntelliJ IDEA" command is required`},
		{name: "failed", editor: EditorTool{Name: "Custom IDE", Command: "exit 1;"}, want: "open Custom IDE:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := openEditorWorktree(context.Background(), Worktree{Path: "/tmp/worktree"}, tc.editor); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("openEditorWorktree() error = %v, want %q", err, tc.want)
			}
		})
	}
}

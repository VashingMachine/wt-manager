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
	"github.com/VashingMachine/wt-manager/internal/shell"
	"github.com/VashingMachine/wt-manager/internal/testkit"
)

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
	var app core.AppConfig
	if err := json.Unmarshal(data, &app); err != nil {
		t.Fatal(err)
	}
	profile := core.FindProfile(app.Profiles, "backend")
	if profile == nil {
		t.Fatal("backend profile missing")
	}
	path := core.ResolveProfilePaths(*profile, home).RepositoryPath
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := NewService()
	if err := s.OpenEditorWorktree(ctx, core.Worktree{Name: "backend", Path: path}, core.EditorTool{Name: "IntelliJ IDEA", Command: "idea"}); err != nil {
		t.Fatal(err)
	}
	logPath, content, err := s.ReadSessionLog()
	if err != nil || !strings.Contains(content, path) || !strings.Contains(content, "SUCCESS") {
		t.Fatalf("live launch diagnostics missing: %v\n%s", err, content)
	}
	t.Logf("Backend launch diagnostics: %s", logPath)
}

// Configured IDE commands preserve flags, quoted executable paths, and worktree paths as a single literal argument.
func TestOpenEditorWorktreeCustomCommand(t *testing.T) {
	bin := t.TempDir()
	executable := filepath.Join(bin, "custom IDE")
	argsPath := filepath.Join(bin, "arguments")
	testkit.WriteExecutable(t, executable, "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$WT_MANAGER_EDITOR_ARGS\"\n")
	t.Setenv("WT_MANAGER_EDITOR_ARGS", argsPath)
	worktreePath := filepath.Join(bin, "worktree with 'quotes'; $(touch unexpected) & spaces")
	editor := core.EditorTool{Name: "Custom IDE", Command: shell.Quote(executable) + ` --new-window --title 'my worktree'`}
	if err := NewService().OpenEditorWorktree(context.Background(), core.Worktree{Path: worktreePath}, editor); err != nil {
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

// fakeTerminal is a terminal app that may or may not have a browser of its own.
type fakeTerminal struct {
	Terminal
	hasBrowser bool
	urls       []string
}

func (f *fakeTerminal) OpenURL(_ context.Context, url string) bool {
	if f.hasBrowser {
		f.urls = append(f.urls, url)
	}
	return f.hasBrowser
}

type fakeBrowser struct {
	urls []string
}

func (f *fakeBrowser) Open(_ context.Context, url string) error {
	f.urls = append(f.urls, url)
	return nil
}

// Assures that a page opens in the terminal app's own browser when it has one, and in the system browser only when it does not.
func TestOpenBrowserPrefersTheTerminalsOwnBrowser(t *testing.T) {
	const url = "https://example.test/pr/9"
	for _, hasBrowser := range []bool{true, false} {
		terminal, browser := &fakeTerminal{hasBrowser: hasBrowser}, &fakeBrowser{}
		svc := New(Integrations{Terminal: terminal, Browser: browser}, nil)

		if err := svc.OpenBrowser(context.Background(), url); err != nil {
			t.Fatalf("OpenBrowser() error = %v", err)
		}
		inTerminal, inSystem := len(terminal.urls) == 1, len(browser.urls) == 1
		if inTerminal != hasBrowser || inSystem == hasBrowser {
			t.Fatalf("terminal has a browser = %v: opened in terminal %v, in system browser %v", hasBrowser, terminal.urls, browser.urls)
		}
	}
}

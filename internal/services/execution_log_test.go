package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Session logs retain complete stdout, stderr, command arguments, directory, and failures in a private temporary file.
func TestSessionLogRetainsFullExecutionOutput(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	s := NewService()
	dir := t.TempDir()
	output := "first line\n" + strings.Repeat("long diagnostic output\n", 150) + "last stdout line\n"
	script := filepath.Join(dir, "failing editor")
	writeExecutable(t, script, "#!/bin/sh\nprintf '%s' "+shellQuote(output)+"\necho 'last stderr line' >&2\nexit 23\n")
	_, err := runCommand(s.commandContext(context.Background()), dir, script, "project with spaces")
	if err == nil {
		t.Fatal("failed command returned no error")
	}
	path, content, err := s.ReadSessionLog()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{script, "project with spaces", dir, output, "last stderr line", "exit status 23", "START", "ERROR"} {
		if !strings.Contains(content, want) {
			t.Fatalf("session log missing %q", want)
		}
	}
	if filepath.Dir(path) != os.TempDir() {
		t.Fatalf("log path = %q, want a temporary file", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("log file permissions: info=%v error=%v", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("file does not contain the full session log: %v", err)
	}
}

// Concurrent operations retain every result and separate service sessions use different files.
func TestSessionLogConcurrentCommandsAndSessionIsolation(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	s := NewService()
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := runCommand(s.commandContext(context.Background()), "", "/bin/sh", "-c", fmt.Sprintf("echo result-%02d", i))
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	path, content, err := s.ReadSessionLog()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		if !strings.Contains(content, fmt.Sprintf("result-%02d\n", i)) {
			t.Fatalf("log lost command %d", i)
		}
	}
	if strings.Count(content, " SUCCESS ") != 12 {
		t.Fatalf("log did not retain every success:\n%s", content)
	}
	otherPath, otherContent, err := NewService().ReadSessionLog()
	if err != nil || otherPath == path || strings.Contains(otherContent, "result-") {
		t.Fatalf("session logs were mixed: path=%q err=%v", otherPath, err)
	}
}

// Public editor operations log the original launcher failure and a successful fallback automatically.
func TestSessionLogEditorFallback(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("IntelliJ application fallback is macOS only")
	}
	t.Setenv("TMPDIR", t.TempDir())
	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "idea"), "#!/bin/sh\necho 'stale Toolbox launcher' >&2\nexit 1\n")
	writeExecutable(t, filepath.Join(bin, "open"), "#!/bin/sh\necho 'application opened'\n")
	t.Setenv("PATH", bin)
	s := NewService()
	if err := s.OpenEditorWorktree(context.Background(), Worktree{Path: "/tmp/backend"}, EditorTool{Name: "IntelliJ IDEA", Command: "idea"}); err != nil {
		t.Fatal(err)
	}
	_, content, err := s.ReadSessionLog()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stale Toolbox launcher", "ERROR", "IntelliJ IDEA", "/tmp/backend", "application opened", "SUCCESS"} {
		if !strings.Contains(content, want) {
			t.Fatalf("log lost launch details %q:\n%s", want, content)
		}
	}
}

// Unavailable temporary storage reports a log error without preventing commands from running.
func TestSessionLogStorageFailureDoesNotFailCommand(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	s := NewService()
	output, err := runCommand(s.commandContext(context.Background()), "", "/bin/sh", "-c", "echo success")
	if err != nil || output != "success\n" {
		t.Fatalf("command output=%q error=%v", output, err)
	}
	if _, _, err := s.ReadSessionLog(); err == nil {
		t.Fatal("missing log storage returned no error")
	}
}

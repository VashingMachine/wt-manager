package fspath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompactPath(t *testing.T) {
	homeDir := filepath.Join(string(os.PathSeparator), "Users", "me")
	got := Compact(filepath.Join(homeDir, "projects", "repo"), homeDir)
	if got != "~/projects/repo" {
		t.Fatalf("Compact() = %q, want ~/projects/repo", got)
	}
}

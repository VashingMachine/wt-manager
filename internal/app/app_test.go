package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Assures that the JSON ticket cache files replaced by the SQLite cache are removed, and nothing else is.
func TestRemoveLegacyCacheDeletesOnlyOldTicketFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".wt-manager", "cache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	legacy := filepath.Join(dir, "jira-site-app-1-mine-sprint.json")
	kept := filepath.Join(dir, "wt-manager.db")
	for _, path := range []string{legacy, kept} {
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	removeLegacyCache(home)

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy ticket cache still exists: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("SQLite cache was removed: %v", err)
	}
}

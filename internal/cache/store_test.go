package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type sample struct {
	Name  string
	Count int
}

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache", "test.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, path
}

// Assures that a write is readable immediately from memory, before the background writer persists it.
func TestStorePutIsReadableImmediately(t *testing.T) {
	store, _ := openTemp(t)
	if err := store.Put("prs", "owner/app", sample{Name: "list", Count: 3}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	var got sample
	fetchedAt, ok := store.Get("prs", "owner/app", &got)
	if !ok || got.Count != 3 || fetchedAt.IsZero() {
		t.Fatalf("Get() = %#v fetchedAt=%v ok=%v", got, fetchedAt, ok)
	}
}

// Assures that cached entries and their fetch times survive closing and reopening the SQLite file.
func TestStorePersistsAcrossRestart(t *testing.T) {
	store, path := openTemp(t)
	fetchedAt := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	if err := store.PutAt("tickets", "board-1", sample{Name: "APP-1"}, fetchedAt); err != nil {
		t.Fatalf("PutAt() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reopened.Close()
	var got sample
	at, ok := reopened.Get("tickets", "board-1", &got)
	if !ok || got.Name != "APP-1" || !at.Equal(fetchedAt) {
		t.Fatalf("Get() after reopen = %#v at=%v ok=%v, want APP-1 at %v", got, at, ok, fetchedAt)
	}
}

// Assures that expiring keeps the value paintable but reports it as needing revalidation, also after a restart.
func TestStoreExpireKeepsValueButMarksStale(t *testing.T) {
	store, path := openTemp(t)
	_ = store.Put("tickets", "mine", sample{Name: "APP-1"})
	store.Expire("tickets", "mine")

	var got sample
	at, ok := store.Get("tickets", "mine", &got)
	if !ok || got.Name != "APP-1" || !at.IsZero() {
		t.Fatalf("Get() after Expire = %#v at=%v ok=%v", got, at, ok)
	}
	store.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reopened.Close()
	at, ok = reopened.Get("tickets", "mine", &got)
	if !ok || !at.IsZero() {
		t.Fatalf("expired entry after reopen at=%v ok=%v, want stale value", at, ok)
	}
}

// Assures that deleting an entry removes it from memory and from disk.
func TestStoreDeleteRemovesPersistedEntry(t *testing.T) {
	store, path := openTemp(t)
	_ = store.Put("detail", "42", sample{Name: "PR"})
	store.Flush()
	store.Delete("detail", "42")
	var got sample
	if _, ok := store.Get("detail", "42", &got); ok {
		t.Fatal("deleted entry is still readable")
	}
	store.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reopened.Close()
	if _, ok := reopened.Get("detail", "42", &got); ok {
		t.Fatal("deleted entry came back after reopen")
	}
}

// Assures that repeated writes to one key coalesce and the last value wins on disk.
func TestStoreLastWriteWins(t *testing.T) {
	store, path := openTemp(t)
	for i := range 50 {
		_ = store.Put("status", "/wt", sample{Count: i})
	}
	store.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reopened.Close()
	var got sample
	if _, ok := reopened.Get("status", "/wt", &got); !ok || got.Count != 49 {
		t.Fatalf("persisted value = %#v ok=%v, want the last write", got, ok)
	}
}

// Assures that the cache and its journal files are readable only by the user.
func TestStoreFilesArePrivate(t *testing.T) {
	store, path := openTemp(t)
	_ = store.Put("prs", "x", sample{Name: "private"})
	store.Flush()
	for _, file := range []string{path, path + "-wal"} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", file, err)
		}
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			t.Fatalf("%s mode = %v, want private", filepath.Base(file), mode)
		}
	}
}

// Assures that a corrupt cache file is recreated instead of disabling the app.
func TestStoreRecreatesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	if err := os.WriteFile(path, []byte("this is not a sqlite database at all, just garbage bytes"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open() on corrupt file error = %v", err)
	}
	defer store.Close()
	_ = store.Put("prs", "x", sample{Name: "ok"})
	store.Flush()
	if err := store.Err(); err != nil {
		t.Fatalf("persisting into recreated cache failed: %v", err)
	}
}

// Assures that the memory-only fallback behaves like the persistent store for reads and writes.
func TestMemoryStoreWorksWithoutDisk(t *testing.T) {
	store := NewMemory()
	_ = store.Put("prs", "x", sample{Name: "ok"})
	store.Flush()
	var got sample
	if _, ok := store.Get("prs", "x", &got); !ok || got.Name != "ok" {
		t.Fatalf("memory Get() = %#v ok=%v", got, ok)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("memory Close() error = %v", err)
	}
}

// Assures that concurrent readers and writers from background commands are safe.
func TestStoreConcurrentAccess(t *testing.T) {
	store, _ := openTemp(t)
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for i := range 100 {
				key := string(rune('a' + (i+worker)%5))
				_ = store.Put("bucket", key, sample{Count: i})
				var got sample
				store.Get("bucket", key, &got)
			}
		})
	}
	wg.Wait()
	store.Flush()
	if err := store.Err(); err != nil {
		t.Fatalf("concurrent writes failed: %v", err)
	}
}

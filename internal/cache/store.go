// Package cache keeps fetched PR, worktree and JIRA state so the TUI can paint
// instantly and revalidate in the background.
//
// It is two-level: an in-memory map serves every read synchronously (it is safe
// to call from the Bubble Tea update loop), and a SQLite file keeps entries
// across restarts. Writes land in memory immediately and are persisted by a
// single background writer that drains its queue in batches, coalescing repeated
// writes to the same key into one row update per transaction. The cache never is
// the source of truth: losing queued writes on a crash only costs a refetch.
package cache

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// schemaVersion invalidates every persisted entry when the table layout or the
// meaning of stored values changes incompatibly.
const schemaVersion = "1"

// retention drops entries nobody has written for this long when the store opens.
const retention = 30 * 24 * time.Hour

const maxBatch = 256

type entryKey struct {
	bucket string
	key    string
}

type entry struct {
	value     []byte
	fetchedAt time.Time
	deleted   bool
}

type op struct {
	key   entryKey
	entry entry
	ack   chan struct{}
}

// Store is safe for concurrent use.
type Store struct {
	mu  sync.RWMutex
	mem map[entryKey]entry

	db *sql.DB

	sendMu sync.RWMutex
	closed bool
	queue  chan op
	done   chan struct{}

	errMu   sync.Mutex
	lastErr error

	now func() time.Time
}

// NewMemory returns a store without persistence, used by tests and as the
// fallback when the SQLite file cannot be opened.
func NewMemory() *Store {
	return &Store{mem: map[entryKey]entry{}, now: time.Now}
}

// DefaultPath is the cache location next to the app's config file.
func DefaultPath(homeDir string) string {
	return filepath.Join(homeDir, ".wt-manager", "cache", "wt-manager.db")
}

// Open opens (or creates) the SQLite cache at path. A corrupt or unreadable file
// is recreated once; if that still fails, Open returns a working memory-only
// store together with the error so the app can keep running without persistence.
func Open(path string) (*Store, error) {
	db, err := openDB(path)
	if err != nil {
		removeDBFiles(path)
		db, err = openDB(path)
	}
	if err != nil {
		return NewMemory(), fmt.Errorf("open cache %s: %w", path, err)
	}
	s := &Store{
		mem:   map[entryKey]entry{},
		db:    db,
		queue: make(chan op, 1024),
		done:  make(chan struct{}),
		now:   time.Now,
	}
	go s.run()
	return s, nil
}

func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// SQLite creates its -wal and -shm files with the database's mode, so the
	// file is created private before SQLite first opens it.
	if file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err == nil {
		file.Close()
	}
	// WAL keeps UI-thread reads from waiting on the writer, and lets a second
	// wt-manager instance share the file; busy_timeout covers the rare overlap.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return db, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return err
	}
	var version string
	err := db.QueryRow(`SELECT value FROM meta WHERE key = 'schema'`).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if version != schemaVersion {
		if _, err := db.Exec(`DROP TABLE IF EXISTS entries`); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS entries (
		bucket TEXT NOT NULL,
		key TEXT NOT NULL,
		value BLOB NOT NULL,
		fetched_at INTEGER NOT NULL,
		written_at INTEGER NOT NULL,
		PRIMARY KEY (bucket, key)
	) WITHOUT ROWID`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO meta (key, value) VALUES ('schema', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, schemaVersion); err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM entries WHERE written_at < ?`, time.Now().Add(-retention).UnixMilli())
	return err
}

func removeDBFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}

// Get decodes the entry into out and returns when it was fetched from its
// source. A zero time means the entry was expired and should be revalidated,
// but its value is still worth painting.
func (s *Store) Get(bucket, key string, out any) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	e, ok := s.lookup(entryKey{bucket, key})
	if !ok {
		return time.Time{}, false
	}
	if err := json.Unmarshal(e.value, out); err != nil {
		return time.Time{}, false
	}
	return e.fetchedAt, true
}

// Put records value as freshly fetched now.
func (s *Store) Put(bucket, key string, value any) error {
	if s == nil {
		return nil
	}
	return s.PutAt(bucket, key, value, s.now())
}

// PutAt records value with an explicit fetch time.
func (s *Store) PutAt(bucket, key string, value any, fetchedAt time.Time) error {
	if s == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.set(entryKey{bucket, key}, entry{value: data, fetchedAt: fetchedAt})
	return nil
}

// Delete forgets an entry.
func (s *Store) Delete(bucket, key string) {
	if s == nil {
		return
	}
	s.set(entryKey{bucket, key}, entry{deleted: true})
}

// Expire keeps an entry's value but marks it stale, so the next reader paints it
// and then revalidates instead of trusting it.
func (s *Store) Expire(bucket, key string) {
	if s == nil {
		return
	}
	k := entryKey{bucket, key}
	e, ok := s.lookup(k)
	if !ok || e.fetchedAt.IsZero() {
		return
	}
	e.fetchedAt = time.Time{}
	s.set(k, e)
}

// Flush blocks until every write queued before the call is on disk.
func (s *Store) Flush() {
	if s == nil || s.db == nil {
		return
	}
	ack := make(chan struct{})
	if !s.enqueue(op{ack: ack}) {
		return
	}
	<-ack
}

// Err reports the most recent persistence failure, if any.
func (s *Store) Err() error {
	if s == nil {
		return nil
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.lastErr
}

// Close flushes queued writes and closes the database. The in-memory layer
// keeps answering reads afterwards.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.sendMu.Lock()
	if s.closed {
		s.sendMu.Unlock()
		return nil
	}
	s.closed = true
	close(s.queue)
	s.sendMu.Unlock()
	<-s.done
	return errors.Join(s.Err(), s.db.Close())
}

func (s *Store) lookup(k entryKey) (entry, bool) {
	s.mu.RLock()
	e, ok := s.mem[k]
	s.mu.RUnlock()
	if ok {
		return e, !e.deleted
	}
	if s.db == nil {
		return entry{}, false
	}

	loaded, found := s.read(k)
	s.mu.Lock()
	defer s.mu.Unlock()
	// A concurrent write may have landed while we were reading the disk; it is
	// newer than what we read, so it wins.
	if e, ok := s.mem[k]; ok {
		return e, !e.deleted
	}
	if !found {
		s.mem[k] = entry{deleted: true}
		return entry{}, false
	}
	s.mem[k] = loaded
	return loaded, true
}

func (s *Store) read(k entryKey) (entry, bool) {
	var value []byte
	var fetchedAt int64
	err := s.db.QueryRow(`SELECT value, fetched_at FROM entries WHERE bucket = ? AND key = ?`, k.bucket, k.key).Scan(&value, &fetchedAt)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.recordErr(err)
		}
		return entry{}, false
	}
	e := entry{value: value}
	if fetchedAt != 0 {
		e.fetchedAt = time.UnixMilli(fetchedAt)
	}
	return e, true
}

func (s *Store) set(k entryKey, e entry) {
	s.mu.Lock()
	s.mem[k] = e
	s.mu.Unlock()
	if s.db != nil {
		s.enqueue(op{key: k, entry: e})
	}
}

func (s *Store) enqueue(o op) bool {
	s.sendMu.RLock()
	defer s.sendMu.RUnlock()
	if s.closed {
		return false
	}
	s.queue <- o
	return true
}

// run is the single writer. It blocks for one queued operation, then takes
// whatever else is already waiting so bursts become one transaction.
func (s *Store) run() {
	defer close(s.done)
	for first := range s.queue {
		batch := []op{first}
	drain:
		for len(batch) < maxBatch {
			select {
			case next, ok := <-s.queue:
				if !ok {
					break drain
				}
				batch = append(batch, next)
			default:
				break drain
			}
		}
		s.apply(batch)
	}
}

func (s *Store) apply(batch []op) {
	latest := map[entryKey]entry{}
	order := make([]entryKey, 0, len(batch))
	var acks []chan struct{}
	for _, o := range batch {
		if o.ack != nil {
			acks = append(acks, o.ack)
			continue
		}
		if _, seen := latest[o.key]; !seen {
			order = append(order, o.key)
		}
		latest[o.key] = o.entry
	}
	if len(order) > 0 {
		s.recordErr(s.write(order, latest))
	}
	for _, ack := range acks {
		close(ack)
	}
}

func (s *Store) write(order []entryKey, latest map[entryKey]entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	upsert, err := tx.Prepare(`INSERT INTO entries (bucket, key, value, fetched_at, written_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(bucket, key) DO UPDATE SET value = excluded.value, fetched_at = excluded.fetched_at, written_at = excluded.written_at`)
	if err != nil {
		return err
	}
	defer upsert.Close()
	remove, err := tx.Prepare(`DELETE FROM entries WHERE bucket = ? AND key = ?`)
	if err != nil {
		return err
	}
	defer remove.Close()

	writtenAt := s.now().UnixMilli()
	for _, k := range order {
		e := latest[k]
		if e.deleted {
			if _, err := remove.Exec(k.bucket, k.key); err != nil {
				return err
			}
			continue
		}
		var fetchedAt int64
		if !e.fetchedAt.IsZero() {
			fetchedAt = e.fetchedAt.UnixMilli()
		}
		if _, err := upsert.Exec(k.bucket, k.key, e.value, fetchedAt, writtenAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) recordErr(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	s.lastErr = err
	s.errMu.Unlock()
}

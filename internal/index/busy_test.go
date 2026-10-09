package index

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// holdExclusive opens a second, foreign connection on path in exclusive
// locking mode and writes through it, so it keeps search.db locked against
// every other connection until it is closed (fault injection: another program
// holding the file; the index itself is the real one).
func holdExclusive(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA locking_mode=EXCLUSIVE; BEGIN; UPDATE meta SET version = version; COMMIT;`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}

// covers: MA-398, R18, S33
// A read-only open waits out a lock another connection holds instead of
// failing at once (busy timeout), and when the lock outlasts the wait it says
// the index is busy — Busy classifies it, the message names the file and
// "busy" and never the rebuild remedy — rather than calling it damaged. A lock
// released within the wait is waited out and the open succeeds. Positive twin:
// with no holder the index opens; a damaged file is not classified busy.
func TestReadonlyOpenWaitsOutABusyIndex(t *testing.T) {
	path := buildReadonlyFixture(t, tmpDir(t))
	ro, err := OpenReadonly(path)
	if err != nil {
		t.Fatalf("positive twin: an idle index did not open: %v", err)
	}
	ro.Close()

	defer func(d time.Duration) { readonlyBusyTimeout = d }(readonlyBusyTimeout)
	readonlyBusyTimeout = 400 * time.Millisecond

	// Held past the wait: busy, not damaged, after waiting.
	holder := holdExclusive(t, path)
	start := time.Now()
	ro, err = OpenReadonly(path)
	waited := time.Since(start)
	holder.Close()
	if err == nil {
		ro.Close()
		t.Fatal("OpenReadonly opened through another connection's exclusive lock")
	}
	if !Busy(err) {
		t.Fatalf("a held lock is not classified busy: %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, path) || !strings.Contains(msg, "busy") || strings.Contains(msg, "reindex -rebuild") {
		t.Fatalf("a busy index must be named busy, without the rebuild remedy: %q", msg)
	}
	if waited < 300*time.Millisecond {
		t.Fatalf("OpenReadonly gave up after %v without waiting out the lock (busy timeout %v)", waited, readonlyBusyTimeout)
	}

	// Released within the wait: the open waits it out and succeeds.
	readonlyBusyTimeout = 5 * time.Second
	holder = holdExclusive(t, path)
	released := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		holder.Close()
		close(released)
	}()
	ro, err = OpenReadonly(path)
	<-released
	if err != nil {
		t.Fatalf("a lock released within the busy timeout was not waited out: %v", err)
	}
	if n, cerr := ro.Count(); cerr != nil || n != 2 {
		t.Errorf("after waiting out the lock: count=%d err=%v, want 2", n, cerr)
	}
	ro.Close()

	// The classification is specific: a damaged file is not busy.
	bad := filepath.Join(tmpDir(t), "search.db")
	if err := os.WriteFile(bad, bytes.Repeat([]byte("not a sqlite database "), 200), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReadonly(bad); err == nil || Busy(err) {
		t.Fatalf("a damaged index must fail and not be classified busy: %v", err)
	}
}

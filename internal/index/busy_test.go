package index

import (
	"bytes"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// covers: MA-398, R18, S33
// The writable open (an export's, a Graph run's, reindex's) waits out a lock
// another connection holds for a moment, as the read-only open does, instead
// of failing at once — which failed the whole run: a lock released within the
// wait is waited out, and the index then opens and takes a row. A damaged file
// is not busy: it still fails at once, never after the busy timeout. The open
// goes through OpenReadonly's escaped file: URI, so a '?', '#' or '%' in the
// archive path is part of the file name, never URI syntax that cuts the path
// short or decodes it: '#' and '%' on every OS, '?' wherever a file name can
// hold one (not Windows). Positive twin: with no holder the index opens for
// writing at once.
func TestWriterOpenWaitsOutABusyIndex(t *testing.T) {
	when := time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC)
	setWait := func(t *testing.T, d time.Duration) {
		t.Helper()
		orig := writerBusyTimeout
		t.Cleanup(func() { writerBusyTimeout = orig })
		writerBusyTimeout = d
	}

	t.Run("idle", func(t *testing.T) {
		ix, err := Open(buildReadonlyFixture(t, tmpDir(t)))
		if err != nil {
			t.Fatalf("positive twin: an idle index did not open for writing: %v", err)
		}
		if err := ix.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a lock released within the wait", func(t *testing.T) {
		setWait(t, 5*time.Second)
		path := buildReadonlyFixture(t, tmpDir(t))
		holder := holdExclusive(t, path)
		released := make(chan struct{})
		go func() {
			time.Sleep(150 * time.Millisecond)
			holder.Close()
			close(released)
		}()
		ix, err := Open(path)
		<-released
		if err != nil {
			t.Fatalf("the writable open did not wait out a lock released within its busy timeout: %v", err)
		}
		if err := ix.Add("s", []string{"Inbox"}, mkMsg("after the wait", "dave", "me", "written once the lock was free", when), "s/Inbox/c.html", "k3"); err != nil {
			t.Fatalf("add after waiting out the lock: %v", err)
		}
		if err := ix.Close(); err != nil {
			t.Fatal(err)
		}
		ro, err := OpenReadonly(path)
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		if n, err := ro.Count(); err != nil || n != 3 {
			t.Fatalf("after waiting out the lock the index holds %d rows (err %v), want 3", n, err)
		}
	})

	t.Run("a damaged file fails at once", func(t *testing.T) {
		setWait(t, time.Minute)
		bad := filepath.Join(tmpDir(t), "search.db")
		if err := os.WriteFile(bad, bytes.Repeat([]byte("not a sqlite database "), 200), 0o644); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		ix, err := Open(bad)
		took := time.Since(start)
		if err == nil {
			ix.Close()
			t.Fatal("a damaged index opened for writing")
		}
		if Busy(err) {
			t.Fatalf("a damaged index was classified busy: %v", err)
		}
		if took > 10*time.Second {
			t.Fatalf("a damaged index failed only after %v (busy timeout %v): it is not busy and must fail at once", took, writerBusyTimeout)
		}
	})

	// Each folder name holds a character that is URI syntax: unescaped, '?'
	// starts the URI's parameters and '#' its fragment, cutting the path short
	// at "mail", and "%20" decodes to a space, naming a folder that does not
	// exist. '#' and '%' are legal in a file name on every OS, so those legs
	// run everywhere; a Windows file name cannot hold a '?', so that leg runs
	// where one can.
	special := []string{"mail#2026", "mail%2026"}
	if runtime.GOOS != "windows" {
		special = append(special, "mail?2026")
	}
	for _, name := range special {
		t.Run("a URI special character in the path: "+name, func(t *testing.T) {
			parent := tmpDir(t)
			path := filepath.Join(parent, name, "search.db")
			if err := os.Mkdir(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			ix, err := Open(path)
			if err != nil {
				t.Fatalf("writable open of %s: %v", path, err)
			}
			if err := ix.Add("s", []string{"Inbox"}, mkMsg("query", "erin", "me", "a path holding URI syntax", when), "s/Inbox/q.html", "kq"); err != nil {
				t.Fatal(err)
			}
			if err := ix.Close(); err != nil {
				t.Fatal(err)
			}
			if cut := filepath.Join(parent, "mail"); fileExists(cut) {
				t.Errorf("the writable open wrote %s: the path was cut short and the rest of %q read as URI syntax", cut, name)
			}
			if !fileExists(path) {
				t.Fatalf("the writable open did not create the index at %s", path)
			}
			ro, err := OpenReadonly(path)
			if err != nil {
				t.Fatal(err)
			}
			defer ro.Close()
			if n, err := ro.Count(); err != nil || n != 1 {
				t.Fatalf("the index at %s holds %d rows (err %v), want 1", path, n, err)
			}
		})
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}

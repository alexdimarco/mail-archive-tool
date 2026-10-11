package lockfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-85, R5, S25
// The archive lock is exclusive: a second Acquire while the first is held fails
// with ErrHeld naming the path and the holder; after Release it succeeds. The
// positive twin (a fresh Acquire succeeds and records the holder) comes first.
func TestExclusiveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	first, err := Acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if h := Holder(path); !strings.Contains(h, "pid=") {
		t.Errorf("holder line not recorded: %q", h)
	}

	second, err := Acquire(path)
	if err == nil {
		second.Release()
		t.Fatal("second acquire succeeded while the lock was held")
	}
	if !errors.Is(err, ErrHeld) {
		t.Errorf("error does not wrap ErrHeld: %v", err)
	}
	if !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "pid=") {
		t.Errorf("refusal must name the lock path and the holder: %v", err)
	}

	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	third, err := Acquire(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	third.Release()
}

// covers: MA-431, R5, S25
// Held reports a run in progress without ever writing: a missing lock file is not held and
// is not created; a held one is seen, its holder line is byte-for-byte what it was; after
// Release it is not held; and a lock path that is a planted link is not followed. It is how
// the workspace's copy export, which must never write the archive, sees a capture.
func TestHeldReadsTheLockWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, Name)
	if Held(path) {
		t.Fatal("a missing lock file reads as held")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("Held created the lock file: %v", err)
	}
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if !Held(path) {
		t.Error("a held lock reads as free")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) || len(before) == 0 {
		t.Errorf("Held changed the holder line: %q -> %q", before, after)
	}
	link := filepath.Join(dir, "link.lock")
	if err := os.Symlink(path, link); err == nil && Held(link) {
		t.Error("Held followed a planted link")
	}
	l.Release()
	if Held(path) {
		t.Error("a released lock reads as held")
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, before) {
		t.Errorf("Held rewrote the stale holder line: %q", got)
	}
}

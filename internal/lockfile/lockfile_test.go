package lockfile

import (
	"errors"
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

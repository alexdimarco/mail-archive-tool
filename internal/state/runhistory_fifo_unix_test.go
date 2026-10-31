//go:build unix

package state

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// unblockPipe opens a pipe for writing and closes it, so a reader blocked opening it
// (the defect this guards against) returns and the test leaves no goroutine stuck.
func unblockPipe(path string) {
	if f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		f.Close()
	}
}

// covers: MA-931
// The run history is read as a planted file may be (gate-2 adversarial pass 1, as7-1):
// a pipe at its name is refused at once, never waited on — a run appends to it while
// it holds the archive lock, and the Desktop reads it on every page — and so are a link
// and a file past its bound, each refusal naming the file; the append then starts a
// fresh history in a regular file, and a refused read leaves what was planted (the
// link's target) untouched. Positive twin first: a history written here reads.
func TestTheRunHistoryIsNeverWaitedOnOrFollowed(t *testing.T) {
	good := t.TempDir()
	if err := AppendRunHistory(good, RunHistoryEntry{When: time.Unix(1, 0).UTC(), Status: RunOK, Exported: 3}); err != nil {
		t.Fatal(err)
	}
	h, err := ReadRunHistory(good)
	if err != nil || len(assure.Reached(t, h, "the positive twin's history")) != 1 || h[0].Exported != 3 {
		t.Fatalf("the positive twin: %+v %v", h, err)
	}

	refused := func(t *testing.T, err error, kind string, untouched func() bool) {
		t.Helper()
		rc, msg := 0, ""
		if err != nil {
			rc, msg = 1, err.Error()
		}
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(RunHistoryName, kind), assure.NoSideEffect(untouched))
	}
	noHang := func(t *testing.T, what string, fn func() error) error {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- fn() }()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatalf("%s waited on the pipe", what)
		}
		return nil
	}

	t.Run("a pipe", func(t *testing.T) {
		dir := t.TempDir()
		pipe := filepath.Join(dir, RunHistoryName)
		if err := syscall.Mkfifo(pipe, 0o600); err != nil {
			t.Skip("no pipes here:", err)
		}
		t.Cleanup(func() { unblockPipe(pipe) })
		err := noHang(t, "the read", func() error { _, err := ReadRunHistory(dir); return err })
		refused(t, err, "a pipe", func() bool {
			fi, err := os.Lstat(pipe)
			return err == nil && fi.Mode()&os.ModeNamedPipe != 0
		})
		// A run's append goes on: a fresh history replaces the pipe.
		if err := noHang(t, "the append", func() error {
			return AppendRunHistory(dir, RunHistoryEntry{When: time.Unix(2, 0).UTC(), Status: RunOK})
		}); err != nil {
			t.Fatalf("the append after a pipe: %v", err)
		}
		if fi, err := os.Lstat(pipe); err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("the append left %v (%v), want a regular history", fi, err)
		}
		if h, err := ReadRunHistory(dir); err != nil || len(h) != 1 {
			t.Fatalf("the fresh history: %+v %v", h, err)
		}
	})

	t.Run("a link", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(good, RunHistoryName)
		before, _ := os.ReadFile(target)
		if err := os.Symlink(target, filepath.Join(dir, RunHistoryName)); err != nil {
			t.Skip("no links here:", err)
		}
		_, err := ReadRunHistory(dir)
		refused(t, err, "a link", func() bool {
			now, err := os.ReadFile(target)
			return err == nil && string(now) == string(assure.Reached(t, before, "the link target's bytes"))
		})
	})

	t.Run("a file past its bound", func(t *testing.T) {
		dir := t.TempDir()
		big := filepath.Join(dir, RunHistoryName)
		f, err := os.Create(big)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(maxRunHistorySize + 1); err != nil {
			t.Skip("no sparse files here:", err)
		}
		f.Close()
		_, err = ReadRunHistory(dir)
		refused(t, err, "bound", func() bool {
			fi, err := os.Stat(big)
			return err == nil && fi.Size() == maxRunHistorySize+1
		})
	})
}

//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-931
// A pipe planted at the run history's name never holds a run (gate-2 adversarial pass
// 1, as7-1): the run appends to the history while it holds the archive lock, so a read
// that waited on the pipe would keep the lock for good and every later run would be
// refused as busy. Positive twin first: a run on the healthy archive records itself in
// the history. Then, with a pipe at that name, a real run finishes, releases the lock
// (the next run takes it and finishes too) and leaves a fresh history in a regular file.
func TestAPlantedRunHistoryPipeNeverHoldsTheLock(t *testing.T) {
	out := tmpDir(t)
	in := completeMaildir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	h, err := state.ReadRunHistory(out)
	if err != nil || len(assure.Reached(t, h, "the positive twin's history")) != 1 {
		t.Fatalf("the positive twin's history: %+v %v", h, err)
	}

	pipe := filepath.Join(out, state.RunHistoryName)
	if err := os.Remove(pipe); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	t.Cleanup(func() { // unblock a reader stuck on the pipe, so no goroutine is left
		if f, err := os.OpenFile(pipe, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			f.Close()
		}
	})
	for i := 1; i <= 2; i++ {
		done := make(chan error, 1)
		go func() {
			_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out}, discard(), nil)
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("run %d after the planted pipe: %v (a lock still held is refused as busy)", i, err)
			}
		case <-time.After(60 * time.Second):
			t.Fatalf("run %d waited on the pipe planted at the run history's name", i)
		}
	}
	if fi, err := os.Lstat(pipe); err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("the history is %v (%v), want a fresh regular file", fi, err)
	}
	if h, err := state.ReadRunHistory(out); err != nil || len(h) != 2 {
		t.Fatalf("the fresh history after two runs: %+v %v", h, err)
	}
}

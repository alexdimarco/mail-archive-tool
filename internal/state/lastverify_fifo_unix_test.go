//go:build unix

package state

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// covers: MA-767, R37
// A pipe at the last-verify record's name is unreadable at once: the read neither
// waits on it — a verify holding the archive lock would hold it for good — nor reads
// it (the adversarial pass, OUT-08).
func TestAPipeAtTheLastVerifyRecordIsNotWaitedOn(t *testing.T) {
	out := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(out, LastVerifyName), 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	done := make(chan LastVerifyState, 1)
	go func() {
		_, st, _ := ReadLastVerify(out)
		done <- st
	}()
	select {
	case st := <-done:
		if st != LastVerifyUnreadable {
			t.Fatalf("a pipe read as %v, want unreadable", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read waited on the pipe")
	}
}

//go:build unix

package container

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-766, R37
// A pipe at the record file's name is damage, as the keyed load refuses it, and the
// check neither waits on it nor reads it (the adversarial pass, DEC-01; the fix
// re-verification, FV-T-04).
func TestAKeylessCheckFindsAPipeAtTheRecordFilesName(t *testing.T) {
	out := wholeArchive(t)
	p := filepath.Join(out, EnvelopeName)
	os.Remove(p)
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	// CheckKeyless itself: the tree snapshot keyless() takes would block reading the
	// pipe.
	done := make(chan Check, 1)
	go func() { done <- CheckKeyless(out, 0) }()
	select {
	case c := <-done:
		wantOutcome(t, c, state.OutcomeDamaged, reasonEnvelopeBad)
	case <-time.After(30 * time.Second):
		t.Fatal("the check waited on the pipe")
	}
}

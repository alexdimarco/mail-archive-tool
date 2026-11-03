//go:build !windows

package delmark

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// covers: MA-1053, R36
// A pipe at the marker's name (§15.3): presence is decided by a lookup that never
// opens it, so it counts as a marker MailArchive did not write; the bounded read
// refuses it at once, never waiting for a writer. The test fails on a timeout if
// either opens the pipe. Positive twin first: a regular marker in the same place.
func TestAPipeAtTheNameIsNeverOpened(t *testing.T) {
	d := t.TempDir()
	plant(t, d, marker)
	if st, _ := Present(d); st != Regular || !Read(d).Known {
		t.Fatal("the positive twin")
	}
	d = t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(d, Name), 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	type answer struct {
		st    State
		err   error
		known bool
	}
	done := make(chan answer, 1)
	go func() {
		st, err := Present(d)
		done <- answer{st, err, Read(d).Known}
	}()
	select {
	case a := <-done:
		if a.st != NotRegular || a.err != nil || a.known {
			t.Fatalf("a pipe at the name: %+v", a)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the pipe at the marker's name was opened and waited on")
	}
}

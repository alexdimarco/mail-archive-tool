//go:build unix

package state

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// covers: MA-772, R37
// The last-run record is read as a planted file may be (phase3 §6 E8): a pipe at its
// name is unreadable at once, not waited on — status would hang on it — and so are a
// link and a file past its bound. The positive twin: a record written here reads.
func TestTheLastRunRecordIsNeverWaitedOnOrFollowed(t *testing.T) {
	out := t.TempDir()
	started := time.Now().UTC().Truncate(time.Second)
	if err := WriteLastRun(out, LastRun{Status: RunOK, Started: started, Finished: started}); err != nil {
		t.Fatal(err)
	}
	if r, st, err := ReadLastRun(out); st != LastRunPresent || err != nil || !r.Started.Equal(started) {
		t.Fatalf("the positive twin: %+v %v %v", r, st, err)
	}

	pipe := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(pipe, LastRunName), 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	done := make(chan LastRunState, 1)
	go func() {
		_, st, _ := ReadLastRun(pipe)
		done <- st
	}()
	select {
	case st := <-done:
		if st != LastRunUnreadable {
			t.Fatalf("a pipe read as %v, want unreadable", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read waited on the pipe")
	}

	link := t.TempDir()
	if err := os.Symlink(filepath.Join(out, LastRunName), filepath.Join(link, LastRunName)); err == nil {
		if _, st, _ := ReadLastRun(link); st != LastRunUnreadable {
			t.Fatalf("a link to a good record read as %v", st)
		}
	}
	big := t.TempDir()
	f, _ := os.Create(filepath.Join(big, LastRunName))
	if err := f.Truncate(maxLastRunSize + 1); err != nil {
		t.Skip("no sparse files here:", err)
	}
	f.Close()
	if _, st, _ := ReadLastRun(big); st != LastRunUnreadable {
		t.Fatalf("a record past its bound read as %v", st)
	}
}

//go:build unix

package presync

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// covers: MA-565
// loadRecord never blocks or over-allocates on a hostile state file (R32): a
// planted FIFO (a naive read would block), a symlink (would be followed) and an
// oversized file each return errUnreadable promptly, so recovery treats the
// record as absent and the archive still runs. The FIFO fixture is Unix-only
// (there is no mkfifo on Windows); the Windows hostile-file twin is a lab item.
func TestLoadRecordHostileFileNeverBlocks(t *testing.T) {
	dir := t.TempDir()

	fifo := filepath.Join(dir, "fifo.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unsupported here: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok, err := loadRecord(fifo); ok || !errors.Is(err, errUnreadable) {
			t.Errorf("FIFO: got (ok=%v, err=%v), want (false, errUnreadable)", ok, err)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loadRecord blocked on a FIFO — R32: it must never hang the archive")
	}

	big := filepath.Join(dir, "big.json")
	if err := os.WriteFile(big, make([]byte, recordMaxBytes+100), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadRecord(big); ok || !errors.Is(err, errUnreadable) {
		t.Errorf("oversized: got (ok=%v, err=%v), want (false, errUnreadable)", ok, err)
	}

	target := filepath.Join(dir, "target.json")
	_ = os.WriteFile(target, []byte(`{"version":1}`), 0o600)
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err == nil {
		if _, ok, err := loadRecord(link); ok || !errors.Is(err, errUnreadable) {
			t.Errorf("symlink: got (ok=%v, err=%v), want (false, errUnreadable)", ok, err)
		}
	}
}

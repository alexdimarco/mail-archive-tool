//go:build unix

package desktop

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-818, R37
// Reading the recovery file never waits on a pipe swapped in for it after its check
// (OUT-B): with a regular file and a named pipe swapped at its path again and again,
// every read returns at once — the file's bytes, or a refusal.
func TestReadingTheRecoveryFileNeverWaitsOnAPipe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MailArchive recovery file.txt")
	reg, fifo := filepath.Join(dir, "reg.tmp"), filepath.Join(dir, "fifo.tmp")
	os.WriteFile(path, []byte("x"), 0o600)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			os.WriteFile(reg, []byte("x"), 0o600)
			os.Rename(reg, path)
			syscall.Mkfifo(fifo, 0o600)
			os.Rename(fifo, path)
		}
	}()
	reads := 0
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); reads++ {
		done := make(chan struct{})
		go func() { readRecoveryFile(path); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			// Unblock the hung open so the test ends.
			for k := 0; k < 50; k++ {
				if f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
					f.Close()
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("a read waited on a pipe swapped in after its check (after %d reads)", reads)
		}
	}
	assure.Reached(t, reads > 0, "the reads ran")
}

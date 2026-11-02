//go:build !windows

package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// covers: MA-1008, R36
// The uninstall plan's fingerprint of deletions.log opens it as every other read of the log does
// (the 1.5.3 re-walk's guards pass, G6): never through a link and never blocking, an ordinary file
// only. Before, a plain open followed a link and waited on a pipe swapped in after uninstall's own
// check, so Remove hung while holding the block on deletes until the Desktop restarted. Now a pipe
// or a link there is "cannot be read" (-1 and no hash) at once; the real run then refuses on its
// own. Positive twin first: an ordinary log gives its size and SHA-256.
func TestTheLogFingerprintNeverWaitsOnAPipeOrFollowsALink(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "deletions.log")
	data := []byte(`{"at":"2026-11-02T10:00:00Z","event":"started","run":"r1","folder":"/x"}` + "\n")
	if err := os.WriteFile(logPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if n, h := logFingerprint(logPath); n != int64(len(data)) || h != hex.EncodeToString(sum[:]) {
		t.Fatalf("positive twin: an ordinary log: %d %q", n, h)
	}

	link := filepath.Join(t.TempDir(), "deletions.log")
	if err := os.Symlink(logPath, link); err != nil {
		t.Fatal(err)
	}
	if n, h := logFingerprint(link); n != -1 || h != "" {
		t.Errorf("a link to a log was followed: %d %q", n, h)
	}

	pipe := filepath.Join(t.TempDir(), "deletions.log")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("this system cannot make a pipe: %v", err)
	}
	done := make(chan [2]any, 1)
	go func() { n, h := logFingerprint(pipe); done <- [2]any{n, h} }()
	select {
	case got := <-done:
		if got[0] != int64(-1) || got[1] != "" {
			t.Errorf("a pipe was read: %v", got)
		}
	case <-time.After(5 * time.Second):
		// Unblock the waiting open so the test ends.
		if w, err := os.OpenFile(pipe, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			w.Close()
		}
		t.Fatal("the fingerprint waited on a pipe")
	}
}

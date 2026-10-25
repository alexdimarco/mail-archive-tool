//go:build unix

package vault

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// covers: MA-746, R37
// The recognition predicate never blocks on a pipe planted as the manifest (the
// archive index's safe read, MA-660): it answers at once, and a pipe is not a signal.
func TestEncryptedMarkerNeverWaitsOnAPipeManifest(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, manifestName), 0o600); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	done := make(chan bool, 1)
	go func() {
		enc, _, _ := EncryptedMarker(dir)
		done <- enc
	}()
	select {
	case enc := <-done:
		if enc {
			t.Fatal("a pipe manifest read as an encrypted archive")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("EncryptedMarker blocked on a pipe planted as the manifest")
	}
}

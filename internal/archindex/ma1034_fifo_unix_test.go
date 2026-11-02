//go:build !windows

package archindex

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// covers: MA-1034, MA-660
// A manifest that is a pipe is refused at once, never waited on (util.OpenRegularFileIn opens it
// without blocking and refuses what is not a regular file). Positive twin first: a manifest file
// beside it is recognised. Off Windows, which has no named pipes in a folder.
func TestAManifestThatIsAPipeIsRefusedAtOnce(t *testing.T) {
	good := filepath.Join(t.TempDir(), "Good")
	realArchive(t, good, 2, localSource)
	if !IsArchive(good) {
		t.Fatal("the positive twin: a manifest file must be recognised")
	}
	dir := filepath.Join(t.TempDir(), "Pipe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, manifestName), 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan bool, 1)
	go func() { done <- IsArchive(dir) }()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a manifest that is a pipe must be refused")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("recognition waited on a manifest that is a pipe")
	}
}

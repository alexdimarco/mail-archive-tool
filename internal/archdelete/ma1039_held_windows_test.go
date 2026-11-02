//go:build windows

package archdelete

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1039, R36
// On Windows the plan's probe finds a file another handle holds open without delete sharing (as
// Outlook, or Go's own os.Open, holds one), and nothing else: it opens a handle asking to delete
// and closes it, changing nothing. Positive twin first: a file nobody holds is not held.
func TestTheHeldProbeFindsAFileHeldOpenOnWindows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "held.html")
	if err := os.WriteFile(path, []byte("page"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if heldIn(root, "held.html") {
		t.Fatal("a file nobody holds is reported held")
	}
	f, err := os.Open(path) // no FILE_SHARE_DELETE
	if err != nil {
		t.Fatal(err)
	}
	if !heldIn(root, "held.html") {
		f.Close()
		t.Fatal("a file held open is not reported held")
	}
	f.Close()
	if heldIn(root, "held.html") {
		t.Fatal("a file released is still reported held")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the probe changed the file: %v", err)
	}
}

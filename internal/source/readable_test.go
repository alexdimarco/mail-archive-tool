package source

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-56, R1, S17
// DataFileReadable filters unreadable Outlook files out of auto-discovery: an
// empty stub, a wrong-format file, and a valid-header-but-corrupt file are all
// reported unreadable, a real .pst is readable, and — critically — a file that
// cannot be opened at all (locked/permission) is reported readable so a real but
// currently-locked mailbox is never silently dropped.
func TestDataFileReadable(t *testing.T) {
	dir := t.TempDir()

	// A real Outlook data file parses → readable.
	real := "../../testdata/support.pst"
	if _, err := os.Stat(real); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	if !DataFileReadable(real) {
		t.Error("a valid .pst must be reported readable")
	}

	// An orphaned empty stub → unreadable (dropped).
	empty := filepath.Join(dir, "orphan.ost")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if DataFileReadable(empty) {
		t.Error("an empty .ost stub must be reported unreadable")
	}

	// A file with the PST signature but no valid structure → unreadable.
	corrupt := filepath.Join(dir, "corrupt.ost")
	if err := os.WriteFile(corrupt, append([]byte("!BDN"), make([]byte, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}
	if DataFileReadable(corrupt) {
		t.Error("a corrupt (header-only) .ost must be reported unreadable")
	}

	// A file that cannot be opened at all (simulated with 0 permissions) is kept:
	// it may be a real mailbox locked by a running Outlook. Skipped as root (which
	// ignores permission bits) and on Windows (where 0-perm files stay readable, so
	// the simulation doesn't hold — the lock-tolerance itself is OS-agnostic).
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := filepath.Join(dir, "locked.ost")
		if err := os.WriteFile(locked, []byte("!BDN whatever"), 0o000); err != nil {
			t.Fatal(err)
		}
		if !DataFileReadable(locked) {
			t.Error("a file that cannot be opened (locked/permission) must be kept, not dropped")
		}
	}
}

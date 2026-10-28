package util

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-849, R37
// A file identity pinned when it was taken is that file's, not whatever is at the path
// when it is compared (P3-8 Windows lab, B1): a file swapped in afterwards does not
// match it. On Windows an unpinned os.Stat loads its identity only at the comparison,
// from the swapped file, and matched it. Positive twin: the same file still matches.
func TestAPinnedIdentityIsTheFileAsItWas(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "program.exe")
	os.WriteFile(p, []byte("the program as it started"), 0o755)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	start, _ := PinIdentity(fi)
	// The positive twin on a separate pinned identity: comparing start itself would
	// fix its identity, pinned or not.
	twin, _ := os.Stat(p)
	twin, _ = PinIdentity(twin)
	again, _ := os.Stat(p)
	if !os.SameFile(twin, again) {
		t.Fatal("positive twin: the same file matches")
	}
	decoy := filepath.Join(dir, "decoy.exe")
	os.WriteFile(decoy, []byte("something else"), 0o755)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(decoy, p); err != nil {
		t.Fatal(err)
	}
	now, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, !os.SameFile(start, now), "a file swapped in does not match the pinned one")
}

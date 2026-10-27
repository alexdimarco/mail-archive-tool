package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-805, MA-759, R36, R37
// A reader's working copy of an encrypted archive's index that a live reader still
// holds — `serve`, `search` or another Desktop, under the OS lock on its lock file —
// refuses the whole uninstall, naming it, nothing removed (§7.4; the P3-3 plan, 3c);
// once the reader lets go, the plan lists it to remove. The positive twin first.
func TestAReaderStillOpenRefusesTheUninstall(t *testing.T) {
	root, _ := scratchFixture(t)
	index := filepath.Join(root, "0123456789abcdef0123456789abcdef", "index")
	db, lock := filepath.Join(index, "reader-42-1.db"), filepath.Join(index, "reader-42-1.db.lock")
	for _, f := range []string{db, lock} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := ScratchTargets(root, nil); err != nil || !slices.Contains(got, db) {
		t.Fatalf("positive twin, no live reader: %v %v", got, err)
	}
	held, err := lockfile.HoldFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ScratchTargets(root, nil)
	_, statErr := os.Stat(db)
	held.Close()
	assure.Reached(t, errors.Is(err, ErrScratchInUse) && statErr == nil, "a live reader refuses, nothing removed")
}

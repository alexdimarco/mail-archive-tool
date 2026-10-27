package app

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-814, R37
// The unencrypted copy is the escape hatch (phase 3 §10, P3-4), so a record this
// computer cannot use must not block it: an encrypted archive holding itself is
// recognised by its own files whether the record can be used or not, and a person
// at a terminal may still unlock it (UnlockableAt, the gate the copy uses — no second
// predicate is needed). A plaintext archive and a folder inside an encrypted one are
// never unlockable, with the record usable or not.
func TestACopyIsNotBlockedByARecordThatCannotBeUsed(t *testing.T) {
	out := sealedArchive(t)
	inner := filepath.Join(out, "inner")
	os.Mkdir(inner, 0o700)
	plain := plainArchive(t)
	check := func(when string) {
		t.Helper()
		if !UnlockableAt(out) {
			t.Fatalf("%s: an encrypted archive holding itself is not unlockable", when)
		}
		if UnlockableAt(inner) || UnlockableAt(plain) {
			t.Fatalf("%s: a folder inside an encrypted archive, or a plaintext archive, is unlockable", when)
		}
	}
	check("with the record usable")
	unusableRecord(t)
	check("while the record cannot be used")
}

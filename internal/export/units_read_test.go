package export

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// covers: MA-795, R37
// The reader's split read (design-encrypted-archives-phase3 §3.2): Locate names a
// unit with the key, ReadSealedAt reads and checks its ciphertext with no key, and
// Open decrypts it — the same plaintext Read gives. ReadSealedAt keeps a unit gone
// as fs.ErrNotExist, refuses bytes that are not the record's, and refuses a path
// outside the unit store.
func TestAUnitReadsInThreeStepsHoldingTheKeyOnlyToNameAndOpenIt(t *testing.T) {
	out := t.TempDir()
	u := &Units{Root: out, Key: testUnitsKey(t)}
	const unit = "0123456789abcdef0123456789abcdef"
	plain := []byte("<p>the minutes</p>")
	s, err := u.Write(unit, vault.UnitMessage, 1, plain)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := u.Locate(unit, vault.UnitMessage, s)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := ReadSealedAt(out, rel, s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := u.Open(unit, vault.UnitMessage, s, sealed)
	whole, _ := u.Read(unit, vault.UnitMessage, s)
	assure.Reached(t, err == nil && bytes.Equal(got, plain) && bytes.Equal(got, whole), "the three steps give Read's plaintext")
	if _, err := u.Open(unit, vault.UnitAttachment, s, sealed); err == nil {
		t.Fatal("a unit opened as another kind")
	}

	for _, bad := range []string{"../units/x", "chunks/ab/cd", filepath.Join(out, rel), "units/../chunks/ab/cd", "units"} {
		_, err := ReadSealedAt(out, bad, s)
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("not a unit path"))
		if !errors.Is(err, ErrUnitStore) {
			t.Fatalf("%s: want ErrUnitStore, got %v", bad, err)
		}
	}

	p := filepath.Join(out, rel)
	raw, _ := os.ReadFile(p)
	swapped := bytes.Clone(raw)
	swapped[0] ^= 1
	os.WriteFile(p, swapped, 0o600)
	if _, err := ReadSealedAt(out, rel, s); !errors.Is(err, ErrUnitStore) {
		t.Fatalf("bytes not the record's: %v", err)
	}
	os.Remove(p)
	if _, err := ReadSealedAt(out, rel, s); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a unit gone: want fs.ErrNotExist, got %v", err)
	}
}

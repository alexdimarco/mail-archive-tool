package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// covers: MA-729, R37
// Copy messages out never reads an encrypted archive as a plaintext one: as a source
// (configured or another archive folder) it is R-enc, saying it is encrypted, never
// that a newer MailArchive wrote it; a destination inside one is R-enc too, so no
// plaintext copy of mail lands in it. Nothing is written either way. The positive
// twins: a plaintext archive opens, and a folder outside every archive is accepted.
func TestCopyExportRefusesAnEncryptedArchiveAsSourceOrDestination(t *testing.T) {
	plain := buildCEArchive(t)
	for _, other := range []bool{false, true} {
		if s, err := OpenArchive(plain.dir, ArchiveOptions{Other: other}); err != nil {
			t.Fatalf("the positive twin, a plaintext archive (other=%v): %v", other, err)
		} else {
			s.Close()
		}
	}
	sealed := sealedArchive(t)
	before := lsTree(t, sealed)
	for _, other := range []bool{false, true} {
		_, err := OpenArchive(sealed, ArchiveOptions{Other: other})
		refusedWith(t, err, "R-enc", []string{sealed, "encrypted archive", "password or recovery key"})
	}
	if !reflect.DeepEqual(lsTree(t, sealed), before) {
		t.Fatal("a refused encrypted source was written to")
	}

	e := newCEEnv(t)
	outside := t.TempDir()
	if _, err := e.CheckDestination(outside, []string{plain.dir}); err != nil {
		t.Fatalf("the positive twin, a folder outside every archive: %v", err)
	}
	inside := filepath.Join(sealed, "exported")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	before = lsTree(t, sealed)
	_, err := e.CheckDestination(inside, []string{plain.dir})
	refusedWith(t, err, "R-enc", []string{sealed, "export into it"})
	if !reflect.DeepEqual(lsTree(t, sealed), before) {
		t.Fatal("a refused destination inside an encrypted archive was written to")
	}
}

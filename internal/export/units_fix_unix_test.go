//go:build unix

package export

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/vault"
)

// covers: MA-747, R37
// A re-capture that has no original of its own never drops the record's original
// because it cannot look at it just now (a permission, a sharing violation, an I/O
// error): the original is carried, and after the manifest save it is still there and
// opens (F11). The positive twin: an original that is truly gone is dropped (MA-723).
func TestAReexportNeverDropsAnOriginalItCannotLookAt(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file")
	}
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	_, first := onlyRecord(t, exp.Manifest)
	eml := filepath.Join(out, exp.Units.Name(first.Unit, vault.UnitOriginal, first.Sealed.EML.Rev))
	if err := os.Chmod(eml, 0); err != nil {
		t.Fatal(err)
	}
	m := fullMessage("v1")
	m.Raw = nil
	exp.Mode = Full
	if _, err := exp.Export("store", []string{"Inbox"}, m); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(eml, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	_, rec := onlyRecord(t, exp.Manifest)
	if rec.Sealed.EML == nil || *rec.Sealed.EML != *first.Sealed.EML {
		t.Fatalf("the original was dropped from the record: %+v", rec.Sealed)
	}
	if _, err := exp.Units.Read(rec.Unit, vault.UnitOriginal, *rec.Sealed.EML); err != nil {
		t.Fatalf("the original must still be there and open: %v", err)
	}
}

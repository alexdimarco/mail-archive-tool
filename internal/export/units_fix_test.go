package export

import (
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// unitFiles counts the unit-shaped files under out.
func unitFiles(t *testing.T, out string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(filepath.Join(out, UnitsDir), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && unitPath.MatchString(strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(out)+"/")) {
			n++
		}
		return nil
	})
	return n
}

// requireOpens fails unless every unit rec names opens.
func requireOpens(t *testing.T, u *Units, rec state.Record) {
	t.Helper()
	for kind, s := range map[vault.UnitKind]*state.SealedUnit{vault.UnitMessage: rec.Sealed.HTML, vault.UnitAttachment: rec.Sealed.Zip, vault.UnitOriginal: rec.Sealed.EML} {
		if s == nil {
			continue
		}
		if _, err := u.Read(rec.Unit, kind, *s); err != nil {
			t.Fatalf("kind %d rev %d, which the saved record names, does not open: %v", kind, s.Rev, err)
		}
	}
}

// covers: MA-747, R37
// A message whose export fails partway (here: its page cannot be written, after its
// original and attachments were sealed) changes nothing its saved record names: the
// revisions it wrote are removed (no record names them), the ones it would have
// superseded are kept, and after the next manifest save every revision the record
// names still opens (the Phase-2a adversarial pass, F05/F10). On a fresh archive the
// same failure leaves no unit behind, so the next run is not refused as "a missing
// manifest beside sealed units". The positive twin: the same re-export without the
// fault supersedes the old revisions after the save (MA-723).
func TestAFailedExportChangesNothingItsRecordNames(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	_, first := onlyRecord(t, exp.Manifest)

	exp.Mode = Full
	exp.BeforeWrite = func(p string) error {
		if strings.HasSuffix(p, ".html") {
			return errors.New("the page cannot be written")
		}
		return nil
	}
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v2")); err == nil {
		t.Fatal("fixture: the failing page did not fail the export")
	}
	if _, rec := onlyRecord(t, exp.Manifest); *rec.Sealed != *first.Sealed {
		t.Fatalf("a failed export changed the record: %+v, was %+v", rec.Sealed, first.Sealed)
	}
	if len(exp.pendingDeletes) != 0 {
		t.Fatalf("a failed export queued %d supersessions of revisions its record still names", len(exp.pendingDeletes))
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	requireOpens(t, exp.Units, first)
	if n := unitFiles(t, out); n != 3 {
		t.Fatalf("the failed export left %d unit files, want exactly the record's 3", n)
	}

	fresh := t.TempDir()
	fx := sealedExporter(t, fresh)
	fx.BeforeWrite = exp.BeforeWrite
	if _, err := fx.Export("store", []string{"Inbox"}, fullMessage("v1")); err == nil {
		t.Fatal("fixture: the failing page did not fail the export")
	}
	if fx.Units.HasUnitFiles() || fx.Manifest.Len() != 0 {
		t.Fatalf("a fresh archive's failed export left units behind (%d records)", fx.Manifest.Len())
	}
}

// covers: MA-747, R37
// The second guard on its own: a revision the saved manifest names is never deleted,
// even when something queued it — the deletion runs after the save and skips it.
func TestARevisionTheManifestNamesIsNeverDeleted(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	_, rec := onlyRecord(t, exp.Manifest)
	exp.queueDelete(rec.Unit, vault.UnitOriginal, rec.Sealed.EML.Rev)
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	requireOpens(t, exp.Units, rec)
}

// covers: MA-747, R37
// A unit is kept on a re-export only when the file on disk is exactly the recorded
// one: a damaged unit of unchanged content is rewritten as a new revision that
// opens (F09). The positive twin: an intact one is kept (MA-723).
func TestADamagedUnitIsRewrittenOnReexport(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	_, first := onlyRecord(t, exp.Manifest)
	page := filepath.Join(out, exp.Units.Name(first.Unit, vault.UnitMessage, first.Sealed.HTML.Rev))
	b, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 0xff
	if err := os.WriteFile(page, b, 0o600); err != nil {
		t.Fatal(err)
	}
	exp.Mode = Full
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	_, rec := onlyRecord(t, exp.Manifest)
	if rec.Sealed.HTML.Rev != first.Sealed.HTML.Rev+1 || rec.Sealed.Zip.Rev != first.Sealed.Zip.Rev {
		t.Fatalf("the damaged page must be rewritten and the intact zip kept: html rev %d, zip rev %d", rec.Sealed.HTML.Rev, rec.Sealed.Zip.Rev)
	}
	requireOpens(t, exp.Units, rec)
}

// covers: MA-747, R37
// A re-capture whose attachments now exceed the in-memory ceiling does not throw away
// the attachments the record already holds: the export is refused and the record,
// with its zip, is kept as it was (F07). The positive twin: a NEW message over the
// ceiling is archived without them (MA-724).
func TestAReexportOverTheCeilingKeepsTheArchivedAttachments(t *testing.T) {
	orig := encryptedZipCeiling
	encryptedZipCeiling = 4096
	t.Cleanup(func() { encryptedZipCeiling = orig })
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	_, first := onlyRecord(t, exp.Manifest)
	big := make([]byte, 16<<10)
	rand.Read(big) // incompressible: the zip stays over the ceiling
	m := fullMessage("v1")
	m.Attachments[0].WriteTo = func(w io.Writer) (int64, error) { n, err := w.Write(big); return int64(n), err }
	exp.Mode = Full
	if _, err := exp.Export("store", []string{"Inbox"}, m); !errors.Is(err, ErrZipTooLarge) {
		t.Fatalf("want ErrZipTooLarge for a re-capture over the ceiling, got %v", err)
	}
	if _, rec := onlyRecord(t, exp.Manifest); rec.Sealed.Zip == nil || *rec.Sealed.Zip != *first.Sealed.Zip {
		t.Fatalf("the record's zip must be kept: %+v", rec.Sealed)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	requireOpens(t, exp.Units, first)
}

// covers: MA-747, R37
// The keyed sweep fails closed and is not stopped by a stray file: with a key whose
// names cannot be derived it refuses and removes nothing (F08); a plain file beside
// the shards (a .DS_Store) is left alone, does not refuse the sweep, and is not a
// unit file (F06/F13). The positive twin: an unnamed old unit is swept (MA-725).
func TestTheSweepFailsClosedAndIgnoresAStrayFile(t *testing.T) {
	out := t.TempDir()
	exp := sealedExporter(t, out)
	if _, err := exp.Export("store", []string{"Inbox"}, fullMessage("v1")); err != nil {
		t.Fatal(err)
	}
	if err := exp.Manifest.Save(); err != nil {
		t.Fatal(err)
	}
	_, dead, _, err := vault.Create("a key that will be destroyed")
	if err != nil {
		t.Fatal(err)
	}
	dead.Destroy()
	later := time.Now().Add(time.Hour)
	if n, err := (&Units{Root: out, Key: dead}).SweepUnits(exp.Manifest, later); err == nil || n != 0 || unitFiles(t, out) != 3 {
		t.Fatalf("a sweep that cannot derive names must refuse and remove nothing: removed %d, err %v, %d units left", n, err, unitFiles(t, out))
	}

	stray := filepath.Join(out, UnitsDir, ".DS_Store")
	if err := os.WriteFile(stray, []byte("finder"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := exp.Units.SweepUnits(exp.Manifest, later); err != nil || n != 0 {
		t.Fatalf("a stray file beside the shards must not refuse the sweep: removed %d, err %v", n, err)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatal("the sweep removed a file that is not a unit")
	}
	only := t.TempDir()
	if err := os.MkdirAll(filepath.Join(only, UnitsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(only, UnitsDir, ".DS_Store"), []byte("finder"), 0o600)
	if (&Units{Root: only, Key: testUnitsKey(t)}).HasUnitFiles() {
		t.Fatal("a stray file is not a unit file")
	}
}

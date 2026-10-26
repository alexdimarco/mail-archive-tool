package encrecord

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// failTrailers damages both copies (their trailers fail) while their bodies still
// decode.
func failTrailers(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{FileName, MirrorName} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(b), "sha256=", "sha256=00", 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// covers: MA-790, MA-785, R37
// Repair re-checks under the lock that the record is unchanged since its scan, and
// refuses — moving nothing aside — when it changed (the P3-1a pass, FID-13).
func TestRepairRefusesARecordThatChangedSinceItsScan(t *testing.T) {
	dir := sandbox(t)
	add(t, idA, filepath.Join(t.TempDir(), "a"))
	failTrailers(t, dir)
	orig := afterRepairScan
	afterRepairScan = func() {
		os.WriteFile(filepath.Join(dir, MirrorName), []byte("MAILARCHIVE-RECORD 1 seq=7\nchanged\n"), 0o600)
	}
	defer func() { afterRepairScan = orig }()
	_, err := Repair()
	aside, _ := filepath.Glob(filepath.Join(dir, "*.damaged-*"))
	assure.Refused(t, code(err), errText(err), assure.Code(1), assure.Names("changed while it was being repaired"),
		assure.NoSideEffect(func() bool { return len(aside) == 0 }))
	if !errors.Is(err, ErrRepairRaced) {
		t.Fatalf("want ErrRepairRaced, got %v", err)
	}
}

// covers: MA-790, MA-785, R37
// A repair cut short after it set the damaged copies aside leaves the record still
// damaged — never empty and usable, which would drop every folder's protection and
// refuse a second repair — and the second repair succeeds (the P3-1a pass, CON-02).
func TestARepairCutShortCanBeRepairedAgain(t *testing.T) {
	dir := sandbox(t)
	pathA := filepath.Join(t.TempDir(), "a")
	add(t, idA, pathA)
	failTrailers(t, dir)
	primary := filepath.Join(dir, FileName)
	orig := beforeRepairWrite
	// The fault: a non-empty folder where the record file goes, so the write fails
	// for any account, root included.
	beforeRepairWrite = func() {
		os.Remove(primary)
		os.MkdirAll(filepath.Join(primary, "x"), 0o700)
	}
	_, err := Repair()
	beforeRepairWrite = orig
	if err == nil {
		t.Fatal("the write was made to fail, yet Repair succeeded")
	}
	if _, lerr := Load(); lerr == nil {
		t.Fatal("a repair cut short left an empty, usable record")
	}
	if err := os.RemoveAll(primary); err != nil {
		t.Fatal(err)
	}
	if _, err := Repair(); err != nil {
		t.Fatalf("the repair could not be retried: %v", err)
	}
	rec, _ := Load()
	m, ok := rec.Lookup(pathA)
	assure.Reached(t, ok && m.ID == idA && m.Path.Unproven, "the path rebuilt by the retried repair")
}

// covers: MA-790, MA-785, R37
// Between two damaged copies, an id's tombstone is taken from the later one, and an
// id recorded again at a path is never left forgotten (the P3-1a pass, CRIT-08).
func TestRepairTakesTheLaterCopysTombstone(t *testing.T) {
	dir := sandbox(t)
	pathA := filepath.Join(t.TempDir(), "a")
	add(t, idA, pathA)
	if _, err := Update("test", func(r *Record) error { r.ForgetID(idA); return nil }); err != nil {
		t.Fatal(err)
	}
	older, _ := os.ReadFile(filepath.Join(dir, FileName)) // forgotten
	add(t, idA, pathA)                                    // recorded again, later
	newer, _ := os.ReadFile(filepath.Join(dir, FileName))
	damage := func(b []byte) []byte { return []byte(strings.Replace(string(b), "sha256=", "sha256=00", 1)) }
	os.WriteFile(filepath.Join(dir, FileName), damage(older), 0o600)
	os.WriteFile(filepath.Join(dir, MirrorName), damage(newer), 0o600)
	if _, err := Repair(); err != nil {
		t.Fatal(err)
	}
	rec, _ := Load()
	e := rec.Archives[idA]
	assure.Reached(t, e != nil && len(e.Paths) == 1 && !e.Forgotten, "the later copy's live entry")

	// The later copy forgot only a path, the earlier one the whole archive: the
	// later's "not forgotten" stands, with no paths left.
	dir = sandbox(t)
	add(t, idA, pathA)
	Update("test", func(r *Record) error { r.ForgetID(idA); return nil })
	older, _ = os.ReadFile(filepath.Join(dir, FileName))
	add(t, idA, pathA)
	Update("test", func(r *Record) error { r.ForgetPath(idA, recordedForm(pathA)); return nil })
	newer, _ = os.ReadFile(filepath.Join(dir, FileName))
	os.WriteFile(filepath.Join(dir, FileName), damage(older), 0o600)
	os.WriteFile(filepath.Join(dir, MirrorName), damage(newer), 0o600)
	if _, err := Repair(); err != nil {
		t.Fatal(err)
	}
	rec, _ = Load()
	if e := rec.Archives[idA]; e == nil || e.Forgotten || len(e.Paths) != 0 {
		t.Fatalf("an earlier tombstone outlived the later copy's entry: %+v", e)
	}

	// The later copy forgot the whole archive, the earlier one still had its path:
	// the path is kept (unproven, it only refuses keyless writes) and an entry with
	// a path is never left forgotten.
	dir = sandbox(t)
	add(t, idA, pathA)
	older, _ = os.ReadFile(filepath.Join(dir, FileName))
	Update("test", func(r *Record) error { r.ForgetID(idA); return nil })
	newer, _ = os.ReadFile(filepath.Join(dir, FileName))
	os.WriteFile(filepath.Join(dir, FileName), damage(older), 0o600)
	os.WriteFile(filepath.Join(dir, MirrorName), damage(newer), 0o600)
	if _, err := Repair(); err != nil {
		t.Fatal(err)
	}
	rec, _ = Load()
	if e := rec.Archives[idA]; e == nil || e.Forgotten || len(e.Paths) != 1 {
		t.Fatalf("a path kept by repair was left forgotten: %+v", e)
	}
}

// covers: MA-790, MA-777
// A lost mark decided on an older look at the record is not made once the record
// lists the id (the P3-1a pass, CON-06).
func TestALostMarkIsNotMadeForAnArchiveTheRecordLists(t *testing.T) {
	sandbox(t)
	add(t, idA, filepath.Join(t.TempDir(), "a"))
	if err := MarkLost(idA); err != nil {
		t.Fatal(err)
	}
	if err := MarkLost(idB); err != nil {
		t.Fatal(err)
	}
	lost, err := Lost()
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, len(lost) == 1 && lost[0] == idB, "only the unlisted id marked lost")
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

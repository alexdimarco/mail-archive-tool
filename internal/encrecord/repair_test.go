package encrecord

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-785, R37
// Repair (§4.3): a usable record and the other cases are refused as not damaged; a
// damaged record's copies are moved aside, never overwritten; every path and
// tombstone that still parses from either copy is rebuilt, each path unproven; and
// what could not be read back is reported.
func TestRepairRebuildsWhatStillParsesAsUnproven(t *testing.T) {
	dir := sandbox(t)
	pathA := filepath.Join(t.TempDir(), "a")
	add(t, idA, pathA)
	add(t, idB, filepath.Join(t.TempDir(), "b"))
	if _, err := Update("test", func(r *Record) error { r.ForgetID(idB); return nil }); err != nil {
		t.Fatal(err)
	}
	// Positive twin: a usable record needs no repair, and is left as it is.
	_, err := Repair()
	if !errors.Is(err, ErrNotDamaged) {
		t.Fatalf("a usable record: %v", err)
	}

	// The trailer of both copies fails: damaged, but the body still decodes.
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
	if _, err := Load(); err == nil {
		t.Fatal("the record with failed trailers loaded")
	}
	rep, err := Repair()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Load()
	if err != nil {
		t.Fatalf("the repaired record: %v", err)
	}
	m, ok := rec.Lookup(pathA)
	assure.Reached(t, ok && m.ID == idA && m.Path.Unproven, "A's path rebuilt, unproven")
	if e := rec.Archives[idB]; e == nil || !e.Forgotten {
		t.Fatalf("B's tombstone was not rebuilt: %+v", e)
	}
	if rep.Paths != 1 || rep.Tombstones != 1 || len(rep.MovedAside) != 2 {
		t.Fatalf("the report: %+v", rep)
	}
	aside, _ := filepath.Glob(filepath.Join(dir, FileName+".damaged-*"))
	if len(aside) != 1 {
		t.Fatalf("the damaged primary kept aside: %v", aside)
	}
}

// covers: MA-785, R37
func TestRepairReportsWhatItCouldNotRebuildAndRefusesOtherCases(t *testing.T) {
	dir := sandbox(t)
	for _, name := range []string{FileName, MirrorName} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("MAILARCHIVE-RECORD 1 seq=4\n{\"archives\":{\"not-an-id\":{\"paths\":[]},"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := Repair()
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, len(rep.NotRebuilt), "what could not be rebuilt")
	if rep.Paths != 0 {
		t.Fatalf("rebuilt paths from nothing: %+v", rep)
	}

	// A newer record is not damaged: its remedy is the upgrade, and repair refuses.
	dir = sandbox(t)
	newer := []byte("MAILARCHIVE-RECORD 9 seq=1 more\n{}\nsha256=x\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), newer, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Repair()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	assure.Refused(t, code(err), msg, assure.Code(1), assure.Names("newer MailArchive"),
		assure.NoSideEffect(func() bool { b, _ := os.ReadFile(filepath.Join(dir, FileName)); return string(b) == string(newer) }))
}

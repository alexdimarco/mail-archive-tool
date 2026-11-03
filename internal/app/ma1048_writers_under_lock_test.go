package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-1048, R36
// The other writers DM-14 lists ask the delete marker again under their archive lock, as app.Run
// does (the slice-2 adversarial pass's LOW-1). A delete on another computer that writes its marker
// after the writer's first ask, stops part way and lets the lock go before the writer takes it
// (through the acquireArchiveLock seam) is caught under the lock: reindex, reindex -rebuild, an
// encrypted archive's index rebuild and the program-copies setting refuse in the marker's words,
// nothing written and the lock not held after; RecordRefusedRun (a scheduled run refused for
// another reason) writes no last-run record or history line. Positive twins first: through the
// same seam, planting nothing, each writer writes.
func TestMA1048_TheOtherWritersAskTheMarkerAgainUnderTheirLock(t *testing.T) {
	ma1054NoCheck(t)
	in := ma956Input(t)
	archive := func(name string) string {
		out := filepath.Join(t.TempDir(), name)
		if _, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Index: true}, discard(), nil); err != nil {
			t.Fatal(err)
		}
		return out
	}
	notHeld := func(out string) bool { return !lockfile.Held(filepath.Join(out, lockfile.Name)) }
	refusedRun := func(out string) {
		RecordRefusedRun(Options{Out: out, Unattended: true}, withKind(state.FailSourceUnreadable, errors.New("the source is gone")))
	}

	// Positive twins: each writer goes through the seam and writes.
	twin := archive("twin")
	seen := 0
	restore := ma1054Seam(t, func() { seen++ })
	os.Remove(filepath.Join(twin, state.LastRunName))
	refusedRun(twin)
	assure.Reached(t, ma956Exists(filepath.Join(twin, state.LastRunName)), "positive twin: the refused run's record")
	if _, _, err := Reindex(twin, discard()); err != nil {
		t.Fatalf("positive twin: reindex: %v", err)
	}
	if _, err := Rebuild(twin, discard()); err != nil {
		t.Fatalf("positive twin: rebuild: %v", err)
	}
	restore()
	if seen != 3 {
		t.Fatalf("positive twin: the writers took their lock through the seam %d times, want 3", seen)
	}

	// A marker written between the first ask and the lock: remove any earlier one, plant at the
	// lock.
	late := func(out string) (before map[string]string, restore func()) {
		if err := os.Remove(filepath.Join(out, delmark.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		return ma1054Files(t, out), ma1054Seam(t, func() { ma1054Plant(t, out) })
	}

	marked := archive("marked")
	before, restore := late(marked)
	refusedRun(marked)
	restore()
	assure.Reached(t, ma1044Same(before, ma1054Files(t, marked)) && ma956Exists(filepath.Join(marked, delmark.Name)),
		"RecordRefusedRun wrote nothing into the folder whose marker came while it waited for the lock")
	// With the marker already there it does not even take the lock (no lock file made).
	if err := os.Remove(filepath.Join(marked, lockfile.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	refusedRun(marked)
	assure.Reached(t, !ma956Exists(filepath.Join(marked, lockfile.Name)), "RecordRefusedRun took no lock in a folder already holding the marker")

	before, restore = late(marked)
	_, _, err := Reindex(marked, discard())
	restore()
	ma1054Refused(t, err, marked, delmark.OnlyMarkerWords("its search index was not changed"), func() bool {
		return ma1044Same(before, ma1054Files(t, marked)) && notHeld(marked)
	})

	before, restore = late(marked)
	_, err = Rebuild(marked, discard())
	restore()
	ma1054Refused(t, err, marked, delmark.OnlyMarkerWords("its search index was not rebuilt"), func() bool {
		return ma1044Same(before, ma1054Files(t, marked)) && notHeld(marked)
	})

	// An encrypted archive: the program-copies setting and its index rebuild. Positive twins.
	asDevBuild(t)
	isolatedCache(t)
	enc, key := encryptedOut(t)
	backup(t, enc, key, ProgramCopiesUnset)
	seen = 0
	restore = ma1054Seam(t, func() { seen++ })
	if err := SealProgramCopies(context.Background(), Options{Out: enc, Key: key}, ProgramCopiesOn, discard()); err != nil {
		t.Fatalf("positive twin: program copies: %v", err)
	}
	if _, err := RebuildEncrypted(context.Background(), Options{Out: enc, Key: key}, discard()); err != nil {
		t.Fatalf("positive twin: the encrypted index rebuild: %v", err)
	}
	restore()
	if seen != 2 {
		t.Fatalf("positive twin: the encrypted writers took their lock through the seam %d times, want 2", seen)
	}

	before, restore = late(enc)
	err = SealProgramCopies(context.Background(), Options{Out: enc, Key: key}, ProgramCopiesOff, discard())
	restore()
	ma1054Refused(t, err, enc, delmark.OnlyMarkerWords("the program-copies setting was not saved"), func() bool {
		return ma1044Same(before, ma1054Files(t, enc)) && notHeld(enc)
	})

	before, restore = late(enc)
	_, err = RebuildEncrypted(context.Background(), Options{Out: enc, Key: key}, discard())
	restore()
	ma1054Refused(t, err, enc, delmark.OnlyMarkerWords("its search index was not rebuilt"), func() bool {
		return ma1044Same(before, ma1054Files(t, enc)) && notHeld(enc)
	})
}

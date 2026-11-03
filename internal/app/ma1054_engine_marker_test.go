package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The delete marker's refusals in the engine (docs/design-archive-delete.md §15.3, §15.10 slice 2).
// These tests run in package app alone, so no unfinished-delete check is installed: the engine
// asks the marker itself (MA-1054). Nothing writes the marker yet (slice 3), so each test plants
// it by hand, as a delete's step 1 (d) will write it.

const ma1054Run = "0123456789abcdef"

// ma1054Plant writes a regular delete marker into dir, as a delete writes it.
func ma1054Plant(t *testing.T, dir string) {
	t.Helper()
	body := `{"what":"` + delmark.What + `","version":1,"run":"` + ma1054Run + `","began":"2026-11-02T15:04:05Z","note":"` + delmark.Note + `"}`
	if err := os.WriteFile(filepath.Join(dir, delmark.Name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ma1054Files is every file under dir with its bytes, the lock file and the planted marker aside.
func ma1054Files(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() || rel == lockfile.Name || rel == delmark.Name {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ma1054Seam runs before as a run reaches for its archive lock (between its first ask and the
// ask under its lock) and returns the function that takes the seam away again.
func ma1054Seam(t *testing.T, before func()) (restore func()) {
	t.Helper()
	orig := acquireArchiveLock
	restore = func() { acquireArchiveLock = orig }
	t.Cleanup(restore)
	acquireArchiveLock = func(path, verb string) (*lockfile.Lock, error) {
		before()
		return orig(path, verb)
	}
	return restore
}

// ma1054NoCheck makes sure no unfinished-delete check is installed (package app alone).
func ma1054NoCheck(t *testing.T) {
	t.Helper()
	t.Cleanup(SetUnfinishedDeleteCheck(nil))
	assure.Reached(t, !UnfinishedDeleteCheckInstalled(), "no check installed")
}

// ma1054Refused checks a refusal by the marker: exit 1, words with the folder, kind
// delete_unfinished, taken back as no archive, never a source-loss verdict, and the side effect
// absent.
func ma1054Refused(t *testing.T, err error, out, words string, absent func() bool) {
	t.Helper()
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1), assure.Names(words+" (Folder: "+out+")"), assure.NoSideEffect(absent))
	if k := FailureKind(err); k != state.FailDeleteUnfinished || !RefusedAsNoArchive(err) {
		t.Errorf("kind %q, taken back %v: want %s, taken back as no archive", k, RefusedAsNoArchive(err), state.FailDeleteUnfinished)
	}
	if FoundSourceUnreachable(err) {
		t.Error("a refusal for the delete marker says nothing about the source")
	}
}

// covers: MA-1054, MA-1048, R36
// The engine asks the delete marker even where no unfinished-delete check is installed
// (§15.3, LOW-4's other half): app.Run refuses a folder holding the marker at its start —
// attended (the folder is not made an archive) and scheduled (nothing written, the lock not
// taken) — and again under its lock (a marker written between, through the acquireArchiveLock
// seam), in delmark's marker-only words with the folder (DM-11, DM-24), kind delete_unfinished,
// taken back as no archive. A link at the name (to nothing) refuses in its own words. Positive
// twin first: with no marker the same run archives.
func TestMA1054_RunAsksTheMarkerWithNoCheckInstalled(t *testing.T) {
	ma1054NoCheck(t)
	in := ma956Input(t)
	run := func(out string, unattended bool) error {
		_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Unattended: unattended}, discard(), nil)
		return err
	}

	// Positive twin.
	ok := filepath.Join(t.TempDir(), "ok")
	if err := run(ok, false); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	assure.Reached(t, ma956Exists(filepath.Join(ok, ma956Manifest)), "the twin's archive")

	// At its start, attended: a folder holding only the marker is not made an archive.
	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := os.Mkdir(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	ma1054Plant(t, fresh)
	ma1054Refused(t, run(fresh, false), fresh, delmark.MsgOnlyMarker, func() bool {
		n, _ := os.ReadDir(fresh)
		return len(n) == 1
	})

	// At its start, scheduled, into an archive: nothing written, the lock not taken.
	half := filepath.Join(t.TempDir(), "half")
	if err := run(half, false); err != nil {
		t.Fatal(err)
	}
	before := ma1054Files(t, half)
	ma1054Plant(t, half)
	ma1054Refused(t, run(half, true), half, delmark.MsgOnlyMarker, func() bool {
		return ma1044Same(before, ma1054Files(t, half)) && !lockfile.Held(filepath.Join(half, lockfile.Name))
	})

	// Under the lock: the marker is written after the run's first ask, before its lock.
	raced := filepath.Join(t.TempDir(), "raced")
	if err := run(raced, false); err != nil {
		t.Fatal(err)
	}
	before = ma1054Files(t, raced)
	restore := ma1054Seam(t, func() { ma1054Plant(t, raced) })
	ma1054Refused(t, run(raced, true), raced, delmark.MsgOnlyMarker, func() bool {
		return ma1044Same(before, ma1054Files(t, raced)) && !lockfile.Held(filepath.Join(raced, lockfile.Name))
	})
	restore()

	// A link at the name, to nothing: never followed, still a marker.
	linked := filepath.Join(t.TempDir(), "linked")
	if err := run(linked, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "nothing"), filepath.Join(linked, delmark.Name)); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("a link needs a privilege the run may not have")
		}
		t.Fatal(err)
	}
	before = ma1054Files(t, linked)
	ma1054Refused(t, run(linked, true), linked, delmark.MsgNotRegular, func() bool { return ma1044Same(before, ma1054Files(t, linked)) })
}

// covers: MA-1054, MA-1048, R36
// app.RunGraph (a Microsoft 365 capture) asks the marker too with no check installed: at its
// start, before the sign-in (no folder made), and again under its lock. Positive twin first.
func TestMA1054_RunGraphAsksTheMarkerWithNoCheckInstalled(t *testing.T) {
	ma1054NoCheck(t)
	_, srv := newFakeDelegServer()
	defer srv.Close()
	capture := func(out string, unattended bool) error {
		cache := filepath.Join(t.TempDir(), "primed.json")
		writePrimedCache(t, cache)
		g := delegOpts(srv, cache)
		g.Unattended = unattended
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Unattended: unattended}, discardLogger())
		return err
	}

	// Positive twin.
	ok := filepath.Join(t.TempDir(), "ok")
	if err := capture(ok, false); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	assure.Reached(t, ma956Exists(filepath.Join(ok, ma956Manifest)), "the twin's archive")

	// At its start.
	fresh := filepath.Join(t.TempDir(), "fresh")
	if err := os.Mkdir(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	ma1054Plant(t, fresh)
	ma1054Refused(t, capture(fresh, false), fresh, delmark.MsgOnlyMarker, func() bool {
		n, _ := os.ReadDir(fresh)
		return len(n) == 1
	})

	// Under the lock, scheduled.
	raced := filepath.Join(t.TempDir(), "raced")
	if err := capture(raced, false); err != nil {
		t.Fatal(err)
	}
	before := ma1054Files(t, raced)
	ma1054Seam(t, func() { ma1054Plant(t, raced) })
	ma1054Refused(t, capture(raced, true), raced, delmark.MsgOnlyMarker, func() bool {
		return ma1044Same(before, ma1054Files(t, raced)) && !lockfile.Held(filepath.Join(raced, lockfile.Name))
	})
}

// covers: MA-1048, R36
// Under the archive lock the delete check comes before the emptied-folder guard (DM-25): a
// scheduled run that queued while a delete removed the archive's anchor (its manifest gone, its
// marker there) is told "Delete not finished", not the emptied-folder guard's words, by app.Run
// and by app.RunGraph. Positive twin first: the same run with the anchor gone and no marker is
// refused by the guard under its lock, as before.
func TestMA1048_UnderTheLockTheDeleteIsToldBeforeTheEmptiedFolder(t *testing.T) {
	ma1054NoCheck(t)
	in := ma956Input(t)
	run := func(out string) error {
		_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Unattended: true}, discard(), nil)
		return err
	}
	_, srv := newFakeDelegServer()
	defer srv.Close()
	capture := func(out string) error {
		cache := filepath.Join(t.TempDir(), "primed.json")
		writePrimedCache(t, cache)
		g := delegOpts(srv, cache)
		g.Unattended = true
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Unattended: true}, discardLogger())
		return err
	}
	archive := func(name string) string {
		out := filepath.Join(t.TempDir(), name)
		if _, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental}, discard(), nil); err != nil {
			t.Fatal(err)
		}
		return out
	}
	unanchor := func(out string, marker bool) func() {
		return func() {
			if err := os.Remove(filepath.Join(out, ma956Manifest)); err != nil {
				t.Fatal(err)
			}
			if marker {
				ma1054Plant(t, out)
			}
		}
	}

	// Positive twin: no marker, the guard speaks under the lock.
	twin := archive("twin")
	restore := ma1054Seam(t, unanchor(twin, false))
	err := run(twin)
	restore()
	if err == nil || FailureKind(err) != state.FailArchiveAbsent || strings.Contains(err.Error(), "Delete not finished") {
		t.Fatalf("positive twin: the emptied-folder guard under the lock: %v (kind %q)", err, FailureKind(err))
	}

	for name, start := range map[string]func(string) error{"Run": run, "RunGraph": capture} {
		out := archive("queued " + name)
		before := ma1054Files(t, out)
		delete(before, ma956Manifest)
		restore := ma1054Seam(t, unanchor(out, true))
		err := start(out)
		restore()
		ma1054Refused(t, err, out, delmark.MsgOnlyMarker, func() bool { return ma1044Same(before, ma1054Files(t, out)) })
		assure.Refused(t, 1, errString(err), assure.Code(1), assure.Forbid("holds no MailArchive archive"))
	}
}

// ma1054LookupFails makes the marker's lookup fail with cause for every name (a dead drive, an
// I/O error), until the test ends.
func ma1054LookupFails(t *testing.T, cause string) {
	t.Helper()
	t.Cleanup(delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New(cause)}
	}))
}

// covers: MA-1048, R36
// A lookup of the marker that fails for another reason than "not there" (DM-22, DM-23): at a
// folder that is there it refuses, fail closed, in the "could not check" words, kind
// archive_unwritable, taken back as no archive, nothing written; where the folder itself cannot be
// reached (a dead drive: "device not ready", "network name deleted") the answer is "no marker",
// so a scheduled run gets MA-689's archive-absent refusal, never "could not check". Positive twin
// first: with the lookup working, the scheduled run archives.
func TestMA1048_ALookupThatFailsIsCouldNotCheckOrTheDriveIsGone(t *testing.T) {
	ma1054NoCheck(t)
	in := ma956Input(t)
	run := func(out string) error {
		_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Unattended: true}, discard(), nil)
		return err
	}
	out := filepath.Join(t.TempDir(), "archive")
	if _, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental}, discard(), nil); err != nil {
		t.Fatal(err)
	}

	// Positive twin.
	if err := run(out); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	ma1054LookupFails(t, "input/output error")
	before := ma1054Files(t, out)
	err := run(out)
	assure.Refused(t, rcOf(err), errString(err), assure.Code(1),
		assure.Names("MailArchive could not check this archive's folder for a delete that did not finish (input/output error), so it was not backed up; nothing was changed. Check that the drive or folder can be read, then try again. (Folder: "+out+")"),
		assure.NoSideEffect(func() bool { return ma1044Same(before, ma1054Files(t, out)) }))
	if k := FailureKind(err); k != state.FailArchiveUnwritable || !RefusedAsNoArchive(err) {
		t.Errorf("kind %q, taken back %v: want %s, taken back as no archive", k, RefusedAsNoArchive(err), state.FailArchiveUnwritable)
	}

	for _, cause := range []string{"The device is not ready.", "The specified network name is no longer available."} {
		ma1054LookupFails(t, cause)
		gone := filepath.Join(t.TempDir(), "unplugged", "Mail")
		err := run(gone)
		if k := FailureKind(err); err == nil || k != state.FailArchiveAbsent || strings.Contains(err.Error(), "could not check") {
			t.Errorf("%s: a dead drive is MA-689's archive-absent refusal: %v (kind %q)", cause, err, k)
		}
		assure.Reached(t, !ma956Exists(gone), "nothing made where the drive was")
	}
}

// covers: MA-1048, R36
// What else writes into an archive folder asks the marker (DM-14): RecordRefusedRun (a scheduled
// run refused for another reason) writes no last-run record or history line into a folder that
// holds it; reindex, the index rebuild, an encrypted archive's index rebuild and the program-copies
// setting refuse in the marker's words, before their lock, nothing written. Positive twins first:
// each works on the same kind of archive without the marker.
func TestMA1048_RecordsReindexAndProgramCopiesWriteNothingIntoAMarkerFolder(t *testing.T) {
	in := ma956Input(t)
	archive := func(name string) string {
		out := filepath.Join(t.TempDir(), name)
		if _, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, Index: true}, discard(), nil); err != nil {
			t.Fatal(err)
		}
		return out
	}
	lockGone := func(out string) bool { return !ma956Exists(filepath.Join(out, lockfile.Name)) }

	// RecordRefusedRun. Positive twin.
	twin := archive("twin")
	os.Remove(filepath.Join(twin, state.LastRunName))
	RecordRefusedRun(Options{Out: twin, Unattended: true}, withKind(state.FailSourceUnreadable, errors.New("the source is gone")))
	assure.Reached(t, ma956Exists(filepath.Join(twin, state.LastRunName)), "positive twin: the refused run's record")
	marked := archive("marked")
	ma1054Plant(t, marked)
	before := ma1054Files(t, marked)
	RecordRefusedRun(Options{Out: marked, Unattended: true}, withKind(state.FailSourceUnreadable, errors.New("the source is gone")))
	assure.Reached(t, ma1044Same(before, ma1054Files(t, marked)), "RecordRefusedRun wrote nothing into the marker folder")

	// Reindex and Rebuild.
	if _, _, err := Reindex(twin, discard()); err != nil {
		t.Fatalf("positive twin: reindex: %v", err)
	}
	if _, err := Rebuild(twin, discard()); err != nil {
		t.Fatalf("positive twin: rebuild: %v", err)
	}
	os.Remove(filepath.Join(marked, lockfile.Name))
	before = ma1054Files(t, marked)
	_, _, err := Reindex(marked, discard())
	ma1054Refused(t, err, marked, delmark.OnlyMarkerWords("its search index was not changed"), func() bool {
		return ma1044Same(before, ma1054Files(t, marked)) && lockGone(marked)
	})
	_, err = Rebuild(marked, discard())
	ma1054Refused(t, err, marked, delmark.OnlyMarkerWords("its search index was not rebuilt"), func() bool {
		return ma1044Same(before, ma1054Files(t, marked)) && lockGone(marked)
	})

	// An encrypted archive: its index rebuild and the program-copies setting.
	asDevBuild(t)
	isolatedCache(t)
	enc, key := encryptedOut(t)
	backup(t, enc, key, ProgramCopiesUnset)
	if err := SealProgramCopies(context.Background(), Options{Out: enc, Key: key}, ProgramCopiesOn, discard()); err != nil {
		t.Fatalf("positive twin: program copies: %v", err)
	}
	if _, err := RebuildEncrypted(context.Background(), Options{Out: enc, Key: key}, discard()); err != nil {
		t.Fatalf("positive twin: the encrypted index rebuild: %v", err)
	}
	ma1054Plant(t, enc)
	os.Remove(filepath.Join(enc, lockfile.Name))
	before = ma1054Files(t, enc)
	err = SealProgramCopies(context.Background(), Options{Out: enc, Key: key}, ProgramCopiesOff, discard())
	ma1054Refused(t, err, enc, delmark.OnlyMarkerWords("the program-copies setting was not saved"), func() bool {
		return ma1044Same(before, ma1054Files(t, enc)) && lockGone(enc)
	})
	_, err = RebuildEncrypted(context.Background(), Options{Out: enc, Key: key}, discard())
	ma1054Refused(t, err, enc, delmark.OnlyMarkerWords("its search index was not rebuilt"), func() bool {
		return ma1044Same(before, ma1054Files(t, enc)) && lockGone(enc)
	})
}

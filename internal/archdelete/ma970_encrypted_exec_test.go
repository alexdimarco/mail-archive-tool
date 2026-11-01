package archdelete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
)

// encRig is an encrypted archive's rig, recorded on this computer.
func encRig(t *testing.T, dir string) *rig {
	t.Helper()
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	return r
}

// covers: MA-970, R36
func TestAnEncryptedDeleteLocksTheArchiveOnThePageAndAlwaysClearsTheMark(t *testing.T) {
	dir := encryptedClean(t)
	r := encRig(t, dir)
	def := schedule.DefaultNameFor(dir)
	heldAtLock := false
	r.keys.events = &r.events
	lockKeys := r.x.Keys
	r.x.Keys = keysFunc{lock: func(f string) error {
		heldAtLock = lockfile.Held(filepath.Join(dir, lockName))
		return lockKeys.LockForDelete(f)
	}, end: lockKeys.EndDelete}
	res := r.run(t, false)
	if res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	if !heldAtLock {
		t.Fatal("step 1 (b) comes after the archive lock (a)")
	}
	if got := strings.Join(r.events, ","); got != "lock-for-delete,schedule:"+def+",end-delete" {
		t.Fatalf("events %s", got)
	}
	if len(r.keys.marked) != 0 {
		t.Fatal("the being-deleted mark is cleared when the delete ends")
	}
}

// keysFunc is a KeyRegistry from two funcs.
type keysFunc struct {
	lock func(string) error
	end  func(string)
}

func (k keysFunc) LockForDelete(f string) error { return k.lock(f) }
func (k keysFunc) EndDelete(f string)           { k.end(f) }

// covers: MA-970, R36
func TestTheArchivesScratchUnderEverySpellingIsRemovedAndNothingElse(t *testing.T) {
	dir := encryptedClean(t)
	r := encRig(t, dir)
	other := dir + string(filepath.Separator) + "." // the same folder, spelled otherwise by a job file
	writeJob(t, schedule.DefaultNameFor(other)+"-x", other)
	mine := scratchFor(r.scratch, dir, testArchiveID)
	crashed := scratchFor(r.scratch, other, testArchiveID)
	another := scratchFor(r.scratch, filepath.Join(t.TempDir(), "another"), testArchiveID)
	writeFile(t, filepath.Join(mine, "index", "search.db"), "plaintext index")
	writeFile(t, filepath.Join(crashed, "snap", ".mailarchive-snapshot-1"), "plaintext mailbox")
	writeFile(t, filepath.Join(another, "snap", ".mailarchive-snapshot-1"), "another archive's")
	res := r.run(t, false)
	if res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	if exists(mine) || exists(crashed) {
		t.Fatal("the archive's scratch folders, under every spelling, are removed")
	}
	if !exists(filepath.Join(another, "snap", ".mailarchive-snapshot-1")) {
		t.Fatal("another archive's scratch is never touched")
	}
}

// covers: MA-970, R36
func TestAScratchFolderHoldingOtherFilesOrAnOpenReaderRefusesUnderTheLock(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, scratch string) func()
		want  string
		c     Case
	}{
		{"unexpected", func(t *testing.T, s string) func() {
			writeFile(t, filepath.Join(s, "notes.txt"), "the person's")
			return func() {}
		}, "MailArchive's working folder for this archive holds files it did not put there: ", CaseScratch},
		{"reader open", func(t *testing.T, s string) func() {
			writeFile(t, filepath.Join(s, "index", "reader-1-1.db"), "index")
			writeFile(t, filepath.Join(s, "index", "reader-1-1.db.lock"), "")
			f, err := lockfile.HoldFile(filepath.Join(s, "index", "reader-1-1.db.lock"))
			if err != nil {
				t.Fatal(err)
			}
			return func() { f.Close() }
		}, msgReaderOpen, CaseReader},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := encryptedClean(t)
			r := encRig(t, dir)
			// Positive twin first: a scratch folder of the archive's own files is no refusal.
			{
				twin := encRig(t, encryptedClean(t))
				writeFile(t, filepath.Join(scratchFor(twin.scratch, twin.dir, testArchiveID), "snap", ".mailarchive-snapshot-2"), "x")
				if res := twin.run(t, false); res.Status != StatusComplete {
					t.Fatalf("twin: %s", res.Message())
				}
			}
			done := tc.setup(t, scratchFor(r.scratch, dir, testArchiveID))
			defer done()
			before := treeWithoutLock(t, dir)
			res := r.run(t, false)
			assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1), assure.Names(tc.want, lockedLine),
				assure.NoSideEffect(func() bool {
					return sameTree(before, treeWithoutLock(t, dir)) && len(r.sched.removed) == 0 && len(r.keys.marked) == 0
				}))
			if res.Refusal == nil || res.Refusal.Case != tc.c || res.Status != StatusRefusedAfterStart {
				t.Fatalf("%+v %q", res.Refusal, res.Status)
			}
		})
	}
}

// covers: MA-970, R36
func TestStep5AScratchFolderThatCannotBeRemovedStopsWithItsWords(t *testing.T) {
	dir := encryptedClean(t)
	r := encRig(t, dir)
	s := scratchFor(r.scratch, dir, testArchiveID)
	writeFile(t, filepath.Join(s, "snap", ".mailarchive-snapshot-3"), "x")
	// Something MailArchive did not put there appears after step 1 (c) checked.
	r.sched.onRemove = func(string) { writeFile(t, filepath.Join(s, "snap", "other.txt"), "y") }
	res := r.run(t, false)
	want := "MailArchive's working folder for this archive is in use or holds files it did not put there: " + s +
		". Steps done: schedules stopped, their settings files removed, settings updated, archive locked. Close MailArchive serve, search or other windows, then click Delete again."
	if res.Status != StatusPartial || !res.Unfinished || res.Lines[0] != want {
		t.Fatalf("got:\n%s\nwant %q", res.Message(), want)
	}
	if !hasLine(res, lockedLine) || strings.Contains(res.Message(), "nothing changed") {
		t.Fatalf("%s", res.Message())
	}
	if _, err := os.Stat(filepath.Join(dir, ".mailarchive-vault.json")); err != nil {
		t.Fatal("the vault stays when step 5 stops")
	}
}

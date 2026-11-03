package archdelete

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/schedule"
)

// The slice-3 adversarial pass's settle (docs/design-archive-delete.md §14's slice-3 pass notes,
// 3 November 2026; MA-1046): a `marked` line that cannot be written stops the delete before its first
// removal (LOW-3), a failed lookup of the marker's name stops in its own words (LOW-5), and a delete
// stopped before step 6 on a drive plugged in again lets this computer's backup into the whole
// archive (LOW-2, pinned as Q1 intends).

// ma1046Steps are a plain archive's steps done before step 6, as "Steps done" words them.
func ma1046Steps(dir string) string {
	return "archive locked, scheduled backup " + schedule.DefaultNameFor(dir) + " stopped, their settings files removed, settings updated"
}

// covers: MA-1046, R36
// A `marked` line that cannot be appended stops the delete before step 6's first removal (the pass's
// LOW-3, replacing the builder's "said, and the delete goes on"): without it, slice 4's rule for a
// log and a folder that disagree would read this computer's log "as today" (DM-27), and stopping
// costs nothing yet. `finished partial` where the log can take it, the row "Delete not finished",
// every archive file still there, the marker kept (the crash table's "name created, before the first
// removal" row), in words that say what happened, name the log and the remedy (lead's words,
// owner-delegated 2026-11-02). With the log back, Delete again writes its own `marked` line and
// finishes. Positive twin first: the same delete with the log writable writes the line and finishes.
func TestMA1046_AMarkedLineThatCannotBeWrittenStopsBeforeAnyRemoval(t *testing.T) {
	{
		twin := newRig(t, plainBesidePersonsFiles(t))
		if res := twin.run(t, false); res.Status != StatusComplete || len(ma1046Marked(t, twin.x.Log)) != 1 {
			t.Fatalf("positive twin: %s", res.Message())
		}
	}
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	logPath, aside := r.x.Log, r.x.Log+".aside"
	// At step 2 (after the started line), the log becomes a folder: no line can be appended.
	r.sched.onRemove = func(string) {
		if err := os.Rename(logPath, aside); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(logPath, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	before := treeWithoutLock(t, dir)
	res := r.run(t, false)
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(aside, logPath); err != nil {
		t.Fatal(err)
	}
	head := "MailArchive could not write to its deletion log, " + logPath + " ("
	tail := "), so none of the archive's files were deleted. Steps done: " + ma1046Steps(dir) + ". Check that file, then click Delete again."
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(3), assure.Names(head, tail),
		assure.Forbid("nothing was deleted", "Nothing was changed"),
		assure.NoSideEffect(func() bool {
			after := treeWithoutLock(t, dir)
			_, marker := after[delmark.Name]
			delete(after, delmark.Name)
			return marker && sameTree(before, after)
		}))
	if res.Status != StatusPartial || !res.Unfinished || res.Removed != 0 || !strings.HasPrefix(res.Lines[0], head) || !strings.HasSuffix(res.Lines[0], tail) {
		t.Fatalf("status %q unfinished %v removed %d:\n%s", res.Status, res.Unfinished, res.Removed, res.Message())
	}
	if u, err := UnfinishedDelete(logPath, dir, ""); err != nil || u == nil {
		t.Fatalf("the row is Delete not finished: %+v %v", u, err)
	}
	if res := r.run(t, false); res.Status != StatusComplete || len(ma1046Marked(t, logPath)) != 1 || exists(filepath.Join(dir, delmark.Name)) {
		t.Fatalf("Delete again, the log back: marked lines %d:\n%s", len(ma1046Marked(t, logPath)), res.Message())
	}
}

// covers: MA-1046, R36
// A lookup of the marker's name at step 6 that fails for another reason than "not there" stops the
// delete before any removal in its own words (the pass's LOW-5): "could not check … for its note",
// never "could not write a note" — the no-anchor re-run never writes one, and nothing was written
// (lead's words, owner-delegated 2026-11-02). For an ordinary delete and for the no-anchor re-run:
// `finished partial`, the row "Delete not finished", the folder unchanged. Positive twin first: the
// lookup working, both finish as before.
func TestMA1046_ALookupThatFailsAtStep6StopsInItsOwnWords(t *testing.T) {
	ordinary := func(t *testing.T) *rig { return newRig(t, smallArchive(t, t.TempDir())) }
	noAnchor := func(t *testing.T) *rig {
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "0123456789abcdef", "folder": dir, "row": dir, "kind": "plain", "identity": idOf(t, dir)})
		if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "search.db"), "index")
		return r
	}
	for name, mk := range map[string]func(*testing.T) *rig{"ordinary": ordinary, "no anchor": noAnchor} {
		t.Run(name, func(t *testing.T) {
			{
				twin := mk(t)
				if res := twin.run(t, false); res.Status == StatusRefused || res.Status == StatusRefusedAfterStart || strings.Contains(res.Message(), "could not check") {
					t.Fatalf("positive twin: %s", res.Message())
				}
			}
			r := mk(t)
			lerr := &fs.PathError{Op: "lstat", Path: delmark.Name, Err: deniedErr("").(*fs.PathError).Err}
			old := markerLookup
			t.Cleanup(func() { markerLookup = old })
			markerLookup = func(*os.Root) (fs.FileInfo, error) { return nil, lerr }
			before := treeWithoutLock(t, r.dir)
			res := r.run(t, false)
			want := "MailArchive could not check the archive's folder for its note that a delete is under way (" + cleanErr(lerr) +
				"), so none of the archive's files were deleted. Steps done: " + ma1046Steps(r.dir) + ". Check that the folder can be read, then click Delete again."
			assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(3), assure.Names(want), assure.Forbid("could not write a note"),
				assure.NoSideEffect(func() bool { return sameTree(before, treeWithoutLock(t, r.dir)) }))
			if res.Status != StatusPartial || !res.Unfinished || res.Lines[0] != want {
				t.Fatalf("status %q unfinished %v first line:\n%q\nwant\n%q", res.Status, res.Unfinished, res.Lines[0], want)
			}
		})
	}
}

// covers: MA-1046, MA-1044, R36
// A delete stopped before step 6 (step 2's scheduler error) leaves no marker and the whole archive
// (Q1). This computer's own log refuses a backup there (G2) — until the drive is unplugged and
// plugged in again on macOS or Linux: the folder's device number changes, the log's started line no
// longer matches by its identity, and with no marker to carry the run id (§15.5's route cannot apply)
// this computer's real backup goes into the whole archive, as another computer's does under Q1 (the
// slice-3 pass's LOW-2: pinned as intended, the archive whole and nothing at risk; noted beside Q1
// and §15.6 for the owner). Windows keys by the volume serial number, which survives a remount.
func TestMA1046_AStopBeforeStep6ThenARemountLetsThisComputersBackupIntoTheWholeArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keys by the volume serial number, which survives a remount")
	}
	r := newRig(t, smallArchive(t, t.TempDir()))
	r.sched.fail[schedule.DefaultNameFor(r.dir)] = errors.New("Access is denied")
	if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished || exists(filepath.Join(r.dir, delmark.Name)) {
		t.Fatalf("precondition: stopped at step 2, no marker:\n%s", res.Message())
	}
	err := app.RefuseUnfinishedDelete(r.dir, r.x.Log)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+r.dir+")"))
	ma1045Remount(t, r.x.Log)
	if u, err := UnfinishedDelete(r.x.Log, r.dir, ""); err != nil || u != nil {
		t.Fatalf("after the remount the log no longer finds the delete: %+v %v", u, err)
	}
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{ma1046Mbox(t)}, Out: r.dir, Mode: export.Incremental, Unattended: true, DeletionsLog: r.x.Log}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatalf("this computer's backup into the whole archive after the remount: %v", err)
	}
	if !IsArchive(r.dir) || !exists(filepath.Join(r.dir, filepath.FromSlash(stemA+".html"))) {
		t.Fatal("the archive is whole")
	}
}

package archdelete

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/schedule"
)

// The delete writes its marker (docs/design-archive-delete.md §15.1, §15.2 with the owner's
// ruling Q1 of 2 November 2026, "When files start going"; §15.10 slice 3; MA-1046): just before
// step 6's first removal, under the archive lock, through step 6's root on the folder planned,
// with DM-27's `marked` line; no stop before that removal leaves one.

// ma1046Marker is the marker in dir: its bytes, and whether it is there.
func ma1046Marker(dir string) ([]byte, bool) {
	b, err := os.ReadFile(filepath.Join(dir, delmark.Name))
	return b, err == nil
}

// ma1046Started is the started line of the delete's first run in the log at path: its run id and
// its time.
func ma1046Started(t *testing.T, path string) (string, time.Time) {
	t.Helper()
	for _, l := range logLines(t, path) {
		if l["event"] == "started" {
			run, _ := l["run"].(string)
			at, err := time.Parse(time.RFC3339Nano, l["at"].(string))
			if err != nil {
				t.Fatal(err)
			}
			return run, at
		}
	}
	t.Fatal("no started line")
	return "", time.Time{}
}

// ma1046Marked are the log's `marked` lines.
func ma1046Marked(t *testing.T, path string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range logLines(t, path) {
		if l["event"] == "marked" {
			out = append(out, l)
		}
	}
	return out
}

// atFirstRemoval runs f at step 6's first removal (removeFileFn's first call), once.
func atFirstRemoval(t *testing.T, f func()) {
	t.Helper()
	var seen []string
	faultOn(t, &seen, func(string) error {
		if len(seen) == 1 {
			f()
		}
		return nil
	})
}

// covers: MA-1046, R36
// The marker holds its fixed fields and nothing else (§15.1): `what`, `version` 1, `run` (the
// started line's run id), `began` (that line's time in UTC, to the second) and the fixed `note` —
// the bytes delmark.Content gives, under 512 bytes, owner-only off Windows; the same field set for a
// plain and an encrypted archive. Nothing of the person, the computer or the archive is in it: a
// folder named with a person's name and an address-like store token, a hostname-like computer name
// and an account name are all absent, as are the folder, the kind and the archive id.
func TestMA1046_TheMarkerHoldsItsFieldsAndNothingElse(t *testing.T) {
	check := func(t *testing.T, r *rig, forbid ...string) {
		t.Helper()
		var got []byte
		var mode fs.FileMode
		atFirstRemoval(t, func() {
			got, _ = ma1046Marker(r.dir)
			if fi, err := os.Lstat(filepath.Join(r.dir, delmark.Name)); err == nil {
				mode = fi.Mode()
			}
		})
		res := r.run(t, false)
		if res.Status != StatusComplete {
			t.Fatalf("%s", res.Message())
		}
		got = assure.Reached(t, got, "the marker at the first removal")
		run, at := ma1046Started(t, r.x.Log)
		if want := delmark.Content(run, at); !bytes.Equal(got, want) || len(got) >= 512 {
			t.Fatalf("the marker's bytes:\n%s\nwant:\n%s", got, want)
		}
		var m map[string]any
		if err := json.Unmarshal(got, &m); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if strings.Join(keys, ",") != "began,note,run,version,what" || m["what"] != delmark.What || m["version"] != float64(1) ||
			m["run"] != run || m["began"] != at.UTC().Format("2006-01-02T15:04:05Z") || m["note"] != delmark.Note {
			t.Fatalf("the marker's fields: %v", m)
		}
		for _, f := range forbid {
			if f != "" && bytes.Contains(bytes.ToLower(got), bytes.ToLower([]byte(f))) {
				t.Errorf("the marker holds %q:\n%s", f, got)
			}
		}
		if runtime.GOOS != "windows" && mode.Perm() != 0o600 {
			t.Errorf("the marker's mode %v, want 0600", mode.Perm())
		}
	}

	t.Run("plain", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "Jane Doe mail")
		writeManifest(t, dir, 6, map[string]string{"path:/Users/jane/jane.doe@example.com.pst": "jane.doe@example.com"},
			[]rec{{Path: "jane.doe@example.com/Inbox/2024-03-01_0900_hello_ab12cd34.html"}})
		writeFile(t, filepath.Join(dir, "jane.doe@example.com", "Inbox", "2024-03-01_0900_hello_ab12cd34.html"), "page")
		r := newRig(t, dir)
		r.x.Computer, r.x.Account = "JANES-MACBOOK-PRO", "janedoe"
		check(t, r, "Jane", "Jane Doe", "example.com", "JANES-MACBOOK", "janedoe", dir, "plain", "Inbox")
	})
	t.Run("encrypted", func(t *testing.T) {
		dir := encryptedClean(t)
		r := encRig(t, dir)
		r.x.Computer, r.x.Account = "JANES-MACBOOK-PRO", "janedoe"
		check(t, r, "JANES-MACBOOK", "janedoe", dir, "encrypted", testArchiveID)
	})
}

// covers: MA-1046, R36
// Where it is written (Q1): after step 5, just before step 6's first removal. A recording scheduler
// sees no marker while step 2 stops the schedules, nor does the stop check before step 4; the
// first removal's seam sees it, with the `marked` line already durable in this computer's log
// (DM-27: this run, and the first run the marker names). After a complete delete it is gone and
// the log reads started, marked, folder removed, finished complete.
func TestMA1046_TheMarkerIsWrittenJustBeforeTheFirstRemovalWithItsMarkedLine(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	def := schedule.DefaultNameFor(dir)
	r.sched.state[def+"-verify"] = schedule.Installed
	var atStep2 []bool
	r.sched.onRemove = func(string) { _, there := ma1046Marker(dir); atStep2 = append(atStep2, there) }
	atStep4 := true
	r.x.BeforeStep = func(step int) error {
		if step == 4 {
			_, atStep4 = ma1046Marker(dir)
		}
		return nil
	}
	var atRemoval bool
	var linesThen []string
	atFirstRemoval(t, func() {
		_, atRemoval = ma1046Marker(dir)
		linesThen = events(logLines(t, r.x.Log))
	})
	res := r.run(t, false)
	if res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	if len(atStep2) != 2 || slices.Contains(atStep2, true) || atStep4 {
		t.Fatalf("a marker before step 6: at step 2 %v, at step 4 %v", atStep2, atStep4)
	}
	if !atRemoval || strings.Join(linesThen, ",") != "started,marked" {
		t.Fatalf("at the first removal: marker %v, log %q", atRemoval, linesThen)
	}
	run, _ := ma1046Started(t, r.x.Log)
	if m := ma1046Marked(t, r.x.Log); len(m) != 1 || m[0]["run"] != run || m[0]["first_run"] != run || m[0]["folder"] != dir {
		t.Fatalf("the marked line: %v", m)
	}
	if exists(filepath.Join(dir, delmark.Name)) || exists(dir) {
		t.Fatal("a complete delete leaves no marker and no folder")
	}
	if ev := events(logLines(t, r.x.Log)); strings.Join(ev, ",") != "started,marked,folder removed,finished complete" {
		t.Fatalf("log %q", ev)
	}
}

// covers: MA-1046, R36
// Every stop before step 6's first removal leaves no marker and no `marked` line (Q1): the typed
// name that does not match, a lock held, a plan changed since, the stop check before step 2 and
// before step 4, step 2's scheduler error, and an encrypted archive's step 5 (its working folder in
// use) and step 1 (c) (a reader open). The archive is whole, so another computer's backup — its
// own deletion log never saw the delete — goes into it as before (the owner's ruling: intended,
// pinned here), while this computer's own log still refuses here (G2, MA-1044's words).
func TestMA1046_EveryStopBeforeTheFirstRemovalLeavesNoMarker(t *testing.T) {
	noMarker := func(t *testing.T, r *rig, res *Result, what string) {
		t.Helper()
		if exists(filepath.Join(r.dir, delmark.Name)) || len(ma1046Marked(t, r.x.Log)) != 0 {
			t.Fatalf("%s: a marker or a marked line after a stop before step 6:\n%s", what, res.Message())
		}
		if !IsArchive(r.dir) {
			t.Fatalf("%s: the archive is whole", what)
		}
	}
	plain := func(t *testing.T) *rig { return newRig(t, smallArchive(t, t.TempDir())) }

	// Positive twin first: the same delete with no stop writes it.
	{
		r := plain(t)
		var there bool
		atFirstRemoval(t, func() { _, there = ma1046Marker(r.dir) })
		if res := r.run(t, false); res.Status != StatusComplete || !there {
			t.Fatalf("positive twin: marker %v:\n%s", there, res.Message())
		}
	}

	t.Run("refusals", func(t *testing.T) {
		r := plain(t)
		p := r.plan(t)
		noMarker(t, r, Execute(p, "not its name", false, r.x), "the typed name")
		writeManifest(t, r.dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}, {Path: stemB + ".html"}})
		noMarker(t, r, Execute(p, p.Name, false, r.x), "a changed plan")
		r.x.BeforeStep = func(step int) error {
			if step == 2 {
				return errors.New("A MailArchive Desktop is running")
			}
			return nil
		}
		noMarker(t, r, r.run(t, false), "the stop check before step 2")
	})
	t.Run("before step 4", func(t *testing.T) {
		r := plain(t)
		r.x.BeforeStep = func(step int) error {
			if step == 4 {
				return errors.New("A MailArchive Desktop is running")
			}
			return nil
		}
		res := r.run(t, false)
		if res.Status != StatusPartial || !res.Unfinished {
			t.Fatalf("%s", res.Message())
		}
		noMarker(t, r, res, "the stop check before step 4")
	})
	t.Run("step 2's scheduler error, and the backups after it", func(t *testing.T) {
		r := plain(t)
		r.sched.fail[schedule.DefaultNameFor(r.dir)] = errors.New("Access is denied")
		res := r.run(t, false)
		if res.Status != StatusPartial || !res.Unfinished {
			t.Fatalf("%s", res.Message())
		}
		noMarker(t, r, res, "step 2's scheduler error")
		mbox := ma1046Mbox(t)
		// This computer's own log refuses here (G2).
		err := app.RefuseUnfinishedDelete(r.dir, r.x.Log)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
			assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+r.dir+")"))
		// Another computer, whose deletion log never saw the delete, backs up into the whole archive.
		another := filepath.Join(t.TempDir(), LogName)
		if _, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: r.dir, Mode: export.Incremental, Unattended: true, DeletionsLog: another}, log.New(io.Discard, "", 0), nil); err != nil {
			t.Fatalf("another computer's backup into a whole archive whose delete stopped at step 2: %v", err)
		}
	})
	t.Run("encrypted: step 1 (c) and step 5", func(t *testing.T) {
		// Step 1 (c): its working folder holds a file MailArchive did not put there.
		dir := encryptedClean(t)
		r := encRig(t, dir)
		writeFile(t, filepath.Join(scratchFor(r.scratch, dir, testArchiveID), "theirs.txt"), "not MailArchive's")
		res := r.run(t, false)
		if res.Status != StatusRefusedAfterStart {
			t.Fatalf("step 1 (c): %s", res.Message())
		}
		noMarker(t, r, res, "step 1 (c)")
		// Step 5: the file appears after step 1 (c) checked.
		dir = encryptedClean(t)
		r = encRig(t, dir)
		s := scratchFor(r.scratch, dir, testArchiveID)
		writeFile(t, filepath.Join(s, "snap", ".mailarchive-snapshot-3"), "x")
		r.sched.onRemove = func(string) { writeFile(t, filepath.Join(s, "snap", "other.txt"), "y") }
		res = r.run(t, false)
		if res.Status != StatusPartial || !res.Unfinished || !strings.HasPrefix(res.Lines[0], "MailArchive's working folder for this archive is in use") {
			t.Fatalf("step 5: %s", res.Message())
		}
		noMarker(t, r, res, "step 5")
	})
}

// ma1046Mbox is an mbox of one message to back up.
func ma1046Mbox(t *testing.T) string {
	t.Helper()
	mbox := filepath.Join(t.TempDir(), "mail.mbox")
	if err := os.WriteFile(mbox, []byte("From a@b Mon Jan  1 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return mbox
}

// covers: MA-1046, R36
// A marker name that cannot be created stops the delete before any removal (Q1): steps 2 to 5 have
// run, so it is never "nothing changed" — `finished partial`, the row "Delete not finished", every
// archive file still there, no `marked` line, in §15.2's Q1 note's words listing the steps done;
// a full drive in DM-7's words (lead's words, owner-delegated 2026-11-02). Delete again, the cause
// gone, writes it and finishes. Positive twin first: the same archive, the name created.
func TestMA1046_ANameThatCannotBeCreatedStopsBeforeAnyRemoval(t *testing.T) {
	failCreate := func(t *testing.T, err error) {
		old := markerCreate
		t.Cleanup(func() { markerCreate = old })
		markerCreate = func(*os.Root) (*os.File, error) { return nil, err }
	}
	steps := "archive locked, scheduled backup %s stopped, their settings files removed, settings updated"
	denied := &fs.PathError{Op: "open", Path: delmark.Name, Err: deniedErr("").(*fs.PathError).Err}
	full := &fs.PathError{Op: "open", Path: delmark.Name, Err: fullErr()}
	for name, c := range map[string]struct {
		err  error
		want string
	}{
		"denied": {denied, "MailArchive could not write a note into the archive's folder that a delete is under way (" + cleanErr(denied) + "), so none of the archive's files were deleted. Steps done: " + steps + ". Check that the folder can be written, then click Delete again."},
		"full":   {full, "The drive is full, so none of the archive's files were deleted. Steps done: " + steps + ". Free a little space, then click Delete again."},
	} {
		t.Run(name, func(t *testing.T) {
			dir := smallArchive(t, t.TempDir())
			r := newRig(t, dir)
			def := schedule.DefaultNameFor(dir)
			before := treeWithoutLock(t, dir)
			failCreate(t, c.err)
			res := r.run(t, false)
			want := strings.Replace(c.want, "%s", def, 1)
			assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(3), assure.Names(want), assure.Forbid("nothing was deleted", "Nothing was changed", "nothing changed"),
				assure.NoSideEffect(func() bool {
					return sameTree(before, treeWithoutLock(t, dir)) && len(ma1046Marked(t, r.x.Log)) == 0
				}))
			if res.Status != StatusPartial || !res.Unfinished || res.Lines[0] != want || res.Removed != 0 {
				t.Fatalf("status %q unfinished %v first line:\n%q\nwant\n%q", res.Status, res.Unfinished, res.Lines[0], want)
			}
			if ev := events(logLines(t, r.x.Log)); strings.Join(ev, ",") != "started,finished partial" {
				t.Fatalf("log %q", ev)
			}
			if u, err := UnfinishedDelete(r.x.Log, dir, ""); err != nil || u == nil {
				t.Fatalf("the row is Delete not finished: %+v %v", u, err)
			}
			markerCreate = func(root *os.Root) (*os.File, error) {
				return root.OpenFile(delmark.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			}
			var there bool
			atFirstRemoval(t, func() { _, there = ma1046Marker(dir) })
			if res := r.run(t, false); res.Status != StatusComplete || !there {
				t.Fatalf("Delete again: marker %v:\n%s", there, res.Message())
			}
		})
	}
}

// covers: MA-1046, R36
// Bytes that cannot be written leave an empty marker and the delete goes on (§15.2 point 3): an
// empty file is a marker (presence is the signal) — another computer, whose log never saw the
// delete, finds a delete only the marker knows, its details not known — and the delete still
// removes it last. Positive twin first: the bytes written give the marker's run.
func TestMA1046_BytesThatCannotBeWrittenLeaveAnEmptyMarkerAndTheDeleteGoesOn(t *testing.T) {
	another := filepath.Join(t.TempDir(), LogName)
	{
		twin := newRig(t, smallArchive(t, t.TempDir()))
		var u *Unfinished
		atFirstRemoval(t, func() { u, _ = UnfinishedAt(another, twin.dir, "") })
		if res := twin.run(t, false); res.Status != StatusComplete || u == nil || !u.MarkerOnly || !u.MarkerDetails.Known {
			t.Fatalf("positive twin: %+v\n%s", u, res.Message())
		}
	}
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	old := markerWrite
	t.Cleanup(func() { markerWrite = old })
	markerWrite = func(*os.File, []byte) error { return errors.New("no space left on device") }
	var got []byte
	there := false
	var u *Unfinished
	var uerr error
	atFirstRemoval(t, func() {
		got, there = ma1046Marker(dir)
		u, uerr = UnfinishedAt(another, dir, "")
	})
	res := r.run(t, false)
	if res.Status != StatusComplete || !there || len(got) != 0 {
		t.Fatalf("an empty marker, and the delete goes on: there %v, %d bytes:\n%s", there, len(got), res.Message())
	}
	if uerr != nil || u == nil || !u.MarkerOnly || u.Marker != delmark.Regular || u.MarkerDetails.Known {
		t.Fatalf("another computer: a delete only the empty marker knows: %+v %v", u, uerr)
	}
	if exists(filepath.Join(dir, delmark.Name)) {
		t.Fatal("the empty marker is removed last as any other")
	}
}

// covers: MA-1046, R36
// The marker is written through step 6's root on the very folder planned, never by path (§15.2
// point 1): a folder swapped for a link to another folder after the re-check gets no file there,
// nor in the folder moved away; the delete stops with msgChanged, nothing removed.
func TestMA1046_AFolderSwappedForALinkAfterTheRecheckGetsNoMarker(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Mail")
	smallArchive(t, dir)
	other := smallArchive(t, filepath.Join(t.TempDir(), "Elsewhere"))
	moved := dir + " moved"
	r := newRig(t, dir)
	swapped := false
	r.sched.onRemove = func(string) {
		if err := os.Rename(dir, moved); err != nil {
			if runtime.GOOS == "windows" {
				return // Windows will not rename a folder whose lock file the delete holds open
			}
			t.Fatal(err)
		}
		if err := os.Symlink(other, dir); err != nil {
			if runtime.GOOS == "windows" {
				return // a link needs a privilege the run may not have
			}
			t.Fatal(err)
		}
		swapped = true
	}
	res := r.run(t, false)
	if !swapped {
		t.Skip("Windows: the folder cannot be swapped while the delete holds its lock file open, or no symbolic-link privilege")
	}
	if res.Status != StatusPartial || res.Lines[0] != msgChanged {
		t.Fatalf("%s", res.Message())
	}
	for _, d := range []string{other, moved} {
		if exists(filepath.Join(d, delmark.Name)) || !IsArchive(d) {
			t.Fatalf("%s: a marker or a removal through the swapped path", d)
		}
	}
}

// covers: MA-1046, R36
// The name is created with O_EXCL (§15.2 point 2): a link already at the name is never followed —
// not to a person's file inside the folder (its bytes unchanged), not to nothing (no file made).
// At step 6, a link planted at the name after the re-check stops the delete before any removal in
// words that list the steps done, the link left as it is. Positive twin first: a free name.
func TestMA1046_TheCreateNeverFollowsALinkAtTheName(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if wrote, err := createMarkerIn(root, []byte("x")); err != nil || !wrote {
		t.Fatalf("positive twin: %v %v", wrote, err)
	}
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "notes.txt"), "the person's notes")
	if err := os.Symlink("notes.txt", filepath.Join(dir, delmark.Name)); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("a link needs a privilege the run may not have")
		}
		t.Fatal(err)
	}
	if _, err := createMarkerIn(root, []byte("MARKER")); err == nil {
		t.Fatal("a link at the name was followed")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "the person's notes" {
		t.Fatalf("the person's file changed: %q", b)
	}
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("made.txt", filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if _, err := createMarkerIn(root, []byte("MARKER")); err == nil || exists(filepath.Join(dir, "made.txt")) {
		t.Fatalf("a dangling link at the name was followed: %v", err)
	}

	// At step 6.
	arch := smallArchive(t, t.TempDir())
	r := newRig(t, arch)
	r.sched.onRemove = func(string) {
		if err := os.Symlink("index.html", filepath.Join(arch, delmark.Name)); err != nil {
			t.Fatal(err)
		}
	}
	before := treeWithoutLock(t, arch)
	before[delmark.Name] = "link→index.html" // the planted link, and nothing else changed
	res := r.run(t, false)
	want := "This archive's folder holds " + delmark.Name + ", which MailArchive did not write (it is a link, a folder or a device), so none of the archive's files were deleted. Steps done: archive locked, scheduled backup " +
		schedule.DefaultNameFor(arch) + " stopped, their settings files removed, settings updated. Remove it by hand, then click Delete again."
	if res.Status != StatusPartial || !res.Unfinished || res.Lines[0] != want || !sameTree(before, treeWithoutLock(t, arch)) {
		t.Fatalf("got:\n%s\nwant first line %q", res.Message(), want)
	}
	if fi, err := os.Lstat(filepath.Join(arch, delmark.Name)); err != nil || fi.Mode()&fs.ModeSymlink == 0 || exists(filepath.Join(arch, "index.html")) {
		t.Fatalf("the link is left as it is, never followed: %v", err)
	}
}

// covers: MA-1046, R36
// A re-run keeps a marker already there and writes one with the first run's id when none is
// there (§15.1, §15.2 points 2 and 4; DM-26):
//   - a first run stopped at step 6 left its marker: Delete again finds it byte for byte, adds its
//     own `marked` line naming that first run, and finishes;
//   - a first run stopped at step 2 left none: Delete again writes one with the first run's id and
//     start, not its own;
//   - another delete's marker (another run id) and an empty one are kept as they are (never
//     rewritten or filled in), with no `marked` line.
func TestMA1046_AReRunKeepsTheMarkerAndWritesOneWithTheFirstRunsIdWhenNoneIsThere(t *testing.T) {
	t.Run("kept from a stop in step 6", func(t *testing.T) {
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		fault := true
		faultOn(t, nil, func(rel string) error {
			if fault && rel == stemA+".html" {
				return inUseErr(rel)
			}
			return nil
		})
		if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished {
			t.Fatalf("%s", res.Message())
		}
		first, there := ma1046Marker(dir)
		run, _ := ma1046Started(t, r.x.Log)
		if !there || !IsArchive(dir) {
			t.Fatal("a fault on the first removal leaves the whole archive and its marker")
		}
		fault = false
		var again []byte
		atFirstRemoval(t, func() { again, _ = ma1046Marker(dir) })
		if res := r.run(t, false); res.Status != StatusComplete {
			t.Fatalf("Delete again: %s", res.Message())
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("the re-run rewrote the marker:\n%s\n%s", first, again)
		}
		m := ma1046Marked(t, r.x.Log)
		if len(m) != 2 || m[1]["first_run"] != run || m[1]["run"] == run {
			t.Fatalf("the re-run's marked line: %v", m)
		}
	})
	t.Run("written with the first run's id", func(t *testing.T) {
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		def := schedule.DefaultNameFor(dir)
		r.sched.fail[def] = errors.New("Access is denied")
		if res := r.run(t, false); res.Status != StatusPartial {
			t.Fatalf("%s", res.Message())
		}
		run, at := ma1046Started(t, r.x.Log)
		delete(r.sched.fail, def)
		var got []byte
		atFirstRemoval(t, func() { got, _ = ma1046Marker(dir) })
		if res := r.run(t, false); res.Status != StatusComplete {
			t.Fatalf("Delete again: %s", res.Message())
		}
		if want := delmark.Content(run, at); !bytes.Equal(got, want) {
			t.Fatalf("the re-run's marker:\n%s\nwant the first run's:\n%s", got, want)
		}
	})
	for name, planted := range map[string][]byte{
		"another delete's": delmark.Content("fedcba9876543210", time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)),
		"an empty one":     {},
	} {
		t.Run(name, func(t *testing.T) {
			dir := smallArchive(t, t.TempDir())
			r := newRig(t, dir)
			writeFile(t, filepath.Join(dir, delmark.Name), string(planted))
			var got []byte
			there := false
			atFirstRemoval(t, func() { got, there = ma1046Marker(dir) })
			if res := r.run(t, false); res.Status != StatusComplete {
				t.Fatalf("%s", res.Message())
			}
			// Changed 3 November 2026 (§15.4, §15.10 slice 4, MA-1051): the plan now finishes the delete a
			// readable marker names, so the marker is this delete's own and its `marked` line names that
			// run (DM-27); an empty marker names none, and gets no `marked` line. Kept as it is either way.
			marked := ma1046Marked(t, r.x.Log)
			wantMarked := 0
			if len(planted) > 0 {
				wantMarked = 1
			}
			if !there || !bytes.Equal(got, planted) || len(marked) != wantMarked || (wantMarked == 1 && marked[0]["first_run"] != "fedcba9876543210") {
				t.Fatalf("kept as it is, its marked line naming its run only when it names one: %q (marked %v)", got, marked)
			}
			if exists(filepath.Join(dir, delmark.Name)) {
				t.Fatal("removed last with the archive, as the delete's own is")
			}
		})
	}
}

// covers: MA-1046, R36
// The no-anchor re-run never creates a marker (DM-9): a folder whose manifest is gone, with this
// computer's started line and a person's file, has no marker at any removal nor after, and ends as
// §5.4 says, `finished partial` naming the file it can no longer identify — never complete
// (MA-1047's corrected line, case (i)). Positive twin first: the same delete with its manifest
// writes one.
func TestMA1046_ANoAnchorReRunNeverCreatesAMarker(t *testing.T) {
	{
		r := newRig(t, smallArchive(t, t.TempDir()))
		var there bool
		atFirstRemoval(t, func() { _, there = ma1046Marker(r.dir) })
		if res := r.run(t, false); res.Status != StatusComplete || !there {
			t.Fatalf("positive twin: %v %s", there, res.Message())
		}
	}
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "0123456789abcdef", "folder": dir, "row": dir, "kind": "plain", "identity": idOf(t, dir)})
	if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "search.db"), "index")
	writeFile(t, filepath.Join(dir, "taxes.pdf"), "the person's")
	ever := false
	faultOn(t, nil, func(string) error {
		if _, there := ma1046Marker(dir); there {
			ever = true
		}
		return nil
	})
	p := r.plan(t)
	if p.Rerun != RerunNoAnchor {
		t.Fatalf("rerun %v", p.Rerun)
	}
	res := Execute(p, p.Name, false, r.x)
	if ever || exists(filepath.Join(dir, delmark.Name)) || len(ma1046Marked(t, r.x.Log)) != 0 {
		t.Fatal("the no-anchor re-run created a marker")
	}
	if res.Status != StatusPartial || !res.Unfinished || !strings.Contains(res.Lines[0], "the folder still holds 2 files the delete can no longer identify") {
		t.Fatalf("%s", res.Message())
	}
}

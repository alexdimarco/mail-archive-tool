package archdelete

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// The log after a remount (docs/design-archive-delete.md §15.5 with DM-6 and DM-10; §15.10 slice 5;
// MA-1050): on macOS and Linux a drive plugged in again can get another device number, so this
// computer's own started line no longer matches its folder by identity; the folder's delete marker,
// naming the delete's run, finds it again — with a second proof each time (the line's path or row, or
// its recorded identity), so a marker copied elsewhere matches nothing. A remount is simulated by
// recording another device number in the log (ma1045Remount), a path change by renaming the folder on
// the same volume (its identity kept, as Windows' volume serial number is under another letter).

// ma1050Stopped is a real delete of a plain archive at dir (with the person's files beside it) that
// stopped in step 6 before the anchor, on this computer: the folder holds the manifest and the
// marker, the log a started line, its `marked` line and a partial. It returns the rig and the run the
// marker names.
func ma1050Stopped(t *testing.T, dir string) (*rig, string) {
	t.Helper()
	r := newRig(t, dir)
	ma1057StopInStep6(t, r)
	d := delmark.Read(dir)
	if !d.Known {
		t.Fatal("precondition: the marker names its run")
	}
	return r, d.Run
}

// ma1050Moved is plainBesidePersonsFiles moved to a folder the test names, so it can be renamed again.
func ma1050Moved(t *testing.T, to string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(plainBesidePersonsFiles(t), to); err != nil {
		t.Fatal(err)
	}
	return to
}

// covers: MA-1050, MA-1044, R36
// §15.5's run-id route: after a remount this computer's own delete is still this folder's — the row
// says "Delete not finished" in the log's words (never the marker-only line), `status` says it, every
// action is refused in MA-1015's words (never "begun on another computer"), and Delete again plans
// with that line's state: it finishes that delete (its first run, its captured values), not one
// "begun on another computer", and ends it in this log. Positive twin first: before the remount the
// line matches by identity alone, and after it the identity alone matches nothing (the remount is
// real).
func TestMA1050_ThisComputersLogStillFindsItsDeleteAfterARemount(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r, run := ma1050Stopped(t, dir)
	if u, err := UnfinishedAt(r.x.Log, dir, ""); err != nil || u == nil || u.MarkerOnly || u.ByRun {
		t.Fatalf("positive twin: before the remount, by identity: %+v %v", u, err)
	}
	ma1045Remount(t, r.x.Log)
	if u, err := UnfinishedDelete(r.x.Log, dir, ""); err != nil || u != nil {
		t.Fatalf("precondition: the device and file number alone no longer find the delete: %+v %v", u, err)
	}

	u, err := UnfinishedAt(r.x.Log, dir, "")
	if err != nil || u == nil || u.MarkerOnly || !u.ByRun || u.FirstRun != run || u.BegunHere {
		t.Fatalf("the run-id route: %+v %v", u, err)
	}
	ru, rerr := NewRowsCheck(r.x.Deps).State(dir, true)
	if rerr != nil || ru == nil || ru.MarkerOnly || MarkerRowText(ru) != "" {
		t.Fatalf("the row keeps the log's \"Delete not finished\": %+v %v %q", ru, rerr, MarkerRowText(ru))
	}
	if su, serr := UnfinishedToSay(r.x.Log, dir, ""); serr != nil || su == nil || su.MarkerOnly {
		t.Fatalf("status says the log's line: %+v %v", su, serr)
	}
	names := func() []string { n := ma1044Names(dir); slices.Sort(n); return n }
	before := names()
	err = ActionRefusal(r.x.Log, dir, "it was not paused")
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not paused; nothing was changed."),
		assure.Forbid("another computer"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
	// Remove from list (MA-1029) and uninstall (MA-1007) still see this computer's delete, as the row
	// says it; another computer's log (positive twin) does not, as slice 4 allows (DM-21).
	{
		other := r.x.Deps
		other.Log = filepath.Join(t.TempDir(), LogName)
		if err := UnlistRefusal(dir, other); err != nil {
			t.Fatalf("positive twin: another computer's Remove from list: %v", err)
		}
	}
	for name, err := range map[string]error{"the route": UnlistRefusal(dir, r.x.Deps), "the page": NewRowsCheck(r.x.Deps).UnlistRefusal(dir)} {
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(msgUnlistUnfinished),
			assure.NoSideEffect(func() bool { return len(r.rows) == 0 }))
		if c := ma1045Case(err); c != CaseUnfinished {
			t.Fatalf("%s: Remove from list's case %q", name, c)
		}
	}
	if pend, err := PendingDeletes(r.x.Log, ""); err != nil || len(pend) != 1 || pend[0] != dir {
		t.Fatalf("uninstall still sees the delete: %q %v", pend, err)
	}

	p := r.plan(t)
	if p.Finishes == nil || p.Finishes.FromMarker || p.Finishes.FirstRun != run || p.Finishes.Captured == nil || len(p.FinishesNotes()) != 0 || p.Disagrees {
		t.Fatalf("Delete again plans with this computer's line: %+v notes %q", p.Finishes, p.FinishesNotes())
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || exists(filepath.Join(dir, delmark.Name)) || exists(filepath.Join(dir, manifestName)) || !exists(filepath.Join(dir, "taxes.pdf")) {
		t.Fatalf("Delete again finishes it:\n%s", res.Message())
	}
	if l := ma1051StartedLine(t, r.x.Log); l["first_run"] != run || l["from_marker"] == true {
		t.Fatalf("its started line carries this computer's delete: %v", l)
	}
	if u, _ := UnfinishedAt(r.x.Log, dir, ""); u != nil {
		t.Fatalf("the log's delete ended: %+v", u)
	}
}

// covers: MA-1050, R36
// The run-id route needs the marker to name the line's run: a folder on another drive whose file
// number happens to be the one the line recorded (two rotation drives formatted alike) and no marker
// is not this delete's — nothing refused, no "This finishes" — nor is one whose marker names another
// run (only the marker then knows a delete), nor a folder found after the line's `folder removed`, a
// new folder whatever marker it holds. Positive twin first: the same folder whose marker names the
// line's run is this delete's.
func TestMA1050_TheRunIdRouteNeedsTheMarkerToNameTheLinesRun(t *testing.T) {
	const run = "2222222222222222"
	dir := smallArchive(t, filepath.Join(t.TempDir(), "Rotation"))
	r := newRig(t, dir)
	id := idOf(t, dir)
	id["volume"] = id["volume"].(uint64) + 1 // the drive the delete began on had another device number
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": run, "folder": dir, "row": dir, "kind": "plain", "identity": id})
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:01Z", "event": "marked", "run": run, "folder": dir, "first_run": run})
	marker := func(run string) {
		t.Helper()
		writeFile(t, filepath.Join(dir, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"`+run+`","began":"2026-11-01T10:00:00Z","note":"x"}`)
	}

	// Positive twin: the marker names the line's run.
	marker(run)
	if u, err := UnfinishedAt(r.x.Log, dir, ""); err != nil || u == nil || u.MarkerOnly || !u.ByRun || u.Run != run {
		t.Fatalf("positive twin: the marker names the line's run: %+v %v", u, err)
	}

	// The rotation drive: the same file number, another device number, no marker.
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if u, err := UnfinishedAt(r.x.Log, dir, ""); err != nil || u != nil {
		t.Fatalf("the rotation drive is not this delete's: %+v %v", u, err)
	}
	if u, err := NewRowsCheck(r.x.Deps).State(dir, true); err != nil || u != nil {
		t.Fatalf("its row says nothing of a delete: %+v %v", u, err)
	}
	if err := BackupRefusal(r.x.Log, dir); err != nil {
		t.Fatalf("its backup is not refused: %v", err)
	}
	if p := r.plan(t); p.Finishes != nil || p.Disagrees || len(p.FinishesNotes()) != 0 {
		t.Fatalf("its plan claims nothing: %+v", p.Finishes)
	}

	// A marker naming another run: only the marker knows a delete.
	marker("3333333333333333")
	if u, err := UnfinishedAt(r.x.Log, dir, ""); err != nil || u == nil || !u.MarkerOnly || u.ByRun {
		t.Fatalf("a marker naming another run: %+v %v", u, err)
	}

	// After the line's `folder removed`: a new folder, whatever marker it holds.
	marker(run)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:02Z", "event": "folder removed", "run": run, "folder": dir})
	if u, err := UnfinishedAt(r.x.Log, dir, ""); err != nil || u == nil || !u.MarkerOnly || u.ByRun {
		t.Fatalf("a folder found after `folder removed`: %+v %v", u, err)
	}
}

// covers: MA-1050, R36
// DM-6: Delete again of a folder whose anchor is gone, after a remount, is §5.4's no-anchor re-run
// planned from this computer's own line — not "This folder no longer holds an archive. Use Remove
// from list." — so it removes what §4 names, removes the marker, keeps the person's file and ends
// the delete complete in this log, its row removed. With an encrypted archive's line, the plan takes
// that line's record and its `key forgotten` state, never a new lookup (R3:NF-NF2-RERUN). Positive
// twin first: before the remount the same re-run is planned by identity.
func TestMA1050_ANoAnchorReRunAfterARemountFinishesFromThisComputersLine(t *testing.T) {
	dir := smallArchive(t, filepath.Join(t.TempDir(), "Mail"))
	writeFile(t, filepath.Join(dir, "taxes.pdf"), "the person's")
	r := newRig(t, dir)
	fault := true
	faultOn(t, nil, func(rel string) error {
		if fault && rel == delmark.Name {
			return inUseErr(rel)
		}
		return nil
	})
	if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished || exists(filepath.Join(dir, manifestName)) || !exists(filepath.Join(dir, delmark.Name)) {
		t.Fatalf("precondition: the delete stops on the marker, after the anchor:\n%s", res.Message())
	}
	fault = false
	run := delmark.Read(dir).Run
	if p := r.plan(t); p.Rerun != RerunNoAnchor || p.Finishes == nil || p.Finishes.ByRun {
		t.Fatalf("positive twin: before the remount: %v %+v", p.Rerun, p.Finishes)
	}
	ma1045Remount(t, r.x.Log)
	p := r.plan(t)
	if p.Rerun != RerunNoAnchor || p.Finishes == nil || p.Finishes.FromMarker || !p.Finishes.ByRun || p.Finishes.FirstRun != run {
		t.Fatalf("after the remount, from this computer's line: %v %+v", p.Rerun, p.Finishes)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || exists(filepath.Join(dir, delmark.Name)) || !exists(filepath.Join(dir, "taxes.pdf")) || len(r.rows) != 1 {
		t.Fatalf("Delete again ends complete, the person's file kept (rows %q):\n%s", r.rows, res.Message())
	}
	if u, _ := UnfinishedAt(r.x.Log, dir, ""); u != nil {
		t.Fatalf("the log's delete ended: %+v", u)
	}

	// An encrypted archive's line: its record and its `key forgotten` state.
	const encRun = "4444444444444444"
	const lineID = "ffffffffffffffffffffffffffffffff"
	vault := filepath.Join(t.TempDir(), "Vault")
	writeFile(t, filepath.Join(vault, ".mailarchive-lastverify.json"), "{}")
	writeFile(t, filepath.Join(vault, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"`+encRun+`","began":"2026-11-01T10:00:00Z","note":"x"}`)
	e := newRig(t, vault)
	e.x.Deps.Record = recordedAt(vault) // a new lookup would give testArchiveID
	id := idOf(t, vault)
	id["volume"] = id["volume"].(uint64) + 1
	appendRawLog(t, e.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": encRun, "folder": vault, "row": vault, "kind": "encrypted",
		"record_id": lineID, "record_spelling": vault, "identity": id})
	appendRawLog(t, e.x.Log, map[string]any{"at": "2026-11-01T10:00:01Z", "event": "marked", "run": encRun, "folder": vault, "first_run": encRun})
	appendRawLog(t, e.x.Log, map[string]any{"at": "2026-11-01T10:00:02Z", "event": "key forgotten", "run": encRun, "folder": vault})
	appendRawLog(t, e.x.Log, map[string]any{"at": "2026-11-01T10:00:03Z", "event": "finished", "run": encRun, "folder": vault, "outcome": "partial", "unfinished": true, "removed": 3})
	ep := e.plan(t)
	if ep.Rerun != RerunNoAnchor || ep.Kind != Encrypted || ep.Record.ID != lineID || !ep.KeyForgottenBefore || ep.Finishes == nil || ep.Finishes.FromMarker {
		t.Fatalf("the encrypted line's record and key state: kind %v record %+v forgotten %v finishes %+v", ep.Kind, ep.Record, ep.KeyForgottenBefore, ep.Finishes)
	}
}

// covers: MA-1050, MA-1007, R36
// DM-10 items 1 and 2: the drive back under another letter (another path, the same identity — a
// rename on the same volume here). The identity route finds this computer's delete at the new path —
// the marker names its run AND the line's recorded identity is the folder's — while a copy of the
// marker in another folder (another identity) matches nothing; and finishing it at the new path ends
// the delete under the old path too (the `first_run` ending), so the old row clears and uninstall is
// no longer blocked by it (another unfinished delete of this computer, kept in the log, still blocks
// it). Positive twin first: the path alone finds nothing at the new place, and the old row is still
// "Delete not finished".
func TestMA1050_APathChangedWithTheIdentityKeptIsFoundAndFinishingThereClearsTheOldRow(t *testing.T) {
	base := t.TempDir()
	oldPath := ma1050Moved(t, filepath.Join(base, "E", "Mail"))
	r, run := ma1050Stopped(t, oldPath)
	newPath := filepath.Join(base, "F", "Mail")
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if u, err := UnfinishedDelete(r.x.Log, newPath, ""); err != nil || u != nil {
		t.Fatalf("positive twin: the path alone finds nothing at the new place: %+v %v", u, err)
	}
	if u, err := UnfinishedDelete(r.x.Log, oldPath, ""); err != nil || u == nil {
		t.Fatalf("positive twin: the old row is \"Delete not finished\": %+v %v", u, err)
	}

	u, err := UnfinishedAt(r.x.Log, newPath, "")
	if err != nil || u == nil || u.MarkerOnly || !u.ByRun || u.FirstRun != run {
		t.Fatalf("the identity route: %+v %v", u, err)
	}
	// A copy of the marker in another folder: the run matches, the identity does not.
	copyDir := smallArchive(t, filepath.Join(base, "G", "Copy"))
	b, err := os.ReadFile(filepath.Join(newPath, delmark.Name))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(copyDir, delmark.Name), string(b))
	if cu, err := UnfinishedAt(r.x.Log, copyDir, ""); err != nil || cu == nil || !cu.MarkerOnly || cu.ByRun {
		t.Fatalf("a copied marker is not this computer's line: %+v %v", cu, err)
	}

	// Another delete of this computer, of another folder, still unfinished: the log stays one that
	// may hold an unfinished delete, so its lines are read whole.
	elsewhere := filepath.Join(base, "H", "Other")
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-02T09:00:00Z", "event": "started", "run": "9999999999999999", "folder": elsewhere, "row": elsewhere, "kind": "plain"})

	// Finish it at the new path.
	r.dir, r.x.Row, r.kr.folder = newPath, newPath, newPath
	p := r.plan(t)
	if p.Finishes == nil || p.Finishes.FromMarker || p.Finishes.FirstRun != run {
		t.Fatalf("the plan at the new path finishes this computer's delete: %+v", p.Finishes)
	}
	if res := Execute(p, p.Name, false, r.x); res.Status != StatusComplete {
		t.Fatalf("finished at the new path:\n%s", res.Message())
	}
	if u, err := UnfinishedDelete(r.x.Log, oldPath, ""); err != nil || u != nil {
		t.Fatalf("the old row clears: %+v %v", u, err)
	}
	if pend, err := PendingDeletes(r.x.Log, ""); err != nil || len(pend) != 1 || pend[0] != elsewhere {
		t.Fatalf("uninstall is blocked only by the other delete: %q %v", pend, err)
	}
}

// covers: MA-1050, MA-1051, R36
// DM-10's third case: the drive back under another path AND another device number (macOS
// "/Volumes/Name 1", a Linux label with a number added). Neither route matches this computer's line,
// so only the marker knows the delete here — but its run is one this computer's log began, so the row,
// the backup's refusal and the plan say it was begun on this computer, never "on another computer or
// user account"; the plan is made from the marker (§15.4), and finishing it at the new path ends the
// delete under the old path too, through the `first_run` ending. Positive twin first: the same folder
// seen by another computer's log gets the marker-only words.
func TestMA1050_TheMarkersRunInThisComputersLogIsSaidToBeBegunHere(t *testing.T) {
	base := t.TempDir()
	oldPath := ma1050Moved(t, filepath.Join(base, "Volumes", "Name"))
	r, run := ma1050Stopped(t, oldPath)
	newPath := filepath.Join(base, "Volumes", "Name 1")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	ma1045Remount(t, r.x.Log)

	other := filepath.Join(t.TempDir(), LogName)
	if ou, err := UnfinishedAt(other, newPath, ""); err != nil || ou == nil || !ou.MarkerOnly || ou.BegunHere ||
		!strings.Contains(MarkerRowText(ou), "it was begun on another computer or user account") {
		t.Fatalf("positive twin: another computer's log: %+v %v", ou, err)
	}
	// A computer that only worked on it from the marker (its started line carries the marker's run as
	// first_run, `from_marker`) did not begin it: the marker-only words there too.
	appendRawLog(t, other, map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "8888888888888888", "folder": filepath.Join(base, "elsewhere"),
		"kind": "plain", "first_run": run, "from_marker": true, "identity": map[string]any{"volume": 1, "file_id": 1}})
	if ou, err := UnfinishedAt(other, newPath, ""); err != nil || ou == nil || !ou.MarkerOnly || ou.BegunHere {
		t.Fatalf("a first run carried from a marker is not begun here: %+v %v", ou, err)
	}

	u, err := UnfinishedAt(r.x.Log, newPath, "")
	if err != nil || u == nil || !u.MarkerOnly || u.ByRun || !u.BegunHere {
		t.Fatalf("begun here: %+v %v", u, err)
	}
	want := "Delete not finished: a delete of this archive begun " + MarkerBegun(u) + " did not finish. It was begun on this computer, which no longer matches it to this folder (the drive may have been plugged in again under another name). Click Delete… to finish it."
	if got := MarkerRowText(u); MarkerBegun(u) == "" || got != want {
		t.Fatalf("the row:\n got %q\nwant %q", got, want)
	}
	if ru, _ := NewRowsCheck(r.x.Deps).State(newPath, true); ru == nil || MarkerRowText(ru) != want {
		t.Fatalf("the page's row: %+v", ru)
	}
	if su, err := UnfinishedToSay(r.x.Log, newPath, ""); err != nil || su == nil || !su.BegunHere {
		t.Fatalf("status: %+v %v", su, err)
	}
	names := func() []string { n := ma1044Names(newPath); slices.Sort(n); return n }
	before := names()
	err = BackupRefusal(r.x.Log, newPath)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names(delmark.BegunHereWords(delmark.WhatNotBackedUp)),
		assure.Forbid("another computer", "click Delete again"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
	if c := ma1045Case(err); c != CaseUnfinished {
		t.Fatalf("case %q", c)
	}

	r.dir, r.x.Row, r.kr.folder = newPath, newPath, newPath
	p := r.plan(t)
	if p.Finishes == nil || !p.Finishes.FromMarker || !p.Finishes.BegunHere || p.Finishes.FirstRun != run {
		t.Fatalf("the plan from the marker: %+v", p.Finishes)
	}
	if notes := p.FinishesNotes(); len(notes) != 1 || notes[0] != MsgMarkerBegunHere {
		t.Fatalf("the plan says begun here: %q", notes)
	}
	if res := Execute(p, p.Name, false, r.x); res.Status != StatusComplete {
		t.Fatalf("finished at the new path:\n%s", res.Message())
	}
	if u, err := UnfinishedDelete(r.x.Log, oldPath, ""); err != nil || u != nil {
		t.Fatalf("the old row clears: %+v %v", u, err)
	}
	if pend, err := PendingDeletes(r.x.Log, ""); err != nil || len(pend) != 0 {
		t.Fatalf("uninstall is unblocked: %q %v", pend, err)
	}
}

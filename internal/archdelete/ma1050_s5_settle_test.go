package archdelete

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/delmark"
)

// Slice 5's adversarial pass of the delete marker, settled (docs/review-delete-marker-slice5-adversarial.md;
// docs/design-archive-delete.md §14, "Slice 5's adversarial pass"; MA-1047, MA-1050, MA-1056): a
// copy of this computer's marker placed in a new folder at the stopped delete's path (MED-1), a
// folder without its anchor holding another delete's marker where this computer's delete wrote its
// own (LOW-1), a copy finished on this computer while the original is still at its path (LOW-2), and
// the anchor and the marker both removed by hand after a stop in step 6 (LOW-3, a stated limit).

// ma1050Kept reports whether every name in want is listed in kept.
func ma1050Kept(kept, want []string) bool {
	for _, n := range want {
		if !slices.Contains(kept, n) {
			return false
		}
	}
	return true
}

// covers: MA-1050, MA-1051, R36
// The pass's MED-1: this computer's delete of a plain archive stopped in step 6; the folder at that
// path is later replaced by a new one of the person's own files (another identity, and no `folder
// removed` line), and a copy of the old marker is placed in it. §15.5's run-id route matches this
// computer's line (the same path, the marker names its run), but the path is no second proof against
// someone who can write the folder, so the plan removes only MailArchive's own names (DM-19), as a plan
// from the marker alone does: the person's README.txt, index.html and mailarchive.log are kept. A run
// of it that stops keeps DM-19's names for the next Delete again (its own started line now matches by
// identity). Positive twin first: another computer's log plans from the marker alone and keeps them.
func TestMA1050_APlantedCopyOfThisComputersMarkerKeepsThePersonsFiles(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	a, run := ma1050Stopped(t, dir)
	b, err := os.ReadFile(filepath.Join(dir, delmark.Name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, dir+".gone"); err != nil {
		t.Fatal(err)
	}
	person := []string{"README.txt", "index.html", "mailarchive.log", "taxes.pdf"}
	for _, n := range person {
		writeFile(t, filepath.Join(dir, n), "the person's "+n)
	}
	const state = ".mailarchive-lastrun.json" // a MailArchive name: it goes
	writeFile(t, filepath.Join(dir, state), "{}")
	if u, _ := UnfinishedAt(a.x.Log, dir, ""); u != nil {
		t.Fatalf("precondition: before the copy is placed the new folder is not the delete's: %+v", u)
	}
	writeFile(t, filepath.Join(dir, delmark.Name), string(b))

	// Positive twin: another computer's log, the marker alone (DM-19).
	o := newRig(t, dir)
	if po := o.plan(t); !slices.Equal(itemRels(po.Remove), []string{state}) || !ma1050Kept(po.Kept, person) {
		t.Fatalf("positive twin: the marker alone: remove %q kept %q", itemRels(po.Remove), po.Kept)
	}

	p := a.plan(t)
	if p.Rerun != RerunNoAnchor || p.Finishes == nil || !p.Finishes.ByRun || p.Finishes.FromMarker || p.Finishes.FirstRun != run {
		t.Fatalf("precondition: the run-id route matches this computer's line: %v %+v", p.Rerun, p.Finishes)
	}
	if !slices.Equal(itemRels(p.Remove), []string{state}) || !ma1050Kept(p.Kept, person) {
		t.Fatalf("only MailArchive's own names: remove %q kept %q", itemRels(p.Remove), p.Kept)
	}

	// A run that stops (the state file held): the next plan, by this run's own started line, keeps
	// DM-19's names.
	fault := true
	faultOn(t, nil, func(rel string) error {
		if fault && rel == state {
			return inUseErr(rel)
		}
		return nil
	})
	res := Execute(p, p.Name, false, a.x)
	fault = false
	if res.Status != StatusPartial || !res.Unfinished || !exists(filepath.Join(dir, delmark.Name)) {
		t.Fatalf("precondition: the run stops with the marker kept:\n%s", res.Message())
	}
	for _, n := range person {
		if !exists(filepath.Join(dir, n)) {
			t.Fatalf("the person's %s was removed:\n%s", n, res.Message())
		}
	}
	p2 := a.plan(t)
	if p2.Rerun != RerunNoAnchor || p2.Finishes == nil || p2.Finishes.FirstRun != run || !slices.Equal(itemRels(p2.Remove), []string{state}) || !ma1050Kept(p2.Kept, person) {
		t.Fatalf("Delete again keeps DM-19's names: %+v remove %q kept %q", p2.Finishes, itemRels(p2.Remove), p2.Kept)
	}
	res = Execute(p2, p2.Name, false, a.x)
	if res.Status != StatusComplete || exists(filepath.Join(dir, state)) || exists(filepath.Join(dir, delmark.Name)) {
		t.Fatalf("Delete again ends complete:\n%s", res.Message())
	}
	for _, n := range person {
		if !exists(filepath.Join(dir, n)) {
			t.Fatalf("the person's %s was removed:\n%s", n, res.Message())
		}
	}
}

// covers: MA-1050, MA-1051, R36
// MED-1 with an encrypted archive's line: an encrypted archive's root names include README.txt, so a
// copy of its marker placed beside the person's README.txt at its path would have it removed. DM-19's
// names there too. Positive twin first: the same line with the folder's identity (no copy: the folder
// the delete began on) plans the encrypted no-anchor names, README.txt among them.
func TestMA1050_APlantedCopyOfAnEncryptedDeletesMarkerKeepsThePersonsReadme(t *testing.T) {
	const run = "4545454545454545"
	setup := func(sameIdentity bool) *rig {
		dir := filepath.Join(t.TempDir(), "Vault")
		writeFile(t, filepath.Join(dir, "README.txt"), "the person's")
		writeFile(t, filepath.Join(dir, "taxes.pdf"), "the person's")
		writeFile(t, filepath.Join(dir, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"`+run+`","began":"2026-11-01T10:00:00Z","note":"x"}`)
		r := newRig(t, dir)
		id := idOf(t, dir)
		if !sameIdentity {
			id["file_id"] = id["file_id"].(uint64) + 1 // the folder the delete began on was another one
		}
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": run, "folder": dir, "row": dir, "kind": "encrypted",
			"record_id": "ffffffffffffffffffffffffffffffff", "record_spelling": dir, "identity": id})
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:01Z", "event": "marked", "run": run, "folder": dir, "first_run": run})
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:02Z", "event": "finished", "run": run, "folder": dir, "outcome": "partial", "unfinished": true, "removed": 1})
		return r
	}
	{
		r := setup(true)
		if p := r.plan(t); p.Finishes == nil || p.Finishes.ByRun || !slices.Contains(itemRels(p.Remove), "README.txt") {
			t.Fatalf("positive twin: the folder the delete began on: %+v remove %q", p.Finishes, itemRels(p.Remove))
		}
	}
	r := setup(false)
	p := r.plan(t)
	if p.Finishes == nil || !p.Finishes.ByRun || p.Kind != Encrypted {
		t.Fatalf("precondition: the run-id route: %+v kind %v", p.Finishes, p.Kind)
	}
	if len(p.Remove) != 0 || !ma1050Kept(p.Kept, []string{"README.txt", "taxes.pdf"}) {
		t.Fatalf("only MailArchive's own names: remove %q kept %q", itemRels(p.Remove), p.Kept)
	}
	res := Execute(p, p.Name, false, r.x)
	if !exists(filepath.Join(r.dir, "README.txt")) || !exists(filepath.Join(r.dir, "taxes.pdf")) {
		t.Fatalf("the person's files kept:\n%s", res.Message())
	}
}

// covers: MA-1056, MA-1050, R36
// The pass's LOW-1 (the slice-5 builder's note (a)): a folder with no anchor, at this computer's line's
// path with its identity, holding a marker that names another delete, while this computer's delete of
// it wrote its `marked` line — DM-27's state without the anchor (two rotation drives, or the marker
// replaced). The plan is made from the folder's marker, as when this computer's log has no line: DM-19's
// names, this computer's own record lookup — never the line's ID and Spelling, so step 8 cannot forget
// the other drive's vault's key — and DM-27's words before the marker's. Positive twin first: the marker
// naming this computer's delete plans from its line, with its record.
func TestMA1056_ANoAnchorFolderHoldingAnotherDeletesMarkerTakesItsOwnRecord(t *testing.T) {
	const run = "7777777777777777"
	const other = "6666666666666666"
	const lineID = "ffffffffffffffffffffffffffffffff"
	setup := func(markerRun string) *rig {
		dir := filepath.Join(t.TempDir(), "Mail")
		writeFile(t, filepath.Join(dir, ".mailarchive-lastverify.json"), "{}")
		writeFile(t, filepath.Join(dir, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"`+markerRun+`","began":"2026-11-01T10:00:00Z","note":"x"}`)
		r := newRig(t, dir)
		r.x.Deps.Record = recordedAt(dir) // this computer's own lookup gives testArchiveID
		r.kr.plan.ID = lineID             // the keyring holds the line's archive (the other drive's vault)
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": run, "folder": dir, "row": dir, "kind": "encrypted",
			"record_id": lineID, "record_spelling": dir, "identity": idOf(t, dir)})
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:01Z", "event": "marked", "run": run, "folder": dir, "first_run": run})
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:02Z", "event": "finished", "run": run, "folder": dir, "outcome": "partial", "unfinished": true, "removed": 1})
		return r
	}
	{
		r := setup(run)
		if p := r.plan(t); p.Disagrees || p.Finishes == nil || p.Finishes.FromMarker || p.Finishes.FirstRun != run || p.Record.ID != lineID {
			t.Fatalf("positive twin: the marker names this computer's delete: %+v record %+v", p.Finishes, p.Record)
		}
	}
	r := setup(other)
	ma1056Refused(t, r.x.Log, r.dir)
	p := r.plan(t)
	if !p.Disagrees || p.Rerun != RerunNoAnchor || p.Finishes == nil || !p.Finishes.FromMarker || p.Finishes.FirstRun != other {
		t.Fatalf("planned from the folder's marker, saying the log disagrees: disagrees %v rerun %v finishes %+v", p.Disagrees, p.Rerun, p.Finishes)
	}
	if p.Kind != Encrypted || p.Record.ID != testArchiveID || p.Record.Spelling != r.dir {
		t.Fatalf("its own record lookup: kind %v record %+v", p.Kind, p.Record)
	}
	if notes := p.FinishesNotes(); len(notes) != 2 || notes[0] != MsgLogDisagrees || notes[1] != MsgMarkerNoRecord {
		t.Fatalf("the plan says why: %q", notes)
	}
	Execute(p, p.Name, false, r.x)
	if len(r.kr.forgot) != 0 {
		t.Fatalf("step 8 forgot the keyring's record of the other drive's vault: %v", r.kr.forgot)
	}
	if l := ma1051StartedLine(t, r.x.Log); l["first_run"] != other || l["from_marker"] != true || l["record_id"] != testArchiveID {
		t.Fatalf("its started line carries the marker's delete and its own record: %v", l)
	}
}

// covers: MA-1050, MA-1044, R36
// The pass's LOW-2: the `first_run` ending (DM-10 item 2) ended an open delete whose folder is still at
// the line's path with the line's identity. This computer's delete stopped in step 6; its marker was
// then removed (by hand, a sync conflict, an antivirus — DM-27's state, where this computer's log is
// the only guard), and a copy of that marker in another folder (one restored from a backup copy) was
// finished here. The original is proven not to be the folder finished elsewhere, so its delete stays
// open in this log and its backups stay refused. Positive twin: once the original is no longer at its
// path (moved, or its drive under another letter), the ending ends it.
func TestMA1050_ACopyFinishedHereLeavesTheOriginalsDeleteOpenWhileItIsThere(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	a, run := ma1050Stopped(t, dir)
	b, err := os.ReadFile(filepath.Join(dir, delmark.Name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	ma1056Refused(t, a.x.Log, dir)

	copyDir := filepath.Join(t.TempDir(), "Mail copy")
	writeFile(t, filepath.Join(copyDir, delmark.Name), string(b))
	writeFile(t, filepath.Join(copyDir, ".mailarchive-lastrun.json"), "{}")
	c := newRig(t, copyDir)
	c.x.Log, c.x.Deps.Log = a.x.Log, a.x.Log
	pc := c.plan(t)
	if pc.Finishes == nil || !pc.Finishes.FromMarker || pc.Finishes.FirstRun != run {
		t.Fatalf("precondition: the copy is planned from its marker: %+v", pc.Finishes)
	}
	if res := Execute(pc, pc.Name, false, c.x); res.Status != StatusComplete {
		t.Fatalf("precondition: the copy finished here:\n%s", res.Message())
	}
	if l := ma1051StartedLine(t, a.x.Log); l["first_run"] != run {
		t.Fatalf("precondition: the copy's started line names the original's run as its first: %v", l)
	}

	if u, err := UnfinishedAt(a.x.Log, dir, ""); err != nil || u == nil || u.MarkerOnly || u.FirstRun != run {
		t.Fatalf("the original's delete stays open in this log: %+v %v", u, err)
	}
	ma1056Refused(t, a.x.Log, dir)

	if err := os.Rename(dir, dir+" moved"); err != nil {
		t.Fatal(err)
	}
	if u, err := UnfinishedDelete(a.x.Log, dir, ""); err != nil || u != nil {
		t.Fatalf("positive twin: the original no longer at its path, the ending ends it: %+v %v", u, err)
	}
}

// covers: MA-1047, R36
// The pass's LOW-3, a stated limit (§15.6, "An anchor lost outside the delete"): a delete stopped in
// step 6 leaving one of the archive's message files, then the manifest and the marker both removed by
// hand. Delete again reads this computer's `marked` line as "step 6 finished" (NoteGone) and ends
// complete; what it cannot name — the archive's message file among it — is kept, never removed, and so
// is every file of the person's. Positive twin first: with the marker left, it is not NoteGone.
func TestMA1047_TheAnchorAndMarkerRemovedByHandKeepWhatIsLeft(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	msg := filepath.Join(dir, filepath.FromSlash(stemA+".html"))
	fault := true
	faultOn(t, nil, func(rel string) error {
		if fault && rel == stemA+".html" {
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, false)
	fault = false
	if res.Status != StatusPartial || !res.Unfinished || !exists(msg) {
		t.Fatalf("precondition: the delete stops in step 6 with the message file left:\n%s", res.Message())
	}
	if err := os.Remove(filepath.Join(dir, manifestName)); err != nil {
		t.Fatal(err)
	}
	if p := r.plan(t); p.NoteGone || p.Rerun != RerunNoAnchor {
		t.Fatalf("positive twin: the marker left: %v %v", p.Rerun, p.NoteGone)
	}
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	p := r.plan(t)
	if !p.NoteGone || p.Rerun != RerunNoAnchor {
		t.Fatalf("precondition: NoteGone: %v %v", p.Rerun, p.NoteGone)
	}
	res = Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete {
		t.Fatalf("Delete again ends complete:\n%s", res.Message())
	}
	for _, f := range []string{msg, filepath.Join(dir, "taxes.pdf"), filepath.Join(dir, "Photos", "beach.jpg"), filepath.Join(dir, "Mailbox", "Inbox", "notes.txt")} {
		if !exists(f) {
			t.Fatalf("%s was removed:\n%s", f, res.Message())
		}
	}
}

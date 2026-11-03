package archdelete

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// When this computer's deletion log and the folder disagree (docs/design-archive-delete.md §15.4's
// DM-27; §15.10 slice 5; MA-1056): the log has a delete of the folder that wrote its `marked` line,
// so that delete created (or found) the folder's marker at step 6 — yet the folder holds its anchor
// with no marker, or a marker naming another delete. The marker was removed (by hand, a sync
// conflict, an antivirus), or this is another drive with the same device and file numbers (two
// rotation drives). Backups stay refused (the log says so: fail closed); Delete again claims to
// finish nothing — no "This finishes the delete begun DATE", no Finishes line in the record, no
// first run carried — says why, and is an ordinary delete with its own record lookup, never the
// started line's ID and Spelling, so step 8 cannot forget another vault's key.

// ma1056Refused asserts every backup and action is refused, nothing written. Since MA-1068 (the
// slice-5 adversarial pass's LOW-4) the words are DM-27's own, naming the drive the delete began on
// and a note removed by hand (was the log's "Delete not finished: click Delete again. …"); the
// refusal, its code and its absent side effect are asserted as before.
func ma1056Refused(t *testing.T, logPath, dir string) {
	t.Helper()
	names := func() []string { n := ma1044Names(dir); slices.Sort(n); return n }
	before := names()
	err := BackupRefusal(logPath, dir)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names(ma1068Refusal("it was not backed up")),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
}

// ma1056Disagrees asserts p is DM-27's plan: an ordinary delete that claims to finish nothing.
func ma1056Disagrees(t *testing.T, p *DeletePlan) {
	t.Helper()
	saved := string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p}))
	if !p.Disagrees || p.Finishes != nil || p.Rerun != RerunNone || p.FinishesLine() != "" || strings.Contains(saved, "Finishes:") {
		t.Fatalf("the plan claims to finish that delete: disagrees %v finishes %+v rerun %v line %q", p.Disagrees, p.Finishes, p.Rerun, p.FinishesLine())
	}
	if notes := p.FinishesNotes(); len(notes) != 1 || notes[0] != MsgLogDisagrees {
		t.Fatalf("the plan says why: %q", notes)
	}
}

// covers: MA-1056, R36
// The marker removed by hand after this computer's delete stopped in step 6: backups refused, and
// Delete again claims nothing, says DM-27's words, and deletes the archive as an ordinary delete —
// its started line carries no first run, its record no Finishes line — ending this computer's
// delete. Positive twin first: the same stop with the marker left is "This finishes the delete
// begun DATE", and with no `marked` line (the delete stopped before step 6 wrote its note) the plan
// is as before DM-27.
func TestMA1056_TheMarkerRemovedByHandClaimsNothing(t *testing.T) {
	{
		dir := plainBesidePersonsFiles(t)
		r, run := ma1050Stopped(t, dir)
		if p := r.plan(t); p.Disagrees || p.Finishes == nil || p.Finishes.FirstRun != run || !strings.HasPrefix(p.FinishesLine(), "This finishes the delete begun ") {
			t.Fatalf("positive twin: the marker left: %+v", p.Finishes)
		}
	}
	{
		dir := plainBesidePersonsFiles(t)
		r := newRig(t, dir)
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "5555555555555555", "folder": dir, "row": dir, "kind": "plain", "identity": idOf(t, dir)})
		if p := r.plan(t); p.Disagrees || p.Finishes == nil || p.Finishes.Run != "5555555555555555" {
			t.Fatalf("positive twin: no marked line, the plan as before: %+v", p.Finishes)
		}
	}

	dir := plainBesidePersonsFiles(t)
	r, run := ma1050Stopped(t, dir)
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	ma1056Refused(t, r.x.Log, dir)
	p := r.plan(t)
	ma1056Disagrees(t, p)
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || exists(filepath.Join(dir, manifestName)) || !exists(filepath.Join(dir, "taxes.pdf")) || strings.Contains(string(res.Record), "Finishes:") {
		t.Fatalf("an ordinary delete:\n%s\n%s", res.Message(), res.Record)
	}
	if l := ma1051StartedLine(t, r.x.Log); l["first_run"] != nil || l["run"] == run {
		t.Fatalf("its started line carries no first run: %v", l)
	}
	if u, _ := UnfinishedAt(r.x.Log, dir, ""); u != nil {
		t.Fatalf("this computer's delete ends: %+v", u)
	}
}

// covers: MA-1056, R36
// A marker naming another delete in the folder this computer's delete marked: backups refused, and
// Delete again claims nothing of this computer's delete and says DM-27's words. Positive twin first:
// the marker naming this computer's delete is "This finishes".
func TestMA1056_AnotherDeletesMarkerClaimsNothing(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r, run := ma1050Stopped(t, dir)
	if p := r.plan(t); p.Disagrees || p.Finishes == nil || p.Finishes.FirstRun != run {
		t.Fatalf("positive twin: %+v", p.Finishes)
	}
	writeFile(t, filepath.Join(dir, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"6666666666666666","began":"2026-11-01T10:00:00Z","note":"x"}`)
	ma1056Refused(t, r.x.Log, dir)
	p := r.plan(t)
	ma1056Disagrees(t, p)
	// Delete under the typed name: an ordinary delete. The other delete's marker is kept as it is
	// until it goes last (§15.2 point 4), with no `marked` line for this run.
	marked := len(ma1046Marked(t, r.x.Log))
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || exists(filepath.Join(dir, delmark.Name)) || strings.Contains(string(res.Record), "Finishes:") || len(ma1046Marked(t, r.x.Log)) != marked {
		t.Fatalf("an ordinary delete:\n%s", res.Message())
	}
}

// covers: MA-1056, R36
// The rotation drive: an encrypted archive on a drive whose folder has the same path and the same
// device and file numbers as the one this computer's delete began on and marked (written by hand,
// with another vault's record), and no marker. Backups refused; Delete again claims nothing, takes
// this computer's own record of the folder — never the started line's ID and Spelling — and step 8
// forgets by that record. Positive twin first: the same line with no `marked` line plans as before.
func TestMA1056_ARotationDriveUsesItsOwnRecordLookup(t *testing.T) {
	const run = "7777777777777777"
	const otherID = "ffffffffffffffffffffffffffffffff"
	started := func(r *rig) {
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": run, "folder": r.dir, "row": r.dir, "kind": "encrypted",
			"record_id": otherID, "record_spelling": filepath.Join(filepath.Dir(r.dir), "other drive's spelling"), "identity": idOf(t, r.dir)})
	}
	{
		r := encRig(t, encryptedClean(t))
		r.x.Deps.Record = recordedAt(r.dir)
		started(r)
		if p := r.plan(t); p.Disagrees || p.Finishes == nil || p.Finishes.Run != run {
			t.Fatalf("positive twin: no marked line: %+v", p.Finishes)
		}
	}
	r := encRig(t, encryptedClean(t))
	r.x.Deps.Record = recordedAt(r.dir)
	started(r)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:01Z", "event": "marked", "run": run, "folder": r.dir, "first_run": run})
	ma1056Refused(t, r.x.Log, r.dir)
	p := r.plan(t)
	ma1056Disagrees(t, p)
	if p.Kind != Encrypted || p.Record.ID != testArchiveID || p.Record.Spelling != r.dir {
		t.Fatalf("its own record lookup: kind %v record %+v", p.Kind, p.Record)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || len(r.kr.asked) != 1 || r.kr.asked[0] != r.dir {
		t.Fatalf("step 8 asks by this computer's record (asked %q):\n%s", r.kr.asked, res.Message())
	}
}

package archdelete

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// The slice-4 adversarial pass of the delete marker, settled (docs/design-archive-delete.md §14,
// "Slice 4's adversarial pass"; MA-1047, MA-1057): the computer that began a delete another one
// finished while keeping the folder (MED-1), the question on another drive (LOW-2), its words for a
// delete of unknown kind (LOW-3), and the folder gone under the lock (LOW-4).

// ma1057StopInStep6 runs this computer's delete of r's folder and stops it in step 6 before the
// anchor (search.db held), as a delete that wrote its marker and its `marked` line.
func ma1057StopInStep6(t *testing.T, r *rig) {
	t.Helper()
	fault := true
	faultOn(t, nil, func(rel string) error {
		if fault && rel == "search.db" {
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, false)
	fault = false
	if res.Status != StatusPartial || !res.Unfinished || !IsArchive(r.dir) {
		t.Fatalf("this computer's delete stops in step 6 with the manifest kept:\n%s", res.Message())
	}
	if _, there := ma1046Marker(r.dir); !there || len(ma1046Marked(t, r.x.Log)) != 1 {
		t.Fatalf("the stop leaves the marker and one marked line: %q", events(logLines(t, r.x.Log)))
	}
}

// covers: MA-1047, MA-1057, R36
// The slice-4 pass's MED-1: computer A's delete stopped in step 6; computer B (its own log never saw
// the delete) finishes it from the marker and ends complete, keeping the folder for the person's own
// files and removing the marker. On A the folder is there with no anchor and no marker, and A's log
// has the delete's `marked` line: the marker went after the anchor, so step 6 had finished (DM-9's
// premise, read from A's own log), and A's Delete again removes what it can still name, keeps the
// person's files, removes A's row and ends complete — no longer "never complete" with Remove from
// list, uninstall and A's backups refused for good. The plan and the result say why; the earlier
// count is "not known". Positive twin first: the same state without a `marked` line (a delete that
// stopped before step 6, then deleted elsewhere) stays §5.4's "never complete", and its words no
// longer recommend Remove from list (which MA-1029 refuses there): they say to move the files out,
// and after that Delete again finishes.
func TestMA1057_ADeleteFinishedElsewhereThatKeptTheFolderEndsHereToo(t *testing.T) {
	person := []string{"taxes.pdf", "Photos/beach.jpg", "Mailbox/Inbox/notes.txt"}
	{
		dir := plainBesidePersonsFiles(t)
		a := newRig(t, dir)
		// A's delete stopped before step 6 (no marker, no marked line); B deleted the archive as an
		// ordinary delete of its own and kept the folder.
		appendRawLog(t, a.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "aaaaaaaaaaaaaaaa", "folder": dir, "row": dir,
			"kind": "plain", "identity": idOf(t, dir)})
		if res := newRig(t, dir).run(t, false); res.Status != StatusComplete || !exists(dir) {
			t.Fatalf("positive twin: B's delete:\n%s", res.Message())
		}
		res := a.run(t, false)
		if res.Status != StatusPartial || !res.Unfinished || len(a.rows) != 0 {
			t.Fatalf("positive twin: no marked line stays never complete:\n%s", res.Message())
		}
		want := "Not fully deleted: the folder still holds 3 files the delete can no longer identify. Move them out of the folder (or delete them by hand), then click Delete again."
		if res.Lines[0] != want || strings.Contains(res.Message(), "Remove from list") {
			t.Fatalf("positive twin's words:\n%s\nwant first line %q", res.Message(), want)
		}
		if err := UnlistRefusal(dir, a.x.Deps); AsRefusal(err) == nil || AsRefusal(err).Case != CaseUnfinished {
			t.Fatalf("positive twin: Remove from list is refused there (MA-1029): %v", err)
		}
		// The words' remedy works: the files moved out, Delete again ends the delete.
		out := t.TempDir()
		for _, n := range []string{"taxes.pdf", "Photos", "Mailbox"} {
			if err := os.Rename(filepath.Join(dir, n), filepath.Join(out, n)); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 2; i++ {
			p, err := Plan(dir, a.x.Deps)
			if err != nil {
				t.Fatalf("positive twin: Delete again %d: %v", i+1, err)
			}
			if res := Execute(p, p.Name, false, a.x); res.Status == StatusComplete {
				break
			}
		}
		if u, _ := UnfinishedDelete(a.x.Log, dir, ""); u != nil || exists(dir) {
			t.Fatalf("positive twin: moved out, Delete again ends the delete: %+v, folder there %v", u, exists(dir))
		}
	}

	dir := plainBesidePersonsFiles(t)
	a := newRig(t, dir)
	ma1057StopInStep6(t, a)
	b := newRig(t, dir)
	pb := b.plan(t)
	if pb.Finishes == nil || !pb.Finishes.FromMarker || pb.Rerun != RerunNone {
		t.Fatalf("B plans from the marker: rerun %v finishes %+v", pb.Rerun, pb.Finishes)
	}
	if res := Execute(pb, pb.Name, false, b.x); res.Status != StatusComplete || exists(filepath.Join(dir, delmark.Name)) || !exists(dir) {
		t.Fatalf("B finishes it, the folder kept for the person's files:\n%s", res.Message())
	}

	p := a.plan(t)
	if p.Rerun != RerunNoAnchor || !p.NoteGone {
		t.Fatalf("A's Delete again: rerun %v note gone %v", p.Rerun, p.NoteGone)
	}
	if notes := p.FinishesNotes(); !contains(notes, MsgNoteGone) {
		t.Fatalf("A's plan says why: %q", notes)
	}
	// A marker there again between the plan and the typed name (a planted one): the plan no longer
	// holds, and nothing changes.
	plantMarker(t, dir)
	if res := Execute(p, p.Name, false, a.x); res.Status != StatusRefusedAfterStart || res.Refusal == nil || res.Refusal.Case != CaseChanged || len(a.rows) != 0 {
		t.Fatalf("a marker back before the delete: %v\n%s", res.Status, res.Message())
	}
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	p = a.plan(t)
	res := Execute(p, p.Name, false, a.x)
	if res.Status != StatusComplete || res.Unfinished || len(a.rows) != 1 {
		t.Fatalf("A's Delete again ends complete, its row removed (rows %q):\n%s", a.rows, res.Message())
	}
	for _, f := range person {
		if !exists(filepath.Join(dir, filepath.FromSlash(f))) {
			t.Fatalf("the person's %s was removed:\n%s", f, res.Message())
		}
	}
	if !strings.Contains(res.Lines[0], "The folder is kept: it holds 3 files that are not the archive's: Mailbox, Photos, taxes.pdf.") ||
		!hasLine(res, "How many files its earlier runs removed is not known: "+notKnownNoteGoneWhy+".") {
		t.Fatalf("A's result:\n%s", res.Message())
	}
	if !strings.Contains(string(res.Record), "not known in full: "+notKnownNoteGoneWhy) {
		t.Fatalf("A's record:\n%s", res.Record)
	}
	if u, _ := UnfinishedDelete(a.x.Log, dir, ""); u != nil {
		t.Fatalf("A's log ends the delete: %+v", u)
	}
	if pend, err := PendingDeletes(a.x.Log, ""); err != nil || len(pend) != 0 {
		t.Fatalf("A's uninstall is unblocked: %q %v", pend, err)
	}
	if exists(filepath.Join(dir, delmark.Name)) {
		t.Fatal("no marker is created")
	}
}

// covers: MA-1057, R36
// The slice-4 pass's LOW-2: where a drive keeps its volume number when it is plugged in again
// (Windows' volume serial number), a parent on another volume than the one the delete's started line
// records is another drive at the same letter or label: "Plug in the drive that holds this archive",
// never the "finished elsewhere" question, even when the person already answered it; nothing is
// stopped, forgotten or removed. Positive twin first: the started line's volume is the parent's, and
// the question is asked. Where the number changes on a remount (macOS, Linux) it cannot tell, and the
// question is asked (the stated limit).
func TestMA1057_AnotherDriveAtTheSamePlaceIsNotAskedTheQuestion(t *testing.T) {
	old := volumeKeptAcrossReplug
	t.Cleanup(func() { volumeKeptAcrossReplug = old })
	volumeKeptAcrossReplug = true
	{
		r, _ := elsewhereRig(t)
		if _, msg, c := refusalOf(t, r.dir, r.x.Deps); c != CaseFinishedElsewhere || msg != ElsewhereQuestion(Encrypted) {
			t.Fatalf("positive twin: case %s %q", c, msg)
		}
	}
	other := func(t *testing.T) *rig {
		t.Helper()
		parent := filepath.Join(t.TempDir(), "Drive")
		dir := filepath.Join(parent, "Vault")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		r := newRig(t, dir)
		r.kr.folder = ""
		id := idOf(t, dir)
		id["volume"] = id["volume"].(uint64) + 1 // the drive the delete began on is not this one
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "aaaaaaaaaaaaaaaa", "folder": dir, "row": dir,
			"kind": "encrypted", "record_id": testArchiveID, "record_spelling": dir, "identity": id})
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := other(t)
	lines := len(logLines(t, r.x.Log))
	for _, said := range []bool{false, true} {
		deps := r.x.Deps
		deps.FinishedElsewhere = said
		rc, msg, c := refusalOf(t, r.dir, deps)
		assure.Refused(t, rc, msg, assure.Code(1), assure.Names(msgOtherDrive),
			assure.NoSideEffect(func() bool {
				return len(r.kr.asked) == 0 && len(r.rows) == 0 && len(r.sched.removed) == 0 && len(logLines(t, r.x.Log)) == lines
			}))
		if c != CaseUnreachable {
			t.Fatalf("said %v: case %s", said, c)
		}
	}
	if runtime.GOOS != "windows" {
		volumeKeptAcrossReplug = false
		r := other(t)
		if _, _, c := refusalOf(t, r.dir, r.x.Deps); c != CaseFinishedElsewhere {
			t.Fatalf("the stated limit off Windows: case %s", c)
		}
	}
}

// covers: MA-1057, R36
// The slice-4 pass's LOW-3: a delete of unknown kind (planned here from a marker, the folder never
// recorded by this computer), then its folder gone: the question promises no record or key to
// forget — step 8 does not apply to it — in the words a plain archive's question has. Positive twin
// first: an encrypted archive's question names the record and key.
func TestMA1057_TheQuestionForADeleteOfUnknownKindForgetsNoKey(t *testing.T) {
	if q := ElsewhereQuestion(Encrypted); !strings.Contains(q, "forgets this computer's record and key of it") {
		t.Fatalf("positive twin: %q", q)
	}
	parent := filepath.Join(t.TempDir(), "Drive")
	dir := filepath.Join(parent, "Shared")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, dir)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "aaaaaaaaaaaaaaaa", "folder": dir, "row": dir,
		"rerun": "no manifest", "from_marker": true, "first_run": ma1051Run, "identity": idOf(t, dir)})
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	_, msg, c := refusalOf(t, dir, r.x.Deps)
	if c != CaseFinishedElsewhere || msg != ElsewhereQuestion(Plain) || strings.Contains(msg, "key") || strings.Contains(msg, "record") {
		t.Fatalf("case %s: %q", c, msg)
	}
	if ElsewhereQuestion(Kind(0)) != ElsewhereQuestion(Plain) {
		t.Fatalf("kind unknown: %q", ElsewhereQuestion(Kind(0)))
	}
}

// covers: MA-1051, MA-1057, R36
// The slice-4 pass's LOW-4: a computer that never began the delete plans to finish it from the
// marker, and the folder is removed before it types the name (the computer that began it finished
// it). Its re-check under the lock now finds a delete of its own log (the started line it just
// wrote) with the folder gone, which is the "finished elsewhere" question; shown there it would be a
// refusal with no answer to give. It says "This archive changed since you clicked Delete" instead, and
// nothing is stopped or removed. Positive twin first: the same plan with the folder left there
// finishes it.
func TestMA1057_TheFolderGoneUnderTheLockIsAChangeNotTheQuestion(t *testing.T) {
	mk := func(t *testing.T) *rig {
		t.Helper()
		parent := filepath.Join(t.TempDir(), "Drive")
		dir := smallArchive(t, filepath.Join(parent, "Arch"))
		plantMarker(t, dir)
		return newRig(t, dir)
	}
	{
		r := mk(t)
		if res := r.run(t, false); res.Status != StatusComplete {
			t.Fatalf("positive twin:\n%s", res.Message())
		}
	}
	r := mk(t)
	p := r.plan(t)
	if p.Finishes == nil || !p.Finishes.FromMarker {
		t.Fatalf("planned from the marker: %+v", p.Finishes)
	}
	if err := os.RemoveAll(r.dir); err != nil {
		t.Fatal(err)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusRefusedAfterStart || res.Refusal == nil || res.Refusal.Case != CaseChanged || res.Lines[0] != msgChanged {
		t.Fatalf("%v %+v\n%s", res.Status, res.Refusal, res.Message())
	}
	if len(r.rows) != 0 || len(r.sched.removed) != 0 || len(r.kr.asked) != 0 {
		t.Fatalf("nothing stopped or removed: rows %q schedules %q", r.rows, r.sched.removed)
	}
}

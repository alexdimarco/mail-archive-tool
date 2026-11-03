package archdelete

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/state"
)

// Finishing from another computer (docs/design-archive-delete.md §15.4; §15.10 slice 4; MA-1051): a
// delete only the folder's marker knows is planned from the marker — with only MailArchive's own
// names when the anchor is gone (DM-19), and its record's lines from the marker labelled as the
// folder's word (DM-8, DM-28).

// ma1051Run and ma1051Began are what plantMarker writes.
const (
	ma1051Run   = "0123456789abcdef"
	ma1051Began = "2026-11-02T15:04:05Z"
)

// ma1051BeganAt is ma1051Began as a time.
func ma1051BeganAt(t *testing.T) time.Time {
	t.Helper()
	b, err := time.Parse(time.RFC3339, ma1051Began)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// plantedBesidePersonsFiles is a folder of the person's own files with a planted marker and no
// anchor: README.txt and index.html (names a plain archive's no-anchor re-run removes), a log of
// theirs, a folder of theirs, and MailArchive's own names — a state file, a run leftover and the lock.
func plantedBesidePersonsFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"README.txt", "index.html", "mailarchive.log", "notes.log", "Photos/beach.jpg",
		".mailarchive-lastrun.json", ".mailarchive-123.tmp", ".mailarchive.lock"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), "x "+f)
	}
	return dir
}

// ma1051StartedLine is the log's last started line.
func ma1051StartedLine(t *testing.T, path string) map[string]any {
	t.Helper()
	var last map[string]any
	for _, l := range logLines(t, path) {
		if l["event"] == "started" {
			last = l
		}
	}
	if last == nil {
		t.Fatal("no started line")
	}
	return last
}

// covers: MA-1051, R36
// A marker alone is not enough to delete ordinary names (DM-19): a planted marker in a folder of
// the person's own files, with no anchor and no line of this computer's log, is planned from the
// marker with only MailArchive's own names — files starting `.mailarchive-` and the run leftovers —
// and everything else kept as files the delete can no longer identify; Delete under the typed name
// removes the marker (DM-9) and ends complete with README.txt, index.html, the logs and the folder
// kept, byte for byte. Positive twin first: the same folder without the marker is no archive
// ("Use Remove from list"), nothing planned.
func TestMA1051_APlantedMarkerBesideThePersonsFilesKeepsThem(t *testing.T) {
	dir := plantedBesidePersonsFiles(t)
	r := newRig(t, dir)
	rc, msg, c := refusalOf(t, dir, r.x.Deps)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(msgNotArchive),
		assure.NoSideEffect(func() bool { return exists(filepath.Join(dir, "README.txt")) }))
	if c != CaseNotArchive {
		t.Fatalf("case %s", c)
	}
	plantMarker(t, dir)
	p := r.plan(t)
	if p.Rerun != RerunNoAnchor || p.Finishes == nil || !p.Finishes.FromMarker {
		t.Fatalf("planned from the marker: rerun %v finishes %+v", p.Rerun, p.Finishes)
	}
	if d := setDiff("removed", removeRels(p), []string{".mailarchive-lastrun.json", ".mailarchive-123.tmp"}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("kept", p.Kept, []string{"README.txt", "index.html", "mailarchive.log", "notes.log", "Photos"}); d != "" {
		t.Fatal(d)
	}
	requireNotPlanned(t, p)
	keep := map[string][]byte{}
	for _, f := range []string{"README.txt", "index.html", "mailarchive.log", "notes.log", "Photos/beach.jpg"} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			t.Fatal(err)
		}
		keep[f] = b
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || exists(filepath.Join(dir, delmark.Name)) || exists(filepath.Join(dir, ".mailarchive-lastrun.json")) {
		t.Fatalf("%s", res.Message())
	}
	for f, b := range keep {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil || string(got) != string(b) {
			t.Fatalf("%s was not kept as it was: %v", f, err)
		}
	}
	if !strings.Contains(res.Lines[0], "The folder is kept: it holds 5 files that are not the archive's") {
		t.Fatalf("%s", res.Message())
	}
}

// covers: MA-1051, R36
// A delete only the marker knows, stopped and run again, keeps planning with only MailArchive's
// own names (DM-19): the first run's started line says it was planned from the marker
// (`from_marker`), so this computer's own log line for it never widens the names to §5.4's, which
// would remove README.txt and index.html. Positive twin first: the first run stops with both kept.
func TestMA1051_ARunAgainOfAMarkerPlanKeepsOnlyMailArchivesNames(t *testing.T) {
	dir := plantedBesidePersonsFiles(t)
	plantMarker(t, dir)
	r := newRig(t, dir)
	fault := true
	faultOn(t, nil, func(rel string) error {
		if fault && rel == ".mailarchive-lastrun.json" {
			return inUseErr(rel)
		}
		return nil
	})
	if res := r.run(t, false); res.Status != StatusPartial || !exists(filepath.Join(dir, "README.txt")) || !exists(filepath.Join(dir, delmark.Name)) {
		t.Fatalf("%s", res.Message())
	}
	if l := ma1051StartedLine(t, r.x.Log); l["from_marker"] != true || l["first_run"] != ma1051Run {
		t.Fatalf("the started line says it was planned from the marker: %v", l)
	}
	fault = false
	p := r.plan(t)
	if p.Finishes == nil || !p.Finishes.FromMarker || contains(removeRels(p), "README.txt") || contains(removeRels(p), "index.html") {
		t.Fatalf("Delete again: finishes %+v remove %q", p.Finishes, removeRels(p))
	}
	if res := Execute(p, p.Name, false, r.x); res.Status != StatusComplete || !exists(filepath.Join(dir, "README.txt")) || !exists(filepath.Join(dir, "index.html")) {
		t.Fatalf("%s", res.Message())
	}
}

// covers: MA-1051, R36
// With the anchor there and no line of this computer's log, the plan finishes the delete the marker
// names (§15.4): it begins "This finishes the delete begun DATE." with the no-record sentence; the
// saved plan and the record say "Finishes: the delete begun DATE (run ID), as the folder's note
// says; MailArchive cannot confirm it" (DM-8) and "Files removed earlier: not known: the delete was
// begun where this computer's deletion log does not see it"; this computer's started line carries
// the marker's run as first_run, its began as first_at, and earlier_unknown; the `marked` line names
// it; the folder is deleted, the marker last. Positive twin first: the same archive with no marker
// is an ordinary delete with no Finishes line.
func TestMA1051_TheAnchorThereFinishesTheDeleteTheMarkerNames(t *testing.T) {
	{
		twin := newRig(t, plainBesidePersonsFiles(t))
		p := twin.plan(t)
		saved := string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p}))
		if p.Finishes != nil || p.FinishesLine() != "" || len(p.FinishesNotes()) != 0 || strings.Contains(saved, "Finishes:") {
			t.Fatalf("positive twin: finishes %+v", p.Finishes)
		}
	}
	dir := plainBesidePersonsFiles(t)
	plantMarker(t, dir)
	r := newRig(t, dir)
	p := r.plan(t)
	began := ma1051BeganAt(t)
	if p.Finishes == nil || !p.Finishes.FromMarker || p.Rerun != RerunNone {
		t.Fatalf("finishes %+v rerun %v", p.Finishes, p.Rerun)
	}
	if got, want := p.FinishesLine(), "This finishes the delete begun "+localStamp(began)+"."; got != want {
		t.Fatalf("FinishesLine %q, want %q", got, want)
	}
	if notes := p.FinishesNotes(); len(notes) != 1 || notes[0] != MsgMarkerNoRecord {
		t.Fatalf("notes %q", notes)
	}
	finishes := "Finishes: the delete begun " + localStamp(began) + " (run " + ma1051Run + "), as the folder's note says; MailArchive cannot confirm it"
	earlier := "Files removed earlier: not known: the delete was begun where this computer's deletion log does not see it"
	saved := string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p}))
	if !containsLine(saved, finishes) || !containsLine(saved, earlier) {
		t.Fatalf("saved plan:\n%s", saved)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || exists(filepath.Join(dir, delmark.Name)) || exists(filepath.Join(dir, manifestName)) {
		t.Fatalf("%s", res.Message())
	}
	if !containsLine(string(res.Record), finishes) || !containsLine(string(res.Record), earlier) {
		t.Fatalf("record:\n%s", res.Record)
	}
	l := ma1051StartedLine(t, r.x.Log)
	at, _ := l["first_at"].(string)
	fa, err := time.Parse(time.RFC3339Nano, at)
	if l["first_run"] != ma1051Run || err != nil || !fa.Equal(began) || l["earlier_unknown"] != true || l["from_marker"] != true {
		t.Fatalf("started line %v", l)
	}
	marked := ma1046Marked(t, r.x.Log)
	if len(marked) != 1 || marked[0]["first_run"] != ma1051Run {
		t.Fatalf("marked lines %v", marked)
	}
}

// covers: MA-1051, R36
// A backup that ran into the archive after the marker's began adds DM-28's sentence to the plan
// ("A backup ran into this archive after its delete began, probably from a computer with an older
// MailArchive."); words only, so nothing else changes. Positive twin first: a last backup before
// began says nothing of it.
func TestMA1051_ABackupAfterTheDeleteBeganIsSaid(t *testing.T) {
	history := func(t *testing.T, dir string, when time.Time) {
		t.Helper()
		b, err := json.Marshal([]state.RunHistoryEntry{{When: when, Status: state.RunOK}})
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, state.RunHistoryName), string(b))
	}
	began := ma1051BeganAt(t)
	twin := plainBesidePersonsFiles(t)
	history(t, twin, began.Add(-time.Hour))
	plantMarker(t, twin)
	if notes := mustPlan(t, twin, newRig(t, twin).x.Deps).FinishesNotes(); len(notes) != 1 || notes[0] != MsgMarkerNoRecord {
		t.Fatalf("positive twin: %q", notes)
	}
	dir := plainBesidePersonsFiles(t)
	history(t, dir, began.Add(time.Hour))
	plantMarker(t, dir)
	p := mustPlan(t, dir, newRig(t, dir).x.Deps)
	if notes := p.FinishesNotes(); len(notes) != 2 || notes[1] != MsgMarkerBackupAfter {
		t.Fatalf("notes %q (last backup %v, began %v)", notes, p.Captured.LastBackup, began)
	}
}

// covers: MA-1051, R36
// The no-anchor re-run from the marker of an encrypted archive this computer recorded (§15.4): its
// kind and record are this computer's record of the folder, looked up while it exists, so step 8
// forgets by that record (case Gone) and the row goes; one this computer did not record touches no
// record and says no key line. Positive twin first: not recorded here, the keyring is never asked.
func TestMA1051_AnEncryptedMarkerOnlyReRunUsesThisComputersRecord(t *testing.T) {
	leftover := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, ".mailarchive-lastverify.json"), "{}")
		plantMarker(t, dir)
		return dir
	}
	{
		dir := leftover(t)
		r := newRig(t, dir)
		p := r.plan(t)
		if p.Kind == Encrypted || p.Record.Recorded || p.KeyLine() != "" {
			t.Fatalf("not recorded: kind %v record %+v", p.Kind, p.Record)
		}
		res := Execute(p, p.Name, false, r.x)
		if res.Status != StatusComplete || len(r.kr.asked) != 0 || exists(dir) || !strings.Contains(string(res.Record), "This computer's record and key: not applicable") {
			t.Fatalf("positive twin: %s (asked %q)\n%s", res.Message(), r.kr.asked, res.Record)
		}
	}
	dir := leftover(t)
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	p := r.plan(t)
	if p.Kind != Encrypted || !p.Record.Recorded || p.Record.ID != testArchiveID || p.Rerun != RerunNoAnchor {
		t.Fatalf("recorded here: kind %v record %+v rerun %v", p.Kind, p.Record, p.Rerun)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || res.KeyForgotten != "forgotten" || len(r.kr.asked) != 1 || r.kr.asked[0] != dir || len(r.rows) != 1 || exists(dir) {
		t.Fatalf("%s (asked %q rows %q)", res.Message(), r.kr.asked, r.rows)
	}
}

// covers: MA-1051, R36
// The marker's began is shown only when it is a valid time (§15.3): one holding a control
// character or a bidi override is not known, so the plan says "This finishes a delete of this
// folder that did not finish." and its record "on a date not recorded"; nothing of it is echoed.
func TestMA1051_AMarkersBadBeganIsNeverShown(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	writeFile(t, filepath.Join(dir, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z‮\u0007","note":"x"}`)
	p := mustPlan(t, dir, newRig(t, dir).x.Deps)
	saved := string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p}))
	if p.FinishesLine() != "This finishes a delete of this folder that did not finish." || strings.ContainsAny(saved, "‮\u0007") ||
		!strings.Contains(saved, "Finishes: a delete of this folder that did not finish, as the folder's note says; MailArchive cannot confirm it") {
		t.Fatalf("%q\n%s", p.FinishesLine(), saved)
	}
}

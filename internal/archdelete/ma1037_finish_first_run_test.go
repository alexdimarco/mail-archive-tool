package archdelete

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// withRunHistory gives the archive at dir a run history (two backups) and a history log whose
// first run header is at first: what the record states as "Last backup" and "First run".
func withRunHistory(t *testing.T, dir string, first, last time.Time) {
	t.Helper()
	hist, err := json.Marshal([]state.RunHistoryEntry{{When: first, Status: state.RunOK}, {When: last, Status: state.RunOK}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, state.RunHistoryName), string(hist))
	writeFile(t, filepath.Join(dir, state.HistoryName), `{"run":1,"at":"`+first.UTC().Format(time.RFC3339)+`"}`+"\n")
}

// covers: MA-1037, R36
// The record of the run that finishes a delete states the archive as the delete's first run read
// it (the 1.5.3 re-walk's N3): the first run removed the run history, so a later run's own read
// says "Last backup: not known", but the first run's started line carried its values forward.
// Positive twin first: the first run's own record has them.
func TestTheFinishingRecordKeepsTheFirstRunsDates(t *testing.T) {
	first := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	last := first.Add(48 * time.Hour)
	dir := smallArchive(t, t.TempDir())
	withRunHistory(t, dir, first, last)
	r := newRig(t, dir)
	held := true
	faultOn(t, nil, func(rel string) error {
		if held && strings.HasSuffix(rel, ".html") {
			return errors.New("The process cannot access the file")
		}
		return nil
	})
	wantLast, wantFirst := localStamp(last)+" (as recorded in the archive)", localStamp(first)+" (as recorded in the archive)"
	res1 := r.run(t, false)
	rec1 := string(res1.Record)
	// Positive twin: the first run's own record.
	if res1.Status != StatusPartial || recordValueOf(rec1, "Last backup") != wantLast || recordValueOf(rec1, "First run") != wantFirst {
		t.Fatalf("the first run's record (%s):\n%s", res1.Status, rec1)
	}
	if _, err := os.Stat(filepath.Join(dir, state.RunHistoryName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture: the first run should have removed the run history: %v", err)
	}
	held = false
	res2 := r.run(t, false)
	rec2 := string(res2.Record)
	if res2.Status != StatusComplete || recordValueOf(rec2, "Last backup") != wantLast || recordValueOf(rec2, "First run") != wantFirst ||
		recordValueOf(rec2, "Messages") != "1 (as recorded in the archive)" {
		t.Fatalf("the finishing record (%s):\n%s", res2.Status, rec2)
	}
}

// covers: MA-1037, R36
// The first run's values fill only what the finishing run cannot read; they never replace what the
// archive itself records now (the settle pass's MED-1). A delete stopped before step 4 removed no
// file, a backup then ran into the whole archive, and the finishing record states that newer last
// backup "as recorded in the archive", with the first run's first run kept. Positive twin first:
// the stopped run's own record states the older last backup.
func TestTheFinishingRecordPrefersWhatTheArchiveRecordsNow(t *testing.T) {
	first := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	last := first.Add(48 * time.Hour)
	newer := last.Add(30 * 24 * time.Hour)
	dir := smallArchive(t, t.TempDir())
	withRunHistory(t, dir, first, last)
	r := newRig(t, dir)
	r.x.BeforeStep = func(step int) error {
		if step == 4 {
			return errors.New("A MailArchive Desktop is running")
		}
		return nil
	}
	res1 := r.run(t, false)
	// Positive twin: the stopped run's record.
	if res1.Status != StatusPartial || recordValueOf(string(res1.Record), "Last backup") != localStamp(last)+" (as recorded in the archive)" {
		t.Fatalf("the stopped run (%s):\n%s", res1.Status, res1.Record)
	}
	r.x.BeforeStep = nil
	// A backup runs into the still-whole archive: its run history now ends at newer.
	withRunHistory(t, dir, first, newer)
	p := r.plan(t)
	if p.Finishes == nil || p.Finishes.Captured == nil {
		t.Fatalf("fixture: the plan does not finish the stopped delete with its first run's values")
	}
	res := Execute(p, p.Name, false, r.x)
	rec := string(res.Record)
	if res.Status != StatusComplete || recordValueOf(rec, "Last backup") != localStamp(newer)+" (as recorded in the archive)" ||
		recordValueOf(rec, "First run") != localStamp(first)+" (as recorded in the archive)" ||
		recordValueOf(rec, "Messages") != "1 (as recorded in the archive)" {
		t.Fatalf("the finishing record (%s):\n%s", res.Status, rec)
	}
}

// TestWithFirstRunFillsOnlyWhatThisRunCannotRead pins the merge rule itself: this run's message
// count and mail dates when it read them, the later last backup, the earlier first run and run
// history start, and the first run's values only where this run has none.
// covers: MA-1037
func TestWithFirstRunFillsOnlyWhatThisRunCannotRead(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2025, 3, day, 0, 0, 0, 0, time.UTC) }
	firstRun := Captured{Known: true, Messages: 5, MailFrom: d(1), MailTo: d(2), FirstRun: d(3), LastBackup: d(4), OldestRun: d(3)}
	// Positive twin: this run read nothing (the files are gone), so the first run's values stand.
	if got := withFirstRun(Captured{}, firstRun); got != firstRun {
		t.Fatalf("this run read nothing: %+v", got)
	}
	own := Captured{Known: true, Messages: 9, MailFrom: d(1), MailTo: d(20), FirstRun: d(10), LastBackup: d(21), OldestRun: d(10), KeyOpen: false}
	want := Captured{Known: true, Messages: 9, MailFrom: d(1), MailTo: d(20), FirstRun: d(3), LastBackup: d(21), OldestRun: d(3)}
	if got := withFirstRun(own, firstRun); got != want {
		t.Fatalf("this run read newer values:\n got %+v\nwant %+v", got, want)
	}
	// An older last backup read now (a run history put back from a copy) never replaces the newer one.
	own2 := Captured{LastBackup: d(2)}
	if got := withFirstRun(own2, firstRun); got.LastBackup != d(4) || got.Messages != 5 {
		t.Fatalf("an older last backup read now: %+v", got)
	}
}

// stopBefore8 deletes r's archive with a stop before step 8: the folder is removed and the delete
// did not finish, so Delete again is §5.4's folder-gone re-run.
func stopBefore8(t *testing.T, r *rig) {
	t.Helper()
	r.x.BeforeStep = func(step int) error {
		if step == 8 {
			return errors.New("A MailArchive Desktop is running")
		}
		return nil
	}
	if res := r.run(t, false); res.Status != StatusPartial || exists(r.dir) {
		t.Fatalf("the stopped run: %s", res.Message())
	}
	r.x.BeforeStep = nil
}

// covers: MA-1037, R36
// A plain archive whose folder an earlier run removed is never said to be "encrypted, key not
// open" (the 1.5.3 re-walk's N5): with the first run's values in the log, the finishing record
// states them; with a started line that carries none (an older one), it says the files were
// already removed. Positive twin first: an encrypted archive's record, its key not open, still
// says "encrypted, key not open".
func TestAPlainFinishingRecordNeverSaysEncrypted(t *testing.T) {
	// Positive twin: encrypted, key not open.
	er := newRig(t, encryptedClean(t))
	ep := er.plan(t)
	if v := recordValueOf(string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: ep})), "Messages"); v != "not known: encrypted, key not open" {
		t.Fatalf("the encrypted twin: %q", v)
	}

	// The first run's values carried.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	stopBefore8(t, r)
	p := r.plan(t)
	if p.Rerun != RerunFolderGone {
		t.Fatalf("rerun %v", p.Rerun)
	}
	res := Execute(p, p.Name, false, r.x)
	rec := string(res.Record)
	if res.Status != StatusComplete || recordValueOf(rec, "Kind") != "plain" || recordValueOf(rec, "Messages") != "1 (as recorded in the archive)" ||
		strings.Contains(rec, "encrypted, key not open") {
		t.Fatalf("the folder-gone record (%s):\n%s", res.Status, rec)
	}

	// A started line that carries none (written before this build): "already removed".
	dir = smallArchive(t, t.TempDir())
	r = newRig(t, dir)
	stopBefore8(t, r)
	stripCaptured(t, r.x.Log)
	p = r.plan(t)
	rec = string(RecordText(RecordInput{Status: StatusComplete, When: time.Now(), Plan: p}))
	for _, k := range []string{"Messages", "Mail dates", "First run"} {
		if v := recordValueOf(rec, k); v != "not known: the archive's files were already removed" {
			t.Fatalf("%s: %q\n%s", k, v, rec)
		}
	}
}

// stripCaptured rewrites deletions.log at path with no line carrying the first run's values, as
// a log written before they were kept.
func stripCaptured(t *testing.T, path string) {
	t.Helper()
	var out []byte
	for _, l := range logLines(t, path) {
		delete(l, "captured")
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		out = append(append(out, b...), '\n')
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1037, R36
// The result of a run that finishes a delete gives the whole delete's count on screen (the 1.5.3
// re-walk's N6), and when an earlier run's count is not known says so in words, never "at least
// 0" (N2). Positive twin first: an ordinary delete's result has no such line.
func TestTheFinishingResultGivesTheTotal(t *testing.T) {
	twin := newRig(t, smallArchive(t, t.TempDir()))
	if res := twin.run(t, false); res.Status != StatusComplete || hasLine(res, "This finishes") {
		t.Fatalf("an ordinary delete: %s", res.Message())
	}

	first := time.Date(2026, 11, 1, 22, 5, 0, 0, time.UTC)
	dir := smallArchive(t, t.TempDir())
	writeFile(t, dir+"/README.txt", "the archive's readme")
	r := newRig(t, dir)
	r.x.Now = func() time.Time { return first }
	held := true
	faultOn(t, nil, func(rel string) error {
		if held && strings.HasSuffix(rel, ".html") {
			return errors.New("The process cannot access the file")
		}
		return nil
	})
	res1 := r.run(t, false)
	held = false
	r.x.Now = func() time.Time { return first.Add(time.Hour) }
	res2 := r.run(t, false)
	want := "This finishes the delete begun " + localStamp(first) + ": " + plural(res1.Removed+res2.Removed, "file", "files") + " (" + size(res1.Bytes+res2.Bytes) + ") removed in all."
	if res2.Status != StatusComplete || len(res2.Lines) < 2 || res2.Lines[1] != want {
		t.Fatalf("the finishing result, want line 2 %q:\n%s", want, res2.Message())
	}

	// A crash before it: not known.
	d2 := smallArchive(t, t.TempDir())
	r2 := newRig(t, d2)
	p := r2.plan(t)
	appendRawLog(t, r2.x.Log, map[string]any{"at": first.Format(time.RFC3339Nano), "event": "started", "run": "crashedrun", "folder": d2, "row": d2,
		"identity": map[string]any{"volume": p.Identity.Volume, "file_id": p.Identity.FileID}, "kind": "plain"})
	res := r2.run(t, false)
	want = "This finishes the delete begun " + localStamp(first) + ". How many files its earlier runs removed is not known: a run stopped before it wrote its record."
	if res.Status != StatusComplete || len(res.Lines) < 2 || res.Lines[1] != want || strings.Contains(res.Message(), "at least 0") ||
		recordValueOf(string(res.Record), "Files removed earlier") != "not known: a run stopped before it wrote its record" {
		t.Fatalf("after a crash, want line 2 %q:\n%s\n%s", want, res.Message(), res.Record)
	}
}

// covers: MA-1037, R36
// An encrypted archive's sealed values never reach deletions.log, a plain file, even when the plan
// read them with the key open (MA-1011): the started line carries only its last backup and where
// its run history begins. Positive twin first: a plain archive's started line carries its message
// count and mail dates.
func TestASealedValueNeverReachesTheLog(t *testing.T) {
	plainRig := newRig(t, smallArchive(t, t.TempDir()))
	plainRig.run(t, false)
	pc, _ := logLines(t, plainRig.x.Log)[0]["captured"].(map[string]any)
	if pc == nil || pc["known"] != true || pc["messages"] != float64(1) {
		t.Fatalf("the plain twin's started line: %v", logLines(t, plainRig.x.Log)[0])
	}

	last := time.Date(2025, 3, 6, 10, 30, 0, 0, time.UTC)
	dir := encryptedClean(t)
	hist, err := json.Marshal([]state.RunHistoryEntry{{When: last, Status: state.RunOK}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, state.RunHistoryName), string(hist)) // not sealed (§8.1 [O2-05])
	r := newRig(t, dir)
	r.x.Deps.Record = recordedAt(dir)
	r.x.Deps.KeyPlan = r.kr.PlanForget
	r.x.Deps.Sealed = func(string, string) (Captured, bool) {
		return Captured{Known: true, KeyOpen: true, Messages: 42, MailFrom: last.Add(-1000 * time.Hour), MailTo: last, FirstRun: last.Add(-48 * time.Hour)}, true
	}
	p := r.plan(t)
	if !p.Captured.Known || p.Captured.Messages != 42 {
		t.Fatalf("fixture: the plan did not read the sealed values: %+v", p.Captured)
	}
	Execute(p, p.Name, false, r.x)
	started := logLines(t, r.x.Log)[0]
	c, _ := started["captured"].(map[string]any)
	if c == nil || c["last_backup"] == nil {
		t.Fatalf("the started line keeps no last backup: %v", started)
	}
	for _, k := range []string{"known", "messages", "mail_from", "mail_to", "first_run_at"} {
		if _, ok := c[k]; ok {
			t.Fatalf("an encrypted archive's started line holds %q: %v", k, c)
		}
	}
}

package archdelete

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1028, R36
// When this computer remembers no key for an encrypted archive, the plan, the result and the
// record say only its record is forgotten (the 1.5.3 friction review's B3), and a deletion record
// of a delete that stopped before step 8 says the record is kept, never "not applicable". Positive
// twin first: with the key remembered, the key is said to be forgotten.
func TestTheKeyWordsSayWhenNoKeyWasRemembered(t *testing.T) {
	// Positive twin: remembered (the rig's keyring).
	r := newRig(t, encryptedClean(t))
	r.x.Deps.Record = recordedAt("")
	r.x.Deps.KeyPlan = r.kr.PlanForget
	p := r.plan(t)
	if !strings.HasPrefix(p.KeyLine(), "If the folder is removed, this computer will forget this archive's key") {
		t.Fatalf("the twin's key line: %q", p.KeyLine())
	}
	res := Execute(p, p.Name, false, r.x)
	if res.KeyForgotten != "forgotten" || !hasLine(res, "This computer forgot the archive's key.") {
		t.Fatalf("the twin's result: %s", res.Message())
	}

	// No key remembered here.
	r = newRig(t, encryptedClean(t))
	r.kr.plan.Remembered = false
	r.x.Deps.Record = recordedAt("")
	r.x.Deps.KeyPlan = r.kr.PlanForget
	p = r.plan(t)
	if got := p.KeyLine(); got != "If the folder is removed, this computer forgets its record of this archive (no key is remembered here)." {
		t.Fatalf("key line: %q", got)
	}
	if v := recordValueOf(string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p})), "This computer's record and key"); v != "record to be forgotten (no key is remembered)" {
		t.Fatalf("saved plan: %q", v)
	}
	res = Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || hasLine(res, "forgot the archive's key") || !hasLine(res, "This computer forgot its record of this archive (no key was remembered here).") ||
		recordValueOf(string(res.Record), "This computer's record and key") != "record forgotten (no key was remembered)" {
		t.Fatalf("result:\n%s\nrecord:\n%s", res.Message(), res.Record)
	}

	// A delete that stopped before step 8: the record still exists.
	r = newRig(t, encryptedClean(t))
	r.x.Deps.Record = recordedAt("")
	r.x.Deps.KeyPlan = r.kr.PlanForget
	r.sched.fail[r.plan(t).Schedules.Entries[0].Name] = os.ErrPermission
	res = r.run(t, false)
	if res.Status != StatusPartial || recordValueOf(string(res.Record), "This computer's record and key") != "kept: the delete stopped before it reached them" {
		t.Fatalf("a stop at step 2: %s\n%s", res.Status, res.Record)
	}
}

// covers: MA-1028, R36
// The shared result and record words of the 1.5.3 friction review's B7, B8, B9 and B5: one file
// is "it", never "them"; the folder kept is counted once ("2 files are left in it: 1 file of
// yours and 1 mail copy the archive did not record", "1 file that is not the archive's"); a name
// that does not match says capital letters count; every moment in the record has one form, this
// computer's time with its offset; "First run: not known" says why, and where the archive's run
// history begins when it has one; the plan's log line says "If you delete it" and, given one, the
// log's path; the result says the log's path and "Removed from Your archives." with periods.
// Positive twins first: several files are "them"; a folder of the person's files alone keeps its
// count.
func TestTheSharedDeleteWordsCountAndDateOnce(t *testing.T) {
	// B7: the kept sentence, counted once.
	for _, c := range []struct {
		kept, copies int
		want         string
	}{
		{3, 0, "The folder is kept: it holds 3 files that are not the archive's."}, // twin
		{1, 0, "The folder is kept: it holds 1 file that is not the archive's."},
		{2, 1, "The folder is kept: 2 files are left in it: 1 file of yours and 1 mail copy the archive did not record."},
		{1, 1, "The folder is kept: 1 file is left in it: 1 mail copy the archive did not record."},
		{5, 2, "The folder is kept: 5 files are left in it: 3 files of yours and 2 mail copies the archive did not record."},
	} {
		if got := keptSentence(c.kept, c.copies); got != c.want {
			t.Errorf("keptSentence(%d, %d) = %q, want %q", c.kept, c.copies, got, c.want)
		}
	}
	// B7: "them" for several files (twin), "it" for one.
	if got := CauseInUse.remedy(2); got != "Close Outlook or any program using them, then click Delete again." {
		t.Errorf("two files: %q", got)
	}
	if got := CauseOther.remedy(1); got != "Close any program using it, then click Delete again." {
		t.Errorf("one file: %q", got)
	}

	// B8: a name that does not match.
	r := newRig(t, smallArchive(t, t.TempDir()))
	p := r.plan(t)
	if res := Execute(p, p.Name+"x", false, r.x); res.Refusal == nil || !strings.HasSuffix(res.Refusal.Message, "Type it exactly as shown (capital letters count).") {
		t.Fatalf("a name that does not match: %+v", res.Refusal)
	}

	// B9: one form for every moment; the first run's "not known" says why.
	dir := smallArchive(t, t.TempDir())
	oldest := time.Date(2025, 3, 4, 10, 30, 0, 0, time.UTC)
	hist, _ := json.Marshal([]state.RunHistoryEntry{{When: oldest, Status: state.RunOK}, {When: oldest.Add(48 * time.Hour), Status: state.RunOK}})
	if err := os.WriteFile(filepath.Join(dir, state.RunHistoryName), hist, 0o600); err != nil {
		t.Fatal(err)
	}
	r = newRig(t, dir)
	p = r.plan(t)
	when := time.Date(2026, 11, 2, 6, 8, 50, 0, time.UTC)
	text := string(RecordText(RecordInput{Status: StatusPlanned, When: when, Plan: p}))
	if got := recordValueOf(text, "Date"); got != when.Local().Format("2006-01-02 15:04:05 -07:00") || strings.Count(got, "(") != 0 {
		t.Errorf("Date: %q", got)
	}
	if got := recordValueOf(text, "Last backup"); got != localStamp(oldest.Add(48*time.Hour))+" (as recorded in the archive)" {
		t.Errorf("Last backup: %q", got)
	}
	if got := recordValueOf(text, "First run"); got != "not known: the archive's history of its runs begins "+localStamp(oldest)+" (as recorded in the archive)" {
		t.Errorf("First run: %q", got)
	}
	if strings.Contains(text, " UTC") {
		t.Errorf("a moment in another form:\n%s", text)
	}

	// B5: the plan's log line; the result's log line and row line.
	if PlanLogLineAt("") != PlanLogLine || !strings.HasPrefix(PlanLogLine, "If you delete it, a line is added") ||
		PlanLogLineAt("/home/a/.config/mailarchive/deletions.log") != "If you delete it, a line is added to this computer's deletion log (/home/a/.config/mailarchive/deletions.log), which keeps the date, the folder and what was removed." {
		t.Errorf("the plan's log line: %q / %q", PlanLogLine, PlanLogLineAt("/x"))
	}
	res := Execute(p, p.Name, false, r.x)
	if !hasLine(res, "Recorded in deletions.log ("+r.x.Log+").") || !hasLine(res, "Removed from Your archives.") {
		t.Fatalf("the result:\n%s", res.Message())
	}
}

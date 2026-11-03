package archdelete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// The computer that began a delete another one finished (docs/design-archive-delete.md §15.4,
// §15.8 question 2; the owner's ruling Q4 of 2 November 2026; §15.10 slice 4; MA-1057): Delete
// again finds the folder gone, its parent reachable and no `folder removed` line, and asks whether
// it was finished elsewhere; on yes it finishes the config side here.

// elsewhereRig is an encrypted archive's delete begun on this computer and stopped in step 6 (its
// started line, with this computer's record), then the folder removed elsewhere: under a parent
// "Drive" that is still there.
func elsewhereRig(t *testing.T) (*rig, string) {
	t.Helper()
	parent := filepath.Join(t.TempDir(), "Drive")
	dir := filepath.Join(parent, "Vault")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := newRig(t, dir)
	r.kr.folder = ""
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "aaaaaaaaaaaaaaaa", "folder": dir, "row": dir,
		"kind": "encrypted", "record_id": testArchiveID, "record_spelling": dir, "identity": idOf(t, dir)})
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	return r, parent
}

// covers: MA-1057, R36
// Delete again with the folder gone, its parent reachable and no `folder removed` line asks the
// question (CaseFinishedElsewhere), in §15.8 question 2's words, and changes nothing: no schedule
// stopped, no record or key asked, no row removed, no log line. Positive twins first: with the
// parent unreachable it is "Reconnect the drive or folder first", never the question, even when
// the person already said it was finished elsewhere (plug the drive in); and with the `folder
// removed` line it is the folder-gone re-run, asked nothing.
func TestMA1057_DeleteAgainWithTheFolderGoneAsksAndChangesNothing(t *testing.T) {
	{
		r, parent := elsewhereRig(t)
		if err := os.RemoveAll(parent); err != nil {
			t.Fatal(err)
		}
		for _, said := range []bool{false, true} {
			deps := r.x.Deps
			deps.FinishedElsewhere = said
			rc, msg, c := refusalOf(t, r.dir, deps)
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(msgUnreachable),
				assure.NoSideEffect(func() bool { return len(r.kr.asked) == 0 && len(r.rows) == 0 && len(r.sched.removed) == 0 }))
			if c != CaseUnreachable {
				t.Fatalf("parent unreachable, said %v: case %s", said, c)
			}
		}
	}
	{
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		crashAfterFolderRemoved(t, r.x.Log, dir, map[string]any{"kind": "plain"})
		if p := r.plan(t); p.Rerun != RerunFolderGone || p.Elsewhere {
			t.Fatalf("folder removed line: rerun %v elsewhere %v", p.Rerun, p.Elsewhere)
		}
	}
	r, _ := elsewhereRig(t)
	lines := len(logLines(t, r.x.Log))
	rc, msg, c := refusalOf(t, r.dir, r.x.Deps)
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(ElsewhereQuestion(Encrypted)),
		assure.NoSideEffect(func() bool {
			return len(r.kr.asked) == 0 && len(r.rows) == 0 && len(r.sched.removed) == 0 && len(logLines(t, r.x.Log)) == lines
		}))
	if c != CaseFinishedElsewhere {
		t.Fatalf("case %s", c)
	}
	want := "This archive's folder is not there. If its delete was finished from another computer or user account, MailArchive can finish it here too: it stops this computer's schedules for it, forgets this computer's record and key of it, and removes its row. If the folder is on a drive that is not plugged in, plug it in instead."
	if ElsewhereQuestion(Encrypted) != want || strings.Contains(ElsewhereQuestion(Plain), "key") {
		t.Fatalf("the question's words: %q / %q", ElsewhereQuestion(Encrypted), ElsewhereQuestion(Plain))
	}
}

// covers: MA-1057, R36
// "It was finished elsewhere" (Deps.FinishedElsewhere) plans the folder-gone re-run, and the delete
// stops this computer's schedules for the folder, forgets this computer's record and key of it by
// the started line's ID and Spelling (step 8), removes its row and ends complete; the plan, the
// result and the deletion record say "the folder was not found here; you said this delete was
// finished elsewhere"; the log ends the delete, so the row and uninstall are unblocked
// (PendingDeletes). Positive twin first: the plan of a folder-gone re-run with its `folder removed`
// line says nothing of elsewhere.
func TestMA1057_ItWasFinishedElsewhereFinishesTheConfigSideHere(t *testing.T) {
	{
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		crashAfterFolderRemoved(t, r.x.Log, dir, map[string]any{"kind": "plain"})
		p := r.plan(t)
		saved := string(RecordText(RecordInput{Status: StatusPlanned, Plan: p}))
		if len(p.FinishesNotes()) != 0 || strings.Contains(saved, "finished elsewhere") || strings.Contains(saved, "Folder here") {
			t.Fatalf("positive twin: %q\n%s", p.FinishesNotes(), saved)
		}
	}
	r, _ := elsewhereRig(t)
	r.sched.state[schedule.DefaultNameFor(r.dir)] = schedule.Installed
	deps := r.x.Deps
	deps.FinishedElsewhere = true
	p := mustPlan(t, r.dir, deps)
	if p.Rerun != RerunFolderGone || !p.Elsewhere || !p.Record.Recorded || p.Record.Spelling != r.dir {
		t.Fatalf("rerun %v elsewhere %v record %+v", p.Rerun, p.Elsewhere, p.Record)
	}
	if notes := p.FinishesNotes(); len(notes) != 1 || notes[0] != MsgElsewhereSaid {
		t.Fatalf("notes %q", notes)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || res.KeyForgotten != "forgotten" || len(r.rows) != 1 || len(r.kr.asked) != 1 || r.kr.asked[0] != r.dir {
		t.Fatalf("%s (rows %q asked %q)", res.Message(), r.rows, r.kr.asked)
	}
	if !contains(r.sched.removed, schedule.DefaultNameFor(r.dir)) {
		t.Fatalf("the schedule is stopped: removed %q", r.sched.removed)
	}
	if !hasLine(res, MsgElsewhereSaid) || !containsLine(string(res.Record), "Folder here: "+elsewhereValue) {
		t.Fatalf("%s\n%s", res.Message(), res.Record)
	}
	if exists(r.dir) {
		t.Fatal("the folder is not made again")
	}
	if u, _ := UnfinishedDelete(r.x.Log, r.dir, ""); u != nil {
		t.Fatalf("the delete is ended: %+v", u)
	}
	if pend, err := PendingDeletes(r.x.Log, ""); err != nil || len(pend) != 0 {
		t.Fatalf("uninstall is unblocked: %q %v", pend, err)
	}
	if l := ma1051StartedLine(t, r.x.Log); l["rerun"] != rerunElsewhereWord {
		t.Fatalf("the started line says so: %v", l)
	}
}

// covers: MA-1057, R36
// The folder there again between the answer and the delete (a drive plugged in after all): the
// delete refuses at its start, "This archive changed since you clicked Delete", and nothing is
// stopped, forgotten or removed. A re-run that stopped before it finished asks again next time.
func TestMA1057_TheFolderBackBeforeTheDeleteRefuses(t *testing.T) {
	r, _ := elsewhereRig(t)
	deps := r.x.Deps
	deps.FinishedElsewhere = true
	p := mustPlan(t, r.dir, deps)
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusRefusedAfterStart || res.Refusal == nil || res.Refusal.Case != CaseChanged || len(r.kr.asked) != 0 || len(r.rows) != 0 || len(r.sched.removed) != 0 {
		t.Fatalf("%s", res.Message())
	}
	if err := os.Remove(r.dir); err != nil {
		t.Fatal(err)
	}
	if _, _, c := refusalOf(t, r.dir, r.x.Deps); c != CaseFinishedElsewhere {
		t.Fatalf("asked again: case %s", c)
	}
}

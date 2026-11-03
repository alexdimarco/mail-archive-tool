package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1057, R36
// Delete… on the row of a folder this computer began deleting, now gone with its parent reachable
// and no `folder removed` line (§15.8 question 2, the owner's ruling Q4): the plan route answers
// the question (elsewhere, with its two answers) and changes nothing; asked again with "It was
// finished elsewhere" it plans the folder-gone re-run, its panel saying so; the delete stops this
// computer's schedule for it and removes its row, and its record says "Folder here: the folder was
// not found here; you said this delete was finished elsewhere". Positive twin first: a row whose
// drive is unplugged (its parent gone too) is "Reconnect the drive or folder first", never asked.
func TestMA1057_TheDesktopAsksThenFinishesADeleteFinishedElsewhere(t *testing.T) {
	e := newDeleteEnv(t)
	gone := func(name string) (string, string) {
		t.Helper()
		parent := filepath.Join(t.TempDir(), "Drive")
		dir := filepath.Join(parent, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		buildArchiveAt(t, dir)
		e.list(dir)
		ma1052Started(t, e.cfg, dir)
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		return dir, parent
	}

	// Positive twin.
	unplugged, drive := gone("Unplugged")
	if err := os.RemoveAll(drive); err != nil {
		t.Fatal(err)
	}
	for _, said := range []bool{false, true} {
		_, p := e.post("/api/archive/delete/plan", map[string]any{"path": unplugged, "elsewhere": said})
		assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names("Reconnect the drive or folder first"),
			assure.NoSideEffect(func() bool { return p["elsewhere"] == nil && p["token"] == nil && e.listed(unplugged) }))
	}

	dir, _ := gone("Finished elsewhere")
	def := schedule.DefaultNameFor(dir)
	e.sched.installed[def] = true
	logBefore := deletionsLog(t, e.cfg)
	p := e.plan(dir)
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names(archdelete.ElsewhereQuestion(archdelete.Plain)),
		assure.NoSideEffect(func() bool {
			return p["elsewhere"] == true && p["token"] == nil && e.listed(dir) && len(e.sched.removedNames()) == 0 && deletionsLog(t, e.cfg) == logBefore
		}))
	if p["yes"] != archdelete.ElsewhereYes || p["no"] != archdelete.ElsewhereNo {
		t.Fatalf("the answers: %v", p)
	}
	_, q := e.post("/api/archive/delete/plan", map[string]any{"path": dir, "elsewhere": true})
	notes, _ := q["finishesNotes"].([]any)
	if q["ok"] != true || len(notes) != 1 || notes[0] != archdelete.MsgElsewhereSaid {
		t.Fatalf("the plan once answered: %v", q)
	}
	d := e.deleteWith(q["token"].(string), q["name"].(string), false)
	if d["status"] != archdelete.StatusComplete || d["rowRemoved"] != true || e.listed(dir) || !contains(e.sched.removedNames(), def) {
		t.Fatalf("the delete: %v (removed %q)", d, e.sched.removedNames())
	}
	if rec := string(recordBytes(t, d)); !strings.Contains(rec, "\nFolder here: the folder was not found here; you said this delete was finished elsewhere\n") {
		t.Fatalf("the record:\n%s", rec)
	}
	if u, err := archdelete.UnfinishedDelete(e.cfg.deletionsLogPath(), dir, ""); err != nil || u != nil {
		t.Fatalf("the delete is ended in the log: %+v %v", u, err)
	}
}

// covers: MA-1057, R36, S41
// The page's side of the question (§15.8 question 2): it shows in the row with "It was finished
// elsewhere" and "Cancel", no panel; Cancel says nothing was changed; the answer asks for the plan
// again saying so, and the panel shows the plan's line saying it. Positive twin first.
func TestMA1057_ThePageAsksBeforeItPlansADeleteFinishedElsewhere(t *testing.T) {
	if passed := runPageScenario(t, "elsewhere"); len(passed) != 4 {
		t.Errorf("the elsewhere scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
}

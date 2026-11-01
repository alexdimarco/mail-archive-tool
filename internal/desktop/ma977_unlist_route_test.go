package desktop

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-977, R36, S41
// Remove from list from the Desktop (design-archive-delete §3.2, §5.2; C3, §14's UnlistDeps.Named):
// without confirm it changes nothing and asks, naming the schedule it would stop; with confirm
// and the names it showed it stops that schedule only, removes its job file and the row, and the
// folder is byte-identical. An entry found at Yes that the question did not name refuses,
// nothing changed; the archive in use while reachable is refused in §7's words. Positive twin
// first: a row with no schedule is removed after Yes.
func TestRemoveFromListAsksThenStopsOnlyWhatItNamed(t *testing.T) {
	e := newDeleteEnv(t)
	plain := e.plainArchive("No Schedule")
	_, q := e.post("/api/archive/unlist", map[string]any{"path": plain})
	if q["ok"] != true || q["ask"] != true || q["question"] != "Remove from list? The folder and its files are kept." || !e.listed(plain) {
		t.Fatalf("the question: %v (listed %v)", q, e.listed(plain))
	}
	before := treeState(t, plain)
	_, done := e.post("/api/archive/unlist", map[string]any{"path": plain, "confirm": true, "named": q["named"]})
	if done["ok"] != true || e.listed(plain) || !reflect.DeepEqual(treeState(t, plain), before) {
		t.Fatalf("Remove from list: %v; listed %v", done, e.listed(plain))
	}
	// Not re-added by a page load.
	e.page()
	if e.listed(plain) {
		t.Error("a page load listed the removed row again")
	}

	// A row with a scheduled backup: the question names it.
	dir := e.plainArchive("Scheduled")
	name := schedule.DefaultNameFor(dir)
	jobFile := plantRunJob(t, dir)
	e.sched.installed[name] = true
	_, q = e.post("/api/archive/unlist", map[string]any{"path": dir})
	if q["ok"] != true || !strings.Contains(q["question"].(string), "Its scheduled backup "+name+" will also be stopped.") {
		t.Fatalf("the question: %v", q)
	}
	if len(e.sched.removedNames()) != 0 {
		t.Fatal("the question stopped a schedule")
	}
	// Yes to a question that named nothing: the entry was not named, nothing is changed.
	before = treeState(t, dir)
	_, m := e.post("/api/archive/unlist", map[string]any{"path": dir, "confirm": true, "named": []string{}})
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names(name, "was not named"),
		assure.NoSideEffect(func() bool {
			_, err := os.Stat(jobFile)
			return e.listed(dir) && len(e.sched.removedNames()) == 0 && err == nil
		}))
	_, done = e.post("/api/archive/unlist", map[string]any{"path": dir, "confirm": true, "named": q["named"]})
	if done["ok"] != true || e.listed(dir) {
		t.Fatalf("Remove from list: %v", done)
	}
	if got := e.sched.removedNames(); !reflect.DeepEqual(got, []string{name}) {
		t.Errorf("stopped %v, want only %s", got, name)
	}
	if _, err := os.Stat(jobFile); !os.IsNotExist(err) {
		t.Errorf("the job file is still there: %v", err)
	}
	if !reflect.DeepEqual(treeState(t, dir), before) {
		t.Error("Remove from list changed the folder")
	}

	// The archive in use, reachable.
	cur := e.plainArchive("In Use")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	_, m = e.post("/api/archive/unlist", map[string]any{"path": cur, "confirm": true})
	assure.Refused(t, http.StatusOK, replyErr(m), assure.Code(http.StatusOK), assure.Names("This is the archive in use", "Add another archive"),
		assure.NoSideEffect(func() bool { return e.listed(cur) && LoadSettings(e.cfg.SettingsPath).Out == cur }))
}

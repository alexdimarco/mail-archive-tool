package desktop

import (
	"net/http"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

// covers: MA-979, R36
// A delete that begins while a Pause or Resume of its folder runs is refused, its plan kept, until
// that action ends (the residual fixes' adversarial pass, finding 2): the check before Pause or
// Resume and the action are one step, so a slow Pause never seals a folder being emptied and a
// Resume never puts back a scheduled backup that folder's delete removed. The seams only time the
// delete; the real Pause and Resume run. Positive twins first: a delete of another archive begins
// and completes while the Pause runs, and the kept plan's delete completes once both have ended.
func TestADeleteCannotBeginWhileAPauseOrResumeOfItsFolderRuns(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Held")
	other := e.plainArchive("Other")
	e.list(dir, other)
	p, op := e.plan(dir), e.plan(other)
	if p["ok"] != true || op["ok"] != true {
		t.Fatalf("the plans: %v %v", p, op)
	}
	tok := p["token"].(string)
	waiting := func() bool { return e.d.pending.get(tok, isDeletePlan) != nil }

	origPause, origResume := pauseArchiveRun, resumeArchiveRun
	t.Cleanup(func() { pauseArchiveRun, resumeArchiveRun = origPause, origResume })

	var twin, during map[string]any
	pauseArchiveRun = func(path, reason, deletionsLog string) error {
		twin = e.deleteWith(op["token"].(string), op["name"].(string), false)
		during = e.deleteWith(tok, p["name"].(string), false)
		return origPause(path, reason, deletionsLog)
	}
	if _, m := e.post("/api/pause-archive", map[string]any{"path": dir}); m["ok"] != true || !seal.IsSealed(dir) {
		t.Fatalf("the Pause: %v", m)
	}
	if twin["status"] != "complete" {
		t.Fatalf("positive twin: a delete of another archive while the Pause runs: %v", twin)
	}
	assure.Refused(t, http.StatusOK, replyErr(during), assure.Code(http.StatusOK), assure.Names(pausingRefusal),
		assure.NoSideEffect(func() bool {
			return during["ok"] == false && archdelete.IsArchive(dir) && e.listed(dir) && waiting()
		}))

	resumes := 0
	resumeArchiveRun = func(path, deletionsLog string) error {
		resumes++
		during = e.deleteWith(tok, p["name"].(string), false)
		return origResume(path, deletionsLog)
	}
	if _, m := e.post("/api/resume-archive", map[string]any{"path": dir}); m["ok"] != true || seal.IsSealed(dir) || resumes != 1 {
		t.Fatalf("the Resume: %v (called %d times)", m, resumes)
	}
	assure.Refused(t, http.StatusOK, replyErr(during), assure.Code(http.StatusOK), assure.Names(pausingRefusal),
		assure.NoSideEffect(func() bool {
			return during["ok"] == false && archdelete.IsArchive(dir) && e.listed(dir) && waiting()
		}))

	if r := e.deleteWith(tok, p["name"].(string), false); r["status"] != "complete" {
		t.Errorf("positive twin: the kept plan's delete once the Pause and Resume ended: %v", r)
	}
}

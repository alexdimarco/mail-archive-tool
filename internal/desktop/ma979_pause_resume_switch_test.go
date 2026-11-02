package desktop

import (
	"net/http"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

// covers: MA-979, R36
// Pause, Resume and a Switch posted from a tab drawn before a delete began (its row still shows
// those buttons) are refused while that folder's delete is under way, in MA-979's words, nothing
// changed (del-s3a re-verification, R1): Pause writes no marker into the folder being emptied,
// Resume leaves it paused and puts back no scheduled backup (wizard.ResumeArchive, which would, is
// never called), and a Switch leaves the current
// archive as it was. Positive twins first: each works on another archive while the delete runs.
func TestPauseResumeAndSwitchOfAnArchiveBeingDeletedAreRefused(t *testing.T) {
	e := newDeleteEnv(t)
	// Resume puts back a scheduled backup through wizard.ResumeArchive alone: counted, still run.
	resumes, origResume := 0, resumeArchiveRun
	t.Cleanup(func() { resumeArchiveRun = origResume })
	resumeArchiveRun = func(p, deletionsLog string) error { resumes++; return origResume(p, deletionsLog) }
	other := e.plainArchive("Other")
	paused := e.plainArchive("PausedOne")
	if err := seal.Seal(paused, "parked"); err != nil {
		t.Fatal(err)
	}

	// Pause and Switch while a delete of dir runs.
	dir := e.plainArchive("Deleting")
	release, done := blockedDelete(t, e, dir)
	if _, m := e.post("/api/pause-archive", map[string]any{"path": other}); m["ok"] != true || !seal.IsSealed(other) {
		t.Fatalf("positive twin: Pause of another archive while a delete runs: %v", m)
	}
	if _, m := e.post("/api/resume-archive", map[string]any{"path": other}); m["ok"] != true || seal.IsSealed(other) || resumes != 1 {
		t.Fatalf("positive twin: Resume of another archive while a delete runs: %v (resumed %d times)", m, resumes)
	}
	if _, m := e.post("/api/settings", map[string]any{"Out": other}); m["ok"] != true || e.d.cfg.effectiveOut() != other {
		t.Fatalf("positive twin: Switch to another archive while a delete runs: %v", m)
	}
	_, p := e.post("/api/pause-archive", map[string]any{"path": dir})
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.NoSideEffect(func() bool { return p["ok"] == false && !seal.IsSealed(dir) }))
	_, s := e.post("/api/settings", map[string]any{"Out": dir})
	assure.Refused(t, http.StatusOK, replyErr(s), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.NoSideEffect(func() bool { return s["ok"] == false && e.d.cfg.effectiveOut() == other }))
	close(release)
	if r := <-done; r["status"] != "complete" {
		t.Fatalf("the delete of %s: %v", dir, r)
	}

	// Resume while a delete of a paused archive runs.
	release, done = blockedDelete(t, e, paused)
	_, r := e.post("/api/resume-archive", map[string]any{"path": paused})
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names(deletingRefusal),
		assure.NoSideEffect(func() bool { return r["ok"] == false && seal.IsSealed(paused) && resumes == 1 }))
	close(release)
	if r := <-done; r["status"] != "complete" {
		t.Fatalf("the delete of %s: %v", paused, r)
	}
}

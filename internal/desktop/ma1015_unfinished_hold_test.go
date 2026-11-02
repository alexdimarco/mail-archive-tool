package desktop

import (
	"net/http"
	"os"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

// covers: MA-1015, R36
// Pause, Resume and a Switch (Save settings naming the folder), posted from a tab drawn before a
// delete (its row still shows those buttons), are refused for a folder whose delete did not
// finish, in MA-987's words, nothing changed (the 1.5.3 friction review's H2): Pause writes no
// marker into the half-deleted folder, Resume leaves it paused and puts back no scheduled backup
// (wizard.ResumeArchive is never called), a Switch leaves the current archive as it was, and a
// save that keeps the folder current saves nothing. Positive twins first: each works on another
// archive while that delete is not finished.
func TestPauseResumeAndSwitchOfAnArchiveWhoseDeleteDidNotFinishAreRefused(t *testing.T) {
	e := newDeleteEnv(t)
	resumes, origResume := 0, resumeArchiveRun
	t.Cleanup(func() { resumeArchiveRun = origResume })
	resumeArchiveRun = func(p string) error { resumes++; return origResume(p) }
	other := e.plainArchive("Other")
	stopped := e.plainArchive("Stopped")
	stopAt2(t, e, stopped)
	parked := e.plainArchive("Parked")
	if err := seal.Seal(parked, "parked"); err != nil {
		t.Fatal(err)
	}
	stopAt2(t, e, parked)

	// Positive twins: another archive is paused, resumed and switched to.
	if _, m := e.post("/api/pause-archive", map[string]any{"path": other}); m["ok"] != true || !seal.IsSealed(other) {
		t.Fatalf("positive twin: Pause of another archive: %v", m)
	}
	if _, m := e.post("/api/resume-archive", map[string]any{"path": other}); m["ok"] != true || seal.IsSealed(other) || resumes != 1 {
		t.Fatalf("positive twin: Resume of another archive: %v (resumed %d times)", m, resumes)
	}
	if _, m := e.post("/api/settings", map[string]any{"Out": other}); m["ok"] != true || e.d.cfg.effectiveOut() != other {
		t.Fatalf("positive twin: Switch to another archive: %v", m)
	}

	_, p := e.post("/api/pause-archive", map[string]any{"path": stopped})
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK),
		assure.Names("Delete not finished: click Delete again", "it was not paused", "nothing was changed"),
		assure.NoSideEffect(func() bool { return p["ok"] == false && !seal.IsSealed(stopped) }))

	_, r := e.post("/api/resume-archive", map[string]any{"path": parked})
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK),
		assure.Names("Delete not finished: click Delete again", "it was not resumed", "nothing was changed"),
		assure.NoSideEffect(func() bool { return r["ok"] == false && seal.IsSealed(parked) && resumes == 1 }))

	_, s := e.post("/api/settings", map[string]any{"Out": stopped})
	assure.Refused(t, http.StatusOK, replyErr(s), assure.Code(http.StatusOK),
		assure.Names("Delete not finished: click Delete again", "it was not made the current archive", "nothing was changed"),
		assure.NoSideEffect(func() bool { return s["ok"] == false && e.d.cfg.effectiveOut() == other }))

	// A delete stopped at step 2 of the current archive leaves it current: a save that keeps it
	// saves nothing.
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: stopped}); err != nil {
		t.Fatal(err)
	}
	_, k := e.post("/api/settings", map[string]any{"Out": stopped, "KeepRaw": true})
	assure.Refused(t, http.StatusOK, replyErr(k), assure.Code(http.StatusOK),
		assure.Names("Delete not finished: click Delete again", "these settings were not saved"),
		assure.NoSideEffect(func() bool { return k["ok"] == false && !LoadSettings(e.cfg.SettingsPath).KeepRaw }))
}

// covers: MA-1015, R36
// A deletions.log that cannot be read refuses Pause, Resume and a Switch, naming the log, since
// whether the folder's delete finished cannot be told (as MA-987's runs); nothing changed.
// Positive twin first: with the log readable, the same Pause and Switch work.
func TestPauseAndSwitchRefuseADeletionLogTheyCannotRead(t *testing.T) {
	e := newDeleteEnv(t)
	a := e.plainArchive("A")
	b := e.plainArchive("B")
	if _, m := e.post("/api/pause-archive", map[string]any{"path": a}); m["ok"] != true || !seal.IsSealed(a) {
		t.Fatalf("positive twin: Pause with a readable log: %v", m)
	}
	if _, m := e.post("/api/settings", map[string]any{"Out": a}); m["ok"] != true || e.d.cfg.effectiveOut() != a {
		t.Fatalf("positive twin: Switch with a readable log: %v", m)
	}

	logPath := e.cfg.deletionsLogPath()
	_ = os.Remove(logPath)
	if err := os.MkdirAll(logPath, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	_, p := e.post("/api/pause-archive", map[string]any{"path": b})
	assure.Refused(t, http.StatusOK, replyErr(p), assure.Code(http.StatusOK),
		assure.Names("could not read its deletion log", logPath, "nothing was changed"),
		assure.NoSideEffect(func() bool { return p["ok"] == false && !seal.IsSealed(b) }))
	_, r := e.post("/api/resume-archive", map[string]any{"path": a})
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK),
		assure.Names("could not read its deletion log", logPath),
		assure.NoSideEffect(func() bool { return r["ok"] == false && seal.IsSealed(a) }))
	_, s := e.post("/api/settings", map[string]any{"Out": b})
	assure.Refused(t, http.StatusOK, replyErr(s), assure.Code(http.StatusOK),
		assure.Names("could not read its deletion log", logPath),
		assure.NoSideEffect(func() bool { return s["ok"] == false && e.d.cfg.effectiveOut() == a }))
}

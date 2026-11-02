package desktop

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1007, R36
// An uninstall's Remove that stops after it began removing keeps deletes blocked (the delete
// residuals' security pass, CON-06): uninstall.Uninstall returns its report with the error only
// once it has begun removing, and deletions.log or settings may be gone, so Delete permanently and
// Remove from list's Yes are refused with uninstallUnfinishedText, the archive, the list, the
// settings and the log untouched. A Remove from a fresh plan may run again to finish the uninstall,
// and one refused before it removed anything (its plan changed) puts that block back, never "".
// Positive twin first: a Remove refused before it removed anything (no report) lifts its block,
// and a delete then goes ahead.
func TestAnUninstallThatStoppedPartWayKeepsDeletesBlocked(t *testing.T) {
	e := newDeleteEnv(t)
	// realRun is what the real run returns: (nil, err) refused before removing anything,
	// (report, err) stopped part way, or nil for a run that finishes.
	var realRun func(rep *uninstall.Report) (*uninstall.Report, error)
	realRuns := 0
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		rep := &uninstall.Report{ConfigFiles: []string{filepath.Join(configDir, "desktop-settings.json")}, KeptArchives: known}
		if dryRun || realRun == nil {
			return rep, nil
		}
		realRuns++
		return realRun(rep)
	}
	shown := func() map[string]any {
		_, m := e.post("/api/uninstall", uninstallBody(false, nil))
		if m["ok"] != true {
			t.Fatalf("the uninstall plan: %v", m)
		}
		return m
	}

	// Positive twin: refused before removing anything, the block is lifted; a delete goes ahead.
	realRun = func(*uninstall.Report) (*uninstall.Report, error) {
		return nil, errors.New("a download is under way, so uninstall stopped before removing anything")
	}
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown())); m["ok"] != false {
		t.Fatalf("positive twin: a Remove refused before removing anything: %v", m)
	}
	if why := e.d.running.blockedWhy(); why != "" {
		t.Fatalf("positive twin: a Remove that removed nothing left deletes blocked: %q", why)
	}
	if r := e.planAndDelete(e.plainArchive("Before")); r["ok"] != true {
		t.Fatalf("positive twin: a delete after a Remove that removed nothing: %v", r)
	}

	// Stopped part way: the schedules and the sign-in went, a settings file could not be removed.
	realRun = func(rep *uninstall.Report) (*uninstall.Report, error) {
		return rep, errors.New("could not remove desktop-capture.log: permission denied")
	}
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown())); m["ok"] != false {
		t.Fatalf("a Remove that stopped part way: %v", m)
	}
	dir := e.plainArchive("After")
	logBefore := deletionsLog(t, e.cfg)
	settingsBefore, _ := os.ReadFile(e.cfg.SettingsPath)
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan of %s: %v", dir, p)
	}
	_, del := e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	assure.Refused(t, http.StatusOK, replyErr(del), assure.Code(http.StatusOK),
		assure.Names("did not finish", "Run Uninstall again", "nothing was deleted"),
		assure.NoSideEffect(func() bool {
			settingsNow, _ := os.ReadFile(e.cfg.SettingsPath)
			return del["ok"] == false && archdelete.IsArchive(dir) && e.listed(dir) &&
				deletionsLog(t, e.cfg) == logBefore && string(settingsNow) == string(settingsBefore)
		}))
	_, un := e.post("/api/archive/unlist", map[string]any{"path": dir, "confirm": true, "named": []string{}})
	assure.Refused(t, http.StatusOK, replyErr(un), assure.Code(http.StatusOK), assure.Names(uninstallUnfinishedText),
		assure.NoSideEffect(func() bool { return un["ok"] == false && e.listed(dir) }))

	// A Remove refused for a changed plan (none sent) puts the block back; it is never lifted.
	runsBefore := realRuns
	if _, m := e.post("/api/uninstall", map[string]any{"confirm": true}); m["planChanged"] != true {
		t.Fatalf("a Remove sending no plan: %v", m)
	}
	if why := e.d.running.blockedWhy(); why != uninstallUnfinishedText || realRuns != runsBefore {
		t.Fatalf("after a Remove refused for its plan: blocked %q, real runs %d (want %d)", why, realRuns, runsBefore)
	}
	// Running Uninstall again finishes it: the block becomes "was uninstalled".
	realRun = func(rep *uninstall.Report) (*uninstall.Report, error) { return rep, nil }
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown())); m["ok"] != true {
		t.Fatalf("running Uninstall again: %v", m)
	}
	if why := e.d.running.blockedWhy(); why != uninstalledText {
		t.Errorf("after the uninstall finished: blocked %q", why)
	}
}

// covers: MA-1007, R36
// Remove from list's Yes holds against an uninstall and a delete for the whole of its work (the
// delete residuals' security pass, CON-06): an uninstall's Remove clicked while it stops the
// row's schedule is refused, naming Remove from list, with nothing removed and no real run; and
// Delete permanently of the row then is refused, the archive untouched. Once it ends, the row is
// gone and an uninstall's Remove goes ahead. Positive twin first: the uninstall's plan (which
// removes nothing) is still shown while Remove from list runs.
func TestRemoveFromListHoldsAgainstAnUninstall(t *testing.T) {
	e := newDeleteEnv(t)
	realRuns := 0
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		if !dryRun {
			realRuns++
		}
		return &uninstall.Report{ConfigFiles: []string{filepath.Join(configDir, "desktop-settings.json")}, KeptArchives: known}, nil
	}
	dir := e.plainArchive("Scheduled")
	name := schedule.DefaultNameFor(dir)
	plantRunJob(t, dir)
	e.sched.installed[name] = true
	// The plan as shown while the row is listed: the Remove clicked during Remove from list sends it.
	_, shown := e.post("/api/uninstall", uninstallBody(false, nil))
	if shown["ok"] != true {
		t.Fatalf("the uninstall plan: %v", shown)
	}
	_, q := e.post("/api/archive/unlist", map[string]any{"path": dir})
	if q["ok"] != true {
		t.Fatalf("the question: %v", q)
	}
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan of %s: %v", dir, p)
	}
	var plan, remove, del map[string]any
	e.sched.onRemove = func(string) {
		e.sched.onRemove = nil
		_, plan = e.post("/api/uninstall", uninstallBody(false, nil))
		_, remove = e.post("/api/uninstall", uninstallBody(true, shown))
		_, del = e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	}
	_, done := e.post("/api/archive/unlist", map[string]any{"path": dir, "confirm": true, "named": q["named"]})
	if done["ok"] != true || e.listed(dir) {
		t.Fatalf("Remove from list: %v (listed %v)", done, e.listed(dir))
	}
	if plan["ok"] != true {
		t.Fatalf("positive twin: the uninstall plan while Remove from list runs: %v", plan)
	}
	assure.Refused(t, http.StatusOK, replyErr(remove), assure.Code(http.StatusOK),
		assure.Names("Remove from list is running", dir, "Nothing was removed"),
		assure.NoSideEffect(func() bool { return remove["ok"] == false && realRuns == 0 && e.d.running.blockedWhy() == "" }))
	assure.Refused(t, http.StatusOK, replyErr(del), assure.Code(http.StatusOK), assure.Names(unlistingRefusal),
		assure.NoSideEffect(func() bool { return del["ok"] == false && archdelete.IsArchive(dir) }))

	// Once it has ended, nothing holds the uninstall (its plan read again: the row is gone).
	_, shown = e.post("/api/uninstall", uninstallBody(false, nil))
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown)); m["ok"] != true || realRuns != 1 {
		t.Fatalf("the uninstall's Remove after Remove from list: %v (real runs %d)", m, realRuns)
	}
}

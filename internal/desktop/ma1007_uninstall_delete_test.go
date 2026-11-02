package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1007, R36
// On the Desktop an uninstall and a delete never overlap (del-s5-A2-02): while a delete is under
// way, Uninstall's plan and its Remove are refused naming that delete, and nothing is removed;
// while Remove runs, Delete permanently is refused with uninstallingText and the archive is
// untouched; once Remove has removed the settings, a delete stays refused (it would write
// deletions.log and the settings again) with uninstalledText. Positive twin first: with no
// delete under way, the plan is shown, and before any uninstall a delete goes ahead.
func TestTheDesktopNeverRunsAnUninstallAndADeleteTogether(t *testing.T) {
	e := newDeleteEnv(t)
	calls := 0
	var whileRemoving func()
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		calls++
		if !dryRun && whileRemoving != nil {
			whileRemoving()
		}
		return &uninstall.Report{ConfigFiles: []string{filepath.Join(configDir, "desktop-settings.json")}, KeptArchives: known}, nil
	}

	// Positive twins: the plan is shown; a delete goes ahead.
	_, shown := e.post("/api/uninstall", uninstallBody(false, nil))
	if shown["ok"] != true {
		t.Fatalf("positive twin: the uninstall plan: %v", shown)
	}
	if r := e.planAndDelete(e.plainArchive("First")); r["ok"] != true {
		t.Fatalf("positive twin: a delete before any uninstall: %v", r)
	}

	// A delete under way refuses the plan and the removal; nothing is asked of uninstall.
	busy := e.plainArchive("Busy")
	end, ok := e.d.running.tryBegin(busy, busy)
	if !ok {
		t.Fatal("could not stand in for a delete under way")
	}
	callsBefore := calls
	for _, confirm := range []bool{false, true} {
		code, m := e.post("/api/uninstall", uninstallBody(confirm, shown))
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names("A delete is running", busy, "Nothing was removed"),
			assure.NoSideEffect(func() bool { return m["ok"] == false && calls == callsBefore }))
	}
	end()

	// While Remove runs, Delete permanently of a plan made meanwhile (another tab) is refused,
	// the archive untouched. (Plans made before Remove are already spent by it: discardFor.)
	during := e.plainArchive("During")
	var mid map[string]any
	whileRemoving = func() {
		p := e.plan(during)
		if p["ok"] != true {
			t.Fatalf("the plan of %s: %v", during, p)
		}
		_, mid = e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	}
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown)); m["ok"] != true {
		t.Fatalf("the removal: %v", m)
	}
	whileRemoving = nil
	assure.Refused(t, http.StatusOK, replyErr(mid), assure.Code(http.StatusOK), assure.Names(uninstallingText),
		assure.NoSideEffect(func() bool { return mid["ok"] == false && archdelete.IsArchive(during) && e.listed(during) }))

	// After it, a delete stays refused: the settings it would write are gone.
	p := e.plan(during)
	if p["ok"] != true {
		t.Fatalf("the plan of %s after the uninstall: %v", during, p)
	}
	_, after := e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	assure.Refused(t, http.StatusOK, replyErr(after), assure.Code(http.StatusOK), assure.Names(uninstalledText),
		assure.NoSideEffect(func() bool { return after["ok"] == false && archdelete.IsArchive(during) }))
	// Remove from list's Yes writes the list and the settings too: refused the same way.
	_, un := e.post("/api/archive/unlist", map[string]any{"path": during, "confirm": true, "named": []string{}})
	assure.Refused(t, http.StatusOK, replyErr(un), assure.Code(http.StatusOK), assure.Names(uninstalledText),
		assure.NoSideEffect(func() bool { return un["ok"] == false && e.listed(during) }))
}

// covers: MA-1007, R36
// A Remove that removes nothing never lifts the block another Remove holds (del-s5
// re-verification, residual 1): a second Remove while one runs is refused "MailArchive is being
// uninstalled…", nothing removed, the first's block kept; and once an uninstall has removed the
// settings, a Remove from a tab still showing the old plan is refused "MailArchive was
// uninstalled…", and a delete after it stays refused, writing neither deletions.log nor the
// settings. Positive twin first: a Remove refused because its plan changed lifts its own block,
// and a delete then goes ahead.
func TestAStaleOrSecondRemoveNeverLiftsTheUninstallBlock(t *testing.T) {
	e := newDeleteEnv(t)
	removed, realRuns := false, 0
	var whileRemoving func()
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		if removed {
			return &uninstall.Report{KeptArchives: known}, nil // the settings are gone: an empty plan
		}
		if !dryRun {
			realRuns++
			if f := whileRemoving; f != nil {
				whileRemoving = nil
				f()
			}
			removed = true
		}
		return &uninstall.Report{ConfigFiles: []string{filepath.Join(configDir, "desktop-settings.json")}, KeptArchives: known}, nil
	}

	// Positive twin: a Remove refused because its plan changed (none was sent) lifts its own block.
	if _, m := e.post("/api/uninstall", map[string]any{"confirm": true}); m["planChanged"] != true {
		t.Fatalf("positive twin: a Remove sending no plan: %v", m)
	}
	if r := e.planAndDelete(e.plainArchive("Before")); r["ok"] != true {
		t.Fatalf("positive twin: a delete after a refused Remove: %v", r)
	}

	// A second Remove while the first runs is refused and keeps the first's block.
	_, shown := e.post("/api/uninstall", uninstallBody(false, nil))
	var second map[string]any
	var blockedAfterSecond string
	whileRemoving = func() {
		_, second = e.post("/api/uninstall", uninstallBody(true, shown))
		blockedAfterSecond = e.d.running.blockedWhy()
	}
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown)); m["ok"] != true {
		t.Fatalf("the first Remove: %v", m)
	}
	assure.Refused(t, http.StatusOK, replyErr(second), assure.Code(http.StatusOK), assure.Names(uninstallingText),
		assure.NoSideEffect(func() bool {
			return second["ok"] == false && realRuns == 1 && blockedAfterSecond == uninstallingText
		}))

	// A Remove from a tab still showing the old plan, after the uninstall, is refused and keeps
	// deletes blocked: a delete then writes neither deletions.log nor the settings.
	logBefore := deletionsLog(t, e.cfg)
	settingsBefore, _ := os.ReadFile(e.cfg.SettingsPath)
	code, stale := e.post("/api/uninstall", uninstallBody(true, shown))
	assure.Refused(t, code, replyErr(stale), assure.Code(http.StatusOK), assure.Names(uninstalledText),
		assure.NoSideEffect(func() bool {
			return stale["ok"] == false && realRuns == 1 && e.d.running.blockedWhy() == uninstalledText
		}))
	dir := e.plainArchive("After")
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan of %s after the uninstall: %v", dir, p)
	}
	_, after := e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	assure.Refused(t, http.StatusOK, replyErr(after), assure.Code(http.StatusOK), assure.Names(uninstalledText),
		assure.NoSideEffect(func() bool {
			settingsNow, _ := os.ReadFile(e.cfg.SettingsPath)
			return after["ok"] == false && archdelete.IsArchive(dir) &&
				deletionsLog(t, e.cfg) == logBefore && string(settingsNow) == string(settingsBefore)
		}))
	// Lifting a refused or stopped Remove's block never lifts "uninstalled".
	e.d.running.unblock()
	if why := e.d.running.blockedWhy(); why != uninstalledText {
		t.Errorf("unblock lifted the uninstalled block: now %q", why)
	}
}

// uninstallBody is the uninstall route's body: the plan, or the removal of the plan shown.
func uninstallBody(confirm bool, shown map[string]any) map[string]any {
	b := map[string]any{"confirm": confirm}
	if confirm && shown != nil {
		b["shown"] = shown
	}
	return b
}

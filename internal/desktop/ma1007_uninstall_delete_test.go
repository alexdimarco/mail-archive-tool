package desktop

import (
	"net/http"
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

// uninstallBody is the uninstall route's body: the plan, or the removal of the plan shown.
func uninstallBody(confirm bool, shown map[string]any) map[string]any {
	b := map[string]any{"confirm": confirm}
	if confirm && shown != nil {
		b["shown"] = shown
	}
	return b
}

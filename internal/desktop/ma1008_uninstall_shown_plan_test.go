package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1008, R36
// The Desktop's Remove removes the uninstall plan the page showed, or nothing (del-s5-A2-03;
// design-archive-delete §8.1, O2-07): when a delete in another tab has made deletions.log since
// the plan was shown, Remove is refused with planChanged, the new plan (naming the log and
// saying to copy it first) and nothing removed; a Remove that sends no plan is refused the same
// way; the new plan sent back goes ahead. The page keeps the plan it shows and sends it with
// Remove, and shows the new one when told it changed. Positive twin first: the plan shown,
// unchanged, is removed.
func TestRemoveRemovesOnlyTheUninstallPlanThatWasShown(t *testing.T) {
	e := newDeleteEnv(t)
	removals := 0
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		targets, err := uninstall.ConfigTargets(configDir, known)
		if err != nil {
			return nil, err
		}
		rep := &uninstall.Report{ConfigFiles: targets, KeptArchives: known}
		for _, f := range targets {
			if filepath.Base(f) == archdelete.LogName {
				rep.DeletionsLog = f
			}
		}
		if !dryRun {
			removals++ // stands in for the removal: nothing on disk is touched here
		}
		return rep, nil
	}
	cfgLog := filepath.Join(filepath.Dir(e.cfg.ConfigPath), archdelete.LogName)

	// Positive twin: the plan shown, unchanged, is removed.
	_, shown := e.post("/api/uninstall", uninstallBody(false, nil))
	if shown["ok"] != true || shown["deletionsLog"] != nil {
		t.Fatalf("positive twin: a plan with no deletion log: %v", shown)
	}
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown)); m["ok"] != true || m["done"] != true || removals != 1 {
		t.Fatalf("positive twin: Remove of the plan shown: %v (removals %d)", m, removals)
	}
	// The stand-in removed nothing: let this Desktop delete and uninstall again, as after a fresh
	// install (unblock lifts only a Remove's own block, never "uninstalled": MA-1007).
	e.d.running.mu.Lock()
	e.d.running.blocked = ""
	e.d.running.mu.Unlock()

	// A delete in another tab makes deletions.log after the plan was shown.
	_, shown = e.post("/api/uninstall", uninstallBody(false, nil))
	if r := e.planAndDelete(e.plainArchive("Elsewhere")); r["ok"] != true {
		t.Fatalf("the other tab's delete: %v", r)
	}
	if _, err := os.Stat(cfgLog); err != nil {
		t.Fatalf("the delete made no deletions.log at %s: %v", cfgLog, err)
	}
	for _, body := range []map[string]any{uninstallBody(true, shown), {"confirm": true}} {
		code, m := e.post("/api/uninstall", body)
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names("has changed since the plan was shown", "nothing was removed", "Read the plan again"),
			assure.NoSideEffect(func() bool { return m["ok"] == false && removals == 1 && fileThere(cfgLog) }))
		if m["planChanged"] != true || m["deletionsLog"] != cfgLog || m["deletionsLogNote"] != uninstall.DeletionsLogNote {
			t.Fatalf("the refusal must carry the new plan, naming the log and the sentence: %v", m)
		}
		shown = m
	}
	// The new plan, read, goes ahead.
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown)); m["ok"] != true || removals != 2 {
		t.Fatalf("Remove of the new plan shown: %v (removals %d)", m, removals)
	}

	// The page sends the plan it shows, and shows the new one when told it changed.
	for _, want := range []string{"uninstallReq(true,unShown)", "unShown=d;", "d.planChanged"} {
		if !strings.Contains(dashboardJS, want) {
			t.Errorf("the page script lacks %q", want)
		}
	}
}

func fileThere(p string) bool { _, err := os.Stat(p); return err == nil }

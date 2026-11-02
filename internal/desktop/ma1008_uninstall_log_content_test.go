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

// covers: MA-1008, R36
// The Desktop's Remove compares what is in deletions.log, not only its name, with the plan the
// page showed (the 1.5.3 re-walk's adversarial pass, F5): a delete that finished elsewhere after the
// preview — `delete-archive -config` pointed at this Desktop's settings, another account's terminal
// — writes lines the person never saw, and Remove took them with the log, unasked. The plan reply
// now carries the log's size and SHA-256 (deletionsLogSize, deletionsLogSHA256), the page sends
// them back with Remove as it sends the whole plan, and a log that changed since — lines added, or
// the same size with other bytes — refuses with MA-1008's "has changed since the plan was shown"
// words and the new plan, nothing removed; the new plan sent back goes ahead. Positive twin first:
// the plan shown, its log unchanged, is removed.
func TestRemoveRefusesADeletionLogThatChangedSinceThePlan(t *testing.T) {
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
	unblock := func() { // as after a fresh install: the stand-in removed nothing (MA-1008's test)
		e.d.running.mu.Lock()
		e.d.running.blocked = ""
		e.d.running.mu.Unlock()
	}
	cfgLog := filepath.Join(filepath.Dir(e.cfg.ConfigPath), archdelete.LogName)
	first := `{"at":"2026-11-02T09:00:00Z","event":"started","run":"r1","folder":"/x/Old mail"}` + "\n" +
		`{"at":"2026-11-02T09:00:05Z","event":"finished","run":"r1","folder":"/x/Old mail","outcome":"complete"}` + "\n"
	if err := os.WriteFile(cfgLog, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}

	// Positive twin: the plan shown, its log unchanged, is removed.
	_, shown := e.post("/api/uninstall", uninstallBody(false, nil))
	if shown["ok"] != true || shown["deletionsLog"] != cfgLog || shown["deletionsLogSHA256"] == nil {
		t.Fatalf("positive twin: a plan naming the log and its content: %v", shown)
	}
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown)); m["ok"] != true || m["done"] != true || removals != 1 {
		t.Fatalf("positive twin: Remove of the plan shown: %v (removals %d)", m, removals)
	}
	unblock()

	// A delete finishes elsewhere after the preview: two lines added to the log.
	_, shown = e.post("/api/uninstall", uninstallBody(false, nil))
	second := `{"at":"2026-11-02T10:00:00Z","event":"started","run":"r2","folder":"/x/Other mail"}` + "\n" +
		`{"at":"2026-11-02T10:00:05Z","event":"finished","run":"r2","folder":"/x/Other mail","outcome":"complete"}` + "\n"
	if err := os.WriteFile(cfgLog, []byte(first+second), 0o600); err != nil {
		t.Fatal(err)
	}
	code, m := e.post("/api/uninstall", uninstallBody(true, shown))
	assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK),
		assure.Names("What uninstall would remove has changed since the plan was shown, so nothing was removed. Read the plan again below, then click Remove."),
		assure.NoSideEffect(func() bool { return m["ok"] == false && m["planChanged"] == true && removals == 1 && fileThere(cfgLog) }))
	unblock()

	// The same size with other bytes is a change too.
	_, shown = e.post("/api/uninstall", uninstallBody(false, nil))
	same := []byte(first + second)
	same[len(same)-3] = 'X'
	if err := os.WriteFile(cfgLog, same, 0o600); err != nil {
		t.Fatal(err)
	}
	code, m = e.post("/api/uninstall", uninstallBody(true, shown))
	assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK),
		assure.Names("has changed since the plan was shown", "nothing was removed"),
		assure.NoSideEffect(func() bool { return m["ok"] == false && m["planChanged"] == true && removals == 1 }))
	unblock()

	// The new plan, read, goes ahead.
	if _, ok := e.post("/api/uninstall", uninstallBody(true, m)); ok["ok"] != true || removals != 2 {
		t.Fatalf("Remove of the new plan shown: %v (removals %d)", ok, removals)
	}
}

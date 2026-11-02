package desktop

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1008, R36
// The Desktop's uninstall plan says what is in deletions.log, not only where it is, and a Remove
// refused because the log changed says how (the 1.5.3 re-walk's guards pass, G5): before, the plan
// named only the log's path, so a plan refused for new lines read the same as the one shown,
// Remove stayed enabled, and a second click removed the new lines unseen. Now the plan reply
// carries the log's line count and when its last line was written (deletionsLogLines,
// deletionsLogLast), the panel shows "It holds N lines, the last written DATE." under the log,
// and the refusal adds "The deletion log changed since the plan was shown: it now holds N lines,
// the last written DATE. Copy it again first if you keep it.", nothing removed. Positive twins
// first: the plan of an unchanged log is removed; a plan refused for another change has no
// sentence about the log.
func TestUninstallPlanSaysWhatTheDeletionLogHolds(t *testing.T) {
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
	unblock := func() {
		e.d.running.mu.Lock()
		e.d.running.blocked = ""
		e.d.running.mu.Unlock()
	}
	when := func(s string) string {
		tm, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm.Local().Format("2006-01-02 15:04:05 -07:00")
	}
	cfgLog := filepath.Join(filepath.Dir(e.cfg.ConfigPath), archdelete.LogName)
	first := `{"at":"2026-11-02T09:00:00Z","event":"started","run":"r1","folder":"/x/Old mail"}` + "\n" +
		`{"at":"2026-11-02T09:00:05Z","event":"finished","run":"r1","folder":"/x/Old mail","outcome":"complete"}` + "\n"
	if err := os.WriteFile(cfgLog, []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}

	// Positive twin: the plan says what the log holds; unchanged, it is removed.
	_, shown := e.post("/api/uninstall", uninstallBody(false, nil))
	if shown["deletionsLogLines"] != float64(2) || shown["deletionsLogLast"] != when("2026-11-02T09:00:05Z") {
		t.Fatalf("the plan must say what the log holds: %v", shown)
	}
	body, _ := json.Marshal(shown)
	if text := uninstallPlanLines(t, body); !strings.Contains(text, "- the deletion log: "+cfgLog+"\n     Copy deletions.log first") ||
		!strings.Contains(text, "\n     It holds 2 lines, the last written "+when("2026-11-02T09:00:05Z")+".") {
		t.Fatalf("the panel must say what the log holds:\n%s", text)
	}
	if _, m := e.post("/api/uninstall", uninstallBody(true, shown)); m["ok"] != true || removals != 1 {
		t.Fatalf("positive twin: Remove of the plan shown: %v (removals %d)", m, removals)
	}
	unblock()

	// Positive twin: a plan refused for another change says nothing of the log.
	_, shown = e.post("/api/uninstall", uninstallBody(false, nil))
	other := map[string]any{}
	for k, v := range shown {
		other[k] = v
	}
	other["schedules"] = []string{"mailarchive-gone"}
	if _, m := e.post("/api/uninstall", uninstallBody(true, other)); m["planChanged"] != true || m["error"] != uninstallPlanChanged {
		t.Fatalf("positive twin: another change is refused with MA-1008's words alone: %v", m)
	}
	unblock()

	// A delete finishes elsewhere after the preview: the refusal says the log changed and how.
	_, shown = e.post("/api/uninstall", uninstallBody(false, nil))
	second := `{"at":"2026-11-02T10:00:00Z","event":"started","run":"r2","folder":"/x/Other mail"}` + "\n" +
		`{"at":"2026-11-02T10:00:05Z","event":"finished","run":"r2","folder":"/x/Other mail","outcome":"complete"}` + "\n"
	if err := os.WriteFile(cfgLog, []byte(first+second), 0o600); err != nil {
		t.Fatal(err)
	}
	code, m := e.post("/api/uninstall", uninstallBody(true, shown))
	assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK),
		assure.Names(uninstallPlanChanged+" The deletion log changed since the plan was shown: it now holds 4 lines, the last written "+when("2026-11-02T10:00:05Z")+". Copy it again first if you keep it."),
		assure.NoSideEffect(func() bool { return m["ok"] == false && m["planChanged"] == true && removals == 1 && fileThere(cfgLog) }))
	body, _ = json.Marshal(m)
	if text := uninstallPlanLines(t, body); !strings.Contains(text, "It holds 4 lines, the last written "+when("2026-11-02T10:00:05Z")+".") {
		t.Fatalf("the new plan must say what the log holds now:\n%s", text)
	}
}

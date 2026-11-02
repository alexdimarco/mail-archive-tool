package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1003, R36
// A Desktop started with -config keeps deletions.log in that settings folder, the one its
// uninstall plans and removes (design-archive-delete §5.4 "in the config folder", §8.1;
// del-s5-A1-04): a delete there writes its lines beside the config file, never in job.Dir(),
// and the uninstall plan of that folder (uninstall.ConfigTargets, which the real Uninstall
// reads) names the log. Positive twin first: before any delete there is no log in either
// folder and the plan names none.
func TestADesktopStartedWithConfigKeepsItsDeletionLogWhereItsUninstallLooks(t *testing.T) {
	e := newDeleteEnv(t)
	cfgDir := filepath.Dir(e.cfg.ConfigPath)
	defaultLog, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(defaultLog) == cfgDir {
		t.Fatalf("the Desktop's -config folder %s must not be job.Dir(), or this test proves nothing", cfgDir)
	}
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		if !dryRun {
			t.Fatal("the plan only: nothing is removed here")
		}
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
		return rep, nil
	}
	planLog := func() string {
		t.Helper()
		_, m := e.post("/api/uninstall", map[string]any{"confirm": false})
		if m["ok"] != true {
			t.Fatalf("the uninstall plan: %v", m)
		}
		s, _ := m["deletionsLog"].(string)
		return s
	}
	cfgLog := filepath.Join(cfgDir, archdelete.LogName)
	dir := e.plainArchive("Retired")
	defaultBefore, _ := os.ReadFile(defaultLog)

	// Positive twin: no delete yet, no log beside the config file, none named.
	if _, err := os.Stat(cfgLog); !os.IsNotExist(err) {
		t.Fatalf("positive twin: a log before any delete: %v", err)
	}
	if got := planLog(); got != "" {
		t.Fatalf("positive twin: the plan names a log before any delete: %q", got)
	}

	if r := e.planAndDelete(dir); r["ok"] != true {
		t.Fatalf("the delete: %v", r)
	}
	b, err := os.ReadFile(cfgLog)
	if err != nil || !strings.Contains(string(b), `"event":"started"`) || !strings.Contains(string(b), jsonString(t, dir)) {
		t.Fatalf("the delete's lines must be in the -config folder's deletions.log %s: %v %q", cfgLog, err, b)
	}
	if after, _ := os.ReadFile(defaultLog); strings.Contains(string(after), jsonString(t, dir)) || len(after) != len(defaultBefore) {
		t.Fatalf("a -config Desktop's delete wrote job.Dir()'s deletions.log %s: %q", defaultLog, after)
	}
	if got := planLog(); got != cfgLog {
		t.Fatalf("the uninstall plan must name the log its Desktop wrote, %s; got %q", cfgLog, got)
	}
	// The rows read the same file: a delete left unfinished there marks its row. The mark is
	// looked for as the row draws it (amended 2026-11-02: since MA-1032 the guide on the same page
	// says "Delete not finished: click Delete again" in bold, so the bare words no longer tell a
	// marked row from the guide).
	const rowMark = `<span class="archive-tag">Delete not finished: click Delete again</span>`
	other := e.plainArchive("Halfway")
	if page := e.page(); strings.Contains(page, rowMark) || !strings.Contains(page, "Switch to this archive</button>") {
		t.Fatal("positive twin: no row is marked not finished yet, and a row offers Switch")
	}
	line, _ := json.Marshal(map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "r-halfway", "folder": other, "row": other, "kind": "plain"})
	f, err := os.OpenFile(cfgLog, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(line, '\n'))
	f.Close()
	if !strings.Contains(e.page(), rowMark) {
		t.Fatalf("a delete left unfinished in %s must mark its row", cfgLog)
	}
}

// jsonString is s as it appears inside a JSON line (quotes and escapes, as deletions.log writes it).
func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

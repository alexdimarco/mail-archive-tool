package desktop

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/uninstall"
)

// uninstallPlanLines runs the page's own uninstallPlanText (the served dashboard.js) under Node
// on the plan route's reply body, and returns the text the panel shows. A computer without Node
// skips, saying so (CI runs it).
func uninstallPlanLines(t *testing.T, reply []byte) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is not installed on this computer, so the page script is not run here (CI runs it): " + err.Error())
	}
	const start = "function uninstallPlanText(d){"
	i := strings.Index(dashboardJS, start)
	if i < 0 {
		t.Fatal("the page script has no uninstallPlanText")
	}
	j := strings.Index(dashboardJS[i:], "\n}\n")
	if j < 0 {
		t.Fatal("uninstallPlanText does not close")
	}
	js := dashboardJS[i:i+j+3] + "process.stdout.write(uninstallPlanText(JSON.parse(require('fs').readFileSync(process.argv[2],'utf8'))));\n"
	dir := t.TempDir()
	script, data := filepath.Join(dir, "plan.js"), filepath.Join(dir, "plan.json")
	if err := os.WriteFile(script, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, reply, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, data).CombinedOutput()
	if err != nil {
		t.Fatalf("uninstallPlanText did not run: %v\n%s", err, out)
	}
	return string(out)
}

// covers: MA-1003, R36
// The Desktop's uninstall plan names the deletion log among what it removes and says to copy
// it first (design-archive-delete §8.1; O2-07): the route carries the log's path and the
// sentence only when there is a log, and the page's panel shows both under "This will remove".
// Positive twin first: with no log the reply keeps exactly the keys it had (MA-739) and the
// panel says nothing of it.
func TestUninstallPlanOnThePageNamesTheDeletionLog(t *testing.T) {
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	var logPath string
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		rep := &uninstall.Report{ConfigFiles: []string{filepath.Join(configDir, "desktop-settings.json")}, KeptArchives: known}
		if logPath != "" {
			rep.ConfigFiles = append(rep.ConfigFiles, logPath)
			rep.DeletionsLog = logPath
		}
		return rep, nil
	}
	cfgDir := t.TempDir()
	d := newDashboard(Config{ConfigPath: filepath.Join(cfgDir, "graph-config.json")})
	plan := func() ([]byte, map[string]json.RawMessage) {
		t.Helper()
		rec := postUninstall(t, d, `{"confirm":false}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return rec.Body.Bytes(), m
	}

	body, m := plan()
	if got, want := strings.Join(keysOf(m), ","), "binaryNote,clearedSignIn,configFiles,done,keptArchives,ok,schedules"; got != want {
		t.Fatalf("positive twin: a plan with no deletion log keeps its keys %s, got %s", want, got)
	}
	if text := uninstallPlanLines(t, body); strings.Contains(text, "deletion") {
		t.Fatalf("positive twin: the panel names no deletion log:\n%s", text)
	}

	logPath = filepath.Join(cfgDir, "deletions.log")
	body, m = plan()
	var gotLog, gotNote string
	json.Unmarshal(m["deletionsLog"], &gotLog)
	json.Unmarshal(m["deletionsLogNote"], &gotNote)
	if gotLog != logPath || gotNote != uninstall.DeletionsLogNote {
		t.Fatalf("the plan must carry the deletion log %q and the sentence, got %q / %q", logPath, gotLog, gotNote)
	}
	text := uninstallPlanLines(t, body)
	want := "- the deletion log: " + logPath + "\n     Copy deletions.log first, somewhere you keep records (your Documents folder, for example), if you need it to check saved deletion records."
	if !strings.Contains(text, want) {
		t.Fatalf("the panel must name the deletion log and say to copy it first (%q):\n%s", want, text)
	}
	if strings.Index(text, want) > strings.Index(text, "It will KEEP") {
		t.Fatalf("the deletion log must be among what is removed:\n%s", text)
	}
}

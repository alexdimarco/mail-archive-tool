package desktop

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/uninstall"
)

// covers: MA-1041, R36, S41
// The Uninstall preview with several deletes not finished says so in the plural, each folder and
// its finishing command on a line of its own, which the note keeps (white-space: pre-line), and the
// disabled Remove's tooltip is plural (the 1.5.3 re-walk's N9). Positive twin first: one delete
// keeps the one-delete tooltip.
func TestTheUninstallPreviewOfSeveralDeletesIsPlural(t *testing.T) {
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	a, b := filepath.Join(t.TempDir(), "Old A"), filepath.Join(t.TempDir(), "Old B")
	folders := []string{a}
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		logPath := filepath.Join(configDir, "deletions.log")
		return &uninstall.Report{ConfigFiles: []string{logPath}, DeletionsLog: logPath}, &uninstall.DeleteNotFinishedError{Log: logPath, Folders: folders}
	}
	d := newDashboard(Config{ConfigPath: filepath.Join(t.TempDir(), "graph-config.json")})
	reply := func() map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal(postUninstall(t, d, `{"confirm":false}`).Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := reply(); m["refuseWaits"] != "Uninstall waits until that delete is finished." {
		t.Fatalf("one delete: %v", m)
	}
	folders = []string{a, b}
	m := reply()
	until, _ := m["refuseUntil"].(string)
	if m["refuseWaits"] != "Uninstall waits until those deletes are finished." || !strings.HasPrefix(until, "Uninstall will refuse until these 2 deletes are finished") ||
		!strings.Contains(until, "\n  - "+a+": ") || !strings.Contains(until, "\n  - "+b+": ") {
		t.Fatalf("two deletes: %v", m)
	}
	isolateHome(t)
	if !strings.Contains(getOverview(t, Config{}), "#uninstallNote{white-space:pre-line") {
		t.Fatal("the uninstall note does not keep its lines")
	}
	if passed := runPageScenario(t, "uninstallWaitsMany"); len(passed) != 1 {
		t.Errorf("the uninstallWaitsMany scenario passed %d of its 1 check: %v", len(passed), passed)
	}
}

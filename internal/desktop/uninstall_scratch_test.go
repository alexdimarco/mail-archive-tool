package desktop

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"mail-archive-tool/internal/uninstall"
)

// covers: MA-739, R37
// The Desktop's uninstall plan lists an encrypted archive's leftover working copies
// ("scratchFiles") only when there are any. The positive twin: a report with Scratch set
// yields scratchFiles naming exactly those paths. With none, the key is absent and the
// plan carries exactly the keys a plaintext-only machine always got, so its output is
// unchanged.
func TestMA739_DesktopPlanListsScratchOnlyWhenAny(t *testing.T) {
	orig := uninstallRun
	t.Cleanup(func() { uninstallRun = orig })
	var scratch []string
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		return &uninstall.Report{
			ConfigFiles:  []string{filepath.Join(configDir, "desktop-settings.json")},
			KeptArchives: known,
			Scratch:      scratch,
		}, nil
	}
	cfgDir := t.TempDir()
	d := newDashboard(Config{ConfigPath: filepath.Join(cfgDir, "graph-config.json")})

	plan := func() map[string]json.RawMessage {
		t.Helper()
		rec := postUninstall(t, d, `{"confirm":false}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	scratch = []string{
		filepath.Join(cfgDir, "scratch", "0123456789abcdef0123456789abcdef", "snap", ".mailarchive-snapshot-1"),
		filepath.Join(cfgDir, "scratch", "0123456789abcdef0123456789abcdef"),
		filepath.Join(cfgDir, "scratch"),
	}
	m := plan()
	var listed []string
	if err := json.Unmarshal(m["scratchFiles"], &listed); err != nil {
		t.Fatalf("a report with leftovers must list them as scratchFiles: %v (plan %v)", err, keysOf(m))
	}
	if !reflect.DeepEqual(listed, scratch) {
		t.Fatalf("scratchFiles = %v, want exactly %v", listed, scratch)
	}

	// No leftovers: no key at all (not null, not []), and nothing else added or dropped.
	scratch = nil
	m = plan()
	if _, ok := m["scratchFiles"]; ok {
		t.Fatalf("a plan with no leftovers must not carry scratchFiles, got %s", m["scratchFiles"])
	}
	want := []string{"binaryNote", "clearedSignIn", "configFiles", "done", "keptArchives", "ok", "schedules"}
	if got := keysOf(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("a plaintext plan's keys = %v, want unchanged %v", got, want)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

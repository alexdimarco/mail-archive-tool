package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/uninstall"
)

// covers: MA-685
// The Desktop uninstall action (the U-C8 Desktop surface): a loopback-only, CSRF-guarded POST
// that previews a full uninstall with {"confirm":false} — naming what would be removed and the
// archives it KEEPS (R36), changing nothing — and performs it with {"confirm":true}. It derives
// the config dir from the Desktop's own ConfigPath and the kept archives from the index, and
// reuses the tested orchestrator (seamed here). A POST without the CSRF token is refused.
func TestMA685_UninstallActionPreviewsAndExecutes(t *testing.T) {
	var gotConfigDir string
	var gotKnown []string
	var gotDry []bool
	orig := uninstallRun
	defer func() { uninstallRun = orig }()
	uninstallRun = func(configDir string, known []string, exe string, dryRun bool) (*uninstall.Report, error) {
		gotConfigDir = configDir
		gotKnown = known
		gotDry = append(gotDry, dryRun)
		return &uninstall.Report{
			Schedules:    []string{"mailarchive-abcd1234"},
			ConfigFiles:  []string{filepath.Join(configDir, "desktop-settings.json")},
			KeptArchives: known,
			BinaryNote:   "drag MailArchive.app to the Trash",
		}, nil
	}

	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "graph-config.json")
	idxPath := filepath.Join(cfgDir, archindex.Name)
	archive := t.TempDir()
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: archive}}}); err != nil {
		t.Fatal(err)
	}
	d := newDashboard(Config{ConfigPath: cfgPath, IndexPath: idxPath})

	// Preview (confirm=false): reports, changes nothing (dryRun true), names the kept archive.
	var out struct {
		OK           bool
		Done         bool
		KeptArchives []string
		Schedules    []string
	}
	rec := postUninstall(t, d, `{"confirm":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Done {
		t.Fatalf("preview must be ok and not done, got %+v", out)
	}
	if len(gotDry) != 1 || gotDry[0] != true {
		t.Fatalf("preview must call uninstall with dryRun=true, got %v", gotDry)
	}
	if gotConfigDir != cfgDir {
		t.Fatalf("configDir = %q, want the dir of ConfigPath %q", gotConfigDir, cfgDir)
	}
	if len(gotKnown) != 1 || gotKnown[0] != archive {
		t.Fatalf("known archives = %v, want [%s]", gotKnown, archive)
	}
	if len(out.KeptArchives) != 1 || out.KeptArchives[0] != archive {
		t.Fatalf("preview must name the kept archive, got %v", out.KeptArchives)
	}

	// Execute (confirm=true): performs it (dryRun false).
	rec = postUninstall(t, d, `{"confirm":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("execute status = %d, body %s", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || !out.Done {
		t.Fatalf("execute must be ok and done, got %+v", out)
	}
	if len(gotDry) != 2 || gotDry[1] != false {
		t.Fatalf("execute must call uninstall with dryRun=false, got %v", gotDry)
	}

	// The Overview renders the Danger-zone uninstall control that drives this endpoint.
	page := d.cfg.overviewFor(d.csrf, d.boot)
	if !strings.Contains(page, `id="uninstallBtn"`) || !strings.Contains(page, "Uninstall MailArchive") {
		t.Fatal("the Overview must render the uninstall control")
	}

	// A POST without the CSRF token is refused by the shared loopback+CSRF guard.
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/uninstall", strings.NewReader(`{"confirm":true}`))
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	rej := httptest.NewRecorder()
	d.uninstallAction(rej, req)
	if rej.Code == http.StatusOK {
		t.Fatalf("a POST without the CSRF token must be refused, got %d", rej.Code)
	}
}

func postUninstall(t *testing.T, d *dashboard, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/uninstall", strings.NewReader(body))
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", d.csrf)
	rec := httptest.NewRecorder()
	d.uninstallAction(rec, req)
	return rec
}

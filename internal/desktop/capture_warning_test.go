package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/state"
)

// covers: MA-375, R12, S45
// OPEN GAP (adversarial pass AC4; escalated, not fixed here): only the scheduled
// HeadlessCapture checks whether the archive holds the desktop's settings folder.
// The dashboard's own "Archive now" runs the same capture with no check, and `status`
// derives config_in_archive from the last run alone, so one click on "Archive now"
// erases the WARN while the settings and sign-in still travel with the archive. The
// design lists P10's desktop surfaces as the Settings POST and HeadlessCapture
// only (MA-375), so covering the interactive run is a decision to take, not a patch:
// refuse it, or warn as the scheduled run does. When the guarantee holds this test
// passes: REMOVE the skip so it becomes a guard.
func TestArchiveNowKeepsTheConfigInArchiveWarning(t *testing.T) {
	t.Skip("open: an interactive Archive now records no config_in_archive warning, so status stops WARNing (R12, MA-375; escalated, AC4)")
	srv := newFakeM365(t)
	archive := t.TempDir()
	cfgDir := filepath.Join(archive, "MailArchive settings")
	cfgPath := filepath.Join(cfgDir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		ConfigPath: cfgPath, Out: archive, SettingsPath: filepath.Join(cfgDir, "settings.json"),
		TokenCachePath: filepath.Join(cfgDir, "tok.json"),
		BaseURL:        srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode",
	}
	h := DashboardHandler(cfg)
	_, csrf := getPage(t, h)
	if rec := postAction(h, "/api/capture", csrf, "127.0.0.1:8097", "http://127.0.0.1:8097"); rec.Code != http.StatusOK {
		t.Fatalf("capture start = %d", rec.Code)
	}
	for i := 0; i < 60; i++ {
		req := httptest.NewRequest("GET", "/api/activity", nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var act map[string]any
		json.Unmarshal(rec.Body.Bytes(), &act)
		if running, _ := act["running"].(bool); !running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	lr, _, _ := state.ReadLastRun(archive)
	if len(lr.Warnings) != 1 || lr.Warnings[0].Code != "config_in_archive" {
		t.Errorf("the interactive capture's last-run record carries %+v, want one config_in_archive", lr.Warnings)
	}
}

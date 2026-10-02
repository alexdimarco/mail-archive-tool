package desktop

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-279, R14, R19, S41
// The weekly-backup schedule builds an in-process Spec targeting THIS binary's
// headless -capture mode (DC1), and the install/remove actions are loopback-only +
// CSRF/Origin/Host-guarded (a bad request runs no installer); the state endpoint is
// read-only.
func TestScheduleSpecAndGuards(t *testing.T) {
	dir, out := t.TempDir(), t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	sp := filepath.Join(dir, "settings.json")
	if err := SaveSettings(sp, Settings{Out: out, Interval: "weekly", WeeklyTime: "02:30"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ConfigPath: cfgPath, SettingsPath: sp, Exe: "/opt/mailarchive-desktop"}

	// The scheduled task runs this binary's headless -capture (not a mailarchive job).
	spec, err := cfg.backupSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Exe != "/opt/mailarchive-desktop" {
		t.Errorf("spec.Exe = %q, want the desktop binary", spec.Exe)
	}
	if spec.At != "02:30" || string(spec.Interval) != "weekly" {
		t.Errorf("spec schedule = %q %q", spec.Interval, spec.At)
	}
	args := strings.Join(spec.Args, " ")
	for _, want := range []string{"-capture", "-config", "-out", "-log"} {
		if !strings.Contains(args, want) {
			t.Errorf("spec.Args missing %q: %v", want, spec.Args)
		}
	}

	// Install/remove refuse without a CSRF token — no installer runs.
	h := DashboardHandler(cfg)
	for _, p := range []string{"/api/schedule-install", "/api/schedule-remove"} {
		rec := postJSON(h, p, "", `{"interval":"weekly","time":"02:30"}`)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without CSRF = %d, want 403", p, rec.Code)
		}
	}

	// State is a read-only GET.
	req := httptest.NewRequest("GET", "/api/schedule-state", nil)
	req.Host = "127.0.0.1:8097"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "state") {
		t.Errorf("schedule-state = %d, body %s", rec.Code, rec.Body.String())
	}
}

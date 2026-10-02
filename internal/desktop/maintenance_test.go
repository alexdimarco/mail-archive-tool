package desktop

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-280, R19, R20, S41
// The Status & health view mirrors the status verb (a read-only summary), and the
// maintenance actions (verify, rebuild) are loopback+CSRF-guarded — a bad request
// starts no job.
func TestMaintenanceAndHealth(t *testing.T) {
	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: t.TempDir()}
	h := DashboardHandler(cfg)
	_, _ = getPage(t, h)

	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Host = "127.0.0.1:8097"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "summary") {
		t.Errorf("health = %d, body %s", rec.Code, rec.Body.String())
	}

	for _, p := range []string{"/api/verify", "/api/rebuild"} {
		r := postAction(h, p, "", "127.0.0.1:8097", "http://127.0.0.1:8097")
		if r.Code != http.StatusForbidden {
			t.Errorf("%s without CSRF = %d, want 403", p, r.Code)
		}
	}
}

package desktop

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-695
// The dashboard's Shutdown button stops the process from within: /api/shutdown
// (loopback+CSRF) cancels the process lifecycle, which serveInstance watches to
// stop the dashboard and reader servers and release the account lock — so the next
// launch starts cleanly with no port conflict and nothing to kill by hand. A stop
// drains the running export first (R36: it never touches an archive). A dashboard
// with no lifecycle refuses gracefully instead of panicking.
func TestMA695_ShutdownStopsFromThePage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	life := NewLifecycle(ctx)
	drained := make(chan struct{})
	life.OnDrain(func(context.Context) error { close(drained); return nil })

	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Lifecycle: life})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/shutdown", nil)
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("X-CSRF-Token", d.csrf)
	d.shutdown(rec, req)
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("shutdown must answer ok, got %s", rec.Body.String())
	}

	// serveInstance selects on life.Context().Done() to stop the servers; the handler
	// must have cancelled it.
	select {
	case <-life.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel the lifecycle, so the servers would never stop")
	}
	// …and the registered drain runs (the running export finalizes before the lock frees).
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not drain the lifecycle")
	}

	// A dashboard with no lifecycle (a test shell, an informational render) refuses
	// gracefully rather than panicking on a nil lifecycle.
	d2 := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s2.json")})
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/shutdown", nil)
	req2.Header.Set("Origin", "http://127.0.0.1:8097")
	req2.Header.Set("X-CSRF-Token", d2.csrf)
	d2.shutdown(rec2, req2)
	if !strings.Contains(rec2.Body.String(), `"ok":false`) {
		t.Fatalf("a dashboard with no lifecycle must refuse gracefully, got %s", rec2.Body.String())
	}
}

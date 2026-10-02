package desktop

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/graphconfig"
)

func getOverview(t *testing.T, cfg Config) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	DashboardHandler(cfg).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d", rec.Code)
	}
	return rec.Body.String()
}

// covers: MA-272, R17, R12, S41
// The Overview page renders the four status cards from real state, the Microsoft
// 365 connection (or the not-configured prompt), and an OS-aware Security & privacy
// panel whose credential-vault line matches this OS.
func TestOverviewContent(t *testing.T) {
	// No config → the four cards, the setup prompt, the security panel.
	body := getOverview(t, Config{ConfigPath: filepath.Join(t.TempDir(), "none.json")})
	for _, want := range []string{
		"Archive engine", "Ready", "Microsoft 365", "Capture", "Idle", "Weekly backup",
		"Not configured", "mailarchive setup",
		"Security &amp; privacy", "Read-only to your mailbox", "No third-party servers",
		vaultPhrase(),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview (no config) missing %q", want)
		}
	}

	// Device config, no saved token → "Sign-in needed" + the tenant/client id.
	dev := filepath.Join(t.TempDir(), "graph-config.json")
	if err := graphconfig.Save(dev, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	// Point the token at an empty tempdir file so "not signed in" is deterministic.
	cfg := Config{ConfigPath: dev, TokenCachePath: filepath.Join(t.TempDir(), "tok.json")}
	body = getOverview(t, cfg)
	for _, want := range []string{"Sign-in needed", "contoso", "APPID"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview (device, not signed in) missing %q", want)
		}
	}
	if strings.Contains(body, "Signed in as") {
		t.Errorf("no token present, but the page claims a sign-in")
	}

	// App config → the "app-only" mode label appears.
	app := filepath.Join(t.TempDir(), "graph-config.json")
	if err := graphconfig.Save(app, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "app"}); err != nil {
		t.Fatal(err)
	}
	if body = getOverview(t, Config{ConfigPath: app, TokenCachePath: filepath.Join(t.TempDir(), "t.json")}); !strings.Contains(body, "app-only") {
		t.Errorf("app-mode config should show the app-only label:\n%s", body)
	}
}

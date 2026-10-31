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
// The Overview page renders its status cards from real state, the Microsoft
// 365 connection (or the not-configured prompt), and an OS-aware Security & privacy
// panel whose credential-vault line matches this OS. Since 2026-10-31 (the owner's
// ruling A for 1.5.2) the "Archive engine: Ready" and "Capture: Idle" tiles are gone:
// each could only ever show that one value, so they told a person nothing.
func TestOverviewContent(t *testing.T) {
	// No config → the two cards, the in-app Microsoft 365 config form, the security panel.
	body := getOverview(t, Config{ConfigPath: filepath.Join(t.TempDir(), "none.json")})
	var labels []string
	for _, c := range (Config{ConfigPath: filepath.Join(t.TempDir(), "none.json")}).cards() {
		labels = append(labels, c.Label)
	}
	if got := strings.Join(labels, " | "); got != "Microsoft 365 | Scheduled backup" {
		t.Errorf("the Overview's status tiles are %q, want only the two that can change: Microsoft 365 and Scheduled backup", got)
	}
	for _, gone := range []string{`<div class="label">Archive engine</div>`, `<div class="label">Capture</div>`, ">Idle</div>"} {
		if strings.Contains(body, gone) {
			t.Errorf("the Overview still draws a tile that can only ever show one value: %q", gone)
		}
	}
	for _, want := range []string{
		`<div class="label">Microsoft 365</div>`, `<div class="label">Scheduled backup</div>`,
		"Not set up (only needed for Microsoft 365 mail)", // the status card, with no archive chosen (MA-938)
		// The Microsoft 365 connection offers an in-app config form (MA-549), not a pointer
		// to the `mailarchive setup` command line (owner report 2026-10-13).
		`id="m365Tenant"`, `id="m365ClientId"`, "Directory (tenant) ID", "Save configuration",
		"Security &amp; privacy", "Read-only to your mail, with one exception you choose.", "No MailArchive servers.",
		vaultPhrase(),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("overview (no config) missing %q", want)
		}
	}
	// The defect: a normal (non-local-origin) archive must not send the user to the
	// command line — the in-app form replaces "run mailarchive setup".
	if strings.Contains(body, "mailarchive setup") {
		t.Errorf("no-config overview should offer the in-app form, not point to `mailarchive setup`")
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

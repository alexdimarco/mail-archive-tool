package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

func newSetup(t *testing.T) (http.Handler, string, graphconfig.SecretStore) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	store := graphconfig.NewFileSecretStore(filepath.Join(dir, "secrets"))
	return SetupHandler(cfgPath, store), cfgPath, store
}

var csrfRe = regexp.MustCompile(`name="csrf" content="([0-9a-f]+)"`)

func getCSRF(t *testing.T, h http.Handler) (string, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "127.0.0.1:8097"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no CSRF token in the wizard page")
	}
	return m[1], body
}

func postSetup(h http.Handler, csrf, host, origin, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/setup", strings.NewReader(body))
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// covers: MA-268, R12, S40
// The wizard page offers the device/app mode toggle (with an app-only secret field)
// and carries the Object-ID clarification at the client-id field.
func TestSetupPageContent(t *testing.T) {
	h, _, _ := newSetup(t)
	_, body := getCSRF(t, h)
	for _, want := range []string{
		`name="auth" value="device"`, `name="auth" value="app"`, `id="secret"`,
		"Application (client) ID", "Not an Object ID", "enterprise application",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wizard page missing %q", want)
		}
	}
}

// covers: MA-266, R4, R19, S40
// The setup POST writes the config and stores the secret, and NEVER returns or
// persists the secret in the clear: the response and the config file omit it, while
// the store holds it and a later GET reports only "configured".
func TestSetupWritesWithoutEchoingSecret(t *testing.T) {
	h, cfgPath, store := newSetup(t)
	csrf, _ := getCSRF(t, h)
	const secret = "super-secret-value-xyz"
	rec := postSetup(h, csrf, "127.0.0.1:8097", "http://127.0.0.1:8097",
		`{"tenant":"contoso","clientId":"APPID","auth":"app","secret":"`+secret+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("response echoed the secret")
	}
	if !strings.Contains(rec.Body.String(), `"configured":true`) {
		t.Errorf("response should report configured:true, got %s", rec.Body.String())
	}
	// Config written, no secret in it.
	cfg, err := graphconfig.Load(cfgPath)
	if err != nil || cfg.Tenant != "contoso" || cfg.ClientID != "APPID" || cfg.Auth != "app" {
		t.Fatalf("config not written correctly: %+v / %v", cfg, err)
	}
	raw, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(raw), secret) {
		t.Fatal("config file on disk contains the secret")
	}
	// Store holds it; a later GET shows "configured", never the value.
	if got, _ := store.Get(graphconfig.AccountKey("contoso", "APPID")); got != secret {
		t.Fatalf("store did not hold the secret: %q", got)
	}
	_, body := getCSRF(t, h)
	if strings.Contains(body, secret) {
		t.Fatal("GET page leaked the stored secret")
	}
	if !strings.Contains(body, "already stored") {
		t.Errorf("GET page should note a secret is already stored")
	}
}

// covers: MA-267, R19, R12, R4, S40
// The POST is refused with the side effect ABSENT when the CSRF token is missing, the
// Origin is cross-site, or the Host is not loopback; a valid tokened same-origin POST
// on the healthy twin succeeds.
func TestSetupRefusesCSRFAndRebind(t *testing.T) {
	noConfig := func(cfgPath string) func() bool {
		return func() bool { _, err := graphconfig.Load(cfgPath); return errors.Is(err, os.ErrNotExist) }
	}
	body := `{"tenant":"c","clientId":"a","auth":"device"}`

	// Missing CSRF token.
	h, cfg, _ := newSetup(t)
	getCSRF(t, h) // mint the page/token, but don't send it
	rec := postSetup(h, "", "127.0.0.1:8097", "http://127.0.0.1:8097", body)
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(noConfig(cfg)))

	// Cross-site Origin.
	h2, cfg2, _ := newSetup(t)
	csrf2, _ := getCSRF(t, h2)
	rec = postSetup(h2, csrf2, "127.0.0.1:8097", "http://evil.example", body)
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(noConfig(cfg2)))

	// Non-loopback Host (DNS-rebind).
	h3, cfg3, _ := newSetup(t)
	csrf3, _ := getCSRF(t, h3)
	rec = postSetup(h3, csrf3, "evil.example", "http://evil.example", body)
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(noConfig(cfg3)))

	// Healthy twin: a tokened, same-origin, loopback POST succeeds.
	h4, cfg4, _ := newSetup(t)
	csrf4, _ := getCSRF(t, h4)
	rec = postSetup(h4, csrf4, "127.0.0.1:8097", "http://127.0.0.1:8097", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthy twin POST status = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, err := graphconfig.Load(cfg4); err != nil {
		t.Errorf("healthy twin did not write the config: %v", err)
	}
}

// covers: MA-270, R4, S40
// Reconfiguring to a different tenant/client deletes the superseded registration's
// stored secret, so an old secret is not orphaned.
func TestSetupReconfigureDeletesOldSecret(t *testing.T) {
	h, _, store := newSetup(t)
	csrf, _ := getCSRF(t, h)

	postSetup(h, csrf, "127.0.0.1:8097", "http://127.0.0.1:8097",
		`{"tenant":"tenantA","clientId":"clientA","auth":"app","secret":"secretA"}`)
	oldAcct := graphconfig.AccountKey("tenantA", "clientA")
	if _, err := store.Get(oldAcct); err != nil {
		t.Fatalf("secretA should be stored: %v", err)
	}

	postSetup(h, csrf, "127.0.0.1:8097", "http://127.0.0.1:8097",
		`{"tenant":"tenantB","clientId":"clientB","auth":"app","secret":"secretB"}`)
	if _, err := store.Get(oldAcct); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("old registration's secret should be deleted, got %v", err)
	}
	if _, err := store.Get(graphconfig.AccountKey("tenantB", "clientB")); err != nil {
		t.Errorf("new secret should be stored: %v", err)
	}
}

package desktop

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

func newConfigureDash(t *testing.T) (http.Handler, string, graphconfig.SecretStore) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	store := graphconfig.NewFileSecretStore(filepath.Join(dir, "secrets"))
	return DashboardHandler(Config{ConfigPath: cfgPath, Store: store}), cfgPath, store
}

// covers: MA-549, R4, R17, R19, S41
// The in-app configure POST writes the config and stores the app-only secret, and NEVER
// echoes or persists the secret in the clear: the response and the config file omit it,
// the credential store holds it, and a later page reports only "a secret is stored".
func TestConfigureM365WritesWithoutEchoingSecret(t *testing.T) {
	h, cfgPath, store := newConfigureDash(t)
	_, csrf := getPage(t, h)
	const secret = "app-secret-value-123"

	rec := postJSON(h, "/api/m365/configure", csrf,
		`{"tenant":"contoso","clientId":"APPID","auth":"app","secret":"`+secret+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("configure POST = %d, body %s", rec.Code, rec.Body.String())
	}
	body := assure.Reached(t, rec.Body.String(), "configure response")
	if strings.Contains(body, secret) {
		t.Fatal("response echoed the secret")
	}
	if !strings.Contains(body, `"ok":true`) || !strings.Contains(body, `"configured":true`) {
		t.Errorf("response should be ok with configured:true, got %s", body)
	}
	// Config written, no secret in it.
	cfg, err := graphconfig.Load(cfgPath)
	if err != nil || cfg.Tenant != "contoso" || cfg.ClientID != "APPID" || cfg.Auth != "app" {
		t.Fatalf("config not written correctly: %+v / %v", cfg, err)
	}
	if raw, _ := os.ReadFile(cfgPath); strings.Contains(string(raw), secret) {
		t.Fatal("config file on disk contains the secret")
	}
	// Store holds it; the reloaded page notes a stored secret, never the value.
	if got, _ := store.Get(graphconfig.AccountKey("contoso", "APPID")); got != secret {
		t.Fatalf("store did not hold the secret: %q", got)
	}
	page, _ := getPage(t, h)
	if strings.Contains(page, secret) {
		t.Fatal("reloaded page leaked the stored secret")
	}
	if !strings.Contains(page, "A secret is already stored") {
		t.Errorf("reloaded page should note a secret is already stored")
	}
}

// covers: MA-549, R19, R12, R4, S41
// The configure POST is refused with the side effect ABSENT when the CSRF token is
// missing, the Origin is cross-site, or the Host is not loopback; the healthy twin — a
// tokened, same-origin, loopback POST — writes the config.
func TestConfigureM365RefusesCSRFAndRebind(t *testing.T) {
	noConfig := func(cfgPath string) func() bool {
		return func() bool { _, err := graphconfig.Load(cfgPath); return errors.Is(err, os.ErrNotExist) }
	}

	// Missing CSRF token (the guard refuses before the body is read).
	h, cfg, _ := newConfigureDash(t)
	getPage(t, h) // mint the page/token, but don't send it
	rec := postAction(h, "/api/m365/configure", "", "127.0.0.1:8097", "http://127.0.0.1:8097")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(noConfig(cfg)))

	// Cross-site Origin.
	h2, cfg2, _ := newConfigureDash(t)
	_, csrf2 := getPage(t, h2)
	rec = postAction(h2, "/api/m365/configure", csrf2, "127.0.0.1:8097", "http://evil.example")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(noConfig(cfg2)))

	// Non-loopback Host (DNS-rebind).
	h3, cfg3, _ := newConfigureDash(t)
	_, csrf3 := getPage(t, h3)
	rec = postAction(h3, "/api/m365/configure", csrf3, "evil.example", "http://evil.example")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(noConfig(cfg3)))

	// Healthy twin: a tokened, same-origin, loopback POST writes the config.
	h4, cfg4, _ := newConfigureDash(t)
	_, csrf4 := getPage(t, h4)
	rec = postJSON(h4, "/api/m365/configure", csrf4, `{"tenant":"c","clientId":"a","auth":"device"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthy twin POST status = %d, body %s", rec.Code, rec.Body.String())
	}
	if _, err := graphconfig.Load(cfg4); err != nil {
		t.Errorf("healthy twin did not write the config: %v", err)
	}
}

// covers: MA-549, R4, S41
// Updating an existing configuration needs no command line and no retyped secret: a blank
// secret on app-only keeps the stored one, and reconfiguring to a different registration
// deletes the superseded secret (MA-270, through the shared seam).
func TestConfigureM365UpdatesExistingConfig(t *testing.T) {
	h, _, store := newConfigureDash(t)
	_, csrf := getPage(t, h)

	if rec := postJSON(h, "/api/m365/configure", csrf,
		`{"tenant":"tenantA","clientId":"clientA","auth":"app","secret":"secretA"}`); rec.Code != http.StatusOK {
		t.Fatalf("initial configure = %d %s", rec.Code, rec.Body.String())
	}
	acctA := graphconfig.AccountKey("tenantA", "clientA")
	if got, _ := store.Get(acctA); got != "secretA" {
		t.Fatalf("secretA should be stored")
	}

	// Same registration, blank secret → keep the stored one.
	rec := postJSON(h, "/api/m365/configure", csrf,
		`{"tenant":"tenantA","clientId":"clientA","auth":"app","secret":""}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":true`) {
		t.Fatalf("blank-secret update = %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := store.Get(acctA); got != "secretA" {
		t.Errorf("a blank secret should keep the stored secretA, got %q", got)
	}

	// A different registration → the superseded secret is deleted.
	if rec := postJSON(h, "/api/m365/configure", csrf,
		`{"tenant":"tenantB","clientId":"clientB","auth":"app","secret":"secretB"}`); rec.Code != http.StatusOK {
		t.Fatalf("reconfigure = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := store.Get(acctA); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the old registration's secret should be deleted, got %v", err)
	}
	if got, _ := store.Get(graphconfig.AccountKey("tenantB", "clientB")); got != "secretB" {
		t.Errorf("the new secret should be stored")
	}
}

// covers: MA-549, R4, S41
// Incomplete input is a user-safe ok:false message with nothing written: a missing tenant,
// and app-only with no secret and none stored.
func TestConfigureM365RejectsIncompleteInput(t *testing.T) {
	h, cfgPath, _ := newConfigureDash(t)
	_, csrf := getPage(t, h)

	rec := postJSON(h, "/api/m365/configure", csrf, `{"tenant":"","clientId":"a","auth":"device"}`)
	if rec.Code != http.StatusOK { // a validation message, not a transport error
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if b := rec.Body.String(); !strings.Contains(b, `"ok":false`) || !strings.Contains(b, "Directory (tenant) ID is required") {
		t.Errorf("want a tenant-required message, got %s", b)
	}
	if _, err := graphconfig.Load(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("nothing should be written on a validation error, got %v", err)
	}

	rec = postJSON(h, "/api/m365/configure", csrf, `{"tenant":"t","clientId":"c","auth":"app","secret":""}`)
	if b := rec.Body.String(); !strings.Contains(b, `"ok":false`) || !strings.Contains(b, "client secret is required") {
		t.Errorf("app-only with no secret should be refused, got %s", b)
	}
	if _, err := graphconfig.Load(cfgPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("nothing should be written when the app-only secret is missing, got %v", err)
	}
}

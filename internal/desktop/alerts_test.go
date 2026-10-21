package desktop

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/alert"
)

// memStore is an in-memory SecretStore (the vault) for tests.
type memStore struct{ m map[string]string }

func newMemStore() *memStore { return &memStore{m: map[string]string{}} }
func (s *memStore) Set(account, secret string) error {
	s.m[account] = secret
	return nil
}
func (s *memStore) Get(account string) (string, error) {
	v, ok := s.m[account]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}
func (s *memStore) Delete(account string) error { delete(s.m, account); return nil }
func (s *memStore) Name() string                 { return "test vault" }

// covers: MA-698
// The alert wiring: the SMTP password lives in the vault (never the settings file);
// fireAlert honours the event gate and reads the password from the vault; a known
// provider's preset fills the host; the save endpoint persists the config and routes
// the password to the vault; the test endpoint sends regardless of the on/off toggle.
func TestMA698_AlertWiring(t *testing.T) {
	store := newMemStore()
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	cfg := Config{SettingsPath: settingsPath, Store: store}

	// The password round-trips through the vault.
	if err := cfg.setAlertPassword("app-pw"); err != nil {
		t.Fatal(err)
	}
	if got := cfg.alertPassword(); got != "app-pw" {
		t.Fatalf("vault password round-trip: got %q", got)
	}

	var sentCfg alert.Config
	var sentPw string
	var sent []alert.Event
	old := sendAlert
	defer func() { sendAlert = old }()
	sendAlert = func(c alert.Config, pw string, ev alert.Event) error {
		sentCfg, sentPw = c, pw
		sent = append(sent, ev)
		return nil
	}

	// Disabled (default) -> nothing is sent, whatever the result.
	cfg.fireAlert(alert.ResultFailed, "/arch", errors.New("boom"), 0, nil)
	if len(sent) != 0 {
		t.Fatal("a disabled alert must not send")
	}

	// Save the config through the endpoint: enable on failure, Office 365 preset, with a
	// password. The password must land in the vault, NOT the settings file.
	d := newDashboard(cfg)
	post := func(h func(http.ResponseWriter, *http.Request), body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/x", strings.NewReader(body))
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", d.csrf)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	rec := post(d.saveAlertSettings, `{"enabled":true,"onFailure":true,"provider":"office365","from":"backup@sa.org","to":"tickets@sa.org","username":"backup@sa.org","password":"vault-pw"}`)
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("save alert settings: %s", rec.Body.String())
	}
	raw, _ := os.ReadFile(settingsPath)
	if strings.Contains(string(raw), "vault-pw") {
		t.Fatal("the SMTP password must NOT be written to the settings file")
	}
	if !strings.Contains(string(raw), "smtp.office365.com") {
		t.Fatalf("the Office 365 preset host should be saved:\n%s", raw)
	}
	if store.m[alertSecretAccount] != "vault-pw" {
		t.Fatalf("the password should be in the vault, got %q", store.m[alertSecretAccount])
	}

	// A failure now fires, with the vault password and the preset-filled host.
	d.cfg.fireAlert(alert.ResultFailed, "/arch", errors.New("drive gone"), 0, nil)
	if len(sent) != 1 || sent[0].Result != alert.ResultFailed || sent[0].Error == "" {
		t.Fatalf("failure alert not sent correctly: %+v", sent)
	}
	if sentPw != "vault-pw" || sentCfg.Host != "smtp.office365.com" {
		t.Fatalf("failure alert used wrong pw/host: pw=%q host=%q", sentPw, sentCfg.Host)
	}

	// A success does NOT fire (OnSuccess is off).
	sent = nil
	d.cfg.fireAlert(alert.ResultOK, "/arch", nil, 0, nil)
	if len(sent) != 0 {
		t.Fatal("a success must not fire when OnSuccess is off")
	}

	// The test endpoint sends regardless of the enabled toggle.
	sent = nil
	rec = post(d.alertTest, `{}`)
	if !strings.Contains(rec.Body.String(), `"ok":true`) || len(sent) != 1 {
		t.Fatalf("alert test: %s (sent=%d)", rec.Body.String(), len(sent))
	}
}

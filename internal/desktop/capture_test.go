package desktop

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
)

// newFakeM365 stands in for the device-code, token, and delegated (/me) Graph
// endpoints so the in-process capture can run with no network.
func newFakeM365(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s)
	}
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"device_code":"DEV","user_code":"WXYZ-1234","verification_uri":"https://microsoft.com/devicelogin","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
	})
	mux.HandleFunc("/me/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"M1","internetMessageId":"<m1@x>","subject":"subj-M1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}]}`)
	})
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "From: a@example.com\r\nSubject: subj-M1\r\nMessage-ID: <m1@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func countHTML(root string) int {
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") && d.Name() != "index.html" {
			n++
		}
		return nil
	})
	return n
}

func postJSON(h http.Handler, path, csrf, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Host = "127.0.0.1:8097"
	req.Header.Set("Origin", "http://127.0.0.1:8097")
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// covers: MA-275, R17, R19, S41
// "Archive now" runs the capture IN-PROCESS (no child binary): a loopback+CSRF POST
// starts it, the device sign-in happens in-process, the archive is written, and the
// activity endpoint reports completion with a saved sign-in afterward.
func TestCaptureInProcess(t *testing.T) {
	srv := newFakeM365(t)
	dir, out := t.TempDir(), t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		ConfigPath: cfgPath, Out: out, SettingsPath: filepath.Join(dir, "settings.json"),
		TokenCachePath: filepath.Join(dir, "tok.json"),
		BaseURL:        srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode",
	}
	h := DashboardHandler(cfg)
	_, csrf := getPage(t, h)

	if rec := postAction(h, "/api/capture", csrf, "127.0.0.1:8097", "http://127.0.0.1:8097"); rec.Code != http.StatusOK {
		t.Fatalf("capture start = %d, body %s", rec.Code, rec.Body.String())
	}

	var act map[string]any
	for i := 0; i < 60; i++ {
		req := httptest.NewRequest("GET", "/api/activity", nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		act = nil
		json.Unmarshal(rec.Body.Bytes(), &act)
		running, _ := act["running"].(bool)
		if !running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if e, _ := act["error"].(string); e != "" {
		t.Fatalf("capture reported an error: %s", e)
	}
	if res, _ := act["result"].(string); !strings.Contains(res, "exported") {
		t.Errorf("capture result = %v, want an 'exported' summary", act["result"])
	}
	if countHTML(out) == 0 {
		t.Error("capture wrote no message page to the archive")
	}
	if _, ok := cfg.signInState(); !ok {
		t.Error("capture did not leave a saved sign-in")
	}
}

// covers: MA-278, R17, S41
// The scheduled headless capture (`mailarchive-desktop --capture`, DC1) runs with no
// dashboard and no prompt: it refuses when not configured or not signed in, and with
// a saved config + sign-in it archives — the command a weekly backup runs whether or
// not the dashboard is open.
func TestHeadlessCapture(t *testing.T) {
	srv := newFakeM365(t)
	dir, out := t.TempDir(), t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	tok := filepath.Join(dir, "tok.json")
	base := Config{
		ConfigPath: cfgPath, SettingsPath: filepath.Join(dir, "settings.json"), TokenCachePath: tok, Out: out,
		BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode",
	}

	// Not configured → refused naming setup.
	if err := HeadlessCapture(base, io.Discard); err == nil || !strings.Contains(err.Error(), "setup") {
		t.Errorf("not-configured should refuse naming setup, got %v", err)
	}
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	// Configured but no saved sign-in → refused (a scheduled run cannot prompt).
	if err := HeadlessCapture(base, io.Discard); err == nil || !strings.Contains(strings.ToLower(err.Error()), "sign") {
		t.Errorf("no saved sign-in should refuse, got %v", err)
	}
	// Saved sign-in → runs headless and archives.
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := HeadlessCapture(base, io.Discard); err != nil {
		t.Fatalf("headless capture with a saved sign-in: %v", err)
	}
	if countHTML(out) == 0 {
		t.Error("headless capture archived nothing")
	}
}

// covers: MA-276, R19, R12, R4, S41
// The settings POST is loopback+CSRF-guarded (a bad request writes nothing) and a
// valid request persists the archive location, keep-raw, and Deleted/Junk choices.
func TestSettingsSaveGuarded(t *testing.T) {
	sp := filepath.Join(t.TempDir(), "settings.json")
	h := DashboardHandler(Config{SettingsPath: sp})
	_, csrf := getPage(t, h)

	// No CSRF → refused, nothing written.
	rec := postJSON(h, "/api/settings", "", `{"out":"/archive"}`)
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden),
		assure.NoSideEffect(func() bool { _, err := os.Stat(sp); return os.IsNotExist(err) }))

	// Valid → persisted.
	rec = postJSON(h, "/api/settings", csrf, `{"out":"/archive","keepRaw":true,"includeDeleted":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings save = %d, body %s", rec.Code, rec.Body.String())
	}
	s := LoadSettings(sp)
	if s.Out != "/archive" || !s.KeepRaw || !s.IncludeDeleted || s.IncludeJunk {
		t.Errorf("settings not persisted correctly: %+v", s)
	}
}

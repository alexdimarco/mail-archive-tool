package desktop

import (
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

var dashCSRFRe = regexp.MustCompile(`name="csrf" content="([0-9a-f]+)"`)

func getPage(t *testing.T, h http.Handler) (string, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "127.0.0.1:8097"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	m := dashCSRFRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no CSRF token in the dashboard page")
	}
	return body, m[1]
}

func postAction(h http.Handler, path, csrf, host, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, nil)
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// covers: MA-274, R19, R12, R4, S41
// The dashboard shows "Signed in as <upn>" when a token exists; the Clear-sign-in
// POST is loopback-only + CSRF/Origin/Host-guarded — a bad request is refused with
// the token UNTOUCHED, a valid one removes it.
func TestSignInStateAndClear(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "contoso", ClientID: "APPID", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	tokPath := filepath.Join(dir, "tok.json")
	if err := os.WriteFile(tokPath, []byte(`{"upn":"alice@contoso.org","token":{"refresh_token":"RT1","access_token":"AT1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := DashboardHandler(Config{ConfigPath: cfgPath, TokenCachePath: tokPath})

	body, csrf := getPage(t, h)
	if !strings.Contains(body, "Signed in as alice@contoso.org") {
		t.Errorf("a token exists but the page does not show the signed-in user")
	}

	tokenPresent := func() bool { _, err := os.Stat(tokPath); return err == nil }

	// Missing CSRF → refused, token untouched.
	rec := postAction(h, "/api/clear-signin", "", "127.0.0.1:8097", "http://127.0.0.1:8097")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(tokenPresent))

	// Cross-site Origin → refused.
	rec = postAction(h, "/api/clear-signin", csrf, "127.0.0.1:8097", "http://evil.example")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(tokenPresent))

	// Non-loopback Host → refused.
	rec = postAction(h, "/api/clear-signin", csrf, "evil.example", "http://evil.example")
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden), assure.NoSideEffect(tokenPresent))

	// Valid same-origin tokened POST → clears the sign-in.
	rec = postAction(h, "/api/clear-signin", csrf, "127.0.0.1:8097", "http://127.0.0.1:8097")
	if rec.Code != http.StatusOK {
		t.Fatalf("valid clear-signin = %d, body %s", rec.Code, rec.Body.String())
	}
	if tokenPresent() {
		t.Error("a valid clear-signin did not remove the saved token")
	}
}

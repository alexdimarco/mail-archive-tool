package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/localweb"
)

// covers: MA-372, R23, X9, X12
// The dashboard's state-changing POSTs are refused without a session even when
// the request is otherwise perfect — the page's own CSRF token, a loopback Origin
// and Host — and change nothing: no settings file is written. Healthy twin first:
// the same POST, inside the session under /s/<P>/, is accepted and writes the
// settings. The locked answer is JSON 401 for an /api/ path and carries neither the
// CSRF token nor the tenant's identity.
func TestDashboardPostsWithoutASessionChangeNothing(t *testing.T) {
	body := func(out string) string {
		b, _ := json.Marshal(map[string]any{"out": out})
		return string(b)
	}
	post := func(h http.Handler, path, csrf, out string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body(out)))
		req.Host = "127.0.0.1:8097"
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// The healthy twin: inside the session the POST is accepted and writes the file.
	h, csrf, settings := dashboardFixture(t)
	surface, ok := localweb.Guarded(h)
	if !ok || surface.Session == nil {
		t.Fatalf("the dashboard handler is not a session-guarded surface")
	}
	cookie := sessionCookie(t, h, surface.Session, "127.0.0.1:8097")
	chosen := filepath.Join(t.TempDir(), "archive")
	rec := post(h, surface.Session.Prefix()+"api/settings", csrf, chosen, cookie)
	var res struct{ OK bool }
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || !res.OK {
		t.Fatalf("a POST inside the session was refused: %d %q", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("the healthy POST wrote no settings file: %v", err)
	}

	// The refusals, each on a fresh dashboard, so "nothing written" is a clean reading.
	for _, c := range []struct {
		name string
		path func(*localweb.Session) string
		with bool // sends the right cookie, at a path outside the prefix
	}{
		{"no cookie, under the prefix", func(s *localweb.Session) string { return s.Prefix() + "api/settings" }, false},
		{"no cookie, bare path", func(*localweb.Session) string { return "/api/settings" }, false},
		{"the right cookie at a path outside the prefix", func(*localweb.Session) string { return "/api/settings" }, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, csrf, settings := dashboardFixture(t)
			s, _ := localweb.Guarded(h)
			var ck *http.Cookie
			if c.with {
				ck = sessionCookie(t, h, s.Session, "127.0.0.1:8097")
			}
			rec := post(h, c.path(s.Session), csrf, filepath.Join(t.TempDir(), "archive"), ck)
			assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusUnauthorized),
				assure.Names(`"ok":false`), assure.Forbid(csrf, "contoso-tenant-0451", "app-client-7788"),
				assure.NoSideEffect(func() bool { _, err := os.Stat(settings); return os.IsNotExist(err) }))
		})
	}
}

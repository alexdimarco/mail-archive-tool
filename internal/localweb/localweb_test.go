package localweb

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// secretPage is what the stand-in surface answers: a subject, an archive path and
// a CSRF-looking token, none of which a refused request may ever see.
const secretPage = "SUBJECT-Quarterly-merger /Users/someone/archive csrf=0123456789abcdef"

// countingInner stands in for a surface's own handler: it counts the requests
// that reach it and answers with secretPage.
type countingInner struct{ calls int }

func (c *countingInner) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.calls++
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(secretPage))
}

func loopbackAt(port int) net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port} }

// anyRedirect stands for "some 3xx" where net/http picks the redirect status.
const anyRedirect = -3

func send(h http.Handler, method, path, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// expectRefused asserts the guard's Host refusal: 403, the fixed plain-text line
// naming the port and the remedy, none of the page's data, and the inner handler
// never reached.
func expectRefused(t *testing.T, h http.Handler, inner *countingInner, method, host, refusal string) {
	t.Helper()
	before := inner.calls
	rec := send(h, method, "/api/search?q=merger", host)
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden),
		assure.Names(refusal),
		assure.Forbid("SUBJECT-Quarterly-merger", "/Users/someone/archive", "0123456789abcdef"),
		assure.NoSideEffect(func() bool { return inner.calls == before }))
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != refusal {
		t.Errorf("%s Host %q: refusal body %q, want exactly %q", method, host, got, refusal)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("%s Host %q: refusal Content-Type %q, want text/plain", method, host, ct)
	}
}

// expectPassed asserts that a request with host reaches the inner handler.
func expectPassed(t *testing.T, h http.Handler, inner *countingInner, method, host string) {
	t.Helper()
	before := inner.calls
	rec := send(h, method, "/api/search?q=merger", host)
	if inner.calls != before+1 || rec.Code != http.StatusOK {
		t.Errorf("%s Host %q: reached the surface %d time(s), status %d — want it passed", method, host, inner.calls-before, rec.Code)
	}
}

// covers: MA-371, R23, R19, S43
// The guard checks the Host first, on every method: 127.0.0.1, localhost and
// [::1] pass with any port — a forwarded port too (ssh -L 9000:127.0.0.1:8099),
// because the rebinding defence rests on the name, never on the port [CC18] —
// while a rebinding page's own name, an empty Host, a trailing dot, userinfo, a
// missing or bad port, or any other address is refused 403 with the fixed line
// and nothing of the page. Only a non-loopback `serve` widens the rule, to its
// own IP literals and its -allow-host names.
func TestHostAllowList(t *testing.T) {
	methods := []string{http.MethodGet, http.MethodPost, http.MethodHead}
	inner := &countingInner{}
	h := Wrap(Surface{Name: "serve", Listener: loopbackAt(8099)}, inner)

	// Positive twins first.
	for _, host := range []string{"127.0.0.1:8099", "localhost:8099", "[::1]:8099", "127.0.0.1:9000", "LocalHost:8099", "[::1]:1"} {
		for _, m := range methods {
			expectPassed(t, h, inner, m, host)
		}
	}
	assure.Reached(t, inner.calls, "loopback requests that reached the surface")

	const refusal = "refused: this page is served only as http://127.0.0.1:8099/ — open it from MailArchive"
	for _, host := range []string{
		// A rebinding page carries its own name; HTTP/1.0 may send no Host.
		"attacker.example:8099", "",
		// A trailing dot.
		"127.0.0.1.:8099", "localhost.:8099", "127.0.0.1.",
		// Userinfo.
		"user@127.0.0.1:8099", "127.0.0.1:8099@attacker.example:8099",
		// No port, or a bad one.
		"127.0.0.1", "localhost", "[::1]",
		"127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:80x", "127.0.0.1:", "127.0.0.1:+80",
		// A malformed IPv6 literal.
		"::1:8099", "[::1]x:8099", "[::1:8099",
		// Not one of the three names.
		"127.0.0.2:8099", "0.0.0.0:8099", "[fe80::1%25en0]:8099", "192.168.1.10:8099",
	} {
		for _, m := range methods {
			expectRefused(t, h, inner, m, host, refusal)
		}
	}

	// A `serve` bound to 0.0.0.0 also accepts this machine's IP literals and its
	// -allow-host names — still with any port — and nothing else.
	lan := Wrap(Surface{Name: "serve", Listener: &net.TCPAddr{IP: net.IPv4zero, Port: 8099},
		ExtraHosts: []string{"192.168.1.10", "[2001:db8::1]", "nas.local"}}, inner)
	for _, host := range []string{"192.168.1.10:8099", "[2001:db8::1]:8099", "NAS.local:8099", "nas.local:9000", "127.0.0.1:8099"} {
		expectPassed(t, lan, inner, http.MethodGet, host)
	}
	for _, host := range []string{"192.168.1.11:8099", "nas.local.:8099", "nas.local", "evil.example:8099", "[2001:db8::2]:8099"} {
		expectRefused(t, lan, inner, http.MethodGet, host, refusal)
	}
}

// covers: MA-378, R19, R23
// Every response a surface sends carries the policy headers — its own CSP (a
// deny-all one when the handler set none), nosniff, no-referrer, no-store and
// same-origin CORP — the Host refusal, errors and redirects included: net/http's
// own error and redirect paths among them (http.FileServer's error path deletes
// Cache-Control before it writes).
func TestEveryResponseCarriesThePolicyHeaders(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "page.html"), []byte("<p>page</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	const ownCSP = "default-src 'none'; style-src 'unsafe-inline'"
	m := NewMux()
	m.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", ownCSP)
		w.Write([]byte("ok"))
	})
	m.HandleFunc("/bare", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("no policy of its own")) })
	m.HandleFunc("/err", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", http.StatusServiceUnavailable) })
	m.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusMovedPermanently) })
	m.HandleFunc("/cached", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write([]byte("tries to be cached"))
	})
	m.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(dir))))
	h := Wrap(Surface{Name: "serve", Listener: loopbackAt(8099)}, m)

	for _, c := range []struct {
		path, host string
		code       int
		csp        string
	}{
		{"/ok", "127.0.0.1:8099", http.StatusOK, ownCSP},
		{"/bare", "127.0.0.1:8099", http.StatusOK, denyCSP},
		{"/err", "127.0.0.1:8099", http.StatusServiceUnavailable, denyCSP},
		{"/moved", "127.0.0.1:8099", http.StatusMovedPermanently, denyCSP},
		{"/cached", "127.0.0.1:8099", http.StatusOK, denyCSP},
		{"/files/page.html", "127.0.0.1:8099", http.StatusOK, denyCSP},
		{"/files/missing.html", "127.0.0.1:8099", http.StatusNotFound, denyCSP}, // FileServer's serveError
		{"/files", "127.0.0.1:8099", anyRedirect, denyCSP},                      // ServeMux's own redirect
		{"/nowhere", "127.0.0.1:8099", http.StatusNotFound, denyCSP},            // no route at all
		{"/ok", "attacker.example:8099", http.StatusForbidden, denyCSP},         // the Host refusal
	} {
		rec := send(h, http.MethodGet, c.path, c.host)
		if c.code == anyRedirect {
			if rec.Code/100 != 3 {
				t.Errorf("GET %s (Host %s): status %d, want a redirect", c.path, c.host, rec.Code)
			}
		} else if rec.Code != c.code {
			t.Errorf("GET %s (Host %s): status %d, want %d", c.path, c.host, rec.Code, c.code)
		}
		hd := rec.Header()
		for k, want := range map[string]string{
			"Content-Security-Policy":      c.csp,
			"X-Content-Type-Options":       "nosniff",
			"Referrer-Policy":              "no-referrer",
			"Cache-Control":                "no-store",
			"Cross-Origin-Resource-Policy": "same-origin",
		} {
			if got := hd.Get(k); got != want {
				t.Errorf("GET %s (Host %s): %s = %q, want %q", c.path, c.host, k, got, want)
			}
		}
	}
}

// covers: MA-378, R23, R19
// The route registry records every pattern in registration order and resolves a
// request to the pattern it reaches; a surface's header middleware (Routed) and
// the guard (Wrap) both still name those routes, and Guarded tells a handler
// built by Wrap from one that skipped it.
func TestMuxRecordsEveryRoute(t *testing.T) {
	m := NewMux()
	noop := func(http.ResponseWriter, *http.Request) {}
	m.HandleFunc("/", noop)
	m.HandleFunc("/app.js", noop)
	m.Handle("/files/", http.HandlerFunc(noop))
	m.HandleFunc("POST /api/act", noop)
	want := []string{"/", "/app.js", "/files/", "POST /api/act"}
	if got := m.Routes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Routes() = %q, want %q", got, want)
	}
	resolve := map[string]string{
		"GET /app.js":           "/app.js",
		"GET /files/a/b.html":   "/files/",
		"POST /api/act":         "POST /api/act",
		"GET /something/else":   "/",
		"HEAD /files/README.md": "/files/",
	}
	headered := Routed(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		m.ServeHTTP(w, r)
	}), m)
	h := Wrap(Surface{Name: "setup", Listener: loopbackAt(8097)}, headered)
	for _, r := range []http.Handler{m, headered, h} {
		if got := RoutesOf(r); !reflect.DeepEqual(got, want) {
			t.Errorf("%T names routes %q, want %q", r, got, want)
		}
		for req, pattern := range resolve {
			method, path, _ := strings.Cut(req, " ")
			if got := RouteOf(r, httptest.NewRequest(method, path, nil)); got != pattern {
				t.Errorf("%T: %s resolves to %q, want %q", r, req, got, pattern)
			}
		}
	}
	if s, ok := Guarded(h); !ok || s.Name != "setup" {
		t.Errorf("Guarded(Wrap(...)) = %+v, %v; want the setup surface", s, ok)
	}
	for _, bare := range []http.Handler{m, headered, http.NotFoundHandler()} {
		if _, ok := Guarded(bare); ok {
			t.Errorf("Guarded(%T) = true for a handler Wrap did not build", bare)
		}
	}
	if got := RoutesOf(http.NotFoundHandler()); got != nil {
		t.Errorf("a plain handler names routes %q", got)
	}
}

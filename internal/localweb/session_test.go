package localweb

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

const sessHost = "127.0.0.1:8099"

// fakeClock is the session's injected clock.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// sessionInner stands in for a surface's own handler behind a session: it records
// each request that reaches it (path and query, as the surface sees them) and
// answers with secretPage. "/files" is a clean-path redirect, as a ServeMux sends.
type sessionInner struct{ seen []string }

func (c *sessionInner) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.seen = append(c.seen, r.URL.RequestURI())
	if r.URL.Path == "/files" {
		http.Redirect(w, r, "/files/", http.StatusMovedPermanently)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Write([]byte(secretPage))
}

// rig is one guarded surface with a session, an injected clock and a recording
// inner handler.
type rig struct {
	h     http.Handler
	s     *Session
	inner *sessionInner
	clock *fakeClock
	log   []*httptest.ResponseRecorder // every response the rig produced
}

func newRig(t *testing.T, surface string) *rig {
	t.Helper()
	g := &rig{s: NewSession(), inner: &sessionInner{}, clock: &fakeClock{t: time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC)}}
	g.s.SetClock(g.clock.Now)
	g.h = Wrap(Surface{Name: surface, Listener: loopbackAt(8099), Session: g.s}, g.inner)
	return g
}

func (g *rig) do(method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = sessHost
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	g.h.ServeHTTP(rec, req)
	g.log = append(g.log, rec)
	return rec
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// redeem opens a link the way a browser follows it and returns the session
// cookie it was given.
func (g *rig) redeem(t *testing.T, token string) (*http.Cookie, *httptest.ResponseRecorder) {
	t.Helper()
	rec := g.do(http.MethodGet, "/_open?t="+token)
	if rec.Code != http.StatusOK {
		t.Fatalf("redeeming a fresh link: status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	c := cookieNamed(rec, "mailarchive_session")
	if c == nil {
		t.Fatalf("redeeming a fresh link set no mailarchive_session cookie")
	}
	return c, rec
}

// prefixP is P, the secret segment of the session's path prefix.
func prefixP(s *Session) string {
	return strings.TrimSuffix(strings.TrimPrefix(s.Prefix(), "/s/"), "/")
}

// covers: MA-372, R23, X9, X12
// A surface with a session admits only a request that carries the session cookie
// under /s/<P>/. The healthy case first: a redeemed link gives a cookie with
// exactly the attributes `Path=/s/<P>/; HttpOnly; SameSite=Strict`, and every
// route then answers under the prefix, the inner handler seeing the prefix
// stripped. Then every other request — no cookie, a wrong one, the right one at a
// path outside the prefix, the link's own token as a cookie, eight decoy cookies
// with none valid, on GET, POST and HEAD, under the prefix or not — gets the
// locked page (401, HTML) or JSON `{"ok":false,"error":…}` (401) for an /api/
// path: what, why and what to do, with none of the page's data, no CSRF token and
// never P; and the inner handler is never reached.
func TestNoSessionGetsTheLockedPage(t *testing.T) {
	g := newRig(t, "serve")
	tok := g.s.Mint(LinkTTL)
	ck, rec := g.redeem(t, tok)
	prefix := g.s.Prefix()
	if !strings.HasPrefix(prefix, "/s/") || !strings.HasSuffix(prefix, "/") || len(prefixP(g.s)) < 20 {
		t.Fatalf("Prefix() = %q, want /s/<P>/ with P a long secret", prefix)
	}
	wantCookie := "mailarchive_session=" + ck.Value + "; Path=" + prefix + "; HttpOnly; SameSite=Strict"
	found := false
	for _, sc := range rec.Result().Header.Values("Set-Cookie") {
		if sc == wantCookie {
			found = true
		}
	}
	if !found {
		t.Errorf("Set-Cookie headers %q: none is exactly %q", rec.Result().Header.Values("Set-Cookie"), wantCookie)
	}

	routes := []struct{ method, path string }{
		{"GET", "/"}, {"GET", "/api/search?q=merger"}, {"GET", "/files/x.html"}, {"GET", "/goback"},
		{"GET", "/api/activity"}, {"POST", "/api/settings"}, {"HEAD", "/"},
	}
	g.inner.seen = nil
	for _, r := range routes {
		got := g.do(r.method, prefix+strings.TrimPrefix(r.path, "/"), ck)
		if got.Code != http.StatusOK {
			t.Errorf("%s %s with the session: status %d, want 200", r.method, r.path, got.Code)
		}
	}
	assure.Reached(t, g.inner.seen, "requests that reached the surface with a session")
	if g.inner.seen[0] != "/" || g.inner.seen[1] != "/api/search?q=merger" {
		t.Errorf("the surface saw %q; the prefix must be stripped (want / and /api/search?q=merger first)", g.inner.seen)
	}

	g.inner.seen = nil
	right := &http.Cookie{Name: "mailarchive_session", Value: ck.Value}
	decoys := make([]*http.Cookie, 8)
	for i := range decoys {
		decoys[i] = &http.Cookie{Name: "mailarchive_session", Value: "decoy" + string(rune('a'+i))}
	}
	cases := []struct {
		name    string
		cookies []*http.Cookie
		prefix  string // "" asks for the bare path
	}{
		{"no cookie, under the prefix", nil, prefix},
		{"no cookie, bare path", nil, "/"},
		{"wrong value", []*http.Cookie{{Name: "mailarchive_session", Value: "not-the-secret"}}, prefix},
		{"right value at a path outside the prefix", []*http.Cookie{right}, "/"},
		{"the link's token as the cookie", []*http.Cookie{{Name: "mailarchive_session", Value: tok}}, prefix},
		{"eight decoys, none valid", decoys, prefix},
		{"the valid cookie past the eighth examined", append(append([]*http.Cookie{}, decoys...), right), prefix},
	}
	forbid := []string{"SUBJECT-Quarterly-merger", "/Users/someone/archive", "0123456789abcdef", prefixP(g.s), ck.Value, tok}
	for _, c := range cases {
		for _, r := range routes {
			rel := strings.TrimPrefix(r.path, "/")
			got := g.do(r.method, c.prefix+rel, c.cookies...)
			what := c.name + ": " + r.method + " " + c.prefix + rel
			if strings.HasPrefix(r.path, "/api/") {
				assure.Refused(t, got.Code, got.Body.String(), assure.Code(http.StatusUnauthorized),
					assure.Names(`"ok":false`, `"error"`), assure.Forbid(forbid...),
					assure.NoSideEffect(func() bool { return len(g.inner.seen) == 0 }))
				if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
					t.Errorf("%s: Content-Type %q, want JSON for an /api/ path", what, ct)
				}
				var j struct {
					OK    bool   `json:"ok"`
					Error string `json:"error"`
				}
				if err := json.Unmarshal(got.Body.Bytes(), &j); err != nil || j.OK || j.Error == "" {
					t.Errorf("%s: body %q is not {\"ok\":false,\"error\":…}", what, got.Body.String())
				}
				continue
			}
			if r.method == http.MethodHead {
				if got.Code != http.StatusUnauthorized || len(g.inner.seen) != 0 {
					t.Errorf("%s: status %d (inner reached %d time(s)), want 401", what, got.Code, len(g.inner.seen))
				}
				continue
			}
			assure.Refused(t, got.Code, got.Body.String(), assure.Code(http.StatusUnauthorized),
				assure.Names("Why", "Do this"), assure.Forbid(forbid...),
				assure.NoSideEffect(func() bool { return len(g.inner.seen) == 0 }))
			if ct := got.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("%s: Content-Type %q, want the HTML locked page", what, ct)
			}
		}
	}
}

// covers: MA-372, X12
// The locked page names, per surface, what happened, why and the way in (design
// §7.6): the desktop's two surfaces point to the shortcut and "Copy MailArchive
// link"; `serve` to the printed link, Enter, `serve -link` and a restart; `setup`
// to its own terminal.
func TestLockedPageSaysWhatWhyAndWhatToDoPerSurface(t *testing.T) {
	for _, c := range []struct {
		surface string
		names   []string
	}{
		{"dashboard", []string{"has to be opened from the app", "Why:", "Do this:", "Start-menu or Desktop shortcut", "Copy MailArchive link"}},
		{"reader", []string{"has to be opened from the app", "Why:", "Do this:", "Start-menu or Desktop shortcut", "Copy MailArchive link"}},
		{"serve", []string{"needs its private link", "Why:", "Do this:", "press Enter there for a new one", "mailarchive serve -link -out", "mailarchive serve -out"}},
		{"setup", []string{"needs its private link", "Why:", "Do this:", "press Enter there for a new one", "mailarchive setup"}},
	} {
		g := newRig(t, c.surface)
		got := g.do(http.MethodGet, "/")
		assure.Refused(t, got.Code, got.Body.String(), assure.Code(http.StatusUnauthorized), assure.Names(c.names...))
		if got.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s: the locked page carries no Content-Security-Policy", c.surface)
		}
	}
}

// covers: MA-372, R23, X9
// An absolute Location from the surface's own handler keeps the session prefix (a
// ServeMux clean-path or trailing-slash redirect), and one that would leave the
// surface — a scheme or a host — is refused rather than followed.
func TestRedirectKeepsThePrefix(t *testing.T) {
	g := newRig(t, "serve")
	ck, _ := g.redeem(t, g.s.Mint(LinkTTL))
	got := g.do(http.MethodGet, g.s.Prefix()+"files", ck)
	if got.Code != http.StatusMovedPermanently || got.Header().Get("Location") != g.s.Prefix()+"files/" {
		t.Errorf("redirect: status %d Location %q, want 301 to %q", got.Code, got.Header().Get("Location"), g.s.Prefix()+"files/")
	}
	// The bare prefix, without its trailing slash, goes to the prefix.
	got = g.do(http.MethodGet, strings.TrimSuffix(g.s.Prefix(), "/"), ck)
	if got.Code/100 != 3 || got.Header().Get("Location") != g.s.Prefix() {
		t.Errorf("the prefix without its slash: status %d Location %q, want a redirect to %q", got.Code, got.Header().Get("Location"), g.s.Prefix())
	}

	for _, loc := range []string{"http://127.0.0.1:8099/files/", "//evil.example/x", "https://evil.example/x"} {
		loc := loc
		h := Wrap(Surface{Name: "serve", Listener: loopbackAt(8099), Session: g.s}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, loc, http.StatusFound)
		}))
		req := httptest.NewRequest(http.MethodGet, g.s.Prefix(), nil)
		req.Host = sessHost
		req.AddCookie(ck)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Location") != "" || rec.Code != http.StatusBadGateway {
			t.Errorf("a redirect to %q: status %d Location %q, want it refused (502, no Location)", loc, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// covers: MA-373, R23, X8
// A link redeems once. The redemption is a 200 page that navigates same-origin
// (a meta refresh and a visible Continue link to /s/<P>/, never a 3xx), setting
// the session cookie and the marker cookie `mailarchive_open=M; Path=/_open;
// HttpOnly; SameSite=Strict`; a second use is "already used" (401) unless it comes
// with the marker, which continues silently; an unknown token gets the neutral
// page; an expired one the expiry page; HEAD never redeems; the token never
// appears in any later response; every response carries the policy headers.
func TestLinkRedeemsOnce(t *testing.T) {
	g := newRig(t, "serve")
	tok := g.s.Mint(LinkTTL)
	ck, rec := g.redeem(t, tok)
	if rec.Code/100 == 3 {
		t.Errorf("redemption was a %d redirect; it must be a 200 page", rec.Code)
	}
	body := rec.Body.String()
	prefix := g.s.Prefix()
	for _, want := range []string{`<meta http-equiv="refresh" content="0;url=` + prefix + `">`, `href="` + prefix + `"`, "Continue"} {
		if !strings.Contains(body, want) {
			t.Errorf("redemption page lacks %q: %s", want, body)
		}
	}
	m := cookieNamed(rec, "mailarchive_open")
	if m == nil {
		t.Fatalf("redemption set no mailarchive_open marker cookie")
	}
	wantMarker := "mailarchive_open=" + m.Value + "; Path=/_open; HttpOnly; SameSite=Strict"
	hasMarker := false
	for _, sc := range rec.Result().Header.Values("Set-Cookie") {
		if sc == wantMarker {
			hasMarker = true
		}
	}
	if !hasMarker {
		t.Errorf("Set-Cookie headers %q: none is exactly %q", rec.Result().Header.Values("Set-Cookie"), wantMarker)
	}
	sum := sha256.Sum256([]byte(tok))
	if m.Value == "" || strings.Contains(m.Value, tok) || strings.Contains(m.Value, string(sum[:4])) {
		t.Errorf("the marker %q is derived from the token", m.Value)
	}
	if n, at := g.s.Opened(); n != 1 || !at.Equal(g.clock.Now()) {
		t.Errorf("Opened() = %d, %v; want 1 at the redemption time", n, at)
	}

	// Another browser presents the used link: the warning, and nothing set.
	used := g.do(http.MethodGet, "/_open?t="+tok)
	assure.Refused(t, used.Code, used.Body.String(), assure.Code(http.StatusUnauthorized),
		assure.Names("already been used", "another account", "start it again"), assure.Forbid(tok, ck.Value),
		assure.NoSideEffect(func() bool { return len(used.Result().Cookies()) == 0 }))
	// The browser that redeemed it comes back with its marker: silent, no warning.
	again := g.do(http.MethodGet, "/_open?t="+tok, m)
	if again.Code != http.StatusOK || strings.Contains(again.Body.String(), "another account") || !strings.Contains(again.Body.String(), "Continue") {
		t.Errorf("a used link with its marker: status %d, body %q; want the Continue page, silently", again.Code, again.Body.String())
	}
	// A wrong marker is no marker.
	wrong := g.do(http.MethodGet, "/_open?t="+tok, &http.Cookie{Name: "mailarchive_open", Value: "0000"})
	if wrong.Code != http.StatusUnauthorized || !strings.Contains(wrong.Body.String(), "already been used") {
		t.Errorf("a used link with a wrong marker: status %d, want the 401 warning", wrong.Code)
	}
	if n, _ := g.s.Opened(); n != 1 {
		t.Errorf("Opened() = %d after refused and repeated presentations, want 1", n)
	}

	// Unknown and expired links get their own, different, texts.
	unknown := g.do(http.MethodGet, "/_open?t=not-a-token-of-this-process")
	assure.Refused(t, unknown.Code, unknown.Body.String(), assure.Code(http.StatusUnauthorized),
		assure.Names("from an earlier session", "get a new one"), assure.Forbid("another account", "already been used"))
	missing := g.do(http.MethodGet, "/_open")
	assure.Refused(t, missing.Code, missing.Body.String(), assure.Code(http.StatusUnauthorized), assure.Names("from an earlier session"))
	old := g.s.Mint(LinkTTL)
	g.clock.Advance(LinkTTL + time.Second)
	expired := g.do(http.MethodGet, "/_open?t="+old)
	assure.Refused(t, expired.Code, expired.Body.String(), assure.Code(http.StatusUnauthorized),
		assure.Names("has expired", "get a new one"), assure.Forbid("another account", "already been used", "earlier session"))
	if n, _ := g.s.Opened(); n != 1 {
		t.Errorf("Opened() = %d after an unknown and an expired link, want 1", n)
	}

	// HEAD and POST never redeem.
	fresh := g.s.Mint(LinkTTL)
	for _, method := range []string{http.MethodHead, http.MethodPost} {
		if got := g.do(method, "/_open?t="+fresh); got.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /_open: status %d, want 405", method, got.Code)
		}
	}
	if _, rec := g.redeem(t, fresh); rec.Code != http.StatusOK {
		t.Errorf("the link a HEAD and a POST did not spend was refused")
	}

	// The token is in no response but the request that carried it, and every
	// response carries the policy headers.
	g.do(http.MethodGet, prefix, ck)
	g.do(http.MethodGet, "/")
	for i, r := range g.log {
		res := r.Result()
		for _, h := range []struct{ k, v string }{{"Cache-Control", "no-store"}, {"Cross-Origin-Resource-Policy", "same-origin"}, {"Referrer-Policy", "no-referrer"}, {"X-Content-Type-Options", "nosniff"}} {
			if res.Header.Get(h.k) != h.v {
				t.Errorf("response %d (status %d): %s = %q, want %q", i, r.Code, h.k, res.Header.Get(h.k), h.v)
			}
		}
		for _, secret := range []string{tok, old, fresh} {
			if strings.Contains(r.Body.String(), secret) {
				t.Errorf("response %d (status %d) contains a launch token", i, r.Code)
			}
			for _, vs := range res.Header {
				for _, v := range vs {
					if strings.Contains(v, secret) {
						t.Errorf("response %d (status %d) carries a launch token in a header", i, r.Code)
					}
				}
			}
		}
	}
}

// covers: MA-373, R23
// An opener token (handed to the browser opener, which an argv watcher can see)
// lives about 60 s and a printed or copied link 10 min, and the two differ
// [CC5]; the table holds at most 16 tokens, the oldest evicted — an evicted
// token is unknown, never a free redemption; every redemption is announced once
// (the notice behind "browser session opened at 14:02 (1 this run)" [CC24]); a
// LAN serve's cookie may carry a Max-Age, a loopback one never does.
func TestTokenLifetimesTableAndNotice(t *testing.T) {
	g := newRig(t, "serve")
	var notices []int
	g.s.OnOpen(func(n int, at time.Time) { notices = append(notices, n) })

	opener, printed := g.s.Mint(OpenerTTL), g.s.Mint(LinkTTL)
	if opener == printed || opener == "" {
		t.Fatalf("the opener token and the printed one must differ and be non-empty: %q, %q", opener, printed)
	}
	g.clock.Advance(OpenerTTL + time.Second)
	if got := g.do(http.MethodGet, "/_open?t="+opener); got.Code != http.StatusUnauthorized || !strings.Contains(got.Body.String(), "has expired") {
		t.Errorf("an opener token after 61 s: status %d, want the expiry page", got.Code)
	}
	ck, rec := g.redeem(t, printed)
	if strings.Contains(strings.Join(rec.Result().Header.Values("Set-Cookie"), "\n"), "Max-Age") || ck.MaxAge != 0 {
		t.Errorf("a loopback session's cookie carries a Max-Age: %q", rec.Result().Header.Values("Set-Cookie"))
	}
	if len(notices) != 1 || notices[0] != 1 {
		t.Errorf("notices after one redemption = %v, want [1]", notices)
	}

	first := g.s.Mint(LinkTTL)
	for i := 0; i < 40; i++ {
		g.s.Mint(LinkTTL)
	}
	if n := len(g.s.tokens); n > 16 {
		t.Errorf("the token table holds %d entries, want at most 16", n)
	}
	if got := g.do(http.MethodGet, "/_open?t="+first); got.Code != http.StatusUnauthorized || !strings.Contains(got.Body.String(), "earlier session") {
		t.Errorf("an evicted token: status %d, want the unknown-token page", got.Code)
	}
	last := g.s.Mint(LinkTTL)
	g.redeem(t, last)
	if len(notices) != 2 || notices[1] != 2 {
		t.Errorf("notices after two redemptions = %v, want [1 2]", notices)
	}

	lan := newRig(t, "serve")
	lan.s.SetMaxAge(24 * time.Hour)
	_, rec = lan.redeem(t, lan.s.Mint(LinkTTL))
	if !strings.Contains(strings.Join(rec.Result().Header.Values("Set-Cookie"), "\n"), "Max-Age=86400") {
		t.Errorf("a LAN session's cookie lacks its Max-Age: %q", rec.Result().Header.Values("Set-Cookie"))
	}
}

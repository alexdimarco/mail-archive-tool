package localweb

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Session is one process's per-launch session (design-local-confidentiality
// §4.C2, P7): a secret path prefix /s/<P>/ under which every page and API of the
// surface lives, a secret cookie value S, and a small table of one-time launch
// tokens. All of it lives in memory only and ends with the process. Several
// surfaces of one process (the desktop's dashboard and reader) share one
// Session, so the cookie a browser got from one is good on the other: a cookie
// for host 127.0.0.1 is sent to every port, and the path scope keeps it from any
// listener that cannot form /s/<P>/.
type Session struct {
	p      string // P: 16 random bytes, base32, the path prefix's secret segment
	secret string // S: 32 random bytes, the cookie value

	mu     sync.Mutex
	now    func() time.Time
	maxAge time.Duration
	tokens []*launchToken // oldest first, at most maxTokens
	opened int
	last   time.Time
	onOpen func(n int, at time.Time)
}

// launchToken is one launch token's table entry. Only sha256(T) is kept, never T.
type launchToken struct {
	hash     [sha256.Size]byte
	expires  time.Time
	used     bool
	marker   string        // M: 16 random bytes, hex, set when the token is redeemed
	redeemed chan struct{} // closed at redemption
}

// Link lifetimes: a token the process hands to the browser opener itself lives
// about a minute (an argv watcher on Linux or macOS has only that long to race
// it); a printed or copied link lives ten [CC5].
const (
	OpenerTTL = 60 * time.Second
	LinkTTL   = 10 * time.Minute
)

const (
	maxTokens     = 16
	maxCookies    = 8 // session cookies examined per request
	maxTokenBytes = 128

	// SessionCookie carries S, scoped to the prefix; MarkerCookie carries M, scoped
	// to /_open, so a browser that redeemed a link can present it again silently
	// [CC19].
	SessionCookie = "mailarchive_session"
	MarkerCookie  = "mailarchive_open"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b) // never fails: Go crashes the process if the system source does
	return b
}

// NewSession returns a session with a fresh prefix and secret.
func NewSession() *Session {
	return &Session{
		p:      strings.ToLower(b32.EncodeToString(randomBytes(16))),
		secret: base64.RawURLEncoding.EncodeToString(randomBytes(32)),
		now:    time.Now,
	}
}

// SetClock replaces the session's clock (tests).
func (s *Session) SetClock(now func() time.Time) {
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

// SetMaxAge gives the session cookie a Max-Age, so closing the browser does not
// end a LAN serve's session (one working day); loopback sessions have none.
func (s *Session) SetMaxAge(d time.Duration) {
	s.mu.Lock()
	s.maxAge = d
	s.mu.Unlock()
}

// OnOpen registers the notice made at each redemption: how many links were
// redeemed this run, counting this one, and when. serve and setup print it to
// stderr; the dashboard header shows the same count [CC24].
func (s *Session) OnOpen(f func(n int, at time.Time)) {
	s.mu.Lock()
	s.onOpen = f
	s.mu.Unlock()
}

// Opened reports how many links were redeemed this run and when the last was.
func (s *Session) Opened() (int, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened, s.last
}

// Prefix is the path every page and API of the session lives under: "/s/<P>/".
func (s *Session) Prefix() string { return "/s/" + s.p + "/" }

// Mint makes a one-time launch token valid for ttl and returns it (T, 32 random
// bytes, base64url). The table keeps its hash only, at most 16 of them: minting a
// 17th evicts the oldest.
func (s *Session) Mint(ttl time.Duration) string {
	t := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	e := &launchToken{hash: sha256.Sum256([]byte(t)), redeemed: make(chan struct{})}
	s.mu.Lock()
	defer s.mu.Unlock()
	e.expires = s.now().Add(ttl)
	s.tokens = append(s.tokens, e)
	if len(s.tokens) > maxTokens {
		s.tokens = append([]*launchToken(nil), s.tokens[len(s.tokens)-maxTokens:]...)
	}
	return t
}

// OpenPath is the path and query a browser opens to redeem token.
func OpenPath(token string) string { return "/_open?t=" + token }

// Link is a one-time link to the surface served at origin (scheme and host:port),
// valid for ttl.
func (s *Session) Link(origin string, ttl time.Duration) string {
	return strings.TrimSuffix(origin, "/") + OpenPath(s.Mint(ttl))
}

// WaitRedeemed reports whether token is redeemed within d (the handoff waits
// about 15 s for the browser it opened to arrive [CC20]).
func (s *Session) WaitRedeemed(tok string, d time.Duration) bool {
	h := sha256.Sum256([]byte(tok))
	s.mu.Lock()
	var ch chan struct{}
	for _, e := range s.tokens {
		if e.hash == h {
			ch = e.redeemed
		}
	}
	s.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	case <-time.After(d):
		return false
	}
}

// admits reports whether the request carries the session cookie: at least one of
// the first eight mailarchive_session cookies equals S, each compared in
// constant time.
func (s *Session) admits(r *http.Request) bool {
	n := 0
	ok := false
	for _, c := range r.Cookies() {
		if c.Name != SessionCookie {
			continue
		}
		if n++; n > maxCookies {
			break
		}
		if subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.secret)) == 1 {
			ok = true
		}
	}
	return ok
}

// inPrefix reports whether path is under /s/<P>/ (or is /s/<P> itself), the
// segment compared in constant time.
func (s *Session) inPrefix(path string) (under, exact bool) {
	rest, found := strings.CutPrefix(path, "/s/")
	if !found || len(rest) < len(s.p) {
		return false, false
	}
	if subtle.ConstantTimeCompare([]byte(rest[:len(s.p)]), []byte(s.p)) != 1 {
		return false, false
	}
	switch rest[len(s.p):] {
	case "":
		return true, true
	}
	return strings.HasPrefix(rest[len(s.p):], "/"), false
}

// open answers GET /_open?t=T [CC19]. An unknown token (not in this process's
// table) and an expired one each get their own 401 page; a used one presented by
// the browser that redeemed it (it carries that redemption's marker M) gets the
// Continue page again, silently; a used one without its marker gets the warning,
// the only case that can mean a raced link. A valid token is marked used and
// answered 200 — not a redirect, because the launch navigation arrives cross-site
// and a SameSite=Strict cookie is withheld from every hop of a cross-site
// redirect chain, while the meta refresh is our own page's navigation.
func (s *Session) open(w http.ResponseWriter, r *http.Request, surface string) {
	tok := r.URL.Query().Get("t")
	h := sha256.Sum256([]byte(tok))
	s.mu.Lock()
	var e *launchToken
	if tok != "" && len(tok) <= maxTokenBytes {
		for _, t := range s.tokens {
			if t.hash == h {
				e = t
			}
		}
	}
	if e == nil {
		s.mu.Unlock()
		lockedPage(w, surface, pageUnknownLink)
		return
	}
	if e.used {
		marker := e.marker
		s.mu.Unlock()
		if c, err := r.Cookie(MarkerCookie); err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(marker)) == 1 {
			s.continuePage(w, marker)
			return
		}
		lockedPage(w, surface, pageUsedLink)
		return
	}
	if s.now().After(e.expires) {
		s.mu.Unlock()
		lockedPage(w, surface, pageExpiredLink)
		return
	}
	e.used = true
	e.marker = hex.EncodeToString(randomBytes(16))
	marker := e.marker
	close(e.redeemed)
	s.opened++
	s.last = s.now()
	n, at, notify := s.opened, s.last, s.onOpen
	s.mu.Unlock()
	if notify != nil {
		notify(n, at)
	}
	s.continuePage(w, marker)
}

// continuePage sets the session and marker cookies and answers the 200 page whose
// meta refresh and Continue link go to the prefix.
func (s *Session) continuePage(w http.ResponseWriter, marker string) {
	s.mu.Lock()
	maxAge := s.maxAge
	s.mu.Unlock()
	sc := &http.Cookie{Name: SessionCookie, Value: s.secret, Path: s.Prefix(), HttpOnly: true, SameSite: http.SameSiteStrictMode}
	if maxAge > 0 {
		sc.MaxAge = int(maxAge / time.Second)
	}
	w.Header().Add("Set-Cookie", sc.String())
	w.Header().Add("Set-Cookie", (&http.Cookie{Name: MarkerCookie, Value: marker, Path: "/_open", HttpOnly: true, SameSite: http.SameSiteStrictMode}).String())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", pageCSP)
	prefix := html.EscapeString(s.Prefix())
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta http-equiv="refresh" content="0;url=%s">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>MailArchive</title>%s</head>
<body><main><h1>Opening MailArchive…</h1><p><a href="%s">Continue</a></p></main></body></html>
`, prefix, pageStyle, prefix)
}

// pageCSP lets a tool page style itself and load nothing else.
const pageCSP = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

const pageStyle = `<style>:root{color-scheme:light dark}body{margin:0;font:16px system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:#f3f5f8;color:#18212b}@media(prefers-color-scheme:dark){body{background:#0f1319;color:#eef2f6}}main{max-width:40rem;margin:12vh auto;padding:0 1.2rem;line-height:1.5}h1{font-size:1.4rem}code{font:.92em ui-monospace,SFMono-Regular,Consolas,monospace}</style>`

// The §7.6 texts a refused browser request is shown: what happened, why, and what
// to do, per surface (X12). None names an archive, a path, an account or P.
type pageKind int

const (
	pageLocked pageKind = iota
	pageUnknownLink
	pageExpiredLink
	pageUsedLink
)

// howToGetALink is the "do this" every link page ends with, per surface.
func howToGetALink(surface string) string {
	switch surface {
	case "dashboard", "reader":
		return `open MailArchive Desktop from its Start-menu or Desktop shortcut. If your browser does not open, use "Copy MailArchive link" in the Start menu.`
	case "setup":
		return `use the link <code>mailarchive setup</code> printed in its terminal, press Enter there for a new one, or start <code>mailarchive setup</code> again.`
	default:
		return `use the link <code>mailarchive serve</code> printed in its terminal, press Enter there for a new one, run <code>mailarchive serve -link -out &lt;folder&gt;</code> from any terminal of your account, or start <code>mailarchive serve -out &lt;folder&gt;</code> again.`
	}
}

// getANew is the shorter "get a new one" of the link pages.
func getANew(surface string) string {
	switch surface {
	case "dashboard", "reader":
		return `get a new one: open MailArchive Desktop from its shortcut, or use "Copy MailArchive link" in the Start menu.`
	case "setup":
		return `get a new one: press Enter in the <code>mailarchive setup</code> terminal, or start <code>mailarchive setup</code> again.`
	default:
		return `get a new one: press Enter in the <code>mailarchive serve</code> terminal, run <code>mailarchive serve -link -out &lt;folder&gt;</code>, or start <code>mailarchive serve</code> again.`
	}
}

// lockedPage answers a refused browser request with a 401 HTML page.
func lockedPage(w http.ResponseWriter, surface string, kind pageKind) {
	var title, why, todo string
	switch kind {
	case pageUnknownLink:
		title = "This link is from an earlier session."
		why = "each link works only while the program that made it runs."
		todo = getANew(surface)
	case pageExpiredLink:
		title = "This link has expired."
		why = "a link works for a short time after it is made."
		todo = getANew(surface)
	case pageUsedLink:
		title = "This link has already been used."
		why = "each link opens one browser once."
		todo = getANew(surface) + ` If you did not open it yourself, another account on this computer may have: stop MailArchive (Ctrl-C, or quit MailArchive Desktop) and start it again — that ends every session — and paste new links into the address bar instead of clicking them.`
	default:
		switch surface {
		case "dashboard", "reader":
			title = "This MailArchive Desktop page has to be opened from the app."
		case "setup":
			title = "This setup page needs its private link."
		default:
			title = "This archive reader needs its private link."
		}
		why = "each launch protects your dashboard and archive with a private link, so other accounts on this computer and web pages cannot open them."
		todo = howToGetALink(surface)
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", pageCSP)
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>MailArchive</title>%s</head>
<body><main><h1>%s</h1><p><b>Why:</b> %s</p><p><b>Do this:</b> %s</p></main></body></html>
`, pageStyle, title, why, todo)
}

// lockedJSON answers a refused API request with JSON and 401, never a page.
func lockedJSON(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	io.WriteString(w, `{"ok":false,"error":"This page needs its private link: open MailArchive from its shortcut, or use the link its terminal printed."}`+"\n")
}

// isAPIPath reports whether a refused request is an API call: its path ends in or
// contains /api/.
func isAPIPath(p string) bool { return strings.Contains(p, "/api/") || strings.HasSuffix(p, "/api") }

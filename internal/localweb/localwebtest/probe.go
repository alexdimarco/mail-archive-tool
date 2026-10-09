// Package localwebtest probes a local web surface the way the binaries' tests of
// MA-371 and MA-378 need: every route the surface registers, by name, with each
// Host given — through the production constructor's handler, never a copy of it.
package localwebtest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/localweb"
)

// RefusalPrefix starts the guard's Host refusal line (design §7.6).
const RefusalPrefix = "refused: this page is served only as http://127.0.0.1:"

var wildcard = regexp.MustCompile(`\{[^}]*\}`)

// Request builds a request that resolves to pattern on the Mux that recorded it:
// the pattern's own method when it names one (else method), and its path with
// each {wildcard} filled in and {$} dropped. A pattern that names a host cannot
// be probed by path, so it is an error.
func Request(method, pattern string) (*http.Request, error) {
	p := pattern
	if i := strings.IndexAny(p, " \t"); i >= 0 {
		method, p = p[:i], strings.TrimSpace(p[i+1:])
	}
	if !strings.HasPrefix(p, "/") {
		return nil, fmt.Errorf("pattern %q names a host; the probe resolves paths only", pattern)
	}
	p = strings.ReplaceAll(p, "{$}", "")
	p = wildcard.ReplaceAllStringFunc(p, func(w string) string {
		if strings.HasSuffix(w, "...}") {
			return "probe/x"
		}
		return "probe"
	})
	return httptest.NewRequest(method, p, nil), nil
}

// Probe describes the probe of one surface.
type Probe struct {
	// Surface is the localweb.Surface name the handler must carry.
	Surface string
	// Refused are Host values the guard must refuse on every route, for GET, POST
	// and HEAD alike.
	Refused []string
	// Passed are Host values that must get past the guard on every route.
	Passed []string
	// Forbid is text no refused response may contain: a subject, a path, a token.
	Forbid []string
	// Unchanged, when set, reports that no refused request changed anything.
	Unchanged func() bool
}

// policy is the header set every local-surface response carries; a CSP of the
// surface's own is checked only for presence.
var policy = map[string]string{
	"X-Content-Type-Options":       "nosniff",
	"Referrer-Policy":              "no-referrer",
	"Cache-Control":                "no-store",
	"Cross-Origin-Resource-Policy": "same-origin",
}

// Run probes h, which must be built by localweb.Wrap for p.Surface, on every route
// it names. A request built from each pattern must resolve back to that pattern
// (resolve by name). With each Refused Host, on GET, POST and HEAD, the guard must
// answer 403 with its refusal line, none of p.Forbid and no change (p.Unchanged);
// with each Passed Host the request must get past the guard. Every response must
// carry the policy headers. Run returns the patterns it probed: exactly the
// routes h names, in order.
func (p Probe) Run(t assure.TB, h http.Handler) []string {
	t.Helper()
	s, ok := localweb.Guarded(h)
	if !ok {
		t.Fatalf("%s: the production constructor returned %T, not a handler built by localweb.Wrap", p.Surface, h)
	}
	if s.Name != p.Surface {
		t.Fatalf("%s: the guard is for surface %q", p.Surface, s.Name)
	}
	routes := localweb.RoutesOf(h)
	if len(routes) == 0 {
		t.Fatalf("%s: the guarded handler names no routes — register them on a localweb.Mux", p.Surface)
	}
	var probed []string
	for _, pattern := range routes {
		req, err := Request(http.MethodGet, pattern)
		if err != nil {
			t.Fatalf("%s: %v", p.Surface, err)
		}
		if got := localweb.RouteOf(h, req); got != pattern {
			t.Fatalf("%s: a probe built for route %q (%s %s) reaches %q", p.Surface, pattern, req.Method, req.URL.Path, got)
		}
		target := req.URL.RequestURI()
		for _, host := range p.Refused {
			for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
				rec := send(h, method, target, host)
				what := fmt.Sprintf("%s: %s %s with Host %q", p.Surface, method, target, host)
				assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusForbidden),
					assure.Names(RefusalPrefix, "open it from MailArchive"),
					assure.Forbid(p.Forbid...),
					assure.NoSideEffect(p.Unchanged))
				checkPolicy(t, rec, what)
			}
		}
		for _, host := range p.Passed {
			rec := send(h, req.Method, target, host)
			what := fmt.Sprintf("%s: %s %s with Host %q", p.Surface, req.Method, target, host)
			if strings.HasPrefix(rec.Body.String(), RefusalPrefix) {
				t.Fatalf("%s was refused by the guard; a Host the surface accepts must pass", what)
			}
			checkPolicy(t, rec, what)
		}
		probed = append(probed, pattern)
	}
	return probed
}

func send(h http.Handler, method, target, host string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func checkPolicy(t assure.TB, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("%s: response has no Content-Security-Policy", what)
	}
	for k, want := range policy {
		if got := rec.Header().Get(k); got != want {
			t.Fatalf("%s: %s = %q, want %q", what, k, got, want)
		}
	}
}

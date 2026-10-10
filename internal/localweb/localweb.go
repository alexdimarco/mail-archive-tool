// Package localweb is the one guard every local web surface is served through —
// `mailarchive serve`, `mailarchive setup`, and MailArchive Desktop's dashboard and
// archive reader (design-local-confidentiality §4.C1, P6). Wrap checks the
// request's Host first, on every route and method, so a DNS-rebinding page (whose
// Host carries the attacker's name) is refused before any handler runs, and it
// stamps the policy headers onto every response, errors and redirects included.
// Routes are registered through Mux, a recording ServeMux, so a structural test
// can probe every route a binary serves by name (MA-378). Sessions (P7) are a
// later slice's.
package localweb

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// Surface names one local web surface and what its Host check accepts.
type Surface struct {
	// Name identifies the surface: "serve", "setup", "dashboard" or "reader".
	Name string
	// Listener is the address the surface is bound to; the refusal names its port.
	Listener net.Addr
	// ExtraHosts are host names or IP literals accepted, with any port, beyond
	// 127.0.0.1, localhost and [::1]. Only a `serve` bound to a non-loopback
	// address has -allow-host names; its IP literals are accepted there too, and a
	// `serve` on a loopback address other than 127.0.0.1 or ::1 accepts its own
	// literal. An IPv6 literal
	// is written bracketed, as a Host header carries it ("[2001:db8::1]").
	ExtraHosts []string
}

// denyCSP is the policy a response gets when its handler set none (the Host
// refusal itself, a bare 503): nothing may load, nothing may frame it.
const denyCSP = "default-src 'none'; frame-ancestors 'none'"

// Wrap is the only way a production binary serves a handler. Every request,
// whatever its route and method, first passes the Host allow-list; one that
// does not is refused 403 with one fixed plain-text line and nothing else, and
// inner never sees it. Every response — the refusal, and whatever inner writes,
// errors and redirects included — carries the policy headers (setPolicy).
func Wrap(s Surface, inner http.Handler) http.Handler {
	g := &guard{s: s, inner: inner, extra: map[string]bool{}}
	for _, h := range s.ExtraHosts {
		g.extra[strings.ToLower(h)] = true
	}
	return g
}

type guard struct {
	s     Surface
	inner http.Handler
	extra map[string]bool
}

func (g *guard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pw := &policyWriter{ResponseWriter: w}
	if !g.allowed(r.Host) {
		g.refuse(pw)
		return
	}
	g.inner.ServeHTTP(pw, r)
	// A handler that wrote nothing gets net/http's own empty 200, written on the
	// underlying writer after it returns, past the policyWriter: write that 200
	// here instead, so it carries the policy headers like every other response.
	if !pw.wrote {
		pw.WriteHeader(http.StatusOK)
	}
}

// allowed reports whether host, a request's Host, names this machine as the
// surface is served: 127.0.0.1, localhost or [::1], or one of the surface's extra
// hosts, with a port — any port, so a forwarded local port (ssh -L
// 9000:127.0.0.1:8099) works. The rebinding defence rests on the name, which a
// rebinding page cannot make one of these, not on the port [CC18].
func (g *guard) allowed(host string) bool {
	name, ok := hostName(host)
	if !ok {
		return false
	}
	switch name {
	case "127.0.0.1", "localhost", "[::1]":
		return true
	}
	return g.extra[name]
}

// hostName returns a Host header's name, lower-cased, after checking its shape:
// a port is required (1–65535, digits only), and an empty name, a trailing dot,
// userinfo, an IPv6 zone and anything else a host name cannot hold are refused.
func hostName(host string) (string, bool) {
	host = strings.ToLower(host)
	i := strings.LastIndexByte(host, ':')
	if i <= 0 {
		return "", false
	}
	name, port := host[:i], host[i+1:]
	if !validPort(port) {
		return "", false
	}
	if strings.HasPrefix(name, "[") {
		if !strings.HasSuffix(name, "]") || strings.ContainsAny(name[1:len(name)-1], "[]") {
			return "", false
		}
	} else if strings.ContainsAny(name, ":[]") {
		return "", false
	}
	if strings.ContainsAny(name, "@/\\% ") || strings.HasSuffix(name, ".") {
		return "", false
	}
	return name, true
}

// ExtraHost normalizes a host name or IP literal for Surface.ExtraHosts — lower
// case, an IPv6 literal bracketed as a Host header carries it — and reports false
// for anything a Host header's name cannot be: empty, with a port, userinfo, a
// path, an IPv6 zone or a trailing dot.
func ExtraHost(name string) (string, bool) {
	h := strings.ToLower(strings.TrimSpace(name))
	if ip := net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")); ip != nil && ip.To4() == nil {
		h = "[" + ip.String() + "]"
	}
	if n, ok := hostName(h + ":1"); !ok || n != h {
		return "", false
	}
	return h, true
}

func validPort(p string) bool {
	if p == "" || len(p) > 5 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	n, _ := strconv.Atoi(p)
	return n >= 1 && n <= 65535
}

// refuse answers a request whose Host is not one of the surface's: 403, one
// line naming the address the page is served on and the remedy, no other
// content (design §7.6, "Host refusal").
func (g *guard) refuse(w http.ResponseWriter) {
	addr := "http://127.0.0.1/"
	if p := listenPort(g.s.Listener); p != "" {
		addr = "http://127.0.0.1:" + p + "/"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	io.WriteString(w, "refused: this page is served only as "+addr+" — open it from MailArchive\n")
}

func listenPort(a net.Addr) string {
	if a == nil {
		return ""
	}
	if t, ok := a.(*net.TCPAddr); ok {
		if t.Port > 0 {
			return strconv.Itoa(t.Port)
		}
		return ""
	}
	if _, p, err := net.SplitHostPort(a.String()); err == nil {
		return p
	}
	return ""
}

// setPolicy stamps the headers every local-surface response carries [§4.C1]:
// the surface's own CSP, or denyCSP when its handler set none; nosniff;
// no-referrer; no-store, so a page opened in the reader is not kept in the
// browser's disk cache (P14); and same-origin CORP, so no other origin can
// embed a response.
func setPolicy(h http.Header) {
	if h.Get("Content-Security-Policy") == "" {
		h.Set("Content-Security-Policy", denyCSP)
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}

// policyWriter applies setPolicy the moment a status is written, so the headers
// reach every answer whatever the handler set or removed before — net/http's
// own error and redirect paths included (http.FileServer's error path deletes
// Cache-Control just before it writes).
type policyWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *policyWriter) WriteHeader(code int) {
	setPolicy(w.Header())
	if code >= 200 || code == http.StatusSwitchingProtocols {
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *policyWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Flush lets a handler stream through the guard.
func (w *policyWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *policyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Routes names the routes of the guarded handler (those of its Mux).
func (g *guard) Routes() []string {
	if r, ok := g.inner.(Router); ok {
		return r.Routes()
	}
	return nil
}

// Route returns the pattern req reaches inside the guard ("" when the guarded
// handler names no routes, or none matches).
func (g *guard) Route(req *http.Request) string {
	if r, ok := g.inner.(Router); ok {
		return r.Route(req)
	}
	return ""
}

// Guarded reports whether h was built by Wrap, and for which surface, so a test
// can refuse a production constructor whose handler skips the guard (MA-378).
func Guarded(h http.Handler) (Surface, bool) {
	g, ok := h.(*guard)
	if !ok {
		return Surface{}, false
	}
	return g.s, true
}

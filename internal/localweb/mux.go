package localweb

import "net/http"

// Mux is the route registry of a local web surface: an http.ServeMux that also
// records every pattern registered on it, so the binaries' structural test can
// probe each route by name (MA-378). It is the only mux production code builds —
// the same test refuses http.NewServeMux and the DefaultServeMux anywhere else —
// so no route escapes the record [CC15]. The inner handlers stay testable
// unwrapped; the binaries put Wrap around them.
type Mux struct {
	mux      *http.ServeMux
	patterns []string
}

// NewMux returns an empty route registry.
func NewMux() *Mux { return &Mux{mux: http.NewServeMux()} }

// Handle registers h for pattern (http.ServeMux's pattern syntax) and records it.
func (m *Mux) Handle(pattern string, h http.Handler) {
	m.mux.Handle(pattern, h)
	m.patterns = append(m.patterns, pattern)
}

// HandleFunc registers f for pattern and records it.
func (m *Mux) HandleFunc(pattern string, f func(http.ResponseWriter, *http.Request)) {
	m.Handle(pattern, http.HandlerFunc(f))
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) { m.mux.ServeHTTP(w, r) }

// Routes returns every registered pattern, in registration order.
func (m *Mux) Routes() []string { return append([]string(nil), m.patterns...) }

// Route returns the pattern r resolves to ("" when none matches).
func (m *Mux) Route(r *http.Request) string {
	_, pattern := m.mux.Handler(r)
	return pattern
}

// Router is a handler that names its routes: a Mux, or a handler built over one
// (Routed, Wrap). The structural test reads the patterns from it and resolves a
// probe request to the pattern it reaches.
type Router interface {
	http.Handler
	Routes() []string
	Route(r *http.Request) string
}

// RoutesOf returns the routes h names, or nil when it names none.
func RoutesOf(h http.Handler) []string {
	if r, ok := h.(Router); ok {
		return r.Routes()
	}
	return nil
}

// RouteOf returns the pattern req reaches in h, or "" when h names no routes.
func RouteOf(h http.Handler, req *http.Request) string {
	if r, ok := h.(Router); ok {
		return r.Route(req)
	}
	return ""
}

// Routed returns h — a surface's own header middleware around the Mux r — still
// naming r's routes, so the guard and the route probe see through it.
func Routed(h http.Handler, r Router) Router { return routed{Handler: h, r: r} }

type routed struct {
	http.Handler
	r Router
}

func (x routed) Routes() []string               { return x.r.Routes() }
func (x routed) Route(req *http.Request) string { return x.r.Route(req) }

// Package server exposes the search index as a small local web app: a search +
// reader UI backed by JSON endpoints, plus a file server for the exported HTML
// and attachment archives.
package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/localweb"
)

// New returns an http.Handler serving the UI, JSON API, and exported files
// rooted at outDir, querying ix. Its routes are registered on a localweb.Mux, so
// the binaries' guard and route probe name them (design-local-confidentiality
// CC15); `serve` and the desktop reader serve it behind localweb.Wrap.
func New(outDir string, ix *index.Index) http.Handler {
	mux := localweb.NewMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageHTML))
	})
	// The UI's script is a same-origin file, never inline, so the UI can run
	// under script-src 'self' (R19).
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write([]byte(appJS))
	})

	// The go-back (point-in-time) view: current mailbox and any ?at=<date>,
	// rendered server-side over the manifest + folded history, intersected with
	// the files on disk (design §3.4). It carries no script (see gobackCSP).
	gb := &gobackServer{outDir: outDir, ix: ix}
	mux.HandleFunc("/goback", gb.serveGoback)

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := parseQuery(r)
		results, total, err := ix.Search(q)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"total":   total,
			"results": results,
			"limit":   q.Limit,
			"offset":  q.Offset,
		})
	})

	mux.HandleFunc("/api/facets", func(w http.ResponseWriter, r *http.Request) {
		folders, err := ix.Folders()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		years, _ := ix.Years()
		count, _ := ix.Count()
		writeJSON(w, map[string]any{"folders": folders, "years": years, "total": count})
	})

	// Serve the exported pages, .eml files and attachment zips — and nothing else
	// the archive holds — through an os.Root that never resolves outside the
	// archive root and never follows a link (R4/R19, design-local-confidentiality
	// P9); the policy headers are applied by secureHeaders below.
	files := http.StripPrefix("/files/", pinFileTypes(http.FileServer(allowedFiles{dir: outDir})))
	mux.Handle("/files/", files)

	return localweb.Routed(secureHeaders(mux), mux)
}

const (
	// uiCSP governs the search UI itself: its own same-origin script and API
	// calls, inline styles, data: images; nothing remote, never framed.
	uiCSP = "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
	// apiCSP: JSON is never a document; deny everything, never framed.
	apiCSP = "default-src 'none'; frame-ancestors 'none'"
	// gobackCSP governs the point-in-time page: it is fully server-rendered and
	// carries NO script at all (default-src 'none' blocks script; only inline
	// styles are allowed), so a hostile folder name or subject folded into it can
	// never execute even if escaping regressed (R19, defence in depth).
	gobackCSP = "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
)

// secureHeaders sets a Content-Security-Policy on every response — the archive
// policy (identical to the meta every exported file carries) for archived
// files, so a crafted email renders inertly instead of executing in the local
// server's origin; the UI/API policies for the tool's own pages — plus nosniff
// and no-referrer.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		switch {
		case strings.HasPrefix(r.URL.Path, "/files/"):
			h.Set("Content-Security-Policy", export.ArchiveCSP)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			h.Set("Content-Security-Policy", apiCSP)
		case r.URL.Path == "/goback":
			h.Set("Content-Security-Policy", gobackCSP)
		default:
			h.Set("Content-Security-Policy", uiCSP)
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// IsLoopback reports whether addr (host:port) binds only to this machine —
// "localhost" or a loopback IP. An empty host (":8099") binds every interface,
// so it is NOT loopback: `serve` has no authentication, and anything else
// exposes the whole archive to the network.
func IsLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// parseQuery builds an index.Query from the request. The inline tokens in the
// `q` box (from:, folder:, after:, before:, has:attach) are parsed by the shared
// index.ParseQuery — the same grammar the terminal `search` verb uses — over the
// explicit folder/sender/sort params; the explicit after/before/year/attach
// params then apply on top.
func parseQuery(r *http.Request) index.Query {
	v := r.URL.Query()
	q := index.ParseQuery(v.Get("q"), index.Query{
		Folder: v.Get("folder"),
		Sender: v.Get("sender"),
		Sort:   v.Get("sort"),
	})

	if t, ok := index.ParseDate(v.Get("after")); ok {
		q.After = t
	}
	if t, ok := index.ParseDate(v.Get("before")); ok {
		q.Before = t
	}
	if v.Get("attach") == "1" || v.Get("attach") == "true" {
		q.HasAttach = true
	}
	if year := v.Get("year"); year != "" {
		if y, err := strconv.Atoi(year); err == nil {
			q.After = time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
			q.Before = time.Date(y+1, 1, 1, 0, 0, 0, 0, time.UTC)
		}
	}

	q.Limit = atoiDefault(v.Get("limit"), 50)
	if q.Limit > 200 {
		q.Limit = 200
	}
	q.Offset = atoiDefault(v.Get("offset"), 0)
	return q
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return n
	}
	return def
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

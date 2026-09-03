// Package server exposes the search index as a small local web app: a search +
// reader UI backed by JSON endpoints, plus a file server for the exported HTML
// and attachment archives.
package server

import (
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
)

// New returns an http.Handler serving the UI, JSON API, and exported files
// rooted at outDir, querying ix.
func New(outDir string, ix *index.Index) http.Handler {
	mux := http.NewServeMux()

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

	// Serve the exported HTML files and attachment zips from a file system that
	// never resolves outside the archive root (R4/R19); the policy headers are
	// applied by secureHeaders below.
	files := http.StripPrefix("/files/", http.FileServer(newContainedDir(outDir)))
	mux.Handle("/files/", files)

	return secureHeaders(mux)
}

const (
	// uiCSP governs the search UI itself: its own same-origin script and API
	// calls, inline styles, data: images; nothing remote, never framed.
	uiCSP = "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
	// apiCSP: JSON is never a document; deny everything, never framed.
	apiCSP = "default-src 'none'; frame-ancestors 'none'"
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
		default:
			h.Set("Content-Security-Policy", uiCSP)
		}
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// containedDir is an http.FileSystem rooted at a directory that refuses any
// path whose resolved location (symlinks followed) lies outside that root, and
// that never lists a directory: a directory is reachable only through its own
// index.html (the archive's pages are the navigation). http.Dir alone blocks
// ".." in the URL but happily follows an on-disk symlink out of the root.
type containedDir struct {
	root string // as given
	real string // symlink-resolved, absolute
}

func newContainedDir(root string) containedDir {
	real := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		real = r
	}
	if a, err := filepath.Abs(real); err == nil {
		real = a
	}
	return containedDir{root: root, real: filepath.Clean(real)}
}

func (c containedDir) Open(name string) (http.File, error) {
	rel := filepath.FromSlash(path.Clean("/" + name))
	resolved, err := filepath.EvalSymlinks(filepath.Join(c.root, rel))
	if err != nil {
		return nil, err // a missing file surfaces as fs.ErrNotExist → 404
	}
	if a, err := filepath.Abs(resolved); err == nil {
		resolved = a
	}
	if !within(c.real, resolved) {
		return nil, fs.ErrNotExist
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		if _, err := os.Stat(filepath.Join(resolved, "index.html")); err != nil {
			f.Close()
			return nil, fs.ErrNotExist
		}
	}
	return f, nil
}

// within reports whether p is root or lies beneath it (case-insensitively on
// Windows, whose file systems are).
func within(root, p string) bool {
	if runtime.GOOS == "windows" {
		root, p = strings.ToLower(root), strings.ToLower(p)
	}
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
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

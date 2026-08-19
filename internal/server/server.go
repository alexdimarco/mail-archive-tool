// Package server exposes the search index as a small local web app: a search +
// reader UI backed by JSON endpoints, plus a file server for the exported HTML
// and attachment archives.
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

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

	// Serve the exported HTML files and attachment zips, under a strict CSP so
	// archived mail can't run scripts or phone home when viewed here (R4).
	files := http.StripPrefix("/files/", http.FileServer(http.Dir(outDir)))
	mux.Handle("/files/", secureFileHeaders(files))

	return mux
}

// secureFileHeaders wraps the archived-file server with a Content-Security-Policy
// that blocks scripts and all remote loads (tracking pixels included), while
// still allowing the inline styles and data: images real mail uses. A crafted
// email's HTML therefore renders inertly instead of executing in the local
// server's origin.
func secureFileHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// parseQuery builds an index.Query from the request, honouring both explicit
// params and inline tokens (from:, folder:, after:, before:, has:attach) in q.
func parseQuery(r *http.Request) index.Query {
	v := r.URL.Query()
	q := index.Query{
		Folder: v.Get("folder"),
		Sender: v.Get("sender"),
		Sort:   v.Get("sort"),
	}

	var terms []string
	for _, f := range strings.Fields(v.Get("q")) {
		low := strings.ToLower(f)
		switch {
		case strings.HasPrefix(low, "from:"):
			q.Sender = f[len("from:"):]
		case strings.HasPrefix(low, "folder:"):
			q.Folder = f[len("folder:"):]
		case strings.HasPrefix(low, "after:"):
			if t, ok := parseDate(f[len("after:"):]); ok {
				q.After = t
			}
		case strings.HasPrefix(low, "before:"):
			if t, ok := parseDate(f[len("before:"):]); ok {
				q.Before = t
			}
		case low == "has:attach" || low == "has:attachment":
			q.HasAttach = true
		default:
			terms = append(terms, f)
		}
	}
	q.Text = strings.Join(terms, " ")

	if t, ok := parseDate(v.Get("after")); ok {
		q.After = t
	}
	if t, ok := parseDate(v.Get("before")); ok {
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

func parseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	return time.Time{}, false
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

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
)

// covers: MA-796, R19
// The reader only reads (design-encrypted-archives-phase3 §7.1, CC15): every route
// answers GET and HEAD, and any other method is 405 with Allow: GET, HEAD — for a
// plaintext folder as for an encrypted archive.
func TestTheReaderOnlyReads(t *testing.T) {
	dir := tmpDir(t)
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	os.MkdirAll(filepath.Join(dir, "store"), 0o755)
	os.WriteFile(filepath.Join(dir, "store", "x.html"), []byte("<p>hi</p>"), 0o644)
	ts := httptest.NewServer(New(dir, ix))
	defer ts.Close()
	for _, p := range []string{"/", "/api/search?q=hi", "/api/facets", "/goback", "/files/store/x.html"} {
		for _, m := range []string{http.MethodGet, http.MethodHead} {
			req, _ := http.NewRequest(m, ts.URL+p, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("positive twin: %s %s: %d", m, p, resp.StatusCode)
			}
		}
		for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			req, _ := http.NewRequest(m, ts.URL+p, strings.NewReader("x"))
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD" {
				t.Errorf("%s %s: %d, Allow %q", m, p, resp.StatusCode, resp.Header.Get("Allow"))
			}
		}
	}
	assure.Reached(t, true, "every route and method judged")
}

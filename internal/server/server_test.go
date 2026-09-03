package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// tmpDir is a temp directory with best-effort cleanup: modernc SQLite can hold
// the search.db file briefly past Close on Windows, so a cleanup failure there
// must not fail an otherwise-passing test (the process/OS reclaims it).
func tmpDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "srvtest")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// covers: MA-38, R4
// Archived mail is served under a CSP that blocks scripts and remote loads, so
// a malicious email cannot execute or phone home when viewed through /files/.
func TestFileServerSetsCSP(t *testing.T) {
	dir := tmpDir(t)
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if err := os.MkdirAll(filepath.Join(dir, "store"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store", "x.html"), []byte("<p>hi</p>"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(dir, ix))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/files/store/x.html")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("archived mail served with no Content-Security-Policy")
	}
	if !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP must block scripts and remote loads, got %q", csp)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options: nosniff")
	}
}

// covers: MA-25
func TestServerEndpoints(t *testing.T) {
	dir := tmpDir(t)
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	m := &model.Message{
		Subject:     "Acme invoice",
		SenderName:  "Bob",
		SenderEmail: "bob@example.com",
		To:          "me@example.com",
		Received:    time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC),
		HTMLBody:    "<p>The revised invoice for Acme is attached.</p>",
		Attachments: []model.Attachment{{Filename: "invoice.pdf"}},
	}
	if err := ix.Add("store", []string{"Inbox"}, m, "store/Inbox/x.html", "Inbox\x00id1"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(New(dir, ix))
	defer ts.Close()

	// Page.
	if resp, err := http.Get(ts.URL + "/"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET /: %v status=%v", err, resp.StatusCode)
	}

	// Search.
	var out struct {
		Total   int            `json:"total"`
		Results []index.Result `json:"results"`
	}
	resp, err := http.Get(ts.URL + "/api/search?q=invoice")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if out.Total != 1 || len(out.Results) != 1 || out.Results[0].Subject != "Acme invoice" {
		t.Fatalf("search: total=%d results=%+v", out.Total, out.Results)
	}
	if !out.Results[0].HasAttach {
		t.Error("expected hasAttach true")
	}

	// Attachment filter that excludes nothing here, then a sender token.
	resp2, err := http.Get(ts.URL + "/api/search?q=from:bob%20invoice")
	if err != nil {
		t.Fatal(err)
	}
	var out2 struct {
		Total int `json:"total"`
	}
	json.NewDecoder(resp2.Body).Decode(&out2)
	resp2.Body.Close()
	if out2.Total != 1 {
		t.Errorf("from:bob invoice total = %d, want 1", out2.Total)
	}

	// Facets.
	if resp, err := http.Get(ts.URL + "/api/facets"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET /api/facets: %v status=%v", err, resp.StatusCode)
	}
}

// serveArchive builds a tiny live archive (index + one exported file) and
// returns a test server over it plus the archive dir.
func serveArchive(t *testing.T, body string) (*httptest.Server, string) {
	t.Helper()
	dir := tmpDir(t)
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	// The body is indexed as PLAIN text so literal markup in it reaches the
	// snippet path verbatim (an HTML body has its tags stripped at index time).
	m := &model.Message{
		Subject:   "Hostile <b>subject</b>",
		Received:  time.Date(2025, 7, 3, 9, 0, 0, 0, time.UTC),
		PlainBody: body,
	}
	if err := os.MkdirAll(filepath.Join(dir, "store", "Inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "store", "Inbox", "x.html"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ix.Add("store", []string{"Inbox"}, m, "store/Inbox/x.html", "Inbox\x00id1"); err != nil {
		t.Fatal(err)
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(dir, ix))
	t.Cleanup(ts.Close)
	return ts, dir
}

func getBody(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 64*1024)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, b.String()
}

// covers: MA-81, R19, R8, S22
// A message body containing markup must reach the search UI as escaped text:
// the snippet carries the tool's own <mark> highlights and nothing else live,
// in FTS mode (snippet() over the body) and in browse mode (the stored preview).
func TestSnippetsAreEscaped(t *testing.T) {
	ts, _ := serveArchive(t, `<p>invoice <script>alert(1)</script> &amp; <img src=x onerror=alert(2)> total</p>`)

	var out struct {
		Results []index.Result `json:"results"`
	}
	for _, q := range []string{"?q=invoice", "?q="} { // FTS mode, then browse mode
		resp, body := getBody(t, ts.URL+"/api/search"+q)
		if resp.StatusCode != 200 {
			t.Fatalf("%s: status %d", q, resp.StatusCode)
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Results) != 1 {
			t.Fatalf("%s: results = %d, want 1", q, len(out.Results))
		}
		snip := out.Results[0].Snippet
		if snip == "" {
			t.Fatalf("%s: empty snippet", q)
		}
		if strings.Contains(snip, "<script") || strings.Contains(snip, "<img") || strings.Contains(snip, "<p>") {
			t.Errorf("%s: live markup in snippet: %q", q, snip)
		}
		if !strings.Contains(snip, "&lt;script") && !strings.Contains(snip, "&lt;img") {
			t.Errorf("%s: markup should be escaped, not dropped: %q", q, snip)
		}
		if q == "?q=invoice" && !strings.Contains(snip, "<mark>invoice</mark>") {
			t.Errorf("FTS snippet lost its highlight: %q", snip)
		}
	}
}

// covers: MA-82, R19, R4, S22
// The file server never follows a symlink out of the archive root, and does
// not list directories that have no index.html (the archive's own pages are
// the navigation).
func TestFileServerConfinement(t *testing.T) {
	ts, dir := serveArchive(t, "<p>hi</p>")

	secret := filepath.Join(tmpDir(t), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "store", "leak.txt")); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	if err := os.Symlink(filepath.Dir(secret), filepath.Join(dir, "store", "leakdir")); err != nil {
		t.Fatal(err)
	}

	// Positive twin first: a real archived file is served.
	if resp, body := getBody(t, ts.URL+"/files/store/Inbox/x.html"); resp.StatusCode != 200 || !strings.Contains(body, "hi") {
		t.Fatalf("real file: status %d body %q", resp.StatusCode, body)
	}
	// A symlinked file and a symlinked directory resolve outside the root → 404.
	for _, p := range []string{"/files/store/leak.txt", "/files/store/leakdir/secret.txt"} {
		resp, body := getBody(t, ts.URL+p)
		if resp.StatusCode != http.StatusNotFound || strings.Contains(body, "TOP-SECRET") {
			t.Errorf("%s: status %d body %q — symlink escaped the archive root", p, resp.StatusCode, body)
		}
	}
	// A directory without index.html is not listed.
	if resp, body := getBody(t, ts.URL+"/files/store/"); resp.StatusCode != http.StatusNotFound || strings.Contains(body, "Inbox") {
		t.Errorf("directory listing served: status %d body %q", resp.StatusCode, body)
	}
	// A directory with index.html serves it.
	if err := os.WriteFile(filepath.Join(dir, "store", "Inbox", "index.html"), []byte("<p>folder page</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if resp, body := getBody(t, ts.URL+"/files/store/Inbox/"); resp.StatusCode != 200 || !strings.Contains(body, "folder page") {
		t.Errorf("folder index not served: status %d body %q", resp.StatusCode, body)
	}
}

// covers: MA-83, R19, R4, S22
// Every served surface carries a strict policy: archived files get the archive
// CSP (identical to the exported meta), the UI gets script-src 'self' (no inline
// script in the page), the API is non-embeddable; all are nosniff.
func TestServedSurfacesCarryPolicy(t *testing.T) {
	ts, _ := serveArchive(t, "<p>hi</p>")

	resp, page := getBody(t, ts.URL+"/")
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("UI CSP = %q", csp)
	}
	if strings.Contains(page, "<script>") {
		t.Error("UI page carries an inline <script>; it must load its script from a same-origin file")
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("UI missing nosniff")
	}
	if resp, js := getBody(t, ts.URL+"/app.js"); resp.StatusCode != 200 || !strings.Contains(js, "fetch(") {
		t.Errorf("/app.js not served: status %d", resp.StatusCode)
	}

	resp, _ = getBody(t, ts.URL+"/api/facets")
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'none'") || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("API headers: csp=%q nosniff=%q", resp.Header.Get("Content-Security-Policy"), resp.Header.Get("X-Content-Type-Options"))
	}

	resp, _ = getBody(t, ts.URL+"/files/store/Inbox/x.html")
	if got := resp.Header.Get("Content-Security-Policy"); got != export.ArchiveCSP {
		t.Errorf("file CSP %q must equal the exported meta policy %q", got, export.ArchiveCSP)
	}
}

// covers: MA-84, R19, S22
// IsLoopback classifies bind addresses: only localhost and loopback IPs are
// private to this machine; an empty host binds every interface.
func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8099": true, "localhost:8099": true, "[::1]:8099": true, "LOCALHOST:1": true,
		":8099": false, "0.0.0.0:8099": false, "192.168.1.10:8099": false, "[::]:8099": false, "example.com:80": false,
	}
	for addr, want := range cases {
		if got := IsLoopback(addr); got != want {
			t.Errorf("IsLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

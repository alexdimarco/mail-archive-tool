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

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// covers: MA-38, R4
// Archived mail is served under a CSP that blocks scripts and remote loads, so
// a malicious email cannot execute or phone home when viewed through /files/.
func TestFileServerSetsCSP(t *testing.T) {
	dir := t.TempDir()
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
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
	dir := t.TempDir()
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
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

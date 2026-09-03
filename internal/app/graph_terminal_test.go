package app

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// newFakeGraphServerWithEmpty serves mailbox u2 with one normal message (M1) and
// one whose MIME has no body at all (M4 — a read receipt / meeting response),
// recording $value fetches like newFakeGraphServer.
func newFakeGraphServerWithEmpty() (*fakeGraphServer, *httptest.Server) {
	f := &fakeGraphServer{mimeHits: map[string]int{}}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(s))
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u2/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
	})
	mux.HandleFunc("/users/u2/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[
			{"id":"M1","internetMessageId":"<m1@x>","receivedDateTime":"2025-03-01T09:00:00Z"},
			{"id":"M4","internetMessageId":"<m4@x>","receivedDateTime":"2025-03-04T09:00:00Z"}]}`)
	})
	mux.HandleFunc("/users/u2/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u2/messages/"), "/$value")
		f.mu.Lock()
		f.mimeHits[id]++
		f.mu.Unlock()
		body := "body\r\n"
		if id == "M4" {
			body = ""
		}
		w.Write([]byte("From: a@example.com\r\nSubject: subj-" + id +
			"\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\n" + body))
	})
	return f, httptest.NewServer(mux)
}

// covers: MA-71, R17, R1, R2
// Graph is complete-at-fetch, so a message captured with no body is recorded as
// a TERMINAL gap (reported as source-empty), and an incremental re-run neither
// re-downloads it nor counts it as fillable — R17 holds unchanged. A legacy
// ("unknown") record from a Graph archive is resolved without a download.
func TestGraphGapsAreTerminalAndNeverRefetched(t *testing.T) {
	out := tmpDir(t)
	f, srv := newFakeGraphServerWithEmpty()
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u2"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	opts := Options{Out: out, Mode: export.Incremental, Index: true}
	logger := log.New(io.Discard, "", 0)

	r1, err := RunGraph(context.Background(), g, opts, logger)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Stats.Exported != 2 || r1.Terminal != 1 || r1.Fillable != 0 {
		t.Fatalf("run 1: exported=%d terminal=%d fillable=%d, want 2/1/0", r1.Stats.Exported, r1.Terminal, r1.Fillable)
	}
	rep, err := os.ReadFile(filepath.Join(out, "attachments-report.tsv"))
	if err != nil || !strings.Contains(string(rep), "terminal") || !strings.Contains(string(rep), "missing-body") {
		t.Errorf("report lacks the terminal row: %v\n%s", err, rep)
	}
	// The body gap's detail column reads for a person, not as the bare token
	// "body" (friction #11).
	if !strings.Contains(string(rep), "(message body)") {
		t.Errorf("report renders the body gap as the bare token, not \"(message body)\":\n%s", rep)
	}
	first := f.hits()

	r2, err := RunGraph(context.Background(), g, opts, logger)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Stats.Exported != 0 || r2.Stats.SkippedManifest != 2 || r2.Stats.Retried != 0 {
		t.Errorf("run 2 stats = %+v, want exported=0 skipped=2 retried=0", r2.Stats)
	}
	for id, n := range f.hits() {
		if n != first[id] {
			t.Errorf("message %s re-downloaded on incremental run (%d→%d)", id, first[id], n)
		}
	}

	// Legacy sentinel on a Graph archive: resolved without a download.
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	// Find M1's record by its Message-ID (the parser strips the angle brackets).
	var key string
	var rec state.Record
	for k, r := range m.Entries {
		if strings.Contains(k, "m1@x") {
			key, rec = k, r
		}
	}
	if key == "" {
		t.Fatalf("no record for m1@x among %d entries", len(m.Entries))
	}
	rec.Missing = []string{"unknown"}
	m.Add(key, rec)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	r3, err := RunGraph(context.Background(), g, opts, logger)
	if err != nil {
		t.Fatal(err)
	}
	if r3.Stats.Resolved != 1 || r3.Unknown != 0 {
		t.Errorf("run 3: resolved=%d unknown=%d, want 1/0", r3.Stats.Resolved, r3.Unknown)
	}
	for id, n := range f.hits() {
		if n != first[id] {
			t.Errorf("sentinel resolution downloaded %s (%d→%d)", id, first[id], n)
		}
	}
}

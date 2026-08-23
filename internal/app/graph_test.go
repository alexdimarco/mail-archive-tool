package app

import (
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/export"
)

// fakeGraphServer serves the token + a small "u1" mailbox (Inbox: M1,M2;
// Archive: M3), recording how many times each message's $value is fetched — so a
// test can prove incremental runs don't re-download already-archived messages.
type fakeGraphServer struct {
	mu       sync.Mutex
	mimeHits map[string]int
}

func newFakeGraphServer() (*fakeGraphServer, *httptest.Server) {
	f := &fakeGraphServer{mimeHits: map[string]int{}}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(s))
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0},{"id":"F_AR","displayName":"Archive","childFolderCount":0}]}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[
			{"id":"M1","internetMessageId":"<m1@x>","receivedDateTime":"2025-03-01T09:00:00Z"},
			{"id":"M2","internetMessageId":"<m2@x>","receivedDateTime":"2025-03-02T09:00:00Z"}]}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_AR/messages", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"M3","internetMessageId":"<m3@x>","receivedDateTime":"2025-03-03T09:00:00Z"}]}`)
	})
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u1/messages/"), "/$value")
		f.mu.Lock()
		f.mimeHits[id]++
		f.mu.Unlock()
		w.Write([]byte("From: a@example.com\r\nSubject: subj-" + id +
			"\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n"))
	})
	return f, httptest.NewServer(mux)
}

func (f *fakeGraphServer) hits() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int{}
	for k, v := range f.mimeHits {
		out[k] = v
	}
	return out
}

func countFiles(t *testing.T, root, suffix, exclude string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, suffix) && d.Name() != exclude {
			n++
		}
		return nil
	})
	return n
}

// covers: MA-62, R17, R2
// RunGraph captures every message in every folder into the normal pipeline
// (HTML + index + manifest), and an incremental re-run archives zero new items
// AND does not re-download any already-archived message's body (skip-by-
// Internet-Message-ID).
func TestRunGraphCapturesAndIncrements(t *testing.T) {
	out := tmpDir(t)
	f, srv := newFakeGraphServer()
	defer srv.Close()

	g := GraphOptions{
		Tenant: "t", ClientID: "c", ClientSecret: "s",
		Mailboxes: []string{"u1"},
		BaseURL:   srv.URL, TokenURL: srv.URL + "/token",
	}
	opts := Options{Out: out, Mode: export.Incremental, Index: true, Pages: true}
	logger := log.New(io.Discard, "", 0)

	// First run: everything captured.
	r1, err := RunGraph(context.Background(), g, opts, logger)
	if err != nil {
		t.Fatalf("first RunGraph: %v", err)
	}
	if r1.Stats.Exported != 3 {
		t.Errorf("first run exported %d, want 3", r1.Stats.Exported)
	}
	if r1.ManifestSize != 3 || r1.Indexed != 3 {
		t.Errorf("first run manifest=%d indexed=%d, want 3/3", r1.ManifestSize, r1.Indexed)
	}
	if got := countFiles(t, out, ".html", "index.html"); got != 3 {
		t.Errorf("html files = %d, want 3", got)
	}
	firstHits := f.hits()
	if len(firstHits) != 3 {
		t.Fatalf("first run fetched %d distinct bodies, want 3: %v", len(firstHits), firstHits)
	}

	// Second run (incremental): nothing new, and no body re-downloaded.
	r2, err := RunGraph(context.Background(), g, opts, logger)
	if err != nil {
		t.Fatalf("second RunGraph: %v", err)
	}
	if r2.Stats.Exported != 0 {
		t.Errorf("second run exported %d, want 0", r2.Stats.Exported)
	}
	if r2.Stats.SkippedManifest != 3 {
		t.Errorf("second run skipped %d, want 3", r2.Stats.SkippedManifest)
	}
	secondHits := f.hits()
	for id, n := range secondHits {
		if n != firstHits[id] {
			t.Errorf("message %s body re-downloaded on incremental run (%d→%d)", id, firstHits[id], n)
		}
	}
}

package app

import (
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
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
	selects  []string // the raw query of every /messages listing request
}

func newFakeGraphServer() (*fakeGraphServer, *httptest.Server) {
	f := &fakeGraphServer{mimeHits: map[string]int{}}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(s))
	}
	recordSelect := func(r *http.Request) {
		f.mu.Lock()
		f.selects = append(f.selects, r.URL.RawQuery)
		f.mu.Unlock()
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0},{"id":"F_AR","displayName":"Archive","childFolderCount":0}]}`)
	})
	// The listings carry message state on the same request (PC16): M1 is unread,
	// high, confidential; M2 is read, normal, normal (no state to show); M3 omits
	// isRead entirely (read state then unknown) and is low/personal.
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		recordSelect(r)
		j(w, `{"value":[
			{"id":"M1","internetMessageId":"<m1@x>","receivedDateTime":"2025-03-01T09:00:00Z","importance":"high","isRead":false,"sensitivity":"confidential"},
			{"id":"M2","internetMessageId":"<m2@x>","receivedDateTime":"2025-03-02T09:00:00Z","importance":"normal","isRead":true,"sensitivity":"normal"}]}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_AR/messages", func(w http.ResponseWriter, r *http.Request) {
		recordSelect(r)
		j(w, `{"value":[{"id":"M3","internetMessageId":"<m3@x>","receivedDateTime":"2025-03-03T09:00:00Z","importance":"low","sensitivity":"personal"}]}`)
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

func (f *fakeGraphServer) selectQueries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.selects...)
}

// messagePage returns the contents of the exported message page whose body
// carries needle (the fake server stamps each message's id into its subject).
func messagePage(t *testing.T, root, needle string) string {
	t.Helper()
	var found string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "index.html" || !strings.HasSuffix(p, ".html") {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr == nil && strings.Contains(string(b), needle) {
			found = string(b)
		}
		return nil
	})
	if found == "" {
		t.Fatalf("no exported message page contains %q under %s", needle, root)
	}
	return found
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

// covers: MA-179, R3, S35
// The Graph source carries message state on the SAME widened listing $select
// (PC16): importance/isRead/sensitivity ride along with no extra request, are
// mapped onto each message, and reach the page's Status row. A field the tenant
// omits stays empty (M2 is normal/read → no row; M3 omits isRead → not marked
// unread), and the incremental fast-path still keys on the id alone, so no body
// is fetched more than once.
func TestRunGraphCapturesMessageState(t *testing.T) {
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

	if _, err := RunGraph(context.Background(), g, opts, logger); err != nil {
		t.Fatalf("RunGraph: %v", err)
	}

	// The one listing request carried the widened $select — no extra round-trip.
	selects := f.selectQueries()
	if len(selects) == 0 {
		t.Fatal("no /messages listing request recorded")
	}
	for _, q := range selects {
		for _, field := range []string{"importance", "isRead", "sensitivity"} {
			if !strings.Contains(q, field) {
				t.Errorf("listing $select %q is missing %q", q, field)
			}
		}
	}
	// State fetched only via the listing: each body downloaded exactly once.
	for id, n := range f.hits() {
		if n != 1 {
			t.Errorf("message %s body fetched %d times, want 1 (state must not cost an extra request)", id, n)
		}
	}

	// M1: unread, high, confidential → full Status row.
	if p := messagePage(t, out, "subj-M1"); !strings.Contains(p, "Unread · Importance: high · Sensitivity: confidential") {
		t.Errorf("M1 page lacks the expected Status row:\n%s", p)
	}
	// M2: read + normal + normal → no Status row.
	if p := messagePage(t, out, "subj-M2"); strings.Contains(p, `data-mailarchive-field="status"`) {
		t.Errorf("M2 (all-normal) should have no Status row:\n%s", p)
	}
	// M3: low + personal, isRead omitted → Status row without "Unread".
	p := messagePage(t, out, "subj-M3")
	if !strings.Contains(p, "Importance: low · Sensitivity: personal") {
		t.Errorf("M3 page lacks the expected Status row:\n%s", p)
	}
	if strings.Contains(p, "Unread") {
		t.Errorf("M3 has isRead omitted and must not be marked Unread:\n%s", p)
	}
}

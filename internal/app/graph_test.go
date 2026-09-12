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
	"mail-archive-tool/internal/state"
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
	// high, confidential and carries two categories; M2 is read, normal, normal
	// (no state to show); M3 omits isRead entirely (read state then unknown) and
	// is low/personal.
	mux.HandleFunc("/users/u1/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		recordSelect(r)
		j(w, `{"value":[
			{"id":"M1","internetMessageId":"<m1@x>","subject":"subj-M1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z","importance":"high","isRead":false,"sensitivity":"confidential","categories":["Board","Legal Hold"]},
			{"id":"M2","internetMessageId":"<m2@x>","subject":"subj-M2","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-02T09:00:00Z","importance":"normal","isRead":true,"sensitivity":"normal"}]}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_AR/messages", func(w http.ResponseWriter, r *http.Request) {
		recordSelect(r)
		j(w, `{"value":[{"id":"M3","internetMessageId":"<m3@x>","subject":"subj-M3","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-03T09:00:00Z","importance":"low","sensitivity":"personal"}]}`)
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

	// The live path writes the go-back timeline (MA-62 extended, §3.3): a run
	// header, a folder-assertion for each newly-captured message, and a clean-run
	// footer.
	ev1, herr := state.ReadHistory(filepath.Join(out, state.HistoryName))
	if herr != nil {
		t.Fatalf("read history log: %v", herr)
	}
	var headers, footers, folders int
	for _, e := range ev1 {
		switch {
		case e.Run > 0 && e.At != "":
			headers++
		case e.Run > 0 && e.Completed != "":
			footers++
		case e.K != "" && !e.Gone:
			folders++
		}
	}
	if headers < 1 || footers < 1 || folders != 3 {
		t.Errorf("run 1 timeline: headers=%d footers=%d folder-assertions=%d, want >=1/>=1/3", headers, footers, folders)
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

// covers: MA-192, R3, S36
// The Graph source's categories array rides on the SAME widened listing $select
// (one field more, no extra request) and reaches the message's own "Categories"
// page field. M1 carries two categories; they appear in the dedicated
// categories field (each in its own span), never folded into the Status line,
// and a message with none (M2) shows no categories field.
func TestRunGraphCapturesCategories(t *testing.T) {
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

	// The one listing request carried "categories" on the same $select — no
	// extra round-trip.
	for _, q := range f.selectQueries() {
		if !strings.Contains(q, "categories") {
			t.Errorf("listing $select %q is missing categories", q)
		}
	}

	// M1: both categories reach the dedicated categories field.
	p := messagePage(t, out, "subj-M1")
	catIdx := strings.Index(p, `data-mailarchive-field="categories"`)
	if catIdx < 0 {
		t.Fatalf("M1 page lacks the categories field:\n%s", p)
	}
	ddEnd := strings.Index(p[catIdx:], "</dd>")
	if ddEnd < 0 {
		t.Fatalf("categories dd not closed:\n%s", p)
	}
	field := p[catIdx : catIdx+ddEnd]
	for _, want := range []string{"Board", "Legal Hold"} {
		if !strings.Contains(field, want) {
			t.Errorf("categories field missing %q:\n%s", want, field)
		}
	}
	// Categories are their OWN field, never a Status segment: the categories
	// text does not sit inside a status dd.
	if statusIdx := strings.Index(p, `data-mailarchive-field="status"`); statusIdx >= 0 {
		statusEnd := strings.Index(p[statusIdx:], "</dd>")
		if statusEnd >= 0 && strings.Contains(p[statusIdx:statusIdx+statusEnd], "Board") {
			t.Errorf("a category leaked into the Status row (QC3):\n%s", p)
		}
	}

	// M2 has no categories → no categories field at all.
	if m2 := messagePage(t, out, "subj-M2"); strings.Contains(m2, `data-mailarchive-field="categories"`) {
		t.Errorf("M2 (no categories) should have no categories field:\n%s", m2)
	}
}

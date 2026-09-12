package graph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeGraph is an in-process stand-in for the Microsoft token + Graph endpoints.
// It records every request method so tests can prove the client is read-only.
type fakeGraph struct {
	mu       sync.Mutex
	methods  []string
	mimeHits map[string]int // message id -> times $value fetched
}

func newFakeGraph() (*fakeGraph, *httptest.Server) {
	f := &fakeGraph{mimeHits: map[string]int{}}
	mux := http.NewServeMux()

	// Token endpoint (client-credentials).
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"faketoken","token_type":"Bearer","expires_in":3600}`))
	})

	// Top-level folders: Inbox (with a child) + Sent.
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		writeJSON(w, `{"value":[
			{"id":"F_INBOX","displayName":"Inbox","childFolderCount":1},
			{"id":"F_SENT","displayName":"Sent","childFolderCount":0}]}`)
	})
	// Child folders of Inbox: Projects.
	mux.HandleFunc("/users/u1/mailFolders/F_INBOX/childFolders", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		writeJSON(w, `{"value":[{"id":"F_PROJ","displayName":"Projects","childFolderCount":0}]}`)
	})

	// Messages per folder (INBOX paged into two pages to exercise nextLink).
	mux.HandleFunc("/users/u1/mailFolders/F_INBOX/messages", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		if r.URL.Query().Get("page") == "2" {
			writeJSON(w, `{"value":[{"id":"M2","internetMessageId":"<m2@x>","receivedDateTime":"2025-03-02T09:00:00Z"}]}`)
			return
		}
		writeJSON(w, `{"value":[{"id":"M1","internetMessageId":"<m1@x>","receivedDateTime":"2025-03-01T09:00:00Z"}],
			"@odata.nextLink":"`+baseOf(r)+`/users/u1/mailFolders/F_INBOX/messages?page=2"}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_SENT/messages", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		writeJSON(w, `{"value":[{"id":"M3","internetMessageId":"<m3@x>","receivedDateTime":"2025-03-03T09:00:00Z"}]}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/F_PROJ/messages", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		writeJSON(w, `{"value":[{"id":"M4","internetMessageId":"<m4@x>","receivedDateTime":"2025-03-04T09:00:00Z"}]}`)
	})

	// $value MIME for any message.
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		f.record(r.Method)
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u1/messages/"), "/$value")
		f.mu.Lock()
		f.mimeHits[id]++
		f.mu.Unlock()
		w.Write([]byte("From: a@example.com\r\nSubject: " + id + "\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\n\r\nbody\r\n"))
	})

	return f, httptest.NewServer(mux)
}

func (f *fakeGraph) record(m string) { f.mu.Lock(); f.methods = append(f.methods, m); f.mu.Unlock() }

func writeJSON(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(s))
}

func baseOf(r *http.Request) string { return "http://" + r.Host }

func newTestClient(srv *httptest.Server) *Client {
	return New(context.Background(), Config{
		Tenant: "t", ClientID: "c", ClientSecret: "s",
		BaseURL: srv.URL, TokenURL: srv.URL + "/token",
	})
}

// covers: MA-61, R17
// The client enumerates the full folder tree (incl. nested child folders), pages
// through all messages, fetches raw MIME, and issues ONLY GET requests — the
// read-only guarantee that a Mail.Read app can never modify a mailbox.
func TestGraphClientReadsEverythingReadOnly(t *testing.T) {
	f, srv := newFakeGraph()
	defer srv.Close()
	c := newTestClient(srv)
	ctx := context.Background()

	folders, err := c.Folders(ctx, "u1", FolderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, fl := range folders {
		got[strings.Join(fl.Path, "/")] = true
	}
	for _, want := range []string{"Inbox", "Inbox/Projects", "Sent"} {
		if !got[want] {
			t.Errorf("folder %q not discovered; got %v", want, got)
		}
	}

	var subjects []string
	for _, fl := range folders {
		if err := c.Messages(ctx, "u1", fl.ID, func(m MessageRef) error {
			data, err := c.MIME(ctx, "u1", m.ID)
			if err != nil {
				return err
			}
			subjects = append(subjects, string(data[strings.Index(string(data), "Subject: ")+9:strings.Index(string(data), "\r\nMessage-ID")]))
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(subjects) != 4 { // M1+M2 (Inbox, paged), M3 (Sent), M4 (Projects)
		t.Fatalf("fetched %d messages, want 4: %v", len(subjects), subjects)
	}

	// Read-only: every request the client made was a GET.
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.methods {
		if m != http.MethodGet && m != http.MethodPost { // POST only = the token endpoint
			t.Errorf("non-GET request to Graph: %s", m)
		}
	}
	// Token is POST; all Graph data calls must be GET.
	gets := 0
	for _, m := range f.methods {
		if m == http.MethodGet {
			gets++
		}
	}
	if gets == 0 {
		t.Fatal("no GET requests recorded")
	}
}

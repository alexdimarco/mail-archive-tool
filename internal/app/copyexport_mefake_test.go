package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/graph"
)

// meFake is a fake Microsoft token endpoint and /me Graph API (the delegated client
// the extraction workspace uses), shared by the copy-export tests that drive the
// real graph client and the Microsoft 365 adapter. A test makes a message's $value
// or the token endpoint misbehave and asks what was requested.
type meFake struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	values   map[string]http.HandlerFunc
	hits     map[string]int
	pages    map[string][]string // folder id -> JSON arrays of message objects, one per page
	honored  bool
	tokenFn  http.HandlerFunc
	meFn     http.HandlerFunc
	tokenHit int
}

func newMeFake(t *testing.T) *meFake {
	t.Helper()
	f := &meFake{t: t, values: map[string]http.HandlerFunc{}, hits: map[string]int{}, pages: map[string][]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.tokenHit++
		fn := f.tokenFn
		f.mu.Unlock()
		if fn != nil {
			fn(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.hits["/me"]++
		fn := f.meFn
		f.mu.Unlock()
		if fn != nil {
			fn(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/me/messages/"), "/$value")
		f.mu.Lock()
		f.hits[id]++
		h := f.values[id]
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		w.Write(ceBody(id))
	})
	mux.HandleFunc("/me/mailFolders/", func(w http.ResponseWriter, r *http.Request) {
		folder := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/me/mailFolders/"), "/messages")
		f.mu.Lock()
		pages, honored := f.pages[folder], f.honored
		f.mu.Unlock()
		page := 0
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		w.Header().Set("Content-Type", "application/json")
		if honored {
			w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
		}
		body := `{"value":[]}`
		if page < len(pages) {
			body = `{"value":` + pages[page]
			if page+1 < len(pages) {
				body += `,"@odata.nextLink":"http://` + r.Host + r.URL.Path + `?page=` + fmt.Sprint(page+1) + `"`
			}
			body = strings.Replace(body, `]`, `]`, 1) + `}`
		}
		io.WriteString(w, body)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// value makes message id's $value answer through h.
func (f *meFake) value(id string, h http.HandlerFunc) {
	f.mu.Lock()
	f.values[id] = h
	f.mu.Unlock()
}

func (f *meFake) hit(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[id]
}

// page sets folder's listing pages: each is a JSON array of message objects.
func (f *meFake) page(folder string, pages ...string) {
	f.mu.Lock()
	f.pages[folder] = pages
	f.mu.Unlock()
}

// msgJSON is a listing entry.
func msgJSON(id string, size int) string {
	b, _ := json.Marshal(map[string]any{"id": id, "subject": "subj-" + id, "receivedDateTime": "2026-03-01T09:00:00Z",
		"from":                          map[string]any{"emailAddress": map[string]string{"address": "a@example.com", "name": "A"}},
		"singleValueExtendedProperties": []map[string]string{{"id": "Integer 0x0E08", "value": fmt.Sprint(size)}}})
	return string(b)
}

// client is a delegated client of the fake whose sign-in cache holds a token valid for
// the given time (zero: an hour), with the download bounds scaled.
func (f *meFake) client(t *testing.T, valid time.Duration) *graph.Client {
	t.Helper()
	if valid == 0 {
		valid = time.Hour
	}
	cache := filepath.Join(t.TempDir(), "graph.token")
	body := `{"upn":"alice@contoso.org","token":{"access_token":"AT0","refresh_token":"RT0","token_type":"Bearer","expiry":"` +
		time.Now().Add(valid).UTC().Format(time.RFC3339Nano) + `"}}`
	if err := os.WriteFile(cache, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := graph.NewDelegated(context.Background(), graph.Config{Tenant: "t", ClientID: "c", BaseURL: f.srv.URL,
		TokenURL: f.srv.URL + "/token", DeviceAuthURL: f.srv.URL + "/devicecode", TokenCachePath: cache, Unattended: true,
		RequestTimeout: 2 * time.Second, MIMEIdleTimeout: 300 * time.Millisecond, MIMEHeaderTimeout: 600 * time.Millisecond})
	if err != nil {
		t.Fatalf("the delegated client: %v", err)
	}
	return c
}

// source is the adapter over that client with scaled windows.
func (f *meFake) source(t *testing.T) *M365Source {
	t.Helper()
	s := NewM365Source(f.client(t, 0), "alice@contoso.org", "tenant-1", nil)
	s.Idle, s.Overall = 300*time.Millisecond, 3*time.Second
	return s
}

// planFor plans msgs (listed in an Inbox) against src's description.
func planFor(t *testing.T, e *CopyExportEnv, src CopySource, msgs []CopyMessage) *CopyPlan {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "dest")
	os.MkdirAll(dest, 0o755)
	p, err := e.PlanCopyExport(CopyPlanInput{Source: src.Describe(), Messages: msgs, Dest: dest,
		Options: CopyOptions{PreserveFolders: true}, Folders: []CopyPlanFolder{{Folder: "Inbox", Messages: len(msgs)}}})
	if err != nil {
		t.Fatalf("PlanCopyExport: %v", err)
	}
	return p
}

// runWith runs plan against src to its end, failing the test on a refusal.
func runWith(t *testing.T, e *CopyExportEnv, p *CopyPlan, src CopySource) *CopyResult {
	t.Helper()
	res, err := e.NewCopyRun(p, src).Run(context.Background())
	if err != nil {
		t.Fatalf("the run was refused: %v", err)
	}
	return res
}

// idMsgs plans messages with the given ids.
func idMsgs(ids ...string) []CopyMessage {
	out := ceMsgs(len(ids))
	for i, id := range ids {
		out[i].SourceID = id
		out[i].InternetMessageID = strings.ToLower(id) + "@x"
		out[i].Subject = "subj-" + id
		out[i].SizeHint = -1
	}
	return out
}

// failureOf reads the failures array of a finished export's manifest.
func failureOf(t *testing.T, root, id string) map[string]any {
	t.Helper()
	for _, f := range manifestOf(t, root)["failures"].([]any) {
		if m := f.(map[string]any); m["source_id"] == id {
			return m
		}
	}
	return nil
}

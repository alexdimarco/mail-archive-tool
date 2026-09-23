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
	"sync"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// fakeDelegServer serves the device-code + token endpoints, /me, and the SAME one
// mailbox under BOTH addressing schemes — /me/... (delegated) and
// /users/alice@contoso.org/... (app-only) — returning identical message content,
// so a test can drive an app run and a device run against one fixture and prove
// the second re-downloads nothing.
type fakeDelegServer struct {
	mu       sync.Mutex
	mimeHits map[string]int
}

func newFakeDelegServer() (*fakeDelegServer, *httptest.Server) {
	f := &fakeDelegServer{mimeHits: map[string]int{}}
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s)
	}

	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"device_code":"DEV","user_code":"WXYZ-1234","verification_uri":"https://microsoft.com/devicelogin","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			j(w, `{"access_token":"AT2","refresh_token":"RT2","token_type":"Bearer","expires_in":3600}`)
			return
		}
		j(w, `{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	folders := `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`
	messages := `{"value":[{"id":"M1","internetMessageId":"<m1@x>","subject":"subj-M1","from":{"emailAddress":{"address":"a@example.com"}},"receivedDateTime":"2025-03-01T09:00:00Z"}]}`
	mime := func(w http.ResponseWriter, id string) {
		f.mu.Lock()
		f.mimeHits[id]++
		f.mu.Unlock()
		io.WriteString(w, "From: a@example.com\r\nSubject: subj-"+id+"\r\nMessage-ID: <"+strings.ToLower(id)+"@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody\r\n")
	}
	// Delegated addressing.
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) { j(w, folders) })
	mux.HandleFunc("/me/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) { j(w, messages) })
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		mime(w, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/me/messages/"), "/$value"))
	})
	// App-only addressing for the same mailbox.
	mux.HandleFunc("/users/alice@contoso.org/mailFolders", func(w http.ResponseWriter, r *http.Request) { j(w, folders) })
	mux.HandleFunc("/users/alice@contoso.org/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) { j(w, messages) })
	mux.HandleFunc("/users/alice@contoso.org/messages/", func(w http.ResponseWriter, r *http.Request) {
		mime(w, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/alice@contoso.org/messages/"), "/$value"))
	})
	return f, httptest.NewServer(mux)
}

func (f *fakeDelegServer) hitsOf(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mimeHits[id]
}

func delegOpts(srv *httptest.Server, cache string) GraphOptions {
	return GraphOptions{
		Auth: "device", Tenant: "t", ClientID: "c",
		BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode",
		TokenCachePath: cache,
	}
}

func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// writePrimedCache writes a valid, unexpired token cache bound to alice so an
// unattended device run needs no interactive prompt.
func writePrimedCache(t *testing.T, path string) {
	t.Helper()
	const body = `{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expiry":"2999-01-01T00:00:00Z"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-253, R17, R12, S20
// Device mode reads the signer from /me and archives that one mailbox: an omitted
// -mailbox adopts the signer; a matching -mailbox is accepted; a -mailbox naming
// anyone else is refused before any walk (no message written), naming the signer.
func TestRunGraphDeviceMailboxMatch(t *testing.T) {
	ctx := context.Background()

	// Omitted -mailbox → archives the signer.
	f, srv := newFakeDelegServer()
	defer srv.Close()
	out := t.TempDir()
	g := delegOpts(srv, filepath.Join(t.TempDir(), "tok.json"))
	if _, err := RunGraph(ctx, g, Options{Out: out, Mode: export.Incremental, Index: false, Pages: false}, discardLogger()); err != nil {
		t.Fatalf("device run (no -mailbox): %v", err)
	}
	if f.hitsOf("M1") != 1 {
		t.Errorf("signer mailbox not archived: M1 hits = %d", f.hitsOf("M1"))
	}

	// Matching -mailbox (case-insensitive) → accepted.
	out2 := t.TempDir()
	g2 := delegOpts(srv, filepath.Join(t.TempDir(), "tok2.json"))
	g2.Mailboxes = []string{"ALICE@contoso.org"}
	if _, err := RunGraph(ctx, g2, Options{Out: out2, Mode: export.Incremental, Index: false, Pages: false}, discardLogger()); err != nil {
		t.Fatalf("device run (matching -mailbox): %v", err)
	}

	// Mismatched -mailbox → refused before any walk, naming the signer.
	out3 := t.TempDir()
	g3 := delegOpts(srv, filepath.Join(t.TempDir(), "tok3.json"))
	g3.Mailboxes = []string{"bob@evil.example"}
	_, err := RunGraph(ctx, g3, Options{Out: out3, Mode: export.Incremental, Index: false, Pages: false}, discardLogger())
	assure.Reached(t, errString(err), "mismatch refusal")
	if !strings.Contains(errString(err), "alice@contoso.org") {
		t.Errorf("refusal must name the signed-in user: %v", err)
	}
	if n := countFiles(t, out3, ".html", "index.html"); n != 0 {
		t.Errorf("a refused mismatch wrote %d message page(s); want 0", n)
	}
}

// covers: MA-256, R12, R18, S20
// An unattended device run with no usable token cache is refused naming the
// interactive-sign-in remedy (a scheduled run cannot prompt); a run with a primed
// cache runs silently and archives.
func TestRunGraphDeviceUnattendedNeedsCache(t *testing.T) {
	ctx := context.Background()
	f, srv := newFakeDelegServer()
	defer srv.Close()

	// No cache + unattended → refused with the sign-in remedy.
	g := delegOpts(srv, filepath.Join(t.TempDir(), "missing.json"))
	g.Unattended = true
	_, err := RunGraph(ctx, g, Options{Out: t.TempDir(), Mode: export.Incremental, Index: false, Pages: false}, discardLogger())
	assure.Reached(t, errString(err), "unattended-no-cache refusal")
	low := strings.ToLower(errString(err))
	if !strings.Contains(low, "sign in") && !strings.Contains(low, "interactively") {
		t.Errorf("refusal must name the sign-in remedy: %v", err)
	}

	// Primed cache + unattended → runs silently and archives.
	cache := filepath.Join(t.TempDir(), "primed.json")
	writePrimedCache(t, cache)
	g2 := delegOpts(srv, cache)
	g2.Unattended = true
	if _, err := RunGraph(ctx, g2, Options{Out: t.TempDir(), Mode: export.Incremental, Index: false, Pages: false}, discardLogger()); err != nil {
		t.Fatalf("primed unattended device run: %v", err)
	}
	if f.hitsOf("M1") != 1 {
		t.Errorf("primed run did not archive: M1 hits = %d", f.hitsOf("M1"))
	}
}

// covers: MA-259, R2, R3, S20
// An archive captured -auth app and then continued -auth device for the SAME
// mailbox exports zero new items and re-downloads no body on the second run — the
// store token is seeded from the mailbox id, not the auth mode.
func TestRunGraphCrossModeContinuity(t *testing.T) {
	ctx := context.Background()
	f, srv := newFakeDelegServer()
	defer srv.Close()
	out := t.TempDir()

	// Run 1: app mode against /users/alice@contoso.org.
	app := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"alice@contoso.org"},
		BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	if _, err := RunGraph(ctx, app, Options{Out: out, Mode: export.Incremental, Index: false, Pages: false}, discardLogger()); err != nil {
		t.Fatalf("app run: %v", err)
	}
	if f.hitsOf("M1") != 1 {
		t.Fatalf("app run should download M1 once, got %d", f.hitsOf("M1"))
	}

	// Run 2: device mode against /me for the same signer, same -out.
	dev := delegOpts(srv, filepath.Join(t.TempDir(), "tok.json"))
	if _, err := RunGraph(ctx, dev, Options{Out: out, Mode: export.Incremental, Index: false, Pages: false}, discardLogger()); err != nil {
		t.Fatalf("device run: %v", err)
	}
	if f.hitsOf("M1") != 1 {
		t.Errorf("device run re-downloaded M1 across a mode switch: hits = %d, want 1", f.hitsOf("M1"))
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

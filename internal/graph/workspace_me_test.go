package graph

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// Probe fake modes: how /me answers.
const (
	probeOK = iota
	probeUnavailable
	probeThrottled
	probeSilent
)

// meFake is a fake sign-in (device code, token) and /me mailbox: every mailbox
// request is recorded, and /me answers as its probe mode says.
type meFake struct {
	rec   recorder
	scope atomic.Value // the scope the device-code request carried
	probe atomic.Int32
	srv   *httptest.Server
}

func newMeFake(t *testing.T) *meFake {
	t.Helper()
	f := &meFake{}
	mux := http.NewServeMux()
	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.scope.Store(r.Form.Get("scope"))
		writeJSON(w, `{"device_code":"DEV","user_code":"WXYZ-1234","verification_uri":"https://microsoft.com/devicelogin","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		f.rec.add(r)
		switch f.probe.Load() {
		case probeUnavailable, probeThrottled:
			w.Header().Set("Retry-After", "3600")
			code := http.StatusServiceUnavailable
			if f.probe.Load() == probeThrottled {
				code = http.StatusTooManyRequests
			}
			http.Error(w, `{"error":{"code":"ApplicationThrottled"}}`, code)
		case probeSilent:
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		default:
			writeJSON(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
		}
	})
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		f.rec.add(r)
		writeJSON(w, `{"value":[{"id":"F_IN","displayName":"Inbox","parentFolderId":"ROOT","childFolderCount":0,"totalItemCount":3}]}`)
	})
	mux.HandleFunc("/me/mailFolders/", func(w http.ResponseWriter, r *http.Request) {
		f.rec.add(r)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			writeJSON(w, `{"value":[{`+pageEnvelope("M1")+`}]}`)
		case strings.HasSuffix(r.URL.Path, "/F_IN"):
			writeJSON(w, `{"id":"F_IN","totalItemCount":3}`)
		default:
			http.Error(w, `{"error":{"code":"ErrorItemNotFound"}}`, http.StatusNotFound)
		}
	})
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		f.rec.add(r)
		w.Write([]byte("From: a@example.com\r\nSubject: s\r\n\r\nbody\r\n"))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *meFake) config(t *testing.T) Config {
	return Config{Tenant: "t", ClientID: "c", BaseURL: f.srv.URL, TokenURL: f.srv.URL + "/token", DeviceAuthURL: f.srv.URL + "/devicecode",
		TokenCachePath: filepath.Join(t.TempDir(), "graph.token"), Prompt: func(DeviceAuth) {}}
}

// covers: MA-403, R17, R9, R29, S52
// The workspace's Graph reads go through the device (/me) client: signed in by
// the device-code grant for exactly Mail.Read and offline_access (the scopes
// constant and the scope the device-code request carried), FolderTree,
// FolderCount, MessagePage, MIMETo and Probe address /me and nothing else, every
// request a GET, and no request ever addresses /users/. An app-only client is
// refused by each of the workspace's read paths before any request is sent, with
// a typed error naming the own-mailbox rule (the positive twin is the device
// client's own reads, first).
func TestWorkspaceClientIsDeviceMeOnly(t *testing.T) {
	f := newMeFake(t)
	ctx := context.Background()
	c, err := NewDelegated(ctx, f.config(t))
	if err != nil {
		t.Fatal(err)
	}
	wantScopes := []string{"https://graph.microsoft.com/Mail.Read", "offline_access"}
	if !reflect.DeepEqual(delegatedScopes, wantScopes) {
		t.Errorf("delegatedScopes = %q, want exactly %q", delegatedScopes, wantScopes)
	}
	if got := f.scope.Load(); got != strings.Join(wantScopes, " ") {
		t.Errorf("the device-code request asked for scope %q, want %q", got, strings.Join(wantScopes, " "))
	}

	folders, err := c.FolderTree(ctx, "someone@else.example", 0)
	if err != nil || len(assure.Reached(t, folders, "the device client's folders")) != 1 {
		t.Fatalf("FolderTree: %v, %v", folders, err)
	}
	if n, err := c.FolderCount(ctx, "someone@else.example", "F_IN"); err != nil || n != 3 {
		t.Fatalf("FolderCount: %d, %v", n, err)
	}
	if refs, _, err := c.MessagePage(ctx, "someone@else.example", "F_IN", "", PageOpts{}); err != nil || len(refs) != 1 {
		t.Fatalf("MessagePage: %v, %v", refs, err)
	}
	var buf bytes.Buffer
	if n, err := c.MIMETo(ctx, "someone@else.example", "M1", &buf, MIMEMaxBytes, 0, MIMEMaxDuration); err != nil || n == 0 {
		t.Fatalf("MIMETo: %d, %v", n, err)
	}
	if err := c.Probe(ctx); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	f.rec.onlyMeGETs(t, "/me") // the userID the caller named is never used: no /users/ path, no write

	// an app-only client reads no mailbox here
	app := New(ctx, Config{Tenant: "t", ClientID: "c", ClientSecret: "s", BaseURL: f.srv.URL, TokenURL: f.srv.URL + "/token"})
	before := len(f.rec.all())
	reads := map[string]func() error{
		"FolderTree":  func() error { _, err := app.FolderTree(ctx, "bob@contoso.org", 0); return err },
		"FolderCount": func() error { _, err := app.FolderCount(ctx, "bob@contoso.org", "F_IN"); return err },
		"MessagePage": func() error { _, _, err := app.MessagePage(ctx, "bob@contoso.org", "F_IN", "", PageOpts{}); return err },
		"Probe":       func() error { return app.Probe(ctx) },
	}
	for name, read := range reads {
		err := read()
		if !errors.Is(err, ErrOwnMailboxOnly) {
			t.Errorf("%s on an app-only client is not the own-mailbox refusal: %v", name, err)
			continue
		}
		assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names("own mailbox"),
			assure.NoSideEffect(func() bool { return len(f.rec.all()) == before }))
	}
}

// covers: MA-403, R17, R9, R29, S52
// MA-403's other half, E3's: the workspace's session built from settings is the
// device client whatever the configured auth mode (even auth=app), and a whole
// Microsoft 365 preview and export issue only GET (MA-506).
func TestWorkspaceSessionIgnoresAppAuthConfig(t *testing.T) {
	t.Skip("MA-403: the workspace session is built from settings in the desktop app, which lands in EW-E3 (with MA-506); E1 holds the device client and its read paths only")
}

// covers: MA-403, R17, R29
// Client.Probe is the one-shot control request of the copy export's connection
// check (extraction design §4.13, RR2b-C7): GET /me?$select=id, sent once. A 2xx
// is nil. A 503 or a 429 with a long Retry-After comes back at once as the typed
// *StatusError, with no second request and no sleep (it never goes through get,
// whose Retry-After wait under a 60 s context would turn a long throttle into a
// deadline error). A server that never answers is bounded and is a transport-class
// error.
func TestProbeIsAOneShotControlRequest(t *testing.T) {
	f := newMeFake(t)
	cfg := f.config(t)
	cfg.RequestTimeout = 300 * time.Millisecond
	c, err := NewDelegated(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	probes := func() int {
		n := 0
		for _, q := range f.rec.all() {
			if q.URL.Query().Get("$select") == "id" {
				n++
			}
		}
		return n
	}

	if err := c.Probe(ctx); err != nil {
		t.Fatalf("a healthy Probe: %v", err)
	}
	if probes() != 1 {
		t.Fatalf("a healthy Probe sent %d request(s)", probes())
	}
	last := f.rec.all()[len(f.rec.all())-1]
	if last.Method != http.MethodGet || last.URL.Path != "/me" || last.URL.Query().Get("$select") != "id" {
		t.Errorf("Probe sent %s %s", last.Method, last.URL.RequestURI())
	}

	for mode, code := range map[int32]int{probeUnavailable: 503, probeThrottled: 429} {
		f.probe.Store(mode)
		before := probes()
		began := time.Now()
		err := c.Probe(ctx)
		var se *StatusError
		if !errors.As(err, &se) || se.Code != code {
			t.Errorf("a %d is not a typed *StatusError with that code: %v", code, err)
		}
		if probes()-before != 1 || time.Since(began) > 2*time.Second {
			t.Errorf("a %d was retried or slept on: %d request(s) in %v", code, probes()-before, time.Since(began))
		}
	}

	f.probe.Store(probeSilent)
	began := time.Now()
	err = c.Probe(ctx)
	if class, _ := Classify(err, ctx); err == nil || class != ClassTransport || time.Since(began) > 3*time.Second {
		t.Errorf("a silent server is not a bounded transport-class error: %v (%v) after %v", err, class, time.Since(began))
	}
}

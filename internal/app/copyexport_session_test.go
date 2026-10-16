package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
)

// sessionProbe is the meFake behind a server that records every request it gets.
type sessionProbe struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []string // "METHOD /path"
	form []url.Values
}

func newSessionProbe(t *testing.T) *sessionProbe {
	t.Helper()
	f := newMeFake(t)
	p := &sessionProbe{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v url.Values
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			v = r.PostForm
		}
		p.mu.Lock()
		p.reqs = append(p.reqs, r.Method+" "+r.URL.Path)
		p.form = append(p.form, v)
		p.mu.Unlock()
		f.srv.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *sessionProbe) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.reqs...)
}

// signInCache writes a saved device sign-in for alice whose access token expired
// long ago, so the first request refreshes it through the token endpoint.
func signInCache(t *testing.T, expired bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graph.token")
	exp := time.Now().Add(time.Hour)
	if expired {
		exp = time.Now().Add(-time.Hour)
	}
	body := `{"upn":"alice@contoso.org","token":{"access_token":"AT0","refresh_token":"RT0","token_type":"Bearer","expiry":"` + exp.UTC().Format(time.RFC3339Nano) + `"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func sessionOpts(t *testing.T, p *sessionProbe, auth, tokenPath string) GraphOptions {
	t.Helper()
	return GraphOptions{Auth: auth, Tenant: "t", ClientID: "c", ClientSecret: "an-app-secret", Mailboxes: []string{"bob@contoso.org"},
		BaseURL: p.srv.URL, TokenURL: p.srv.URL + "/token", DeviceAuthURL: p.srv.URL + "/devicecode", TokenCachePath: tokenPath,
		Prompt: func(graph.DeviceAuth) { t.Error("the workspace session prompted for a device code") }}
}

// covers: MA-506, MA-403, R17, R9, R29, S52
// The workspace's Graph session is the device (/me) client whatever the options say:
// with Auth "app", an app secret and another mailbox in the options, OpenGraphSession
// still builds the delegated client for the signer, refreshes through the refresh-token
// grant (never client credentials), and sends only GET requests that address /me and
// never /users/. The positive twin, a plain device session, comes first. A missing
// sign-in is ErrNotSignedIn, with no request and no device-code prompt.
func TestOpenGraphSessionIsTheDeviceClientWhateverTheAuthMode(t *testing.T) {
	ctx := context.Background()

	twin := newSessionProbe(t)
	c, upn, err := OpenGraphSession(ctx, sessionOpts(t, twin, "device", signInCache(t, false)))
	if err != nil || c == nil || upn != "alice@contoso.org" {
		t.Fatalf("the positive twin, a device session: %v, %q, %v", c, upn, err)
	}

	p := newSessionProbe(t)
	c, upn, err = OpenGraphSession(ctx, sessionOpts(t, p, "app", signInCache(t, true)))
	if err != nil || c == nil {
		t.Fatalf("an app-auth configuration did not give the signer's own session: %v", err)
	}
	if upn != "alice@contoso.org" {
		t.Errorf("the session's signer = %q, want the signed-in alice@contoso.org (never the options' bob)", upn)
	}
	reqs := assure.Reached(t, p.requests(), "requests the session made")
	for _, r := range reqs {
		if !strings.HasPrefix(r, "GET /me") && r != "POST /token" {
			t.Errorf("the session sent %q: only GET /me… and the refresh-token POST are allowed", r)
		}
		if strings.Contains(r, "/users/") {
			t.Errorf("the session addressed /users/: %q", r)
		}
	}
	p.mu.Lock()
	for _, f := range p.form {
		if f != nil && f.Get("grant_type") == "client_credentials" {
			t.Errorf("the session used the app-only client-credentials grant")
		}
	}
	p.mu.Unlock()

	none := newSessionProbe(t)
	opts := sessionOpts(t, none, "app", filepath.Join(t.TempDir(), "absent.token"))
	_, _, err = OpenGraphSession(ctx, opts)
	assure.Refused(t, 1, errString(err), assure.Code(1), assure.Names("not signed in"),
		assure.NoSideEffect(func() bool { return len(none.requests()) == 0 && errors.Is(err, ErrNotSignedIn) }))
}

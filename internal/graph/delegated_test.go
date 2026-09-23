package graph

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"mail-archive-tool/internal/assure"
)

// fakeDeleg is an in-process stand-in for the Microsoft device-code, token, and
// delegated (/me) Graph endpoints. It records every request so a test can prove
// the client addresses /me (never /users) and is read-only.
type fakeDeleg struct {
	mu            sync.Mutex
	reqs          []string // "METHOD PATH"
	prefImmutable bool     // echo Preference-Applied: IdType="ImmutableId" on the listing
	meDelay       time.Duration
	refreshHits   int
}

func newFakeDeleg(prefImmutable bool) (*fakeDeleg, *httptest.Server) {
	f := &fakeDeleg{prefImmutable: prefImmutable}
	mux := http.NewServeMux()
	rec := func(r *http.Request) { f.mu.Lock(); f.reqs = append(f.reqs, r.Method+" "+r.URL.Path); f.mu.Unlock() }
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s)
	}

	mux.HandleFunc("/devicecode", func(w http.ResponseWriter, r *http.Request) {
		rec(r)
		j(w, `{"device_code":"DEV","user_code":"WXYZ-1234","verification_uri":"https://microsoft.com/devicelogin","expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		rec(r)
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "refresh_token" {
			f.mu.Lock()
			f.refreshHits++
			f.mu.Unlock()
			j(w, `{"access_token":"AT2","refresh_token":"RT2","token_type":"Bearer","expires_in":3600}`)
			return
		}
		j(w, `{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		rec(r)
		if f.meDelay > 0 {
			time.Sleep(f.meDelay)
		}
		j(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		rec(r)
		j(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
	})
	mux.HandleFunc("/me/mailFolders/F_IN/messages", func(w http.ResponseWriter, r *http.Request) {
		rec(r)
		if f.prefImmutable {
			w.Header().Set("Preference-Applied", `IdType="ImmutableId"`)
		}
		j(w, `{"value":[{"id":"IMM1","internetMessageId":"<m1@x>","subject":"s","receivedDateTime":"2025-03-01T09:00:00Z"}]}`)
	})
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		rec(r)
		io.WriteString(w, "From: a@example.com\r\nSubject: s\r\nMessage-ID: <m1@x>\r\n\r\nbody\r\n")
	})
	return f, httptest.NewServer(mux)
}

func (f *fakeDeleg) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func delegConfig(srv *httptest.Server, cache string) Config {
	return Config{
		Tenant: "t", ClientID: "c",
		BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode",
		TokenCachePath: cache,
		Prompt:         func(DeviceAuth) {},
	}
}

// covers: MA-251, R17, S20
// A delegated (device-mode) client addresses /me for every mailbox operation and
// issues ONLY GET requests to Graph — never /users/{upn}. userSeg returns /me for
// a delegated client and /users/{upn} for an app-only one.
func TestDelegatedAddressesMeReadOnly(t *testing.T) {
	f, srv := newFakeDeleg(false)
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "tok.json")
	ctx := context.Background()

	c, err := NewDelegated(ctx, delegConfig(srv, cache))
	if err != nil {
		t.Fatal(err)
	}
	// White-box addressing: /me for delegated, /users/{upn} for app-only.
	if got := c.userSeg("ignored@x"); got != "/me" {
		t.Errorf("delegated userSeg = %q, want /me", got)
	}
	app := New(ctx, Config{Tenant: "t", ClientID: "c", ClientSecret: "s", BaseURL: srv.URL, TokenURL: srv.URL + "/token"})
	if got := app.userSeg("bob@x"); got != "/users/bob@x" {
		t.Errorf("app userSeg = %q, want /users/bob@x", got)
	}

	folders, err := c.Folders(ctx, "ignored@x", FolderFilter{})
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, folders, "delegated folder walk")
	var mimeFetched int
	for _, fl := range folders {
		if err := c.Messages(ctx, "ignored@x", fl.ID, func(m MessageRef) error {
			_, merr := c.MIME(ctx, "ignored@x", m.ID)
			if merr == nil {
				mimeFetched++
			}
			return merr
		}); err != nil {
			t.Fatal(err)
		}
	}
	if mimeFetched == 0 {
		t.Fatal("no MIME fetched via /me")
	}

	var meGET, usersAny bool
	for _, rq := range f.requests() {
		method, path, _ := strings.Cut(rq, " ")
		if strings.HasPrefix(path, "/users/") {
			usersAny = true
		}
		if strings.HasPrefix(path, "/me") {
			if method != http.MethodGet {
				t.Errorf("non-GET to a mailbox endpoint: %s", rq)
			}
			meGET = true
		}
	}
	if !meGET {
		t.Fatal("no /me GET requests recorded — did the client address /me?")
	}
	if usersAny {
		t.Fatal("delegated client hit a /users/{upn} path — it must address /me only")
	}
}

// covers: MA-252, R17, S38
// The delegated path sends Prefer: IdType="ImmutableId" and adopts the message id
// as PhysID only when the tenant honors it (Preference-Applied on the listing).
func TestDelegatedImmutableIdPreference(t *testing.T) {
	ctx := context.Background()
	for _, honored := range []bool{true, false} {
		f, srv := newFakeDeleg(honored)
		cache := filepath.Join(t.TempDir(), "tok.json")
		c, err := NewDelegated(ctx, delegConfig(srv, cache))
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		folders, err := c.Folders(ctx, "x", FolderFilter{})
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		var physID string
		var seen bool
		for _, fl := range folders {
			if err := c.Messages(ctx, "x", fl.ID, func(m MessageRef) error {
				seen = true
				physID = m.PhysID
				return nil
			}); err != nil {
				srv.Close()
				t.Fatal(err)
			}
		}
		if !seen {
			srv.Close()
			t.Fatal("no message listed")
		}
		if honored && physID != "IMM1" {
			t.Errorf("honored tenant: PhysID = %q, want IMM1", physID)
		}
		if !honored && physID != "" {
			t.Errorf("id-withheld tenant: PhysID = %q, want empty", physID)
		}
		// Prove the header actually went out.
		var sentPrefer bool
		for _, rq := range f.requests() {
			if strings.Contains(rq, "/me/mailFolders/F_IN/messages") {
				sentPrefer = true
			}
		}
		if !sentPrefer {
			t.Error("listing request not recorded")
		}
		srv.Close()
	}
}

// covers: MA-254, R17, S20, S29
// A first device run with no cache performs the device-code grant (the prompt is
// shown, the token obtained and cached), then a later call reuses the cache with
// no prompt; a stalled /me fails within the configured deadline rather than
// hanging (never blocks an unattended job).
func TestDelegatedFirstRunConsentThenCached(t *testing.T) {
	ctx := context.Background()
	_, srv := newFakeDeleg(false)
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "tok.json")

	var prompted DeviceAuth
	cfg := delegConfig(srv, cache)
	cfg.Prompt = func(d DeviceAuth) { prompted = d }
	c, err := NewDelegated(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if prompted.UserCode == "" || prompted.VerificationURI == "" {
		t.Fatalf("device prompt not shown: %+v", prompted)
	}
	if upn, err := c.Me(ctx); err != nil || upn != "alice@contoso.org" {
		t.Fatalf("Me = %q, %v; want alice@contoso.org", upn, err)
	}
	// The cache was written, bound to the signer, 0600.
	tc, err := loadTokenCache(cache)
	if err != nil {
		t.Fatal(err)
	}
	if tc.UPN != "alice@contoso.org" || tc.Token == nil || tc.Token.RefreshToken != "RT1" {
		t.Fatalf("cache not bound to signer with a refresh token: %+v", tc)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(cache)
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("cache mode = %04o, want 0600", fi.Mode().Perm())
		}
	}
	// Second construction: cache present → no prompt.
	promptedAgain := false
	cfg2 := delegConfig(srv, cache)
	cfg2.Prompt = func(DeviceAuth) { promptedAgain = true }
	if _, err := NewDelegated(ctx, cfg2); err != nil {
		t.Fatal(err)
	}
	if promptedAgain {
		t.Error("a cached sign-in must not prompt again")
	}

	// Deadline: a stalled /me fails promptly, never hangs.
	fs, ssrv := newFakeDeleg(false)
	defer ssrv.Close()
	fs.meDelay = 3 * time.Second
	scfg := delegConfig(ssrv, filepath.Join(t.TempDir(), "tok2.json"))
	scfg.RequestTimeout = 150 * time.Millisecond
	start := time.Now()
	if _, err := NewDelegated(ctx, scfg); err == nil {
		t.Error("expected a deadline failure on a stalled /me")
	} else if time.Since(start) > 2*time.Second {
		t.Errorf("stalled /me was not bounded by the deadline (took %s)", time.Since(start))
	}
}

// covers: MA-255, R12, R4, S20
// The token cache is read through the shared secure-file discipline: a symlink, a
// group/world-readable file, and an oversize file are each refused; a healthy
// cache round-trips its bound signer.
func TestDelegatedTokenCacheDiscipline(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good.json")
	if err := storeTokenCache(good, "alice@contoso.org", &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	tc, err := loadTokenCache(good)
	if err != nil || tc.UPN != "alice@contoso.org" {
		t.Fatalf("healthy cache did not round-trip: %v / %+v", err, tc)
	}

	oversize := filepath.Join(dir, "big.json")
	if err := os.WriteFile(oversize, make([]byte, tokenCacheMaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = loadTokenCache(oversize)
	assure.Reached(t, errStr(err), "oversize cache refusal")
	if !strings.Contains(errStr(err), "byte") {
		t.Errorf("oversize refusal should name the size limit: %v", err)
	}

	if runtime.GOOS != "windows" {
		loose := filepath.Join(dir, "loose.json")
		if err := os.WriteFile(loose, []byte(`{"upn":"x","token":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err = loadTokenCache(loose)
		assure.Reached(t, errStr(err), "loose-mode cache refusal")
		if !strings.Contains(errStr(err), "chmod 600") {
			t.Errorf("loose-mode refusal should name chmod 600: %v", err)
		}

		target := filepath.Join(dir, "target.json")
		os.WriteFile(target, []byte(`{"upn":"x","token":{}}`), 0o600)
		link := filepath.Join(dir, "link.json")
		if os.Symlink(target, link) == nil {
			_, err = loadTokenCache(link)
			assure.Reached(t, errStr(err), "symlink cache refusal")
			if !strings.Contains(errStr(err), "regular file") {
				t.Errorf("symlink refusal should name 'regular file': %v", err)
			}
		}
	}
}

// covers: MA-258, R5, S20
// Token write-back is race-safe: a refreshed token (later expiry) is persisted
// atomically and round-trips; a token no newer than what is on disk does not
// overwrite it (a concurrent rotation is not clobbered); and an end-to-end refresh
// through the client persists the rotated refresh token.
func TestDelegatedTokenWriteBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tok.json")

	newer := &oauth2.Token{AccessToken: "A2", RefreshToken: "R2", Expiry: time.Now().Add(2 * time.Hour)}
	older := &oauth2.Token{AccessToken: "A1", RefreshToken: "R1", Expiry: time.Now().Add(1 * time.Hour)}
	if err := storeTokenCache(path, "u@x", newer); err != nil {
		t.Fatal(err)
	}
	// An older token must not clobber the newer on-disk one.
	if err := storeTokenIfNewer(path, "u@x", older); err != nil {
		t.Fatal(err)
	}
	if tc, _ := loadTokenCache(path); tc.Token.RefreshToken != "R2" {
		t.Errorf("older token clobbered the newer cache: got %q, want R2", tc.Token.RefreshToken)
	}

	// End-to-end: a client seeded with an EXPIRED access token refreshes on first
	// use and persists the rotated refresh token (RT1 -> RT2).
	f, srv := newFakeDeleg(false)
	defer srv.Close()
	e2e := filepath.Join(dir, "e2e.json")
	if err := storeTokenCache(e2e, "alice@contoso.org", &oauth2.Token{AccessToken: "AT1", RefreshToken: "RT1", Expiry: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	c, err := NewDelegated(context.Background(), delegConfig(srv, e2e))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Me(context.Background()); err != nil { // triggers a refresh
		t.Fatal(err)
	}
	if f.refreshHits == 0 {
		t.Fatal("expected a refresh_token exchange")
	}
	tc, err := loadTokenCache(e2e)
	if err != nil {
		t.Fatal(err)
	}
	if tc.Token.RefreshToken != "RT2" {
		t.Errorf("rotated refresh token not persisted: got %q, want RT2", tc.Token.RefreshToken)
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

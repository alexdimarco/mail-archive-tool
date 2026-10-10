package graph

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// meClient is a delegated (/me) client on srv whose saved sign-in holds the given
// access token, so a test chooses whether the first request refreshes (an expired
// token) or not. cfg's bounds are used as given.
func meClient(t *testing.T, srv *httptest.Server, cfg Config, expiry time.Time) *Client {
	t.Helper()
	cfg.Tenant, cfg.ClientID = "t", "c"
	cfg.BaseURL, cfg.TokenURL, cfg.DeviceAuthURL = srv.URL, srv.URL+"/token", srv.URL+"/devicecode"
	cfg.TokenCachePath = filepath.Join(t.TempDir(), "graph.token")
	cfg.Prompt = func(DeviceAuth) {}
	if err := storeTokenCache(cfg.TokenCachePath, "alice@contoso.org",
		&oauth2.Token{AccessToken: "AT", RefreshToken: "RT", Expiry: expiry}); err != nil {
		t.Fatal(err)
	}
	c, err := NewDelegated(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// foreignHost is a second fake server that records every request it receives
// and the Authorization header each carried: the host a hostile 302 points at.
type foreignHost struct {
	*httptest.Server
	mu   sync.Mutex
	auth []string
}

func newForeignHost(t *testing.T) *foreignHost {
	t.Helper()
	f := &foreignHost{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		writeJSON(w, `{"userPrincipalName":"mallory@evil.example","id":"X","value":[]}`)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *foreignHost) hits() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auth...)
}

// covers: MA-528, R17, R29
// Redirects and bodies are bounded (extraction design XC26, RR2b-C4). A Graph
// answer that redirects to another host is not followed: the foreign fake records
// zero requests, so the bearer token never reaches it, and the refusal is a
// *graph.RedirectError naming that host, found with errors.As through the
// *url.Error — on a JSON request and on SG's $value client alike. A redirect loop
// on the base host stops at the eleventh hop with the same typed error. The
// positive twin first: a redirect within the base host is followed, carrying the
// token on each hop.
func TestRedirectsAndBodiesAreBounded(t *testing.T) {
	foreign := newForeignHost(t)
	var mu sync.Mutex
	var sameHostAuth []string
	var loopHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	record := func(r *http.Request) {
		mu.Lock()
		sameHostAuth = append(sameHostAuth, r.Header.Get("Authorization"))
		mu.Unlock()
	}
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("via") {
		case "foreign":
			http.Redirect(w, r, foreign.URL+"/me", http.StatusFound)
		case "loop":
			n := loopHits.Add(1)
			http.Redirect(w, r, "/me?via=loop&n="+strconv.FormatInt(n, 10), http.StatusFound)
		case "same":
			record(r)
			http.Redirect(w, r, "/moved", http.StatusFound)
		default:
			writeJSON(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
		}
	})
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		writeJSON(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/me/messages/"), "/$value")
		switch id {
		case "FOREIGN":
			http.Redirect(w, r, foreign.URL+"/me/messages/FOREIGN/$value", http.StatusFound)
		case "LOOP":
			n := loopHits.Add(1)
			http.Redirect(w, r, "/me/messages/LOOP/$value?n="+strconv.FormatInt(n, 10), http.StatusFound)
		case "SAME":
			record(r)
			http.Redirect(w, r, "/moved-value", http.StatusFound)
		}
	})
	mux.HandleFunc("/moved-value", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		io.WriteString(w, "From: a@example.com\r\n\r\nbody\r\n")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := meClient(t, srv, Config{}, time.Now().Add(time.Hour))
	ctx := context.Background()
	foreignAddr := strings.TrimPrefix(foreign.URL, "http://")

	// the positive twin: a same-host redirect is followed, token on both hops
	if upn, err := c.Me(ctx); err != nil || upn != "alice@contoso.org" {
		t.Fatalf("fixture: /me: %q, %v", upn, err)
	}
	for _, how := range []string{"JSON", "$value"} {
		mu.Lock()
		sameHostAuth = nil
		mu.Unlock()
		var err error
		if how == "JSON" {
			var body struct{ UPN string }
			_, err = c.getJSON(ctx, srv.URL+"/me?via=same", &body)
		} else {
			var buf bytes.Buffer
			_, err = c.MIMETo(ctx, "", "SAME", &buf, 0, 0, 0)
			if err == nil && !strings.Contains(buf.String(), "body") {
				t.Fatalf("the followed $value redirect delivered %q", buf.String())
			}
		}
		mu.Lock()
		hops := append([]string(nil), sameHostAuth...)
		mu.Unlock()
		if err != nil || len(hops) != 2 || hops[0] != "Bearer AT" || hops[1] != "Bearer AT" {
			t.Fatalf("%s: a redirect within the base host was not followed with the token on each hop: %v, %q", how, err, hops)
		}
	}

	for _, how := range []string{"JSON", "$value"} {
		t.Run(how+"/foreign host", func(t *testing.T) {
			var err error
			if how == "JSON" {
				var body struct{ UPN string }
				_, err = c.getJSON(ctx, srv.URL+"/me?via=foreign", &body)
			} else {
				_, err = c.MIMETo(ctx, "", "FOREIGN", io.Discard, 0, 0, 0)
			}
			var re *RedirectError
			var ue *url.Error
			if !errors.As(err, &re) || !errors.As(err, &ue) {
				t.Fatalf("a redirect to another host is not a *RedirectError inside a *url.Error: %T %v", err, err)
			}
			if re.Host != foreignAddr || !strings.Contains(err.Error(), foreignAddr) {
				t.Errorf("the refusal does not name the foreign host %q: %+v, %v", foreignAddr, re, err)
			}
			if got := foreign.hits(); len(got) != 0 {
				t.Errorf("the foreign host received %d request(s), with Authorization %q: the bearer token left the base host", len(got), got)
			}
			if class, _ := Classify(err, ctx); class != ClassTransport {
				t.Errorf("the refusal's class (before the caller's own matching) = %v", class)
			}
		})
		t.Run(how+"/redirect loop", func(t *testing.T) {
			loopHits.Store(0)
			var err error
			if how == "JSON" {
				var body struct{ UPN string }
				_, err = c.getJSON(ctx, srv.URL+"/me?via=loop", &body)
			} else {
				_, err = c.MIMETo(ctx, "", "LOOP", io.Discard, 0, 0, 0)
			}
			var re *RedirectError
			if !errors.As(err, &re) || re.Hops != 11 {
				t.Fatalf("a same-host redirect loop is not stopped at the eleventh hop with a *RedirectError: %v", err)
			}
			// the original request and ten followed hops were served; the eleventh redirect was refused
			if n := loopHits.Load(); n != 11 {
				t.Errorf("the loop was served %d times, want 11", n)
			}
			if !strings.Contains(err.Error(), "11") && !strings.Contains(err.Error(), "10") {
				t.Errorf("the hop-limit refusal does not name the limit: %v", err)
			}
		})
	}
}

// covers: MA-527, R17, R29
// F15 and RR2b-C3 (extraction design XC26). Client.Timeout is set on the JSON
// clients, so a /token response whose body stalls after its headers is released
// within reqTimeout: the call that held the token source's mutex fails with a
// transport-class error instead of hanging, and an unrelated list issued behind
// it returns within about reqTimeout (before the fix it blocked behind the mutex
// for ever). SG's $value client is built without that timeout: a download that
// takes ten times reqTimeout, with a byte arriving every 20 ms, completes
// byte-exact through the same client set.
func TestClientTimeoutBoundsTokenFetchNotValue(t *testing.T) {
	const req = 100 * time.Millisecond
	var mu sync.Mutex
	tokenHits := 0
	stalled := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokenHits++
		n := tokenHits
		mu.Unlock()
		if n == 1 { // headers and a part of the body, then nothing
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, `{"access_token":"AT`)
			w.(http.Flusher).Flush()
			close(stalled)
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			return
		}
		writeJSON(w, `{"access_token":"AT2","refresh_token":"RT2","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	mux.HandleFunc("/me/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
	})
	long := strings.Repeat("x", 50)
	mux.HandleFunc("/me/messages/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		for i := 0; i < len(long); i++ {
			io.WriteString(w, long[i:i+1])
			w.(http.Flusher).Flush()
			time.Sleep(20 * time.Millisecond)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := meClient(t, srv, Config{RequestTimeout: req}, time.Now().Add(-time.Minute)) // expired: the first request refreshes
	ctx := context.Background()

	first := make(chan error, 1)
	go func() { _, err := c.Me(ctx); first <- err }()
	select {
	case <-stalled:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture: the stalled /token request never arrived")
	}
	began := time.Now()
	var listErr error
	within(t, 3*time.Second, "an unrelated list behind a stalled /token body", func() {
		_, listErr = c.Folders(ctx, "ignored", FolderFilter{IncludeDeleted: true, IncludeJunk: true})
	})
	if waited := time.Since(began); waited > req+900*time.Millisecond {
		t.Errorf("the unrelated list waited %v behind the stalled token fetch; reqTimeout is %v", waited, req)
	}
	if listErr != nil {
		if class, _ := Classify(listErr, ctx); class != ClassTransport {
			t.Errorf("the unrelated list failed with a non-transport error: %v", listErr)
		}
	}
	var firstErr error
	within(t, 3*time.Second, "the call that held the token source", func() { firstErr = <-first })
	if class, _ := Classify(firstErr, ctx); firstErr == nil || class != ClassTransport {
		t.Fatalf("the stalled token fetch is not a typed transport-class error: %v (%v)", firstErr, class)
	}
	if fs, err := c.Folders(ctx, "ignored", FolderFilter{IncludeDeleted: true, IncludeJunk: true}); err != nil || len(fs) != 1 {
		t.Fatalf("the source did not recover after the stalled fetch: %v, %v", fs, err)
	}

	// the $value client carries no overall timeout: ten times reqTimeout, byte-exact
	var buf bytes.Buffer
	start := time.Now()
	n, err := c.MIMETo(ctx, "", "LONG", &buf, 0, 0, 0)
	if err != nil || n != int64(len(long)) || buf.String() != long {
		t.Fatalf("a $value download longer than reqTimeout was cut: n=%d, err=%v", n, err)
	}
	if took := time.Since(start); took < 5*req {
		t.Fatalf("fixture: the download took %v, not longer than reqTimeout %v", took, req)
	}
}

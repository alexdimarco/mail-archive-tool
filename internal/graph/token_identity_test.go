package graph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"mail-archive-tool/internal/assure"
)

// refreshServer is a fake token endpoint for the refresh grant. It answers by the
// refresh token it is given and records every one it sees: RT1 rotates to
// RT_A2 (account A's normal refresh), RT_A_ROT to RT_A3 (a concurrent run's
// rotation of the same account), RT_B to RT_B2 (account B's) and anything else is
// a revoked grant (400 invalid_grant).
type refreshServer struct {
	*httptest.Server
	mu   sync.Mutex
	seen []string
}

func newRefreshServer(t *testing.T) *refreshServer {
	t.Helper()
	rs := &refreshServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		rt := r.Form.Get("refresh_token")
		rs.mu.Lock()
		rs.seen = append(rs.seen, rt)
		rs.mu.Unlock()
		next := map[string][2]string{"RT1": {"AT_A2", "RT_A2"}, "RT_A_ROT": {"AT_A3", "RT_A3"}, "RT_B": {"AT_B2", "RT_B2"}}
		if n, ok := next[rt]; ok {
			writeJSON(w, `{"access_token":"`+n[0]+`","refresh_token":"`+n[1]+`","token_type":"Bearer","expires_in":3600}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked"}`))
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *refreshServer) saw(rt string) bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	for _, s := range rs.seen {
		if s == rt {
			return true
		}
	}
	return false
}

// identityBackends are the two places a sign-in lives: the 0600 file and a vault
// (the TokenStore seam). exists reports whether anything is stored.
var identityBackends = []struct {
	name string
	make func(t *testing.T) (be tokenBackend, exists func() bool)
}{
	{"file", func(t *testing.T) (tokenBackend, func() bool) {
		path := filepath.Join(t.TempDir(), "graph.token")
		return tokenBackend{path: path}, func() bool { _, err := os.Stat(path); return err == nil }
	}},
	{"vault", func(t *testing.T) (tokenBackend, func() bool) {
		m := &memTokenStore{}
		return tokenBackend{store: m}, func() bool { return m.data != nil }
	}},
}

// sourceFor builds a live deviceSource for upn whose access token has expired, so
// its first Token() refreshes through rs.
func sourceFor(rs *refreshServer, be tokenBackend, upn, refresh string) *deviceSource {
	conf := &oauth2.Config{ClientID: "c", Scopes: delegatedScopes,
		Endpoint: oauth2.Endpoint{TokenURL: rs.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}}
	tok := &oauth2.Token{AccessToken: "AT_OLD", RefreshToken: refresh, Expiry: time.Now().Add(-time.Minute)}
	return newDeviceSource(context.Background(), conf, be, upn, tok)
}

// covers: MA-515, R17, R5, S20
// Token write-back is identity-safe (extraction design XC7), in the graph package
// and so for capture too. A live source persists a refreshed token only into an
// existing entry of its own account whose expiry is older: a store that was
// cleared (a vault entry or token file deleted) is not re-created on the next
// refresh, an entry for another account is not overwritten, and reload refuses a
// stored token for another account instead of refreshing with it. MA-258's
// same-UPN rotation, and the reload of a concurrent run's rotation of the same
// account, still work (the positive twins, first).
func TestTokenWriteBackIsIdentitySafe(t *testing.T) {
	older := &oauth2.Token{AccessToken: "AT_X", RefreshToken: "RT_X", Expiry: time.Now().Add(-10 * time.Minute)}

	for _, bk := range identityBackends {
		t.Run(bk.name+"/same-account rotation persists", func(t *testing.T) {
			rs := newRefreshServer(t)
			be, _ := bk.make(t)
			if err := be.put("alice@contoso.org", older); err != nil {
				t.Fatal(err)
			}
			tok, err := sourceFor(rs, be, "alice@contoso.org", "RT1").Token()
			if err != nil || tok.AccessToken != "AT_A2" {
				t.Fatalf("refresh: %v, %v", tok, err)
			}
			tc, err := be.load()
			if err != nil || tc.UPN != "alice@contoso.org" || tc.Token.RefreshToken != "RT_A2" {
				t.Fatalf("the same-account rotation was not persisted: %+v, %v", tc, err)
			}
		})

		t.Run(bk.name+"/cleared store is not re-created", func(t *testing.T) {
			rs := newRefreshServer(t)
			be, exists := bk.make(t)
			if err := be.put("alice@contoso.org", older); err != nil {
				t.Fatal(err)
			}
			src := sourceFor(rs, be, "alice@contoso.org", "RT1")
			if err := be.clear(); err != nil {
				t.Fatal(err)
			}
			if exists() {
				t.Fatal("fixture: the sign-in was not cleared")
			}
			tok, err := src.Token()
			if err != nil || tok.AccessToken != "AT_A2" {
				t.Fatalf("the live source must still work from memory: %v, %v", tok, err)
			}
			if exists() {
				t.Error("a live source re-created a cleared sign-in on its refresh")
			}
			if err := be.putIfNewer("alice@contoso.org", &oauth2.Token{RefreshToken: "RT_Y", Expiry: time.Now().Add(time.Hour)}); err != nil || exists() {
				t.Errorf("putIfNewer wrote into a missing store: err=%v, exists=%v", err, exists())
			}
		})

		t.Run(bk.name+"/another account's entry is not overwritten", func(t *testing.T) {
			rs := newRefreshServer(t)
			be, _ := bk.make(t)
			if err := be.put("bob@contoso.org", older); err != nil {
				t.Fatal(err)
			}
			if _, err := sourceFor(rs, be, "alice@contoso.org", "RT1").Token(); err != nil {
				t.Fatal(err)
			}
			tc, err := be.load()
			if err != nil || tc.UPN != "bob@contoso.org" || tc.Token.RefreshToken != "RT_X" {
				t.Errorf("a live source for alice overwrote bob's entry: %+v, %v", tc, err)
			}
		})

		t.Run(bk.name+"/reload adopts the same account's rotation", func(t *testing.T) {
			rs := newRefreshServer(t)
			be, _ := bk.make(t)
			rotated := &oauth2.Token{AccessToken: "AT_R", RefreshToken: "RT_A_ROT", Expiry: time.Now().Add(-time.Minute)}
			if err := be.put("alice@contoso.org", rotated); err != nil {
				t.Fatal(err)
			}
			tok, err := sourceFor(rs, be, "alice@contoso.org", "RT_STALE").Token()
			if err != nil || tok.AccessToken != "AT_A3" || !rs.saw("RT_A_ROT") {
				t.Fatalf("a concurrent rotation of the same account was not adopted: %v, %v", tok, err)
			}
		})

		t.Run(bk.name+"/reload refuses another account's token", func(t *testing.T) {
			rs := newRefreshServer(t)
			be, _ := bk.make(t)
			bobs := &oauth2.Token{AccessToken: "AT_B", RefreshToken: "RT_B", Expiry: time.Now().Add(time.Hour)}
			if err := be.put("bob@contoso.org", bobs); err != nil {
				t.Fatal(err)
			}
			tok, err := sourceFor(rs, be, "alice@contoso.org", "RT_STALE").Token()
			code, msg := 0, ""
			if err != nil {
				code, msg = 1, err.Error()
			}
			var re *oauth2.RetrieveError
			assure.Refused(t, code, msg, assure.Code(1), assure.Names("invalid_grant"),
				assure.NoSideEffect(func() bool {
					tc, lerr := be.load()
					return !rs.saw("RT_B") && lerr == nil && tc.UPN == "bob@contoso.org" && tc.Token.RefreshToken == "RT_B"
				}))
			if tok != nil || !errors.As(err, &re) {
				t.Errorf("the refusal is not the revoked grant's own typed error: %v, %v", tok, err)
			}
		})
	}

	// AI1 tombstone: a sign-in as another account that lands between putIfNewer's
	// load and its write is not overwritten by the old account's rotation.
	t.Run("a sign-in inside the load-to-write window is not lost", func(t *testing.T) {
		hs := &windowStore{memTokenStore: &memTokenStore{}}
		be := tokenBackend{store: hs}
		if err := be.put("alice@contoso.org", older); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		hs.onLoad = func() {
			go func() {
				_ = be.put("bob@contoso.org", &oauth2.Token{RefreshToken: "RT_B", Expiry: time.Now().Add(time.Hour)})
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(200 * time.Millisecond):
			}
		}
		fresh := &oauth2.Token{RefreshToken: "RT_A2", Expiry: time.Now().Add(time.Hour)}
		if err := be.putIfNewer("alice@contoso.org", fresh); err != nil {
			t.Fatal(err)
		}
		<-done
		tc, err := be.load()
		if err != nil || tc.UPN != "bob@contoso.org" || tc.Token.RefreshToken != "RT_B" {
			t.Errorf("the old account's write-back overwrote a newer sign-in: %+v, %v", tc, err)
		}
	})
}

// windowStore runs onLoad once, inside the first LoadToken, to place an event in
// the window between a caller's load and its write.
type windowStore struct {
	*memTokenStore
	onLoad func()
}

func (w *windowStore) LoadToken() ([]byte, error) {
	d, err := w.memTokenStore.LoadToken()
	if f := w.onLoad; f != nil {
		w.onLoad = nil
		f()
	}
	return d, err
}

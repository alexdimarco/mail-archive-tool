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
)

// covers: MA-517, R17, R29, S52
// ForceRefresh is the copy export's one forced token refresh after a 401 (extraction
// design §4.13, XC9): a cached access token the server stopped accepting is
// dropped and a new one is fetched from the refresh token now, so the next
// request carries it; when the token endpoint refuses the refresh the error is the
// refresh's own, an *oauth2.RetrieveError, which the export reads as "sign-in
// refused" (R-c), never as a network failure.
func TestForceRefreshFetchesANewTokenOrReportsTheRefusal(t *testing.T) {
	var mu sync.Mutex
	var refreshes int
	refuse := false
	var auth []string
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		defer mu.Unlock()
		if refuse {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked"}`))
			return
		}
		refreshes++
		writeJSON(w, `{"access_token":"AT2","refresh_token":"RT2","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/me", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = append(auth, r.Header.Get("Authorization"))
		mu.Unlock()
		writeJSON(w, `{"userPrincipalName":"alice@contoso.org","id":"OID1"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cache := filepath.Join(t.TempDir(), "graph.token")
	// The cached token is valid for half an hour, so the refreshed one (an hour) is
	// newer and the write-back replaces it: the refused case below then has no
	// concurrent rotation to fall back on.
	primed := `{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","token_type":"Bearer","expiry":"` +
		time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339) + `"}}`
	if err := os.WriteFile(cache, []byte(primed), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewDelegated(context.Background(), Config{Tenant: "t", ClientID: "c", BaseURL: srv.URL,
		TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode", TokenCachePath: cache, Unattended: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.Me(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if refreshes != 0 || auth[len(auth)-1] != "Bearer AT1" {
		t.Fatalf("a valid cached token was refreshed or not used: refreshes %d, authorization %v", refreshes, auth)
	}
	mu.Unlock()

	if err := c.ForceRefresh(ctx); err != nil {
		t.Fatalf("ForceRefresh with a working refresh token failed: %v", err)
	}
	if _, err := c.Me(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if refreshes != 1 || auth[len(auth)-1] != "Bearer AT2" {
		t.Fatalf("ForceRefresh did not fetch and use a new token: refreshes %d, authorization %v", refreshes, auth)
	}
	refuse = true
	mu.Unlock()

	err = c.ForceRefresh(ctx)
	var re *oauth2.RetrieveError
	if !errors.As(err, &re) || re.ErrorCode != "invalid_grant" {
		t.Fatalf("a refused refresh is not the token endpoint's own *oauth2.RetrieveError: %v", err)
	}
}

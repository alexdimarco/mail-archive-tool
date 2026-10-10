package graph

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// covers: MA-401, R17, R29
// AC1 tombstone (adversarial pass, slice EW-E1): a page whose request failed can
// be fetched again through the loop's own guard. The guard counts a link only once
// it has been fetched, so a transient fault is not read as "the listing did not
// terminate"; a link that was fetched successfully is still a repeat.
func TestPageGuardAllowsRetryOfFailedPage(t *testing.T) {
	var hits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/mailFolders/F/messages", func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, `{"error":{"code":"BadGateway"}}`, http.StatusBadGateway)
			return
		}
		writeJSON(w, `{"value":[]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := meClient(t, srv, Config{}, time.Now().Add(time.Hour))
	ctx := context.Background()
	link := srv.URL + "/me/mailFolders/F/messages?$skiptoken=2"
	g := c.NewPageGuard()

	var se *StatusError
	if _, _, err := c.MessagePage(ctx, "ignored", "F", link, PageOpts{Guard: g}); !errors.As(err, &se) || se.Code != 502 {
		t.Fatalf("fixture: the first fetch is not the 502: %v", err)
	}
	if _, _, err := c.MessagePage(ctx, "ignored", "F", link, PageOpts{Guard: g}); err != nil || hits.Load() != 2 {
		t.Fatalf("the retry of a failed page through the same guard was refused or unsent: %v (%d requests)", err, hits.Load())
	}
	var le *ListingError
	if _, _, err := c.MessagePage(ctx, "ignored", "F", link, PageOpts{Guard: g}); !errors.As(err, &le) || le.Kind != ListingRepeat {
		t.Errorf("a page fetched successfully is no longer a repeat: %v", err)
	}
}

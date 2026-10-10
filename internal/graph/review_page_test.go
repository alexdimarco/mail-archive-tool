package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// covers: MA-314, R10, R17, S46
// AO2: a JSON listing page is bounded in size, not only in time. A page that
// streams on past the bound fails the listing with a typed *ListingError of kind
// page-size instead of growing the heap until the request timeout.
func TestFoldersPageSizeIsBounded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u/mailFolders/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("/users/u/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"value":[`)
		item := `{"id":"F","displayName":"` + strings.Repeat("n", 900) + `","childFolderCount":0},`
		for sent := 0; sent <= maxPageBytes+(1<<20); sent += len(item) {
			if _, err := io.WriteString(w, item); err != nil {
				return
			}
		}
		io.WriteString(w, `{"id":"G","displayName":"g","childFolderCount":0}]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	_, err := c.Folders(context.Background(), "u", FolderFilter{})
	var le *ListingError
	if !errors.As(err, &le) || le.Kind != ListingPageSize || le.Limit != maxPageBytes {
		t.Fatalf("an over-size page is not the typed page-size stop: %v", err)
	}
}

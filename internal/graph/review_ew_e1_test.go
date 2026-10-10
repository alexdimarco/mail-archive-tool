package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// covers: MA-528, R17, R29
// AO1 tombstone (adversarial pass, slice EW-E1): the first request FolderTree
// makes, the well-known folder id, reads its JSON body through the same size limit
// as every other Graph page (XC26). A body that streams past the limit is refused
// with the typed page-size stop, not decoded in full.
func TestWellKnownFolderIDBodyIsBounded(t *testing.T) {
	var sent atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/mailFolders/deletedItems", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"`)
		chunk := []byte(strings.Repeat("A", 1<<20))
		for i := 0; i < maxPageBytes/(1<<20)+8; i++ {
			n, err := w.Write(chunk)
			sent.Add(int64(n))
			if err != nil {
				return
			}
		}
		_, _ = io.WriteString(w, `"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := meClient(t, srv, Config{}, time.Now().Add(time.Hour))

	id, err := c.WellKnownFolderID(context.Background(), "ignored", "deletedItems")
	var le *ListingError
	if !errors.As(err, &le) || le.Kind != ListingPageSize || len(id) != 0 {
		t.Fatalf("an over-limit well-known body was not refused: id %d bytes, err %v (server sent %d)", len(id), err, sent.Load())
	}
}

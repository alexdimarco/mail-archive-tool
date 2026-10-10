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

// covers: MA-515, R17, R5, S20
// AI1's cross-process half, an open gap and not accepted: the write-back lock is
// per process, so a sign-in made by the dashboard process inside a scheduled
// run's load-to-write window can still be overwritten by that run's rotation
// (the loss is a forced re-sign-in; no token is written under another identity).
// Closing it needs a file or vault lock shared by the processes, which is an owner
// decision (platform locking on all three OSes).
func TestTokenWriteBackCrossProcessSignIn(t *testing.T) {
	t.Skip("MA-515 (XC7, 'a live source never decides who is signed in'): the write-back lock is per process; a cross-process sign-in inside putIfNewer's load-to-write window is still overwritten. Needs an owner decision on a cross-process token-store lock")
}

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

package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"mail-archive-tool/internal/graph/graphtest"
)

// covers: MA-402, R17, R29
// MIMETo's size stop names the limit applied and the response's declared length
// (-1 when it sent none), for the early refusal and for a body that streams past
// the limit alike (extraction design §4.13, RR2b-C8): the copy export passes a
// limit below the shared cap when its free-space budget is smaller, and tells an
// item above the cap from its own budget by Declared.
func TestTooLargeErrorNamesDeclaredLength(t *testing.T) {
	srv := valueServer(t, map[string]http.HandlerFunc{
		"DECLARED": graphtest.Sized(4096, true),  // Content-Length 4096, then nothing
		"CHUNKED":  graphtest.Sized(4096, false), // streamed, no length
	})
	c := boundedClient(srv, Config{MIMETimeout: time.Second, MIMEIdleTimeout: 200 * time.Millisecond})
	ctx := context.Background()
	for _, tc := range []struct {
		id       string
		declared int64
	}{{"DECLARED", 4096}, {"CHUNKED", -1}} {
		var tl *TooLargeError
		_, err := c.MIMETo(ctx, "u", tc.id, io.Discard, 1024, 0, 5*time.Second)
		if !errors.As(err, &tl) {
			t.Fatalf("%s: a body over the limit is not a typed *TooLargeError: %v", tc.id, err)
		}
		if tl.Limit != 1024 || tl.Declared != tc.declared {
			t.Errorf("%s: TooLargeError = {Limit %d, Declared %d}, want {1024, %d}", tc.id, tl.Limit, tl.Declared, tc.declared)
		}
	}
}

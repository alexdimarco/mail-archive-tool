package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"mail-archive-tool/internal/graph/graphtest"
)

// covers: MA-402, R17, R29
// graph.Classify is the one classifier both consumers share (fault-isolation
// §4.8.1, extraction §4.13). It sorts the client's errors through errors.As on
// the typed StatusError and SG's typed stops, never on their text, and says
// whether a breaker counts the fault as transport. The walk context being done
// wins over every other class (a download's own timers are never read as the
// caller's cancel, and the reverse); a failed token refresh is auth, never the
// network error it travels inside; 500 with any Graph code is an item failure,
// never transport. What each class means for an item (the 404 class, the 403
// handling, the breaker thresholds) stays with each consumer.
func TestClassifyIsTheSharedClassifier(t *testing.T) {
	srv := valueServer(t, map[string]http.HandlerFunc{
		"THROTTLED": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
		},
		"STALL": graphtest.StallMidBody("partial"),
	})
	c := boundedClient(srv, Config{MIMEIdleTimeout: 100 * time.Millisecond})
	ctx := context.Background()
	_, throttled := c.MIME(ctx, "u", "THROTTLED")
	_, stalled := c.MIME(ctx, "u", "STALL")

	// A token endpoint that refuses the client: the Graph request fails inside
	// the transport with the refresh's *oauth2.RetrieveError.
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid_client","error_description":"AADSTS7000215: Invalid client secret provided."}`)
	}))
	defer tokens.Close()
	refusing := New(ctx, Config{Tenant: "t", ClientID: "c", ClientSecret: "bad", BaseURL: srv.URL, TokenURL: tokens.URL})
	_, refresh := refusing.MIME(ctx, "u", "ANY")

	// No response at all: the server is gone.
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	unreachable := New(ctx, Config{Tenant: "t", ClientID: "c", ClientSecret: "s", BaseURL: goneURL, TokenURL: srv.URL + "/token"})
	_, network := unreachable.MIME(ctx, "u", "ANY")

	var syntax error = json.Unmarshal([]byte("{"), new(map[string]any))
	done, cancel := context.WithCancel(ctx)
	cancel()

	cases := []struct {
		name      string
		err       error
		walkCtx   context.Context
		class     Class
		transport bool
	}{
		{"no error", nil, ctx, ClassNone, false},
		{"401", &StatusError{Code: 401, GraphCode: "InvalidAuthenticationToken"}, ctx, ClassAuth, false},
		{"failed token refresh", refresh, ctx, ClassAuth, false},
		{"403", &StatusError{Code: 403, GraphCode: "ErrorAccessDenied"}, ctx, ClassForbidden, false},
		{"404", &StatusError{Code: 404}, ctx, ClassNotFound, false},
		{"ErrorItemNotFound under another status", &StatusError{Code: 400, GraphCode: "ErrorItemNotFound"}, ctx, ClassNotFound, false},
		{"500 conversion failure", &StatusError{Code: 500, GraphCode: "ErrorMimeContentConversionFailed"}, ctx, ClassFailed, false},
		{"502", &StatusError{Code: 502}, ctx, ClassTransport, true},
		{"504", &StatusError{Code: 504}, ctx, ClassTransport, true},
		{"503 after get's retries", throttled, ctx, ClassTransport, true},
		{"no response", network, ctx, ClassTransport, true},
		{"a transport's own context.Canceled while the walk is live", &url.Error{Op: "Get", URL: "u", Err: context.Canceled}, ctx, ClassTransport, true},
		{"idle stop", stalled, ctx, ClassTimeout, true},
		{"overall stop, wrapped", fmt.Errorf("fetch message M: %w", &TimeoutError{Kind: TimeoutOverall, Limit: time.Hour, MessageID: "M"}), ctx, ClassTimeout, true},
		{"over the size cap", &TooLargeError{MessageID: "M", Limit: 1}, ctx, ClassTooLarge, false},
		{"anything else", syntax, ctx, ClassFailed, false},
		{"the walk context is done", stalled, done, ClassCancelled, false},
		{"the walk context is done, any error", &StatusError{Code: 500}, done, ClassCancelled, false},
	}
	for _, tc := range cases {
		if tc.name != "no error" && tc.err == nil {
			t.Fatalf("%s: the fixture produced no error to classify", tc.name)
		}
		class, transport := Classify(tc.err, tc.walkCtx)
		if class != tc.class || transport != tc.transport {
			t.Errorf("%s: Classify(%v) = (%v, transport %v), want (%v, transport %v)", tc.name, tc.err, class, transport, tc.class, tc.transport)
		}
	}
}

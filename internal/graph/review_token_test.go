package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// tokenServer serves a token endpoint that takes delay to answer, and an
// immediate $value body.
func tokenServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u/messages/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "From: a@example.com\r\n\r\nbody")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// covers: MA-314, R10, R17
// AC3: a token refresh neither delays MIMETo's own stops nor counts against the
// $value header window. The overall cap ends the call at the cap while the token
// endpoint is still answering, as the typed overall stop; and a refresh longer
// than the header window, followed by prompt headers, is a success.
func TestMIMEToTokenRefreshIsOutsideItsTimers(t *testing.T) {
	t.Run("overall cap during a stalled refresh", func(t *testing.T) {
		srv := tokenServer(t, 2*time.Second)
		c := boundedClient(srv, Config{RequestTimeout: 5 * time.Second})
		start := time.Now()
		_, err := c.MIMETo(context.Background(), "u", "M", io.Discard, 0, 100*time.Millisecond, 300*time.Millisecond)
		var te *TimeoutError
		if !errors.As(err, &te) || te.Kind != TimeoutOverall || isContextErr(err) {
			t.Fatalf("the overall cap during a refresh is not the typed overall stop: %v", err)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("the stop came after %v, held by the token refresh (cap 300ms)", d)
		}
	})
	t.Run("refresh longer than the header window", func(t *testing.T) {
		srv := tokenServer(t, 500*time.Millisecond)
		c := boundedClient(srv, Config{RequestTimeout: 5 * time.Second, MIMEHeaderTimeout: 200 * time.Millisecond})
		_, err := c.MIMETo(context.Background(), "u", "M", io.Discard, 0, 200*time.Millisecond, 10*time.Second)
		if err != nil {
			t.Fatalf("a refresh of 500ms with prompt headers failed against a 200ms header window: %v", err)
		}
	})
}

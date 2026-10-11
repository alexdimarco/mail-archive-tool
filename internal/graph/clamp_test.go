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

// covers: MA-313, R17, R10, S46
// get's Retry-After sleeps are clamped (fault-isolation §4.8.6, FC10): a server that
// asks for an hour is waited for at most the clamp each (scaled to 50 ms) and the
// request's budget in all (scaled to 120 ms), so a throttled mailbox cannot hold an
// unattended run, and the archive lock, for the 2 h overall cap. With sleeps of 50,
// 50 and 20 ms the fourth answer is still a 503: the request ends as throttled
// (transport-class, which the breaker counts), after four requests and about the
// budget.
func TestRetryAfterSleepsAreClamped(t *testing.T) {
	var hits atomic.Int32
	srv := valueServer(t, map[string]http.HandlerFunc{
		"RA": func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusServiceUnavailable)
		},
	})
	c := boundedClient(srv, Config{RetryAfterMax: 50 * time.Millisecond, RetryAfterBudget: 120 * time.Millisecond})
	began := time.Now()
	_, err := c.MIME(context.Background(), "u", "RA")
	took := time.Since(began)
	var thr *throttledError
	if !errors.As(err, &thr) {
		t.Fatalf("a request throttled past its budget returned %v, want the throttled error", err)
	}
	if class, transport := Classify(err, context.Background()); class != ClassTransport || !transport {
		t.Errorf("the exhausted request classifies as %v (transport %v), want transport", class, transport)
	}
	if n := hits.Load(); n != 4 {
		t.Errorf("%d requests, want 4: sleeps of 50, 50 and 20 ms spend a 120 ms budget", n)
	}
	if took < 100*time.Millisecond || took > 3*time.Second {
		t.Errorf("the request took %v, want about the 120 ms budget and nowhere near the hour asked for", took)
	}
}

// covers: MA-314, R17, R10, S46
// Capture's overall cap is MIMEMaxDuration (2 h) from fault-isolation S3, in the
// same slice as the Retry-After clamp and the breakers (RR6-C2); a caller's
// MIMETimeout, the deprecated alias for it, still sets it, and the clamp's own
// defaults are 60 s each and 5 min in all.
func TestCaptureOverallCapDefaultsToTwoHours(t *testing.T) {
	srv := valueServer(t, nil)
	c := boundedClient(srv, Config{})
	if c.mimeTimeout != MIMEMaxDuration || MIMEMaxDuration != 2*time.Hour {
		t.Errorf("capture's overall cap is %v (MIMEMaxDuration %v), want 2h", c.mimeTimeout, MIMEMaxDuration)
	}
	if c.retryMax != 60*time.Second || c.retryBudget != 5*time.Minute {
		t.Errorf("the Retry-After clamp defaults are %v each and %v in all, want 60s and 5m", c.retryMax, c.retryBudget)
	}
	if c := boundedClient(srv, Config{MIMETimeout: 3 * time.Second}); c.mimeTimeout != 3*time.Second {
		t.Errorf("a set MIMETimeout is %v, want 3s", c.mimeTimeout)
	}
}

// covers: MA-313, R17, R10, S46
// ProbeFolder, the breaker's control request, is one request with no retry loop: a
// 503 that asks for an hour comes back at once as the typed status error (never a
// Retry-After wait), and a 2xx is nil.
func TestProbeFolderIsOneUnretriedRequest(t *testing.T) {
	var hits atomic.Int32
	mode := atomic.Int32{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u/mailFolders/F1", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if mode.Load() == 1 {
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, `{"id":"F1"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := boundedClient(srv, Config{})
	if err := c.ProbeFolder(context.Background(), "u", "F1"); err != nil {
		t.Fatalf("a healthy control request failed: %v", err)
	}
	mode.Store(1)
	hits.Store(0)
	began := time.Now()
	err := c.ProbeFolder(context.Background(), "u", "F1")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusServiceUnavailable {
		t.Fatalf("a throttled control request returned %v, want the 503 status error", err)
	}
	if hits.Load() != 1 || time.Since(began) > 2*time.Second {
		t.Errorf("%d requests in %v, want one and no Retry-After wait", hits.Load(), time.Since(began))
	}
}

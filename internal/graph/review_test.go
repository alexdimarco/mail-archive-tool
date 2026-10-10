package graph

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The tombstones of slice FI-SG's adversarial pass (docs/review-fi-sg-adversarial.md):
// each is a confirmed exploit of the primitive, kept as a permanent guard.

// stalledErrorBody answers status with the start of a JSON body and then holds
// the connection until the client goes or a minute passes.
func stalledErrorBody(status int, part string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, part)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(time.Minute):
		}
	}
}

// slowWriter sleeps on every Write: a destination that is busy (a disk, a NAS).
type slowWriter struct {
	delay time.Duration
	n     int
}

func (w *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	w.n += len(p)
	return len(p), nil
}

// covers: MA-314, R10, R17, S46
// AI1: the idle watchdog and the typed stops cover a non-200 answer's body too. A
// server that sends error headers and then stalls (or trickles) is a typed idle
// stop after the idle window, not a *StatusError after the overall cap, and
// Classify reads it as a timeout (transport), never as a plain status.
func TestMIMEToErrorBodyStallIsAnIdleStop(t *testing.T) {
	srv := valueServer(t, map[string]http.HandlerFunc{
		"E500": stalledErrorBody(http.StatusInternalServerError, `{"error":{"code":"x"`),
		"E404": stalledErrorBody(http.StatusNotFound, `{"error":`),
	})
	c := boundedClient(srv, Config{MIMEIdleTimeout: 100 * time.Millisecond, MIMEHeaderTimeout: 200 * time.Millisecond})
	for _, id := range []string{"E500", "E404"} {
		start := time.Now()
		_, err := c.MIMETo(context.Background(), "u", id, io.Discard, 0, 100*time.Millisecond, 3*time.Second)
		var te *TimeoutError
		if !errors.As(err, &te) || te.Kind != TimeoutIdle || te.MessageID != id {
			t.Fatalf("%s: a stalled error body is not a typed idle stop: %v", id, err)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("%s: the stall held the call for %v, not the idle window", id, d)
		}
		if isContextErr(err) {
			t.Errorf("%s: the stop matches a context error: %v", id, err)
		}
		if class, transport := Classify(err, context.Background()); class != ClassTimeout || !transport {
			t.Errorf("%s: Classify = %v (transport %v), want timeout", id, class, transport)
		}
	}
}

// covers: MA-314, R10, R17, S46
// AI2: the idle window times the server, not the destination. A writer that is
// slow for longer than the idle window does not fail a download whose bytes
// arrive on time.
func TestMIMEToSlowWriterIsNotAnIdleStop(t *testing.T) {
	chunk := strings.Repeat("a", 1000)
	srv := valueServer(t, map[string]http.HandlerFunc{
		"CHUNKS": func(w http.ResponseWriter, r *http.Request) {
			for i := 0; i < 3; i++ {
				io.WriteString(w, chunk)
				w.(http.Flusher).Flush()
				time.Sleep(20 * time.Millisecond)
			}
		},
	})
	c := boundedClient(srv, Config{})
	w := &slowWriter{delay: 400 * time.Millisecond}
	n, err := c.MIMETo(context.Background(), "u", "CHUNKS", w, 0, 200*time.Millisecond, 10*time.Second)
	if err != nil {
		t.Fatalf("a healthy download into a slow writer failed: %v", err)
	}
	if n != 3000 || w.n != 3000 {
		t.Errorf("wrote %d (returned %d), want 3000", w.n, n)
	}
}

// covers: MA-402, R17, R29
// AI3: StatusError.GraphCode is the body's error.code however long the body is.
// An error body cut by the 2048-byte bound still yields its code, so Classify's
// ErrorItemNotFound rule does not depend on the size of Graph's error JSON.
func TestStatusErrorGraphCodeSurvivesTheBound(t *testing.T) {
	long := `{"error":{"code":"ErrorItemNotFound","message":"` + strings.Repeat("x", 3000) + `"}}`
	other := `{"error":{"message":"` + strings.Repeat("y", 3000) + `","code":"ErrorAccessDenied"}}`
	srv := valueServer(t, map[string]http.HandlerFunc{
		"LONG": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500); io.WriteString(w, long) },
		// Code after a field that fills the bound is beyond it: no code, never a wrong one.
		"LATE": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500); io.WriteString(w, other) },
		"SHORT": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			io.WriteString(w, `{"error":{"code":"ErrorItemNotFound"}}`)
		},
	})
	c := boundedClient(srv, Config{})
	for id, want := range map[string]string{"LONG": "ErrorItemNotFound", "SHORT": "ErrorItemNotFound", "LATE": ""} {
		_, err := c.MIMETo(context.Background(), "u", id, io.Discard, 0, 0, 5*time.Second)
		var se *StatusError
		if !errors.As(err, &se) {
			t.Fatalf("%s: not a *StatusError: %v", id, err)
		}
		if se.GraphCode != want {
			t.Errorf("%s: GraphCode = %q, want %q (Text is %d bytes)", id, se.GraphCode, want, len(se.Text))
		}
		if id != "LATE" {
			if class, _ := Classify(err, context.Background()); class != ClassNotFound {
				t.Errorf("%s: Classify = %v, want not-found", id, class)
			}
		}
	}
}

// covers: MA-402, R17, R29
// AI4: Error() is today's text byte for byte on HTTP/1.1 too, where the status
// line carries the server's own reason phrase: "graph " + the response's status
// + ": " + the body.
func TestStatusErrorKeepsTheServersReasonPhrase(t *testing.T) {
	srv := valueServer(t, map[string]http.HandlerFunc{
		"PHRASE": func(w http.ResponseWriter, r *http.Request) {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			buf.WriteString("HTTP/1.1 401 Authentication Required\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
			buf.Flush()
		},
	})
	c := boundedClient(srv, Config{})
	_, err := c.MIMETo(context.Background(), "u", "PHRASE", io.Discard, 0, 0, 5*time.Second)
	if want := "graph 401 Authentication Required: {}"; err == nil || err.Error() != want {
		t.Fatalf("Error() = %v, want %q", err, want)
	}
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Errorf("not a *StatusError with Code 401: %v", err)
	}
	if got := (&StatusError{Code: 404, Text: "x"}).Error(); got != "graph 404 Not Found: x" {
		t.Errorf("a StatusError built without a response reads %q", got)
	}
}

// covers: MA-402, R17, R29
// AN1: the server's reason phrase is kept, but bounded: a hostile status line of
// a megabyte does not become a megabyte error string (log, last-run, status).
func TestStatusErrorBoundsTheServersReasonPhrase(t *testing.T) {
	long := strings.Repeat("R", 1<<20)
	srv := valueServer(t, map[string]http.HandlerFunc{
		"HUGE": func(w http.ResponseWriter, r *http.Request) {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			buf.WriteString("HTTP/1.1 401 " + long + "\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
			buf.Flush()
		},
	})
	c := boundedClient(srv, Config{})
	_, err := c.MIMETo(context.Background(), "u", "HUGE", io.Discard, 0, 0, 5*time.Second)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Fatalf("not a *StatusError with Code 401: %v", err)
	}
	if n := len(err.Error()); n > statusTextMax+statusLineMax+64 {
		t.Errorf("Error() is %d bytes for a 1 MiB reason phrase", n)
	}
	if !strings.HasPrefix(err.Error(), "graph 401 RRR") {
		t.Errorf("the bounded text lost its start: %.40q", err.Error())
	}
}

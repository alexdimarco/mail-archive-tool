// Package graphtest holds test doubles for the Microsoft Graph endpoints capture
// reads: $value handlers that misbehave in the ways fault isolation's shared
// Graph primitive SG must survive (docs/design-fault-isolation.md §4.8.6), and a
// fake Graph server for the shipped capture path (MA-540). Only tests import it.
package graphtest

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// hold is how long a misbehaving handler waits for its client to give up before
// it returns on its own, so a test server never hangs its own Close.
const hold = 30 * time.Second

// Body is the RFC 5322 message the fake serves as id's $value: its Message-ID
// is the lower-cased id at x, the one the listing announces.
func Body(id string) string {
	return "From: a@example.com\r\nSubject: subj-" + id + "\r\nMessage-ID: <" + strings.ToLower(id) +
		"@x>\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody-" + id + "\r\n"
}

// BodyNoMid is Body without a Message-ID: a message capture knows only by a
// content hash.
func BodyNoMid(id string) string {
	return "From: a@example.com\r\nSubject: subj-" + id +
		"\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n\r\nbody-" + id + "\r\n"
}

// Fail answers with an HTTP error status and Graph's error body carrying code:
// 500 with ErrorMimeContentConversionFailed is the poison message of F9.
func Fail(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, `{"error":{"code":"`+code+`","message":"the service said no"}}`)
	}
}

// Status answers with a bare HTTP status and, when retryAfter is not empty, that
// Retry-After (seconds): a 503 with "0" is a throttle capture retries at once.
func Status(status int, retryAfter string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
	}
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// wait blocks until the client has gone or d has passed.
func wait(r *http.Request, d time.Duration) {
	select {
	case <-r.Context().Done():
	case <-time.After(d):
	}
}

// StallMidBody sends the response headers and part of a body, then nothing more
// until the client gives up: MIMETo's idle stop.
func StallMidBody(part string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, part)
		flush(w)
		wait(r, hold)
	}
}

// NoHeaders accepts the request and never answers it: MIMETo's header stop.
func NoHeaders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { wait(r, hold) }
}

// Trickle sends one byte of body every interval, n bytes in all, or until the
// client gives up when n < 0: progress that never trips the idle window.
func Trickle(every time.Duration, n int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flush(w)
		for i := 0; n < 0 || i < n; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(every):
			}
			if _, err := w.Write([]byte{'x'}); err != nil {
				return
			}
			flush(w)
		}
	}
}

// DelayHeaders answers with body only after d: headers later than a JSON
// request's timeout but inside the $value header window.
func DelayHeaders(d time.Duration, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wait(r, d)
		io.WriteString(w, body)
	}
}

// ThrottleOnce answers its first request 503 with the given Retry-After (in
// seconds) and every later one with body: get's throttling retry.
func ThrottleOnce(retryAfter, body string) http.HandlerFunc {
	var mu sync.Mutex
	served := false
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		first := !served
		served = true
		mu.Unlock()
		if first {
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, body)
	}
}

// Sized sends a body of n bytes. With contentLength it declares the length in
// its headers and then sends nothing until the client gives up, so a client
// that reads the body is caught waiting; without it the body is streamed
// chunked and complete.
func Sized(n int64, contentLength bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if contentLength {
			w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
			w.WriteHeader(http.StatusOK)
			flush(w)
			wait(r, hold)
			return
		}
		w.WriteHeader(http.StatusOK)
		chunk := []byte(strings.Repeat("x", 32<<10))
		for left := n; left > 0; {
			c := chunk
			if int64(len(c)) > left {
				c = c[:left]
			}
			if _, err := w.Write(c); err != nil {
				return
			}
			left -= int64(len(c))
		}
	}
}

// Block reports on started that the request arrived, then holds it until the
// client gives up: the window in which a test cancels the run's context.
func Block(started chan<- struct{}) http.HandlerFunc {
	var once sync.Once
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "From: a@example.com\r\n")
		flush(w)
		once.Do(func() { close(started) })
		wait(r, hold)
	}
}

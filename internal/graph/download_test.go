package graph

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/graph/graphtest"
)

// valueServer serves the token endpoint and mailbox u's $value for each message
// id: through its handler when it has one (graphtest's misbehaving ones), else
// the plain graphtest.Body.
func valueServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u/messages/"), "/$value")
		if h, ok := handlers[id]; ok {
			h(w, r)
			return
		}
		io.WriteString(w, graphtest.Body(id))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// boundedClient is an app-only client on srv with cfg's bounds.
func boundedClient(srv *httptest.Server, cfg Config) *Client {
	cfg.Tenant, cfg.ClientID, cfg.ClientSecret = "t", "c", "s"
	cfg.BaseURL, cfg.TokenURL = srv.URL, srv.URL+"/token"
	return New(context.Background(), cfg)
}

// signalWriter collects what it is given and closes reached once it holds at
// least after bytes, so a test can see a body arrive while it is still being sent.
type signalWriter struct {
	buf     bytes.Buffer
	after   int
	reached chan struct{}
	once    sync.Once
}

func (w *signalWriter) Write(p []byte) (int, error) {
	n, _ := w.buf.Write(p)
	if w.buf.Len() >= w.after {
		w.once.Do(func() { close(w.reached) })
	}
	return n, nil
}

// isContextErr reports whether err would be read as a context error by the
// callers that test the chain (RunGraph, recordRun and the CLI).
func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// covers: MA-402, R17, R29
// MIMETo is the one bounded $value download both consumers share. It streams the
// body byte-exact into the writer (the first part reaches the writer before the
// server sends the rest), stops a body idle beyond the configured window with a
// typed *TimeoutError, refuses a body over the size cap with a typed
// *TooLargeError, and on a cancel returns the caller's own context error. After
// any stop Client.MIME returns no bytes, so capture never keeps part of a
// download (no partial output). The client's MIMETimeout is a 1 s wall clock,
// which MIMETo itself never consults.
func TestMIMEToContract(t *testing.T) {
	part1, part2 := strings.Repeat("a", 64<<10), strings.Repeat("b", 64<<10)
	got1 := make(chan struct{})
	streamed := make(chan bool, 1)
	holding := make(chan struct{})
	srv := valueServer(t, map[string]http.HandlerFunc{
		"STREAM": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, part1)
			w.(http.Flusher).Flush()
			select {
			case <-got1:
				streamed <- true
			case <-time.After(2 * time.Second):
				streamed <- false
			}
			io.WriteString(w, part2)
		},
		"STALL": graphtest.StallMidBody("From: a@example.com\r\n\r\npartial"),
		"BIG":   graphtest.Sized(4096, false),
		"HOLD":  graphtest.Block(holding),
	})
	c := boundedClient(srv, Config{MIMETimeout: time.Second, MIMEIdleTimeout: 200 * time.Millisecond})
	ctx := context.Background()

	// Byte-exact, and streamed: part 1 reaches the writer while part 2 is unsent.
	w := &signalWriter{after: len(part1), reached: got1}
	n, err := c.MIMETo(ctx, "u", "STREAM", w, 0, 0, 5*time.Second)
	if err != nil {
		t.Fatalf("a healthy $value failed: %v", err)
	}
	if !<-streamed {
		t.Error("the first part did not reach the writer before the body completed: MIMETo buffered instead of streaming")
	}
	if got := w.buf.String(); got != part1+part2 || n != int64(len(part1+part2)) {
		t.Errorf("the body is not byte-exact: got %d bytes (n=%d), want %d", len(got), n, len(part1+part2))
	}

	// Idle: a typed stop naming the message and the window, never a context error.
	var te *TimeoutError
	_, err = c.MIMETo(ctx, "u", "STALL", io.Discard, 0, 200*time.Millisecond, 5*time.Second)
	if !errors.As(err, &te) || te.Kind != TimeoutIdle || te.Limit != 200*time.Millisecond || te.MessageID != "STALL" {
		t.Fatalf("a body idle past the window is not a typed idle stop: %v (%+v)", err, te)
	}
	if isContextErr(err) {
		t.Errorf("the idle stop matches a context error, which capture reads as an interrupt: %v", err)
	}
	if err.Error() != te.Error() {
		t.Errorf("MIMETo returned its idle stop inside another error, not the stop itself: %v", err)
	}
	if !strings.Contains(err.Error(), "STALL") || !strings.Contains(err.Error(), "200ms") {
		t.Errorf("the idle stop does not name the message and the limit: %v", err)
	}
	if data, err := c.MIME(ctx, "u", "STALL"); data != nil || !errors.As(err, &te) {
		t.Errorf("MIME kept part of a stalled download: %d bytes, err %v", len(data), err)
	}

	// Size cap: a typed refusal naming the message and the cap.
	var tl *TooLargeError
	_, err = c.MIMETo(ctx, "u", "BIG", io.Discard, 1024, 0, 5*time.Second)
	if !errors.As(err, &tl) || tl.Limit != 1024 || tl.MessageID != "BIG" {
		t.Fatalf("a body over the size cap is not a typed *TooLargeError: %v (%+v)", err, tl)
	}
	if !strings.Contains(err.Error(), "BIG") || !strings.Contains(err.Error(), "1024") {
		t.Errorf("the size-cap refusal does not name the message and the cap: %v", err)
	}

	// Cancel: the caller's own context error, not a typed stop of MIMETo's.
	cctx, cancel := context.WithCancel(ctx)
	go func() { <-holding; cancel() }()
	_, err = c.MIMETo(cctx, "u", "HOLD", io.Discard, 0, 0, 5*time.Second)
	if !errors.Is(err, context.Canceled) || errors.As(err, &te) {
		t.Fatalf("a cancelled download does not return the caller's context error: %v", err)
	}
	if data, err := c.MIME(cctx, "u", "HOLD"); data != nil || !errors.Is(err, context.Canceled) {
		t.Errorf("MIME kept bytes after a cancel: %d bytes, err %v", len(data), err)
	}
}

// covers: MA-402, R17, R29
// The $value request has its own response-header window, never below the idle
// window (XC26), on its own base transport rather than the JSON requests' shared
// one: headers that arrive after the JSON request timeout but inside the window
// are a success; a request that never answers is a typed header stop whose
// Limit is the idle window when MIMEHeaderTimeout is set lower; and the timer
// times each attempt, so a Retry-After sleep longer than the window but inside
// the overall cap still downloads.
func TestMIMEToHeaderWindow(t *testing.T) {
	srv := valueServer(t, map[string]http.HandlerFunc{
		"SLOW":      graphtest.DelayHeaders(250*time.Millisecond, graphtest.Body("SLOW")),
		"MUTE":      graphtest.NoHeaders(),
		"THROTTLED": graphtest.ThrottleOnce("1", graphtest.Body("THROTTLED")),
	})
	c := boundedClient(srv, Config{
		RequestTimeout:    100 * time.Millisecond,
		MIMETimeout:       3 * time.Second,
		MIMEIdleTimeout:   400 * time.Millisecond,
		MIMEHeaderTimeout: 100 * time.Millisecond, // below idle: the window is the idle window
	})
	ctx := context.Background()

	var buf bytes.Buffer
	if _, err := c.MIMETo(ctx, "u", "SLOW", &buf, 0, 400*time.Millisecond, 3*time.Second); err != nil {
		t.Fatalf("headers after the 100 ms JSON timeout but inside the header window failed: %v", err)
	}
	if buf.String() != graphtest.Body("SLOW") {
		t.Errorf("SLOW body = %q", buf.String())
	}

	start := time.Now()
	_, err := c.MIMETo(ctx, "u", "MUTE", io.Discard, 0, 400*time.Millisecond, 3*time.Second)
	var te *TimeoutError
	if !errors.As(err, &te) || te.Kind != TimeoutHeader || te.MessageID != "MUTE" {
		t.Fatalf("a request that never answers is not a typed header stop: %v (%+v)", err, te)
	}
	if te.Limit < 400*time.Millisecond {
		t.Errorf("the header window is %v, below the 400ms idle window", te.Limit)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("the header stop took %v", d)
	}
	if isContextErr(err) {
		t.Errorf("the header stop matches a context error: %v", err)
	}
	if err.Error() != te.Error() {
		t.Errorf("MIMETo returned its header stop inside another error, not the stop itself: %v", err)
	}

	buf.Reset()
	if _, err := c.MIMETo(ctx, "u", "THROTTLED", &buf, 0, 400*time.Millisecond, 3*time.Second); err != nil {
		t.Fatalf("a 1 s Retry-After sleep inside the overall cap failed (the header timer ran in the sleep?): %v", err)
	}
	if buf.String() != graphtest.Body("THROTTLED") {
		t.Errorf("THROTTLED body = %q", buf.String())
	}
}

// covers: MA-402, R17, R29
// A non-success Graph response is a typed *StatusError{Code, GraphCode, Text}:
// callers tell 404/401/403/500 apart with errors.As, GraphCode is the body's
// error.code, Text is bounded, and Error() is byte-for-byte today's text ("graph
// " + the status line + ": " + the trimmed body, at most 2048 bytes), which the
// last-run record, status's remedy needles and the CLI read. The listing path
// and the $value path return the same type.
func TestStatusErrorIsTypedAndKeepsItsText(t *testing.T) {
	long := `{"error":{"code":"ErrorInternalServerError","message":"` + strings.Repeat("x", 5000) + `"}}`
	bodies := map[string]struct {
		code int
		body string
	}{
		"M404":  {http.StatusNotFound, `{"error":{"code":"ErrorItemNotFound","message":"The specified object was not found in the store."}}`},
		"M401":  {http.StatusUnauthorized, `{"error":{"code":"InvalidAuthenticationToken","message":"AADSTS700082: the credential has expired"}}`},
		"M403":  {http.StatusForbidden, `{"error":{"code":"ErrorAccessDenied","message":"Access is denied. Check credentials and try again."}}`},
		"M500":  {http.StatusInternalServerError, `{"error":{"code":"ErrorMimeContentConversionFailed","message":"The MIME content conversion failed."}}`},
		"M502":  {http.StatusBadGateway, "  <html><body>Bad Gateway</body></html>\r\n"},
		"MLONG": {http.StatusInternalServerError, long},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u/messages/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/users/u/messages/"), "/$value")
		b, ok := bodies[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(b.code)
		io.WriteString(w, b.body)
	})
	mux.HandleFunc("/users/u/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"code":"ErrorAccessDenied","message":"Access is denied."}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)
	ctx := context.Background()

	// todaysText is the client's text before StatusError: "graph " + the
	// response's status line + ": " + the first 2048 body bytes, trimmed.
	todaysText := func(path string) string {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "graph " + resp.Status + ": " + strings.TrimSpace(string(b))
	}

	wantGraphCode := map[string]string{
		"M404": "ErrorItemNotFound", "M401": "InvalidAuthenticationToken",
		"M403": "ErrorAccessDenied", "M500": "ErrorMimeContentConversionFailed", "M502": "",
	}
	for id, b := range bodies {
		_, err := c.MIME(ctx, "u", id)
		var se *StatusError
		if !errors.As(err, &se) {
			t.Fatalf("%s: a %d $value response is not a *StatusError: %v", id, b.code, err)
		}
		if se.Code != b.code {
			t.Errorf("%s: Code = %d, want %d", id, se.Code, b.code)
		}
		if want, ok := wantGraphCode[id]; ok && se.GraphCode != want {
			t.Errorf("%s: GraphCode = %q, want %q", id, se.GraphCode, want)
		}
		if len(se.Text) > statusTextMax {
			t.Errorf("%s: Text is %d bytes, want at most %d (bounded)", id, len(se.Text), statusTextMax)
		}
		if want := todaysText("/users/u/messages/" + id + "/$value"); err.Error() != want {
			t.Errorf("%s: Error() changed:\n got %q\nwant %q", id, clip(err.Error()), clip(want))
		}
	}
	if b := bodies["MLONG"]; len(b.body) <= statusTextMax {
		t.Fatal("the long body must exceed the bound for the bounded-Text case to mean anything")
	}

	// The listing path returns the same type, and errors.As sees through a
	// caller's wrapping (capture wraps it as "list folders: %w").
	_, err := c.Folders(ctx, "u", FolderFilter{IncludeDeleted: true, IncludeJunk: true})
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusForbidden || se.GraphCode != "ErrorAccessDenied" {
		t.Fatalf("a 403 listing is not a typed *StatusError{403, ErrorAccessDenied}: %v (%+v)", err, se)
	}
	if want := todaysText("/users/u/mailFolders"); err.Error() != want {
		t.Errorf("listing Error() changed:\n got %q\nwant %q", clip(err.Error()), clip(want))
	}
}

// clip shortens a long string for a failure message.
func clip(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// covers: MA-314, R10, R17
// The primitive download bounds, scaled (idle 200 ms, overall cap 5 s, size cap
// 1 MiB), each passed to MIMETo explicitly. The client's MIMETimeout is a 1 s
// wall clock, shorter than the trickle, which MIMETo never consults:
//   - a body trickling 1 byte per 50 ms for 1.5 s completes;
//   - a body that stalls mid-way past the idle window is a typed idle stop, the
//     caller's context stays live, and the next item downloads;
//   - a body that keeps streaming past the overall cap is a typed overall stop,
//     and so is the overall cap firing inside get's Retry-After sleep;
//   - a body exactly at the size cap completes; one byte more is refused with
//     no byte past the cap delivered; a Content-Length over the cap is refused
//     before the body is read.
func TestMIMEToBounds(t *testing.T) {
	const idle, overall, sizeCap = 200 * time.Millisecond, 5 * time.Second, int64(1 << 20)
	srv := valueServer(t, map[string]http.HandlerFunc{
		"TRICKLE":  graphtest.Trickle(50*time.Millisecond, 30),
		"STALL":    graphtest.StallMidBody("From: a@example.com\r\n\r\npartial"),
		"ENDLESS":  graphtest.Trickle(50*time.Millisecond, -1),
		"ATCAP":    graphtest.Sized(sizeCap, false),
		"OVERCAP":  graphtest.Sized(sizeCap+1, false),
		"DECLARED": graphtest.Sized(sizeCap+100, true),
		"THROTTLE": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusServiceUnavailable)
		},
	})
	c := boundedClient(srv, Config{MIMETimeout: time.Second, MIMEIdleTimeout: idle})
	ctx := context.Background()

	t.Run("trickle inside the idle window completes", func(t *testing.T) {
		var buf bytes.Buffer
		if _, err := c.MIMETo(ctx, "u", "TRICKLE", &buf, sizeCap, idle, overall); err != nil {
			t.Fatalf("a 1.5 s trickle (1 byte per 50 ms) failed: %v", err)
		}
		if buf.String() != strings.Repeat("x", 30) {
			t.Errorf("trickle body = %q", buf.String())
		}
	})

	t.Run("a stall is a typed idle stop and the next item downloads", func(t *testing.T) {
		start := time.Now()
		_, err := c.MIMETo(ctx, "u", "STALL", io.Discard, sizeCap, idle, overall)
		var te *TimeoutError
		if !errors.As(err, &te) || te.Kind != TimeoutIdle || te.Limit != idle {
			t.Fatalf("a stall past %v is not a typed idle stop: %v", idle, err)
		}
		if isContextErr(err) || ctx.Err() != nil {
			t.Fatalf("the idle stop reached the caller as a context error (%v) or cancelled its context (%v)", err, ctx.Err())
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("the idle stop took %v", d)
		}
		var buf bytes.Buffer
		if _, err := c.MIMETo(ctx, "u", "NEXT", &buf, sizeCap, idle, overall); err != nil || buf.String() != graphtest.Body("NEXT") {
			t.Errorf("the next item after a stall did not download: %v", err)
		}
	})

	t.Run("the overall cap", func(t *testing.T) {
		for _, id := range []string{"ENDLESS", "THROTTLE"} {
			t.Run(id, func(t *testing.T) {
				t.Parallel()
				start := time.Now()
				_, err := c.MIMETo(ctx, "u", id, io.Discard, sizeCap, idle, overall)
				var te *TimeoutError
				if !errors.As(err, &te) || te.Kind != TimeoutOverall || te.Limit != overall || te.MessageID != id {
					t.Fatalf("%s past the %v overall cap is not a typed overall stop: %v", id, overall, err)
				}
				if isContextErr(err) {
					t.Errorf("%s: the overall stop matches a context error: %v", id, err)
				}
				if d := time.Since(start); d < overall || d > overall+3*time.Second {
					t.Errorf("%s: the overall stop fired after %v, want about %v", id, d, overall)
				}
			})
		}
	})

	t.Run("the size cap at its boundary", func(t *testing.T) {
		var buf bytes.Buffer
		if n, err := c.MIMETo(ctx, "u", "ATCAP", &buf, sizeCap, idle, overall); err != nil || n != sizeCap {
			t.Fatalf("a body exactly at the cap failed: n=%d err=%v", n, err)
		}
		buf.Reset()
		_, err := c.MIMETo(ctx, "u", "OVERCAP", &buf, sizeCap, idle, overall)
		var tl *TooLargeError
		if !errors.As(err, &tl) || tl.Limit != sizeCap {
			t.Fatalf("one byte over the cap is not refused: %v", err)
		}
		if int64(buf.Len()) > sizeCap {
			t.Errorf("%d bytes past the cap reached the writer", int64(buf.Len())-sizeCap)
		}
		buf.Reset()
		_, err = c.MIMETo(ctx, "u", "DECLARED", &buf, sizeCap, idle, overall)
		// Had the body been read, the server's silence would end in the idle stop.
		if !errors.As(err, &tl) || buf.Len() != 0 {
			t.Fatalf("a Content-Length over the cap was not refused before its body was read: %v (%d bytes written)", err, buf.Len())
		}
	})

	// The shared figure (§4.8.6's confirmation): at or above 256 MiB, above the
	// worst case of the documented routes, 150 MiB of attachments as base64 with
	// CRLF line breaks (×4/3 ×78/76) plus 256 KiB of headers.
	worst := int64(150<<20)*4/3*78/76 + 256<<10
	if MIMEMaxBytes < 256<<20 || MIMEMaxBytes < worst {
		t.Errorf("MIMEMaxBytes = %d, want at least 256 MiB and above the %d-byte worst case", MIMEMaxBytes, worst)
	}
}

// covers: MA-314, R10, R17
// The gap half of MA-314's stall case: a $value that stalls past the idle window
// becomes a fillable fetch-timeout gap, and the walk goes on to the next item.
func TestMIMEStallIsAFetchTimeoutGap(t *testing.T) {
	t.Skip("MA-314: the fetch-timeout gap case needs the S1a/S3 capture-gap ledger; until fault-isolation S3 a stall fails the mailbox through today's error path (MA-540); lands in FI-S3")
}

// covers: MA-314, R10, R17
// MA-314's fetch-too-large gap case: a body over the size cap is a terminal gap,
// with nothing written to the archive.
func TestOverCapBodyIsAFetchTooLargeGap(t *testing.T) {
	t.Skip("MA-314: the fetch-too-large gap case is open, not accepted: capture passes no size cap until Microsoft's documentation bounds a stored item (owner ruling RR6-OR1, ESC-8's safe interim; fault-isolation §4.8.6), and the gap needs S3's ledger; lands with FI-S3 once a documented figure bounds it")
}

package app

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph/graphtest"
)

// The breaker tests run the time rules of design §4.13 at a scale of one to a
// thousand: the ticker is 1 ms (1 s), the no-progress window 300 ms (5 minutes), the
// suspend threshold 30 ms (30 s), the backoff 1..20 ms (1..60 s). The suspend-
// counting clock is read through a seam that a test can make jump.

// dropConn closes the connection without answering: no HTTP response at all.
func dropConn(w http.ResponseWriter, r *http.Request) {
	if hj, ok := w.(http.Hijacker); ok {
		if c, _, err := hj.Hijack(); err == nil {
			c.Close()
		}
	}
}

// outage makes h misbehave, with dropConn, until the given time, then serve normally.
func until(t0 time.Time, d time.Duration, bad, good http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if time.Since(t0) < d {
			bad(w, r)
			return
		}
		good(w, r)
	}
}

func okBody(id string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.Write(ceBody(id)) }
}

// jumpClock is a suspend-counting clock a test can make jump: the real clock plus an
// offset.
type jumpClock struct {
	base time.Time
	off  atomic.Int64
}

func newJumpClock() *jumpClock { return &jumpClock{base: time.Now()} }

func (c *jumpClock) now() time.Duration { return time.Since(c.base) + time.Duration(c.off.Load()) }

func (c *jumpClock) jump(d time.Duration) { c.off.Add(int64(d)) }

// ids makes message ids "S01".."Snn".
func seqIDs(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%02d", prefix, i+1)
	}
	return out
}

// covers: MA-516, R29, R10, S52
// Transport backoff and the time breaker: a 30 (scaled) second outage then recovery
// completes with zero failures, backing off on the same message; and a sustained
// outage whose control request also goes unanswered is R-u once the window has passed,
// outcome 1, with the message in flight listed retryable, the run finalized STOPPED
// (never cancelled: a breaker stop is not the user's Cancel).
func TestCopyExportTransportOutageAndTheBreaker(t *testing.T) {
	// Recovery inside the window: no failure.
	f := newMeFake(t)
	t0 := time.Now()
	f.value("A1", until(t0, 30*time.Millisecond, dropConn, okBody("A1")))
	e := newCEEnv(t)
	src := f.source(t)
	res := runWith(t, e, planFor(t, e, src, idMsgs("A1", "A2")), src)
	if res.Outcome != CopyOutcomeComplete || res.Counts.Failed != 0 {
		t.Fatalf("a short outage ended as %s, %+v", res.Label, res.Counts)
	}
	if f.hit("A1") < 2 {
		t.Errorf("A1 was requested %d times: the outage was not retried on the same message", f.hit("A1"))
	}

	// A sustained outage: the connection drops and the control request is not answered.
	f = newMeFake(t)
	f.value("A1", dropConn)
	f.meFn = dropConn
	e = newCEEnv(t)
	src = f.source(t)
	res = runWith(t, e, planFor(t, e, src, idMsgs("A1", "A2", "A3")), src)
	if res.Stop == nil || res.Stop.RID != "R-u" || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("a sustained outage ended as %s (%+v), want R-u", res.Label, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("down for 5 minutes", "Export the remaining messages", "which also tries again the message that was downloading"))
	if res.Status == CopyStatusCancelled || !strings.Contains(readText(t, res.Root, copyMarkerName), "STOPPED") || strings.Contains(readText(t, res.Root, copyMarkerName), "CANCELLED") {
		t.Errorf("a breaker stop was recorded as a cancel: %s, marker %q", res.Status, readText(t, res.Root, copyMarkerName))
	}
	if fail := failureOf(t, res.Root, "A1"); fail == nil || fail["class"] != classRetryable {
		t.Errorf("the message in flight is %v, want a retryable failure", fail)
	}
	if rest, _ := e.RunRemaining(res.RunID); len(rest) != 3 || rest[0].SourceID != "A2" || rest[2].SourceID != "A1" {
		t.Errorf("the remaining messages are %d, want A2, A3 and then the in-flight A1 last", len(rest))
	}
}

// covers: MA-516, R29, R10, S52
// Twenty-five consecutive server errors with a body (500) are run-fatal R-u2, outcome
// 1: each is listed as retryable, the 25th is the in-flight message of the stop, and
// the rest are not attempted. 429, 502, 503 and 504 are not server-class: they back off
// (the transport class), never counting toward the 25.
func TestCopyExportTwentyFiveServerErrorsAreRU2(t *testing.T) {
	f := newMeFake(t)
	ids := seqIDs("S", 30)
	for _, id := range ids {
		f.value(id, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			io.WriteString(w, `{"error":{"code":"ErrorInternalServerError","message":"boom"}}`)
		})
	}
	e := newCEEnv(t)
	src := f.source(t)
	res := runWith(t, e, planFor(t, e, src, idMsgs(ids...)), src)
	if res.Stop == nil || res.Stop.RID != "R-u2" || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("25 server errors in a row ended as %s (%+v), want R-u2", res.Label, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("25 messages in a row", "Export the remaining messages"))
	if res.Counts.Failed != 25 || res.Counts.NotAttempted != 5 {
		t.Errorf("counts = %+v, want 25 failed and 5 not attempted", res.Counts)
	}
	if f.hit("S26") != 0 {
		t.Errorf("S26 was requested after the stop")
	}
}

// covers: MA-516, R29, R10, S52
// Awake time (RR2-C8, RR2b-C9): with the suspend-counting clock read through a seam
// that jumps an hour between two ticks, 180 ms (3 minutes) of outage before the jump
// and 150 ms (2.5 minutes) after it make no R-u, because the jump restarts the window;
// the run recovers and completes with zero failures. The same outage with no jump
// reaches the window and is R-u.
func TestCopyExportASuspendRestartsTheWindow(t *testing.T) {
	run := func(jump bool) *CopyResult {
		f := newMeFake(t)
		t0 := time.Now()
		bad := func(w http.ResponseWriter, r *http.Request) { dropConn(w, r) }
		f.value("A1", until(t0, 330*time.Millisecond, bad, okBody("A1")))
		f.meFn = until(t0, 330*time.Millisecond, bad, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"id":"x"}`) })
		e := newCEEnv(t)
		clk := newJumpClock()
		e.boot = clk.now
		if jump {
			time.AfterFunc(180*time.Millisecond, func() { clk.jump(time.Hour) })
		}
		src := f.source(t)
		return runWith(t, e, planFor(t, e, src, idMsgs("A1", "A2")), src)
	}
	slept := run(true)
	if slept.Outcome != CopyOutcomeComplete || slept.Counts.Failed != 0 || slept.Stop != nil {
		t.Fatalf("a suspend between two parts of an outage ended as %s (%+v), want complete with zero failures", slept.Label, slept.Stop)
	}
	awake := run(false)
	if awake.Stop == nil || awake.Stop.RID != "R-u" {
		t.Fatalf("the same outage with no suspend ended as %s (%+v), want R-u", awake.Label, awake.Stop)
	}
}

// covers: MA-516, R29, R10, S52
// A download whose bytes keep arriving never trips the breaker however long it takes
// (a 720 ms body, 12 scaled minutes, against a 300 ms window), and a 360 ms (6
// minutes) pause for a capture is not counted either: with the control request
// unanswerable, neither ends in R-u.
func TestCopyExportASlowHealthyDownloadAndACapturePauseAreNotAnOutage(t *testing.T) {
	f := newMeFake(t)
	f.meFn = dropConn // the control request would not be answered
	f.value("SLOW", func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		for i := 0; i < 72; i++ {
			w.Write([]byte("x"))
			fl.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	})
	e := newCEEnv(t)
	e.sizeCap = 1 << 20
	src := f.source(t)
	res := runWith(t, e, planFor(t, e, src, idMsgs("SLOW", "A2")), src)
	if res.Outcome != CopyOutcomeComplete || res.Stop != nil {
		t.Fatalf("a slow download with bytes arriving ended as %s (%+v)", res.Label, res.Stop)
	}

	f = newMeFake(t)
	f.meFn = dropConn
	e = newCEEnv(t)
	t0 := time.Now()
	e.captureBusy = func() bool { return time.Since(t0) < 360*time.Millisecond }
	src = f.source(t)
	res = runWith(t, e, planFor(t, e, src, idMsgs("A1", "A2")), src)
	if res.Outcome != CopyOutcomeComplete || res.Stop != nil {
		t.Fatalf("a 6-minute capture pause ended as %s (%+v), want it not counted", res.Label, res.Stop)
	}
	if time.Since(t0) < 360*time.Millisecond {
		t.Error("the run did not wait out the capture")
	}
}

// covers: MA-516, R29, R10, S52
// A download cut again and again after its body started is listed, never retried for
// ever (RR2b-C1): a $value whose body stalls partway five times is listed as retryable
// ("kept stopping partway (5 tries)") and the run exports the rest, outcome 3; a cut
// whose attempt spans a suspend is not counted, so the same stall seven times, each
// across a jump of the suspend clock, is exported on the sixth try; a trickle that
// runs the overall cap is listed after that one attempt, never backed off.
func TestCopyExportADownloadCutAgainAndAgainIsListed(t *testing.T) {
	stall := graphtest.StallMidBody("From: a@example.com\r\n\r\npartial")

	f := newMeFake(t)
	f.value("CUT", stall)
	e := newCEEnv(t)
	e.window = 5 * time.Second // this case is about the per-message bound, not the breaker
	src := f.source(t)
	src.Idle = 60 * time.Millisecond
	res := runWith(t, e, planFor(t, e, src, idMsgs("A1", "CUT", "A2")), src)
	if res.Outcome != CopyOutcomePartial || res.Counts.Exported != 2 {
		t.Fatalf("a message cut five times ended the run as %s, %+v", res.Label, res.Counts)
	}
	if fail := failureOf(t, res.Root, "CUT"); fail == nil || fail["class"] != classRetryable || !strings.Contains(fail["reason"].(string), "kept stopping partway (5 tries)") {
		t.Errorf("CUT is listed as %v", fail)
	}
	if f.hit("CUT") != 5 {
		t.Errorf("CUT was requested %d times, want 5", f.hit("CUT"))
	}

	// Cuts that span a suspend are not counted.
	f = newMeFake(t)
	clk := newJumpClock()
	var attempts atomic.Int32
	f.value("CUT", func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) > 6 {
			w.Write(ceBody("CUT"))
			return
		}
		clk.jump(time.Hour) // a suspend inside this attempt
		time.Sleep(10 * time.Millisecond)
		stall(w, r)
	})
	e = newCEEnv(t)
	e.window = 5 * time.Second
	e.boot = clk.now
	src = f.source(t)
	src.Idle = 60 * time.Millisecond
	res = runWith(t, e, planFor(t, e, src, idMsgs("CUT")), src)
	if res.Outcome != CopyOutcomeComplete || f.hit("CUT") != 7 {
		t.Fatalf("cuts across a suspend ended as %s after %d requests, want a complete export on the seventh", res.Label, f.hit("CUT"))
	}

	// A trickle past the overall cap: listed after one attempt.
	f = newMeFake(t)
	f.value("TRICKLE", graphtest.Trickle(10*time.Millisecond, -1))
	e = newCEEnv(t)
	e.window = 5 * time.Second
	src = f.source(t)
	src.Overall = 150 * time.Millisecond
	res = runWith(t, e, planFor(t, e, src, idMsgs("TRICKLE", "A2")), src)
	if f.hit("TRICKLE") != 1 || res.Counts.Exported != 1 || res.Counts.Failed != 1 {
		t.Fatalf("a trickle past the overall cap: %d requests, %+v, want one attempt listed and the rest exported", f.hit("TRICKLE"), res.Counts)
	}
	if fail := failureOf(t, res.Root, "TRICKLE"); fail == nil || !strings.Contains(fail["reason"].(string), "didn't finish downloading") {
		t.Errorf("TRICKLE is listed as %v", fail)
	}
}

// covers: MA-516, R29, R10, S52
// A message's awake-time bound (2 h, here 80 ms): one that only ever fails with a
// transport error is listed as retryable once the bound is spent, and the run goes on.
func TestCopyExportAMessageThatNeverAnswersIsListedAfterItsBound(t *testing.T) {
	f := newMeFake(t)
	f.value("DEAD", dropConn)
	e := newCEEnv(t)
	e.downloadMax = 80 * time.Millisecond
	e.window = 5 * time.Second
	src := f.source(t)
	res := runWith(t, e, planFor(t, e, src, idMsgs("DEAD", "A2")), src)
	if res.Counts.Exported != 1 || res.Counts.Failed != 1 || res.Stop != nil {
		t.Fatalf("a message that never answers: %s, %+v (%+v)", res.Label, res.Counts, res.Stop)
	}
	if fail := failureOf(t, res.Root, "DEAD"); fail == nil || fail["class"] != classRetryable || !strings.Contains(fail["reason"].(string), "didn't finish downloading within 2 hours") {
		t.Errorf("DEAD is listed as %v", fail)
	}
}

// covers: MA-516, R29, R10, S52
// The control request (RR2b-C7): a $value that never sends headers while JSON requests
// succeed is, after the window, the message's stall, not the connection's: it is listed
// "didn't start sending", the run continues and it is never R-u; and a 429 with a long
// Retry-After to the $value and to the control request is R-v2 (throttled), never R-u,
// with the message in flight listed retryable and the stop recorded as a stop, not a cancel.
func TestCopyExportTheControlRequestTellsAStalledMessageFromALostConnection(t *testing.T) {
	f := newMeFake(t)
	f.value("HANG", graphtest.NoHeaders())
	e := newCEEnv(t)
	src := f.source(t)
	res := runWith(t, e, planFor(t, e, src, idMsgs("A1", "HANG", "A3")), src)
	if res.Stop != nil || res.Outcome != CopyOutcomePartial || res.Counts.Exported != 2 {
		t.Fatalf("a message that sends no headers ended the run as %s (%+v)", res.Label, res.Stop)
	}
	if fail := failureOf(t, res.Root, "HANG"); fail == nil || fail["class"] != classRetryable || !strings.Contains(fail["reason"].(string), "didn't start sending") {
		t.Errorf("HANG is listed as %v", fail)
	}

	var once sync.Once
	f = newMeFake(t)
	throttle := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}
	f.value("THR", throttle)
	f.meFn = func(w http.ResponseWriter, r *http.Request) { once.Do(func() {}); throttle(w, r) }
	e = newCEEnv(t)
	src = f.source(t)
	res = runWith(t, e, planFor(t, e, src, idMsgs("A1", "THR", "A3")), src)
	if res.Stop == nil || res.Stop.RID != "R-v2" || res.Outcome != CopyOutcomeFailed || res.Status == CopyStatusCancelled {
		t.Fatalf("a throttled export ended as %s (%+v), want R-v2", res.Label, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("throttled", "Try again in a few minutes", "Export the remaining messages"),
		assure.Forbid("connection to Microsoft 365"))
	if fail := failureOf(t, res.Root, "THR"); fail == nil || fail["class"] != classRetryable {
		t.Errorf("THR is listed as %v", fail)
	}
}

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/oauth2"

	"mail-archive-tool/internal/graph"
)

// Failure classes (design §4.13): what an error from the source or the destination
// means for the message in flight and for the run. One classifier, graph.Classify,
// sorts every Graph error (XC25); the export's own table maps its classes onto
// per-message outcomes and run-fatal stops. A copy export is a one-off for one
// selection, so a message that is gone is terminal for that selection (capture
// retries it on every later run), and a message that would hold the run for ever is
// listed for a later retry instead (RR2b-C1, RR2b-C7).

type verdictKind int

const (
	verdictTransport verdictKind = iota // back off on the same message
	verdictList                         // list this message with its reason and move on
	verdictAuth                         // 401: one forced token refresh and one retry
	verdictFatal                        // the run stops
	verdictCancelled                    // the walk context is done
)

// verdict is the engine's reading of one failed attempt.
type verdict struct {
	kind   verdictKind
	class  string // classRetryable or classTerminal, for verdictList
	reason string // the listed reason
	stop   *CopyStop
	// cut is true when the attempt delivered body bytes and then ended in a
	// transport-class stop before the body did (RR2b-C1).
	cut bool
	// serverClass counts toward the 25-in-a-row run-fatal (R-u2).
	serverClass bool
	// headerStall counts a response-header stop (for the "didn't start sending" text).
	headerStall bool
	// overall is MIMETo's overall cap with no suspend: it reaches the message's 2 h at once.
	overall bool
}

// ceWriteError wraps an error of the destination writer, so a download that failed
// because its own disk did is told from one the server failed (the error comes
// back out of MIMETo wrapped).
type ceWriteError struct{ err error }

func (e *ceWriteError) Error() string { return e.err.Error() }
func (e *ceWriteError) Unwrap() error { return e.err }

// Windows error codes that mean the destination is gone or full (design §4.13): the
// per-OS table, matched with errors.As on syscall.Errno and never with
// errors.Is(err, syscall.ENOSPC), because on Windows syscall.ENOSPC is an invented
// constant no disk-full code maps to.
var windowsFatalCodes = map[uintptr]bool{
	112:  true, // ERROR_DISK_FULL
	39:   true, // ERROR_HANDLE_DISK_FULL
	19:   true, // ERROR_WRITE_PROTECT
	21:   true, // ERROR_NOT_READY
	1167: true, // ERROR_DEVICE_NOT_CONNECTED
	55:   true, // ERROR_DEV_NOT_EXIST
	64:   true, // ERROR_NETNAME_DELETED
	59:   true, // ERROR_UNEXP_NET_ERR
	82:   true, // ERROR_CANNOT_MAKE
}

// destinationFatal reports whether err, for the OS named goos, says the destination
// is full, read-only, unplugged or unreachable: run-fatal (R-g).
func destinationFatal(goos string, err error) bool {
	var en syscall.Errno
	if !errors.As(err, &en) {
		return false
	}
	if goos == "windows" {
		return windowsFatalCodes[uintptr(en)]
	}
	switch en {
	case syscall.ENOSPC, syscall.EDQUOT, syscall.EROFS, syscall.EIO, syscall.ENXIO, syscall.ENODEV, syscall.ESTALE, syscall.ETIMEDOUT:
		return true
	}
	return false
}

// sharingViolation reports a Windows rename refused because an antivirus scanner
// or the sync client holds the file (ERROR_SHARING_VIOLATION 32, ERROR_ACCESS_DENIED 5):
// retried in place before the message fails (XC10).
func sharingViolation(goos string, err error) bool {
	var en syscall.Errno
	return goos == "windows" && errors.As(err, &en) && (en == 32 || en == 5)
}

// classifyFetch reads one failed attempt. mctx is the attempt's own context (its
// cancellation cause names a breaker stop), walk the run's, which alone decides
// "cancelled". delivered is the body bytes the attempt wrote, spanned whether it
// spanned a suspend. refreshed is true once this message has had its forced refresh.
func (r *CopyRun) classifyFetch(err error, mctx, walk context.Context, delivered int64, spanned, refreshed bool) verdict {
	if walk.Err() != nil {
		return verdict{kind: verdictCancelled}
	}
	// A breaker stop cancelled this message's own context: its cause says which.
	var cause *ceBreakerCause
	if errors.As(context.Cause(mctx), &cause) {
		return cause.verdict
	}
	cap := r.env.cap()
	// The destination's own failure, which came back out of the download.
	var we *ceWriteError
	if errors.As(err, &we) {
		if destinationFatal(runtime.GOOS, we.err) {
			return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-g", Class: stopClassDestination, Message: we.err.Error()}}
		}
		return verdict{kind: verdictList, class: classRetryable, reason: "a file could not be written to the destination (" + leaveName(we.err) + ")"}
	}
	// A refused redirect is its own class: Classify would read it as a network error.
	var re *graph.RedirectError
	if errors.As(err, &re) {
		if re.Hops > 10 { // past the hop limit, on the same host
			return verdict{kind: verdictList, class: classRetryable, reason: "Microsoft 365 redirected this download more than 10 times"}
		}
		return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-u3", Class: stopClassRedirected, Message: re.Host}}
	}
	var tl *graph.TooLargeError
	if errors.As(err, &tl) {
		switch {
		case tl.Declared > cap, tl.Limit >= cap:
			return verdict{kind: verdictList, class: classTerminal, reason: fmt.Sprintf("larger than this version of MailArchive exports (%d MiB)", cap>>20)}
		}
		// A stop at the budget, with a declared length within the cap or none: the
		// disk is what ran out, not the message.
		return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-g", Class: stopClassDestination, Message: "no space left"}}
	}
	var rerr *oauth2.RetrieveError
	if errors.As(err, &rerr) {
		switch rerr.ErrorCode {
		case "invalid_grant", "interaction_required", "invalid_client", "unauthorized_client", "consent_required":
			return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-c", Class: stopClassSignIn}}
		}
		return verdict{kind: verdictTransport, cut: delivered > 0 && !spanned}
	}
	var se *graph.StatusError
	if errors.As(err, &se) {
		switch {
		case se.Code == http.StatusUnauthorized:
			if refreshed {
				return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-c", Class: stopClassSignIn}}
			}
			return verdict{kind: verdictAuth}
		case se.Code == http.StatusForbidden:
			return verdict{kind: verdictList, class: classTerminal, reason: "Microsoft 365 refused access to this message (HTTP 403)"}
		case se.Code == http.StatusNotFound || se.GraphCode == "ErrorItemNotFound":
			return verdict{kind: verdictList, class: classTerminal, reason: "no longer in the mailbox (moved or deleted after you selected it)"}
		case se.GraphCode == "ErrorMimeContentConversionFailed":
			return verdict{kind: verdictList, class: classRetryable, reason: "Microsoft 365 could not convert this message to MIME (ErrorMimeContentConversionFailed)"}
		case se.Code == http.StatusBadGateway || se.Code == http.StatusServiceUnavailable || se.Code == http.StatusGatewayTimeout || se.Code == http.StatusTooManyRequests:
			return verdict{kind: verdictTransport}
		case se.Code >= 500:
			return verdict{kind: verdictList, class: classRetryable, reason: fmt.Sprintf("Microsoft 365 returned a server error (HTTP %d)", se.Code), serverClass: true}
		}
		return verdict{kind: verdictList, class: classRetryable, reason: fmt.Sprintf("Microsoft 365 refused this message (HTTP %d)", se.Code)}
	}
	var te *graph.TimeoutError
	if errors.As(err, &te) {
		switch te.Kind {
		case graph.TimeoutOverall:
			if spanned { // the sleep counted against the cap: a cut, not a failure
				return verdict{kind: verdictTransport}
			}
			return verdict{kind: verdictList, class: classRetryable, overall: true,
				reason: "didn't finish downloading within 2 hours of download time"}
		case graph.TimeoutHeader:
			return verdict{kind: verdictTransport, headerStall: true}
		default: // idle
			return verdict{kind: verdictTransport, cut: delivered > 0 && !spanned}
		}
	}
	class, _ := graph.Classify(err, walk)
	switch class {
	case graph.ClassTransport:
		return verdict{kind: verdictTransport, cut: delivered > 0 && !spanned}
	case graph.ClassAuth:
		if refreshed {
			return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-c", Class: stopClassSignIn}}
		}
		return verdict{kind: verdictAuth}
	case graph.ClassCancelled:
		return verdict{kind: verdictCancelled}
	}
	return verdict{kind: verdictList, class: classRetryable, reason: "Microsoft 365 could not deliver this message"}
}

// ceBreakerCause is the cancellation cause of a message's own context when the
// no-progress breaker stopped it: the run is never read as cancelled from it
// (RR2b-C7).
type ceBreakerCause struct{ verdict verdict }

func (c *ceBreakerCause) Error() string { return "stopped by the no-progress breaker" }

// ceWatch is the run's no-progress breaker (design §4.13, RR2-C8, RR2b-C7, RR2b-C9).
// A 1-second ticker in the run counts awake time: the gap between two ticks is read
// on a clock that keeps counting in suspend, and a gap above the suspend threshold
// is a suspend, is not counted, and restarts the window. Time spent paused for a
// capture is not counted either. After a window of awake time with no progress, one
// control request decides what stopped: no answer is R-u, a 429 or 503 is R-v2, and
// a 2xx means the stall is the message's, which is listed while the run moves on.
type ceWatch struct {
	env *CopyExportEnv
	src CopySource

	progress atomic.Int64 // bumped on every progress event
	suspends atomic.Int64 // suspends seen
	awakeNs  atomic.Int64 // awake time since the run began

	mu     sync.Mutex
	idle   time.Duration
	cancel context.CancelCauseFunc // the in-flight message's own cancel
	fatal  *CopyStop               // a run-fatal stop the breaker decided
	stalls int                     // header stops of the message in flight, for its listed reason
}

func (w *ceWatch) bump() { w.progress.Add(1) }

func (w *ceWatch) awake() time.Duration { return time.Duration(w.awakeNs.Load()) }

// setMessage registers the in-flight message's cancel, and clears it with nil.
func (w *ceWatch) setMessage(c context.CancelCauseFunc) {
	w.mu.Lock()
	w.cancel = c
	w.stalls = 0
	w.mu.Unlock()
}

func (w *ceWatch) stoppedFatal() *CopyStop {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fatal
}

// bootNow reads the clock that keeps counting in suspend.
func (e *CopyExportEnv) bootNow() time.Duration {
	if e.boot != nil {
		return e.boot()
	}
	return suspendClock()
}

// loop is the breaker's goroutine; it returns when ctx is done.
func (w *ceWatch) loop(ctx context.Context) {
	e := w.env
	t := time.NewTicker(e.tickEvery())
	defer t.Stop()
	last := e.bootNow()
	lastSeq := w.progress.Load()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		b := e.bootNow()
		d := b - last
		last = b
		if d > e.suspendAbove() {
			// A suspend: its time counts neither toward the window nor the message's
			// bound, and the window starts again.
			w.suspends.Add(1)
			w.mu.Lock()
			w.idle = 0
			w.mu.Unlock()
			lastSeq = w.progress.Load()
			continue
		}
		if e.busy() {
			lastSeq = w.progress.Load()
			continue // paused for a capture: the run's own pause, not counted
		}
		w.awakeNs.Add(int64(d))
		seq := w.progress.Load()
		w.mu.Lock()
		if seq != lastSeq {
			w.idle, lastSeq = 0, seq
		} else {
			w.idle += d
		}
		trip := w.idle >= e.noProgress() && w.fatal == nil
		w.mu.Unlock()
		if trip {
			w.trip(ctx)
			lastSeq = w.progress.Load()
		}
	}
}

// trip is what the breaker does once the window has passed with no progress: one
// control request, sent once with no retry loop (Probe), decides.
func (w *ceWatch) trip(ctx context.Context) {
	err := w.src.Probe(ctx)
	var se *graph.StatusError
	switch {
	case ctx.Err() != nil:
		return
	case err == nil:
		// The connection answers, so the stall is the message's. It is listed, the
		// window restarts, and the run moves on.
		w.mu.Lock()
		w.idle = 0
		c, n := w.cancel, w.stalls
		w.mu.Unlock()
		w.bump()
		if c != nil {
			reason := "Microsoft 365 didn't start sending this message within 2 minutes"
			if n > 0 {
				reason += fmt.Sprintf(", %d times", n)
			}
			c(&ceBreakerCause{verdict{kind: verdictList, class: classRetryable, reason: reason}})
		}
	case errors.As(err, &se) && (se.Code == http.StatusTooManyRequests || se.Code == http.StatusServiceUnavailable):
		w.fatalStop(&CopyStop{RID: "R-v2", Class: stopClassThrottled})
	case errors.As(err, &se) && se.Code == http.StatusUnauthorized:
		if rerr := w.src.RefreshAuth(ctx); rerr != nil {
			w.fatalStop(&CopyStop{RID: "R-c", Class: stopClassSignIn})
			return
		}
		w.mu.Lock()
		w.idle = 0
		w.mu.Unlock()
	case errors.As(err, &se) && se.Code == http.StatusForbidden:
		w.fatalStop(&CopyStop{RID: "R-c", Class: stopClassSignIn})
	default:
		w.fatalStop(&CopyStop{RID: "R-u", Class: stopClassConnection})
	}
}

// fatalStop records a run-fatal stop and aborts the in-flight message by cancelling
// that message's own context, never the walk's: the run reads "cancelled" only
// from the walk context, so a breaker stop is never recorded as the user's Cancel.
func (w *ceWatch) fatalStop(s *CopyStop) {
	w.mu.Lock()
	w.fatal = s
	c := w.cancel
	w.mu.Unlock()
	if c != nil {
		c(&ceBreakerCause{verdict{kind: verdictFatal, stop: s}})
	}
}

// noteHeaderStall counts a response-header stop of the in-flight message.
func (w *ceWatch) noteHeaderStall() {
	w.mu.Lock()
	w.stalls++
	w.mu.Unlock()
}

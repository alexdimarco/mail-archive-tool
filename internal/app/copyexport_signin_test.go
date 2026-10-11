package app

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-517, R17, R29, S52
// Sign-in classification: a fake token endpoint that refuses its refresh token
// (400 invalid_grant) after some messages is run-fatal R-c, never R-u: at most one
// message is affected (the one in flight, listed retryable and read again, last, by the
// remaining messages), and the others are not attempted, not marked failed. A transient
// 401 whose one forced refresh succeeds is retried and the run goes on.
func TestCopyExportSignInRefusedMidRunIsRC(t *testing.T) {
	f := newMeFake(t)
	var calls atomic.Int32
	f.tokenFn = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) > 6 {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":"invalid_grant","error_description":"the refresh token was revoked"}`)
			return
		}
		io.WriteString(w, `{"access_token":"AT","refresh_token":"RT","token_type":"Bearer","expires_in":5}`)
	}
	e := newCEEnv(t)
	src := NewM365Source(f.client(t, 5*time.Second), "alice@contoso.org", "tenant-1", nil) // a token that is always due for refresh
	src.Idle, src.Overall = 300*time.Millisecond, 3*time.Second
	ids := seqIDs("S", 10)
	res := runWith(t, e, planFor(t, e, src, idMsgs(ids...)), src)
	if res.Stop == nil || res.Stop.RID != "R-c" || res.Outcome != CopyOutcomeFailed {
		t.Fatalf("a refused refresh token ended as %s (%+v), want R-c", res.Label, res.Stop)
	}
	assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("Sign in again", "Export the remaining messages"),
		assure.Forbid("down for 5 minutes", "check your network"))
	if res.Counts.Exported < 1 || res.Counts.Failed != 1 {
		t.Errorf("counts = %+v, want some exported and exactly the one message in flight failed", res.Counts)
	}
	rest, _ := e.RunRemaining(res.RunID)
	if len(rest) != res.Counts.NotAttempted+1 || rest[len(rest)-1].SourceID != ids[res.Counts.Exported] {
		t.Errorf("the remaining messages are %d, want the %d not attempted and then the in-flight %s last", len(rest), res.Counts.NotAttempted, ids[res.Counts.Exported])
	}
	if sum := mustSummary(t, e, res.RunID); sum.Sealed == nil || sum.Sealed.Stop == nil || sum.Sealed.Stop.RID != "R-c" || sum.Sealed.Stop.InFlight != ids[res.Counts.Exported] {
		t.Errorf("the sealed line's stop = %+v, want R-c with the in-flight message", sum.Sealed)
	}
}

// covers: MA-517, R17, R29, S52
// A transient 401 whose forced token refresh succeeds is retried once, and the run
// continues: the message is exported, not listed, and the token endpoint saw the refresh.
func TestCopyExportATransient401IsRetriedAfterAForcedRefresh(t *testing.T) {
	f := newMeFake(t)
	var first atomic.Bool
	f.value("T1", func(w http.ResponseWriter, r *http.Request) {
		if first.CompareAndSwap(false, true) {
			w.WriteHeader(401)
			io.WriteString(w, `{"error":{"code":"InvalidAuthenticationToken","message":"expired"}}`)
			return
		}
		w.Write(ceBody("T1"))
	})
	e := newCEEnv(t)
	src := f.source(t)
	before := f.tokenHit
	res := runWith(t, e, planFor(t, e, src, idMsgs("A1", "T1", "A3")), src)
	if res.Outcome != CopyOutcomeComplete || res.Stop != nil {
		t.Fatalf("a transient 401 ended as %s (%+v)", res.Label, res.Stop)
	}
	if f.hit("T1") != 2 || f.tokenHit != before+1 {
		t.Errorf("T1 was requested %d times and the token endpoint saw %d refreshes, want 2 and 1", f.hit("T1"), f.tokenHit-before)
	}
}

// covers: MA-517, R17, R29, S52
// The run reads the saved sign-in (locally, no network) every 100 messages, which
// catches a sign-in changed outside the dashboard: a different account, or none, stops
// the run with R-z between messages, naming both accounts, with no message in flight;
// the stop is recorded in the sealed line without a message.
func TestCopyExportReadsTheSignInEveryHundredMessages(t *testing.T) {
	for _, tc := range []struct{ name, now string }{{"another account", "bob@contoso.org"}, {"signed out", ""}} {
		e, src := newCEEnv(t), newFakeSrc()
		var polls atomic.Int32
		src.signedIn = func() (string, bool) {
			if polls.Add(1) >= 2 { // the first poll is before message 1, the second before message 101
				return tc.now, tc.now != ""
			}
			return "alice@contoso.org", true
		}
		res := mustRun(t, e, cePlan(t, e, src, ceMsgs(250)), src)
		if res.Stop == nil || res.Stop.RID != "R-z" || res.Outcome != CopyOutcomeFailed {
			t.Fatalf("%s: ended as %s (%+v), want R-z", tc.name, res.Label, res.Stop)
		}
		names := []string{"alice@contoso.org", "100 of 250 messages", "Export the remaining messages"}
		if tc.now != "" {
			names = append(names, "now: "+tc.now)
		} else {
			names = append(names, "now: signed out")
		}
		assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names(names...))
		if res.Counts.Exported != 100 || res.Counts.Failed != 0 || res.Counts.NotAttempted != 150 {
			t.Errorf("%s: counts = %+v, want 100 exported, none failed, 150 not attempted", tc.name, res.Counts)
		}
		if s := mustSummary(t, e, res.RunID).Sealed; s == nil || s.Stop == nil || s.Stop.RID != "R-z" || s.Stop.InFlight != "" {
			t.Errorf("%s: the sealed stop = %+v, want R-z with no message in flight", tc.name, s)
		}
		if strings.Contains(res.Stop.Message, "Microsoft 365 refused") {
			t.Errorf("%s: worded as a refused sign-in", tc.name)
		}
	}
}

// covers: MA-517, R17, R29, S52
// A 403 on a $value is told from a revoked sign-in by one control request (design §4.13,
// XC9): when the control request is answered, the 403 is that item's alone, listed
// terminal and never retried in-run, and the run goes on; when the control request is
// refused too (a 403 or a 401), the sign-in is gone and the run stops with R-c, outcome
// 1, the message in flight listed retryable and the rest not attempted; when the control
// request gets no answer at all, the sign-in is not blamed (never R-c): the connection
// is, and the breaker ends the run with R-u.
func TestCopyExportA403IsToldFromARevokedSignInByAControlRequest(t *testing.T) {
	denied := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, `{"error":{"code":"ErrorAccessDenied","message":"denied"}}`)
	}

	// Answered: this item's alone.
	f := newMeFake(t)
	f.value("D1", denied)
	e := newCEEnv(t)
	src := f.source(t)
	probes := f.hit("/me")
	res := runWith(t, e, planFor(t, e, src, idMsgs("A1", "D1", "A3")), src)
	if res.Stop != nil || res.Outcome != CopyOutcomePartial || res.Counts.Exported != 2 || res.Counts.Failed != 1 {
		t.Fatalf("a 403 on one item with a working sign-in ended as %s (%+v) %+v, want the item listed and the run on", res.Label, res.Stop, res.Counts)
	}
	if fail := failureOf(t, res.Root, "D1"); fail == nil || fail["class"] != classTerminal {
		t.Errorf("D1 is listed as %v, want terminal", fail)
	}
	if f.hit("D1") != 1 {
		t.Errorf("D1 was requested %d times: a 403 is never retried in-run", f.hit("D1"))
	}
	if got := f.hit("/me") - probes; got != 1 {
		t.Errorf("%d control requests for one 403, want 1", got)
	}

	// Refused: the sign-in is gone.
	for _, code := range []int{403, 401} {
		f = newMeFake(t)
		f.value("D1", denied)
		f.meFn = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			io.WriteString(w, `{"error":{"code":"Forbidden","message":"no"}}`)
		}
		e = newCEEnv(t)
		src = f.source(t)
		res = runWith(t, e, planFor(t, e, src, idMsgs("A1", "D1", "A3", "A4")), src)
		if res.Stop == nil || res.Stop.RID != "R-c" || res.Outcome != CopyOutcomeFailed {
			t.Fatalf("a 403 with the control request refused (%d) ended as %s (%+v), want R-c", code, res.Label, res.Stop)
		}
		assure.Refused(t, int(res.Outcome), res.Stop.Message, assure.Code(1), assure.Names("Sign in again", "Export the remaining messages", "1 of 4 messages"),
			assure.Forbid("down for 5 minutes"))
		if res.Counts.Exported != 1 || res.Counts.Failed != 1 || res.Counts.NotAttempted != 2 {
			t.Errorf("counts = %+v, want 1 exported, the in-flight D1 failed, 2 not attempted", res.Counts)
		}
		if fail := failureOf(t, res.Root, "D1"); fail == nil || fail["class"] != classRetryable {
			t.Errorf("the message in flight is %v, want a retryable failure", fail)
		}
		if f.hit("A3") != 0 {
			t.Errorf("the run went on past the refused sign-in (A3 requested %d times)", f.hit("A3"))
		}
	}

	// No answer to the control request: the connection, never the sign-in.
	f = newMeFake(t)
	f.value("D1", denied)
	f.meFn = dropConn
	e = newCEEnv(t)
	src = f.source(t)
	res = runWith(t, e, planFor(t, e, src, idMsgs("A1", "D1", "A3")), src)
	if res.Stop == nil || res.Stop.RID != "R-u" {
		t.Fatalf("a 403 whose control request is not answered ended as %s (%+v), want R-u, never R-c", res.Label, res.Stop)
	}
}

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

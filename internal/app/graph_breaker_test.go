package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
)

// MA-313: the Graph breaker (§4.8.5, FC10): a rate bound and a time bound on the
// faults that are about the service, decided by one control request, with known
// poison spared, a run-level breaker over two mailboxes, and the Retry-After clamp.

// breakerServer is mailbox upn's folders: first a Sent folder (F_SE) holding the
// messages in sent, then an Inbox (F_IN) of n messages I1..In, whose $value is
// served by fail(i) (nil: the healthy body).
func breakerServer(upn string, sent []string, n int, fail func(i int) http.HandlerFunc) *graphtest.Server {
	s := graphtest.NewServer()
	breakerMailbox(s, upn, sent, n, fail)
	return s
}

// breakerMailbox adds such a mailbox to s. Message ids are unique across a fake's
// mailboxes, so each carries its mailbox's name.
func breakerMailbox(s *graphtest.Server, upn string, sent []string, n int, fail func(i int) http.HandlerFunc) {
	s.AddFolder(upn, "F_SE", "Sent", 100)
	s.AddFolder(upn, "F_IN", "Inbox", 100)
	for _, id := range sent {
		s.AddMessage(upn, "F_SE", id)
	}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("%s-I%d", strings.ToUpper(upn), i)
		s.AddMessage(upn, "F_IN", id)
		if fail != nil {
			if h := fail(i); h != nil {
				s.SetValue(id, h)
			}
		}
	}
}

// breakerOptions are RunGraph's options for mailboxes of s, with the $value
// windows scaled to 40 ms.
func breakerOptions(s *graphtest.Server, mailboxes ...string) GraphOptions {
	g := isoOptions(s)
	g.Mailboxes = mailboxes
	g.MIMEIdleTimeout, g.MIMEHeaderTimeout = 40*time.Millisecond, 40*time.Millisecond
	return g
}

// outage is a service that answers every $value 503 and throttles it with no wait.
func outage(int) http.HandlerFunc { return graphtest.Status(503, "0") }

// controlDown makes the breaker's control request fail too: the service is down.
func controlDown(s *graphtest.Server) { s.SetProbe(graphtest.Status(503, "")) }

// probes counts the control requests mailbox upn's walk made.
func probes(s *graphtest.Server, upn string) int {
	return s.Requests("/users/"+upn+"/mailFolders/F_") - s.Requests("/users/"+upn+"/mailFolders/F_IN/messages") - s.Requests("/users/"+upn+"/mailFolders/F_SE/messages")
}

func gapsOf(t *testing.T, out, reason string) int {
	t.Helper()
	n := 0
	for _, g := range graphGaps(t, out) {
		if g.Reason == reason {
			n++
		}
	}
	return n
}

// covers: MA-313, R5, R12, R18, S46
// The breaker's trip (§4.8.5, FC10). 25 consecutive 503s (no wait: Retry-After 0)
// and a failing control request end the mailbox with the breaker's refusal, which
// names the threshold that fired and the last status; the 25 gaps recorded so far
// are kept; there is no sweep (the message the first folder no longer lists is not
// marked gone) and the 26th item is never requested; one control request was made.
// The rate bound trips on 1 success per 10 attempts (a full window of 50 with 25
// faults or more), and the time bound on failed downloads that have taken longer
// than its bound (scaled). Each needs the control request to fail too.
func TestGraphBreakerTripsOnlyWithAFailingControlRequest(t *testing.T) {
	run := func(t *testing.T, s *graphtest.Server, g GraphOptions, out string) error {
		t.Helper()
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0))
		return err
	}
	// withDeleted archives D in Sent in a first run, then deletes it at the source and
	// adds the Inbox the case run meets.
	setup := func(t *testing.T, n int, fail func(int) http.HandlerFunc) (*graphtest.Server, GraphOptions, string) {
		s := breakerServer("u1", []string{"D"}, 0, nil)
		g := breakerOptions(s, "u1")
		out := tmpDir(t)
		if err := run(t, s, g, out); err != nil {
			t.Fatalf("the healthy first run: %v", err)
		}
		s.RemoveMessage("u1", "F_SE", "D")
		for i := 1; i <= n; i++ {
			id := fmt.Sprintf("U1-I%d", i)
			s.AddMessage("u1", "F_IN", id)
			if h := fail(i); h != nil {
				s.SetValue(id, h)
			}
		}
		s.ResetRequests()
		return s, g, out
	}
	wantTrip := func(t *testing.T, err error, threshold, last string) {
		t.Helper()
		var be *GraphBreakerError
		if !errors.As(err, &be) || errors.Is(err, ErrPartial) {
			t.Fatalf("the run returned %v; want the breaker's refusal", err)
		}
		for _, want := range []string{"mailbox u1", "stopped after " + threshold, "a control request also failed", "(last: ", last, "Messages archived so far are kept", "nothing was marked deleted", "mailarchive status -out"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal %q does not say %q", err.Error(), want)
			}
		}
	}
	wantNoSweep := func(t *testing.T, out string) {
		t.Helper()
		k, rec := recordForID(t, out, "d@x")
		wantDeferred(t, out, "D (no longer listed: the tripped mailbox is not swept)", k, rec)
	}

	t.Run("25 consecutive transport faults", func(t *testing.T) {
		s, g, out := setup(t, 30, outage)
		defer s.Close()
		controlDown(s)
		wantTrip(t, run(t, s, g, out), "25 downloads in a row failed", "503")
		if n := gapsOf(t, out, "fetch-transport"); n != 25 {
			t.Errorf("%d fetch-transport gaps, want the 25 recorded before the trip", n)
		}
		if n := s.Requests("/users/u1/messages/U1-I26/"); n != 0 {
			t.Errorf("the 26th item was requested %d times after the trip", n)
		}
		if n := probes(s, "u1"); n != 1 {
			t.Errorf("%d control requests, want one", n)
		}
		wantNoSweep(t, out)
	})

	t.Run("24 faults then a success: no trip", func(t *testing.T) {
		s, g, out := setup(t, 25, func(i int) http.HandlerFunc {
			if i == 25 {
				return nil
			}
			return outage(i)
		})
		defer s.Close()
		controlDown(s)
		if err := run(t, s, g, out); !errors.Is(err, ErrPartial) {
			t.Fatalf("the run returned %v, want ErrPartial: 24 faults and a success is no trip", err)
		}
		if n := gapsOf(t, out, "fetch-transport"); n != 24 || probes(s, "u1") != 0 {
			t.Errorf("%d gaps and %d control requests; want 24 and none", n, probes(s, "u1"))
		}
	})

	t.Run("rate: one success in ten", func(t *testing.T) {
		s, g, out := setup(t, 60, func(i int) http.HandlerFunc {
			if i%10 == 0 {
				return nil
			}
			return outage(i)
		})
		defer s.Close()
		controlDown(s)
		err := run(t, s, g, out)
		wantTrip(t, err, "", "503")
		if !strings.Contains(err.Error(), "of the last 50 downloads failed") {
			t.Errorf("the refusal %q does not name the rate bound", err)
		}
		wantNoSweep(t, out)
	})

	t.Run("time: failed downloads that took longer than the bound", func(t *testing.T) {
		s, g, out := setup(t, 10, func(int) http.HandlerFunc { return graphtest.NoHeaders() })
		defer s.Close()
		controlDown(s)
		g.BreakerTimeBound = 250 * time.Millisecond
		g.MIMEIdleTimeout, g.MIMEHeaderTimeout = 100*time.Millisecond, 100*time.Millisecond
		err := run(t, s, g, out)
		wantTrip(t, err, "failed downloads had taken", "the download's response limit")
		if n := gapsOf(t, out, "fetch-timeout"); n < 3 || n > 4 {
			t.Errorf("%d fetch-timeout gaps, want the 3 or 4 whose time crossed 250 ms", n)
		}
		wantNoSweep(t, out)
	})
}

// covers: MA-313, R5, R12, R18, S46
// What never trips the breaker, whatever the control request says (§4.8.5): 25
// consecutive 500s are item-class (fetch-failed), and 404s are vanished-at-fetch
// (each fetched once); neither is counted, no control request is sent, and every
// item is a gap. And 25 consecutive per-item timeouts while the control request
// is answered make a control request and no trip: the counts start again and the
// walk goes on to the end.
func TestGraphBreakerSparesItemFaultsAndAnsweredControlRequests(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		fail         func(int) http.HandlerFunc
		down         bool
		probed       bool
	}{
		{"item-class 500s", "fetch-failed", func(int) http.HandlerFunc { return graphtest.Fail(500, "ErrorMimeContentConversionFailed") }, true, false},
		{"404s", "vanished-at-fetch", func(int) http.HandlerFunc { return graphtest.Fail(404, "ErrorItemNotFound") }, true, false},
		{"timeouts with the control request answered", "fetch-timeout", func(int) http.HandlerFunc { return graphtest.NoHeaders() }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := breakerServer("u1", nil, 30, tc.fail)
			defer s.Close()
			if tc.down {
				controlDown(s)
			}
			out := tmpDir(t)
			_, err := RunGraph(context.Background(), breakerOptions(s, "u1"), Options{Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0))
			var be *GraphBreakerError
			if !errors.Is(err, ErrPartial) || errors.As(err, &be) {
				t.Fatalf("the run returned %v, want ErrPartial and no breaker", err)
			}
			if n := gapsOf(t, out, tc.reason); n != 30 {
				t.Errorf("%d %s gaps, want all 30 items", n, tc.reason)
			}
			if got := probes(s, "u1"); (got > 0) != tc.probed {
				t.Errorf("%d control requests; want some: %v", got, tc.probed)
			}
			if tc.reason == "vanished-at-fetch" {
				for _, i := range []int{1, 30} {
					if n := s.Requests(fmt.Sprintf("/users/u1/messages/U1-I%d/", i)); n != 1 {
						t.Errorf("item %d fetched %d times, want once", i, n)
					}
				}
			}
		})
	}
}

// covers: MA-313, R5, R12, R18, S46
// Known poison is evidence about the item and never counts (RR6-C4): 25 items that
// answer 504 on every run are 25 fetch-transport gaps after run 1 (its control
// request is answered, so nothing trips); in run 2 the same 504s, a control
// request that now fails and two new healthy items: the run walks past the poison
// without a trip or a control request, and archives the new items.
func TestGraphBreakerNeverCountsKnownPoison(t *testing.T) {
	poison := func(i int) http.HandlerFunc {
		if i <= 25 {
			return graphtest.Status(504, "")
		}
		return nil
	}
	s := breakerServer("u1", nil, 27, poison)
	defer s.Close()
	g := breakerOptions(s, "u1")
	out := tmpDir(t)
	if _, err := isoRun(g, out); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 1 returned %v, want ErrPartial (the control request was answered: no trip)", err)
	}
	if n := gapsOf(t, out, "fetch-transport"); n != 25 {
		t.Fatalf("run 1 left %d gaps, want 25", n)
	}
	s.AddMessage("u1", "F_IN", "U1-N1")
	s.AddMessage("u1", "F_IN", "U1-N2")
	controlDown(s)
	s.ResetRequests()
	if _, err := isoRun(g, out); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 returned %v, want ErrPartial: known poison walks past", err)
	}
	if !archived(t, out, "u1-n1@x") || !archived(t, out, "u1-n2@x") {
		t.Error("the items after the poison were not archived")
	}
	if n := probes(s, "u1"); n != 0 {
		t.Errorf("%d control requests in run 2, want none: known poison is not counted", n)
	}
}

// covers: MA-313, R5, R12, R18, S46
// Two consecutive tripped mailboxes stop the run (§4.8.5, §7.5): u1 and u2 each
// trip, the run ends with the refusal naming both, and u3 is never read. A tripped
// mailbox followed by a healthy one does not: the run goes on to u3.
func TestGraphRunBreakerStopsAfterTwoTrippedMailboxes(t *testing.T) {
	build := func(healthyMiddle bool) (*graphtest.Server, GraphOptions) {
		s := graphtest.NewServer()
		breakerMailbox(s, "u1", nil, 30, outage)
		if healthyMiddle {
			breakerMailbox(s, "u2", nil, 3, nil)
		} else {
			breakerMailbox(s, "u2", nil, 30, outage)
		}
		breakerMailbox(s, "u3", nil, 30, outage)
		controlDown(s)
		return s, breakerOptions(s, "u1", "u2", "u3")
	}
	t.Run("two in a row", func(t *testing.T) {
		s, g := build(false)
		defer s.Close()
		_, err := isoRun(g, tmpDir(t))
		var rb *RunBreakerError
		if !errors.As(err, &rb) || !strings.Contains(err.Error(), "stopped the run after two mailboxes in a row could not reach Microsoft 365") ||
			!strings.Contains(err.Error(), "u1") || !strings.Contains(err.Error(), "u2") || !strings.Contains(err.Error(), "re-run when it is reachable") {
			t.Fatalf("the run returned %v, want the run-level breaker's refusal naming both mailboxes", err)
		}
		if n := s.Requests("/users/u3/"); n != 0 {
			t.Errorf("mailbox u3 was read (%d requests) after two mailboxes tripped", n)
		}
	})
	t.Run("a healthy mailbox between", func(t *testing.T) {
		s, g := build(true)
		defer s.Close()
		_, err := isoRun(g, tmpDir(t))
		var rb *RunBreakerError
		if err == nil || errors.As(err, &rb) {
			t.Fatalf("the run returned %v, want the failure of u1 and u3 and no run-level stop", err)
		}
		if n := s.Requests("/users/u3/"); n == 0 {
			t.Error("mailbox u3 was not read: a healthy mailbox between resets the count of consecutive trips")
		}
		if !strings.Contains(err.Error(), "2 mailbox(es) failed") {
			t.Errorf("the run's error %q does not count u1 and u3 as failed", err)
		}
	})
}

// covers: MA-313, R5, R12, R18, S46
// The Retry-After clamp (FC10), through RunGraph: a 503 that asks for 3600 seconds
// waits for the clamp (scaled to 100 ms), not for the hour, and the item is archived.
func TestGraphRetryAfterIsClamped(t *testing.T) {
	s := graphtest.NewServer()
	defer s.Close()
	s.AddFolder("u1", "F_IN", "Inbox", 10)
	s.AddMessage("u1", "F_IN", "T1")
	s.SetValue("T1", graphtest.ThrottleOnce("3600", graphtest.Body("T1")))
	g := isoOptions(s)
	g.RetryAfterMax = 100 * time.Millisecond
	began := time.Now()
	returnsWithin(t, 20*time.Second, "the run", func() {
		if _, err := isoRun(g, tmpDir(t)); err != nil {
			t.Errorf("the run returned %v", err)
		}
	})
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("the run took %v: the hour's Retry-After was not clamped", took)
	}
}

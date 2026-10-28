package backupalert

import (
	"errors"
	"testing"
)

// covers: MA-879, R37
// An email that keeps failing to send (a revoked password, a laptop offline) is tried again at
// every failed run, but the Desktop notice goes once per failing streak (the part A pass): over
// four failed runs whose sends all fail, Notify is true on the first only and every run tries the
// email; when a send then works, it is delivered once and nothing more is sent; the first success
// after is a recovery for the notice. Positive twin first: with email working, the first failure
// notifies and sends.
func TestAnUnsendableFailureEmailRaisesItsNoticeOnce(t *testing.T) {
	ok := newStreakRig(t, alertsOnFailure)
	if o := ok.report(ResultFailed); !o.Notify || ok.results() != "failed" {
		t.Fatalf("the positive twin: notify=%v sent %q", o.Notify, ok.results())
	}

	g := newStreakRig(t, alertsOnFailure)
	down := errors.New("the mail server did not answer")
	g.fail = []error{down, down, down, down}
	notices := 0
	for i := 0; i < 4; i++ {
		if o := g.report(ResultFailed); o.Notify {
			notices++
			if i != 0 {
				t.Errorf("failed run %d raised the notice again", i+1)
			}
		}
	}
	if notices != 1 || len(g.fail) != 0 || len(g.sent) != 0 {
		t.Fatalf("four failed runs with the email down: %d notices (want 1), %d sends not tried, sent %q", notices, len(g.fail), g.results())
	}
	if o := g.report(ResultFailed); o.Notify || g.results() != "failed" {
		t.Fatalf("the email works again: notify=%v sent %q, want it delivered once with no notice", o.Notify, g.results())
	}
	if o := g.report(ResultFailed); o.Notify || g.results() != "failed" {
		t.Fatalf("after delivery the streak is told: notify=%v sent %q", o.Notify, g.results())
	}
	if o := g.report(ResultOK); !o.Recovered {
		t.Fatal("the first success after the streak is not a recovery")
	}

	// A streak whose email never went: the success is still the notice's recovery, and it sends no
	// "working again" for a failure email that never arrived.
	n := newStreakRig(t, alertsOnFailure)
	n.fail = []error{down}
	n.report(ResultFailed)
	if o := n.report(ResultOK); !o.Recovered || n.results() != "" {
		t.Fatalf("a success after an undelivered failure: recovered=%v sent %q", o.Recovered, n.results())
	}
}

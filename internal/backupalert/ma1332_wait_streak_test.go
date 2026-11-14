package backupalert

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1332, R37
// The tidy-up wait's alert is a streak of its own in the alert record (the B3 adversarial pass,
// round 3's H3), for every caller of Report (the scheduled local run-job and the scheduled Microsoft
// 365 capture): after it, a failure for another reason sends its own email and raises its own
// notice, once; a second wait alert in the wait's streak sends nothing; a wait alert inside a real
// failure's streak sends nothing and keeps the streak a real one, so the failure after it sends
// nothing either. Positive twin first: two real failures send one email and one notice.
func TestTheTidyUpWaitsAlertIsAStreakOfItsOwn(t *testing.T) {
	waitErr := fmt.Errorf("%w %s", &state.NewerFormatError{Name: "/archive/.mailarchive-manifest.json", Version: 8, Understood: 7}, state.TidyWaitSinceLine(time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)))
	if !state.TidyingElsewhere(waitErr) {
		t.Fatal("premise: the wait's error is not read as a tidy-up")
	}
	realErr := errors.New("the backup drive is not there")
	g := newStreakRig(t, alertsOnFailure)
	report := func(err error) Outcome {
		return Report(g.settings, nil, g.key, "/archive", ResultFailed, err, 0, nil)
	}
	if o := report(realErr); !o.Notify || len(g.sent) != 1 {
		t.Fatalf("positive twin: the first failure: %+v, sent %q", o, g.results())
	}
	if o := report(realErr); o.Notify || len(g.sent) != 1 {
		t.Fatalf("positive twin: the second failure: %+v, sent %q", o, g.results())
	}
	// Inside the real failure's streak, a wait alert is told already, and the streak stays real.
	if o := report(waitErr); o.Notify || len(g.sent) != 1 {
		t.Fatalf("a wait alert inside a real failure's streak: %+v, sent %q", o, g.results())
	}
	if o := report(realErr); o.Notify || len(g.sent) != 1 {
		t.Fatalf("a real failure after a wait alert inside a real streak: %+v, sent %q", o, g.results())
	}
	g.report(ResultOK) // the recovery ends that streak
	sent := len(g.sent)

	if o := report(waitErr); !o.Notify || len(g.sent) != sent+1 {
		t.Fatalf("the wait's alert: %+v, sent %q", o, g.results())
	}
	if o := report(waitErr); o.Notify || len(g.sent) != sent+1 {
		t.Fatalf("a second wait alert: %+v, sent %q", o, g.results())
	}
	if o := report(realErr); !o.Notify || len(g.sent) != sent+2 || g.sent[len(g.sent)-1].Error != realErr.Error() {
		t.Fatalf("a real failure after the wait's alert: %+v, sent %q", o, g.results())
	}
	if o := report(realErr); o.Notify || len(g.sent) != sent+2 {
		t.Fatalf("a second real failure after it: %+v, sent %q", o, g.results())
	}
}

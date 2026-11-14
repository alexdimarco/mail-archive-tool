package backupalert

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1339, R37
// The B3 adversarial pass, round 4's J4: a real failure whose email could not be sent (the mail
// server down that hour) still owes its email. A wait for another computer's tidy-up inside that
// failure's streak sends nothing and does not mark the streak told, so the wait's email ("Nothing is
// lost") never stands for the real failure: the next real failure, once sending works again, sends
// the real failure's email, and the one after it nothing. No second notice is raised. Positive twin
// first: with sending working throughout, the real failure's email goes at once and the wait inside
// its streak sends nothing.
func TestAWaitAlertNeverTellsARealFailureWhoseEmailIsOwed(t *testing.T) {
	waitErr := fmt.Errorf("%w %s", &state.NewerFormatError{Name: "/archive/.mailarchive-manifest.json", Version: 8, Understood: 7}, state.TidyWaitSinceLine(time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)))
	realErr := errors.New("the backup drive is not there")
	twin := newStreakRig(t, alertsOnFailure)
	report := func(g *streakRig, err error) Outcome {
		return Report(g.settings, nil, g.key, "/archive", ResultFailed, err, 0, nil)
	}
	if o := report(twin, realErr); !o.Notify || len(twin.sent) != 1 || twin.sent[0].Error != realErr.Error() {
		t.Fatalf("positive twin: the real failure: %+v, sent %q", o, twin.results())
	}
	if o := report(twin, waitErr); o.Notify || len(twin.sent) != 1 {
		t.Fatalf("positive twin: the wait inside its streak: %+v, sent %q", o, twin.results())
	}

	g := newStreakRig(t, alertsOnFailure)
	g.fail = []error{errors.New("smtp: connection refused")} // the first send is refused
	if o := report(g, realErr); !o.Notify || len(g.sent) != 0 {
		t.Fatalf("the real failure, its email refused: %+v, sent %q", o, g.results())
	}
	if o := report(g, waitErr); o.Notify || len(g.sent) != 0 {
		t.Fatalf("the wait inside the real failure's streak sent its own email or a notice: %+v, sent %q", o, g.results())
	}
	if o := report(g, realErr); o.Notify || len(g.sent) != 1 || g.sent[0].Error != realErr.Error() {
		t.Fatalf("the real failure's email was not sent once sending worked again: %+v, sent %q", o, g.results())
	}
	if o := report(g, realErr); o.Notify || len(g.sent) != 1 {
		t.Fatalf("the real failure was told twice: %+v, sent %q", o, g.results())
	}
}

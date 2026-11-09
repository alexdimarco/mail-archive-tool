package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1290, MA-1291
// The C5 round-2 pass's two new WARNs (the lead's calls of 2026-11-09) reach `status` and Status &
// health: tbstreak_clock_behind (a backup whose clock was behind the streak record) and
// alert_send_failed (an email about Thunderbird given up after 5 failed sends), read from the
// last-run record through the real Gather, posture WARN, each recorded sentence in the summary both
// surfaces print. Positive twin first: the existing tbstreak_unreadable is shown the same way.
func TestTheClockAndSendWarningsAreShown(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	show := func(w state.RunWarning) (Report, []string) {
		t.Helper()
		out := t.TempDir()
		if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now.Add(-2 * time.Minute), Finished: now.Add(-time.Minute), Warnings: []state.RunWarning{w}}); err != nil {
			t.Fatal(err)
		}
		g := Gather(out, "")
		rep := Assess(g, now)
		return rep, Summary(g, rep)
	}
	shown := func(rep Report, sum []string, w state.RunWarning) bool {
		return strings.Contains(strings.Join(rep.Codes, " "), w.Code) && rep.Posture == "WARN" && strings.Contains(strings.Join(sum, "\n"), w.Message)
	}
	twin := state.RunWarning{Code: "tbstreak_unreadable", Message: "MailArchive did not trust its record of how long Thunderbird has been downloading (x)."}
	if rep, sum := show(twin); !shown(rep, sum, twin) {
		t.Fatalf("positive twin: %+v %q", rep, sum)
	}
	for _, w := range []state.RunWarning{
		{Code: "tbstreak_clock_behind", Message: "This backup's clock read Mar 1, 2025, earlier than a previous backup of this archive, so MailArchive did not count it toward how long Thunderbird has been downloading. Check this computer's date and time."},
		{Code: "alert_send_failed", Message: "MailArchive could not send its email about Thunderbird (5 tries). Check the email settings in MailArchive Desktop's Settings, then use Send test email."},
	} {
		if rep, sum := show(w); !shown(rep, sum, w) {
			t.Errorf("%s is not shown: codes %v posture %s summary %q", w.Code, rep.Codes, rep.Posture, sum)
		}
	}
}

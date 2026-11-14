package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1332, R37, R45
// The B3 adversarial pass, round 3's H3: the tidy-up wait's alert does not use up the failure
// streak. Its one email and notice say the archive waits for another computer ("Nothing is lost");
// once the tidy-up has ended, the next scheduled run that fails for a reason of its own (here its
// source was moved away) sends its own failure email and raises its own notice, and a second such
// failure sends nothing more (one per streak, as ever). Positive twin first: on an archive that
// never waited, a failure sends one email and one notice, and a success after it sends the
// recovery.
func TestATidyUpWaitsAlertNeverHidesTheNextRealFailure(t *testing.T) {
	g := newLocalRunRig(t)
	okJob := g.jobAt(filepath.Join(g.dir, "ok.json"), g.mbox)
	gone := filepath.Join(g.dir, "Inbox-moved")
	moveAway := func() {
		if err := os.Rename(g.mbox, gone); err != nil {
			t.Fatal(err)
		}
	}
	putBack := func() {
		if err := os.Rename(gone, g.mbox); err != nil {
			t.Fatal(err)
		}
	}
	moveAway()
	if RunHeadless(okJob) == 0 || len(g.sent) != 1 || len(g.notices) != 1 {
		t.Fatalf("positive twin: a failure sent %q with %d notices", g.results(), len(g.notices))
	}
	putBack()
	if RunHeadless(okJob) != 0 || !strings.HasSuffix(g.results(), "recovered") {
		t.Fatalf("positive twin: the success after sent %q", g.results())
	}

	// The archive waits 4 days for another computer's tidy-up: the wait's one alert.
	id := state.ArchiveID(g.out)
	noticesBefore := len(g.notices)
	stampVersion(t, g.out, "8")
	if _, err := gmailanswer.NoteTidyWaitFor(g.out, id, time.Now().Add(-4*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if RunHeadless(okJob) != 0 {
		t.Fatal("the waiting backup is not a skip")
	}
	sent, notices := len(g.sent), len(g.notices)
	if sent != 3 || notices != noticesBefore+1 || !strings.Contains(g.sent[2].Error, "waited since") {
		t.Fatalf("premise: the wait's alert: sent %q (%d notices)", g.results(), notices)
	}
	if RunHeadless(okJob) != 0 || len(g.sent) != sent || len(g.notices) != notices {
		t.Fatalf("a second waiting backup alerted again: sent %q", g.results())
	}

	// The other computer finishes; this computer's next run fails for its own reason.
	stampVersion(t, g.out, "6")
	moveAway()
	if RunHeadless(okJob) == 0 {
		t.Fatal("premise: the run with its source moved away did not fail")
	}
	if len(g.sent) != sent+1 || len(g.notices) != notices+1 {
		t.Fatalf("a real failure right after the tidy-up wait's alert sent %d emails and raised %d notices, want 1 and 1 (sent %q)", len(g.sent)-sent, len(g.notices)-notices, g.results())
	}
	if d := g.sent[len(g.sent)-1].Error; strings.Contains(d, "waited since") {
		t.Fatalf("the real failure's email is worded as the wait: %q", d)
	}
	if RunHeadless(okJob) == 0 || len(g.sent) != sent+1 || len(g.notices) != notices+1 {
		t.Fatalf("a second real failure in the same streak sent again: %q", g.results())
	}
}

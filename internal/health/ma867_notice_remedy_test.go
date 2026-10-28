package health

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-867, R37
// Where MailArchive Desktop shows its section 4 notice (design-desktop-settings-card: a capture
// counted, and the archive's record local or its schedule a run-job), the card offers Remove only,
// so the repair remedy names Remove, never a Repair the card does not show: on every surface for a
// counted capture on a local archive (the card's route), and on the Desktop for a run-job where a
// capture is counted too. Positive twin first: a Microsoft 365 archive's counted capture is told to
// click Repair on the Scheduled backup card, on every surface. The record's kind is read at Gather
// (localMailOnly): local mail files only, never a Microsoft 365 mailbox, and at least one source.
func TestMA867_TheNoticesArchivesAreToldToRemove(t *testing.T) {
	ev := func(ks ...schedule.Kind) schedule.KindEvidence {
		e := schedule.KindEvidence{Corroborated: map[schedule.Kind]bool{}}
		for _, k := range ks {
			e.Corroborated[k] = true
		}
		return e
	}
	capture := []string{"desktop", "-capture", "-out", "/a"}
	runjob := []string{"run-job", "-job", "x"}
	in := func(kindEv schedule.KindEvidence, job []string, surface string, local bool) Input {
		return Input{Out: "/a", GOOS: "linux", Surface: surface, KindEv: kindEv, LocalMailOnly: local,
			Desc: schedule.Descriptor{Name: "mailarchive-1234abcd", Job: job}}
	}
	for _, surface := range []string{"", "desktop", "gui"} {
		got := repairHow(in(ev(schedule.KindCapture), capture, surface, false))
		if !strings.Contains(got, "click Repair on this archive's Scheduled backup card") || strings.Contains(got, "click Remove") {
			t.Fatalf("the positive twin, a Microsoft 365 capture on %q: %q", surface, got)
		}
	}
	for _, surface := range []string{"", "desktop", "gui"} {
		got := repairHow(in(ev(schedule.KindCapture), capture, surface, true))
		if !strings.Contains(got, "click Remove on this archive's Scheduled backup card") || strings.Contains(got, "click Repair") {
			t.Errorf("a capture on a local archive, %q surface: %q", surface, got)
		}
		if (surface != "desktop") != strings.Contains(got, "in MailArchive Desktop") {
			t.Errorf("%q surface: where the card is, said wrongly: %q", surface, got)
		}
	}
	both := repairHow(in(ev(schedule.KindRunJob, schedule.KindCapture), runjob, "desktop", true))
	if !strings.Contains(both, "click Remove") || strings.Contains(both, "afresh") || strings.Contains(both, "Settings") {
		t.Errorf("a run-job where a capture is counted, on the Desktop: %q", both)
	}
	// A run-job with no capture counted keeps its Repair.
	if got := repairHow(in(ev(schedule.KindRunJob), runjob, "desktop", true)); !strings.Contains(got, "click Repair on this archive's Scheduled backup card") {
		t.Errorf("a run-job with no capture counted: %q", got)
	}
	// "Set up afresh" points at Settings, the Desktop's one editor.
	if got := bothInstalls(in(ev(), runjob, "", false)); !strings.Contains(got, "How often in MailArchive Desktop's Settings") {
		t.Errorf("set up afresh does not point at Settings: %q", got)
	}
	for _, c := range []struct {
		stores map[string]string
		want   bool
	}{
		{map[string]string{"path:/mail/Inbox": "Inbox"}, true},
		{map[string]string{"path:/mail/Inbox": "Inbox", "mailbox:a@contoso.org": "a"}, false},
		{map[string]string{}, false},
	} {
		if got := localMailOnly(c.stores); got != c.want {
			t.Errorf("localMailOnly(%v) = %v, want %v", c.stores, got, c.want)
		}
	}
}

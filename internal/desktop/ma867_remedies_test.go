package desktop

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/schedule"
)

// desktopRemedies is what the Desktop's Status & health card says for the archive at out with its
// recorded schedule missing from the scheduler (the not_installed remedy, and any other remedy
// that tails with the repair route): health.Gather as the card runs it, on the desktop surface.
func desktopRemedies(t *testing.T, out string) string {
	t.Helper()
	in := health.Gather(out, "")
	in.Surface = "desktop"
	in.SchedState = schedule.NotInstalled
	if !in.HasDescriptor {
		t.Fatal("fixture: no schedule is recorded")
	}
	return strings.Join(health.Assess(in, time.Now()).Reasons, "\n")
}

// covers: MA-867, R37
// Every health remedy names a button that renders, and can succeed, for that archive's kind
// (health.go's rule; C-2, design-desktop-settings-card section 5), section 4's archives included.
// Positive twin first: a run-job with no capture counted is told to click Repair on the Scheduled
// backup card, which shows it. Then a Microsoft 365 archive's counted capture: Repair, on the
// card. Then section 4's archives — a local archive's run-job, and a capture on a local archive,
// where Microsoft 365 is set up — whose card offers Remove only: the remedy names Remove, never a
// Repair that is not there (and that the route would refuse).
func TestEveryRemedyNamesAButtonTheCardShows(t *testing.T) {
	const repair, remove = "click Repair on this archive's Scheduled backup card", "click Remove on this archive's Scheduled backup card"
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	if r := desktopRemedies(t, out); !strings.Contains(r, repair) {
		t.Fatalf("the positive twin: the run-job's remedy does not name Repair:\n%s", r)
	}
	if c := cardOf(g.page(), "sec-backup"); !strings.Contains(c, `id="schedRepair"`) {
		t.Fatal("the positive twin: the card the remedy names has no Repair")
	}

	// Section 4: the same run-job where a capture is counted.
	countCapture(t, out)
	r := desktopRemedies(t, out)
	if !strings.Contains(r, remove) || strings.Contains(r, "click Repair") || strings.Contains(r, "afresh") {
		t.Errorf("section 4's run-job: the remedy does not name Remove alone:\n%s", r)
	}
	if c := cardOf(g.page(), "sec-backup"); !strings.Contains(c, `id="schedRemove"`) || strings.Contains(c, `id="schedRepair"`) {
		t.Errorf("section 4's card does not offer Remove only:\n%s", c)
	}

	// A Microsoft 365 archive whose capture is installed and counted: Repair, on the card.
	m365 := archiveAt(t, t.TempDir(), "M365", map[string]string{"mailbox:u@contoso.com": "b"})
	m := newA3Rig(t, m365, "darwin")
	countCapture(t, m365)
	writeCaptureDescriptor(t, m365, schedule.Weekly, "03:00")
	m.fake.tasks[m365] = true
	if r := desktopRemedies(t, m365); !strings.Contains(r, repair) {
		t.Errorf("the counted capture's remedy does not name Repair:\n%s", r)
	}
	if c := cardOf(m.page(), "sec-backup"); !strings.Contains(c, `id="schedRepair"`) {
		t.Error("the counted capture's card has no Repair")
	}

	// Section 4: a capture installed on a local archive where it is counted.
	local := tbProfileArchive(t)
	l := newA3Rig(t, local, "darwin")
	countCapture(t, local)
	writeCaptureDescriptor(t, local, schedule.Weekly, "03:00")
	l.fake.tasks[local] = true
	if r := desktopRemedies(t, local); !strings.Contains(r, remove) || strings.Contains(r, "click Repair") {
		t.Errorf("section 4's capture: the remedy does not name Remove alone:\n%s", r)
	}
	if c := cardOf(l.page(), "sec-backup"); !strings.Contains(c, `id="schedRemove"`) || strings.Contains(c, `id="schedRepair"`) {
		t.Errorf("section 4's capture card does not offer Remove only:\n%s", c)
	}
}

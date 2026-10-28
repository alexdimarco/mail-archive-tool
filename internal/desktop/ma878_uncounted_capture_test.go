package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-878, MA-857, R37
// A local archive whose recorded schedule is a Microsoft 365 capture not counted here (Microsoft
// 365 isn't set up on this computer) is status and Remove only (section 3.2's table; the part A
// pass): a post with a cadence, and an empty-body Repair, are refused in plain words before
// anything is written — no install, the archive's record and the job folder unchanged — and the
// card calls it a Microsoft 365 capture, never an export. Positive twin first: the same kind of
// archive with nothing recorded gets its local backup.
func TestAnUncountedCaptureIsNeverReplacedByALocalBackup(t *testing.T) {
	isolateHome(t)
	fresh := newA3Rig(t, tbProfileArchive(t), "darwin")
	if body := fresh.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin: %s", body)
	}

	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	writeCaptureDescriptor(t, g.out, schedule.Weekly, "03:00")
	g.fake.tasks[g.out] = true
	for _, post := range []string{`{"interval":"daily","time":"04:00"}`, `{}`} {
		u := g.snapshot()
		body := g.post(post)
		assure.Refused(t, refusedCode(body), body,
			assure.Names("installed backup is a Microsoft 365 capture", "remove it on the Scheduled backup card", "Nothing was changed."),
			assure.NoSideEffect(func() bool { return u.holds() && fileBytes(g.jobPath) == nil }))
	}
	card := cardOf(g.page(), "sec-backup")
	if !strings.Contains(card, "a scheduled Microsoft 365 capture") || strings.Contains(card, "an export backup") {
		t.Errorf("the card does not call the recorded capture a capture:\n%s", card)
	}
}

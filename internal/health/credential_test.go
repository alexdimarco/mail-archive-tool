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

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

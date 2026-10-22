package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-561
// The pre-sync's WARN outcome codes surface in the health view as WARN (never
// RED), carrying their recorded what/why/remedy message and their code in
// status -json; the note-class pre-sync codes and any unknown code stay GREEN,
// since the record is writable by anyone who can write the archive.
func TestPresyncWarningsSurfaceAsWarnNeverRed(t *testing.T) {
	now := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)

	for _, w := range []state.RunWarning{
		{Code: "presync_cannot_start", Message: "Thunderbird could not be started; the archive ran anyway"},
		{Code: "presync_quit_timeout", Message: "Thunderbird did not close; quit it from the Dock"},
		{Code: "presync_internal_error", Message: "the Thunderbird pre-sync failed unexpectedly; the archive ran anyway"},
		{Code: "presync_stuck", Message: "Thunderbird has not synced on the last 3 runs"},
	} {
		in := healthyInput(now)
		in.LastRun.Warnings = []state.RunWarning{w}
		rep := Assess(in, now)
		if rep.Posture != "WARN" {
			t.Errorf("%s: posture %s, want WARN (never RED) (%v)", w.Code, rep.Posture, rep.Reasons)
		}
		if !strings.Contains(strings.Join(rep.Reasons, "\n"), "WARN: "+w.Message) {
			t.Errorf("%s: reasons lack the recorded message: %v", w.Code, rep.Reasons)
		}
		if doc := JSON(in, rep); !contains(doc.ReasonCodes, w.Code) {
			t.Errorf("%s: status -json reason_codes %v lack it", w.Code, doc.ReasonCodes)
		}
	}

	// A note-class pre-sync code and an unknown code stay GREEN (not surfaced).
	for _, code := range []string{"presync_no_activity", "presync_already_running", "presync_made_up"} {
		in := healthyInput(now)
		in.LastRun.Warnings = []state.RunWarning{{Code: code, Message: "x"}}
		if rep := Assess(in, now); rep.Posture != "GREEN" || len(rep.Reasons) != 0 {
			t.Errorf("%s: should stay GREEN (a note or unknown code), got %s %v", code, rep.Posture, rep.Reasons)
		}
	}
}

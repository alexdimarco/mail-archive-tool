package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
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

// covers: MA-584
// A scheduled backup configured to pre-sync Thunderbird says so in status — read
// from the descriptor's argv for a flat export schedule and from the run-job's job
// file for a GUI/Desktop run-job — as a GREEN informational note (never a warning),
// and not at all when the schedule does not pre-sync.
func TestStatusSurfacesConfiguredPresync(t *testing.T) {
	now := time.Date(2026, 10, 24, 2, 0, 0, 0, time.UTC)
	const want = "syncs Thunderbird before it runs"

	// No pre-sync configured -> no note.
	if rep := Assess(healthyInput(now), now); strings.Contains(strings.Join(rep.Reasons, "\n"), want) {
		t.Errorf("a schedule with no pre-sync must not claim one: %v", rep.Reasons)
	}

	// Flat export schedule (-thunderbird-sync in the argv -> Desc.Presync) -> GREEN note.
	flat := healthyInput(now)
	flat.Desc.Presync = &schedule.PresyncDesc{Kind: "thunderbird"}
	rep := Assess(flat, now)
	if rep.Posture != "GREEN" {
		t.Errorf("a configured pre-sync is informational; posture should stay GREEN, got %s (%v)", rep.Posture, rep.Reasons)
	}
	if !strings.Contains(strings.Join(rep.Reasons, "\n"), want) {
		t.Errorf("status should note the configured pre-sync (flat schedule): %v", rep.Reasons)
	}

	// Run-job schedule (the setting lives in the job file) -> the note too.
	rj := healthyInput(now)
	jp := filepath.Join(t.TempDir(), "j.json")
	if err := job.Write(jp, job.Job{Name: "x", Out: "/a", Mode: "incremental", ThunderbirdSync: &job.ThunderbirdSync{}}); err != nil {
		t.Fatal(err)
	}
	rj.KindEv = schedule.KindEvidence{JobPath: jp}
	if rep := Assess(rj, now); !strings.Contains(strings.Join(rep.Reasons, "\n"), want) {
		t.Errorf("status should note the configured pre-sync (run-job): %v", rep.Reasons)
	}
}

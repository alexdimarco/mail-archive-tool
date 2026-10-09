package main

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-466, R18, R14, S29
// OPEN GAP ENCODER (R18, X6; N11 — owner ruling pending). `status` should watch
// a verify that `schedule` installed the way it watches the archive's backup:
// once the last verify is older than twice the verify's own cadence, a WARN
// (verify_stale) names the remedy. Today it cannot. A verify is its own
// scheduler entry and records nothing archive-local (the descriptor records the
// backup, friction #4), so status never learns the verify's cadence, and
// verify_stale fires only for a descriptor an older release wrote for a verify
// (MA-155). Closing this needs the owner's design ruling: a verify descriptor
// (for example .mailarchive-verify-schedule.json) and its health semantics.
// When that is built, REMOVE the t.Skip: the test then asserts it end to end —
// a backup and a weekly verify installed through the real binary in the
// sandboxed scheduler, the last verify finished 30 days ago, and `status -json`
// carrying verify_stale — after the positive twin, a verify that ran a day ago
// and is not stale.
func TestStatusWarnsWhenAScheduledVerifyStops(t *testing.T) {
	t.Skip("confirmed-open R18/X6 gap (N11): a verify installed by `schedule` records nothing archive-local, so status cannot know its cadence and verify_stale never fires for it; pending the owner's ruling on a verify descriptor and its health semantics. Un-skip when that is built.")
	sandboxScheduler(t)
	out := t.TempDir()
	for _, args := range [][]string{
		{"schedule", "-install", "--", "-out", out, "-auto"},
		{"schedule", "-interval", "weekly", "-at", "05:00", "-install", "--", "verify", "-out", out},
	} {
		if code, _, stderr := runCLIOut(t, args...); code != 0 {
			t.Fatalf("%v exited %d: %s", args, code, stderr)
		}
	}

	// reasonCodes records a verify that finished ago, then reads status -json.
	reasonCodes := func(ago time.Duration) []string {
		t.Helper()
		when := time.Now().Add(-ago).UTC()
		if err := state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Attested: true}); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runCLIOut(t, "status", "-json", "-out", out)
		if code != 0 {
			t.Fatalf("status -json exited %d: %s", code, stderr)
		}
		var doc struct {
			ReasonCodes []string `json:"reason_codes"`
		}
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
			t.Fatalf("status -json is not valid JSON: %v\n%s", err, stdout)
		}
		return doc.ReasonCodes
	}

	// Positive twin: a weekly verify that ran a day ago is not stale.
	if codes := reasonCodes(24 * time.Hour); slices.Contains(codes, "verify_stale") {
		t.Errorf("a verify that ran a day ago was reported stale: %v", codes)
	}
	// The weekly verify stopped: its last run was 30 days ago.
	if codes := reasonCodes(30 * 24 * time.Hour); !slices.Contains(codes, "verify_stale") {
		t.Errorf("status does not warn that the scheduled weekly verify stopped (last verify 30 days ago); reason codes: %v", codes)
	}
}

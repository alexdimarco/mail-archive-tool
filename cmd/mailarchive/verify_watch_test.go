package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-466, R18, R14, S29
// `status` watches a verify that `schedule` installed the way it watches the
// archive's backup (R18, X6; owner ruling FB-4): the verify records its own
// descriptor (.mailarchive-verify-schedule.json, MA-457), and once the last
// verify is older than twice that verify's own cadence a WARN (verify_stale)
// names the remedy. End to end: a backup and a weekly verify installed through
// the real binary in the sandboxed scheduler; the positive twin first, a verify
// that ran a day ago is not stale; then the last verify finished 30 days ago
// and `status -json` carries verify_stale. This was the open-gap encoder for
// N11 until the verify descriptor was built; it is now a permanent guard. It is
// also what an archive sees after another archive's verify took over a shared
// -name on launchd or Task Scheduler (FB-4's residual, not detected at
// install): its own verify stops running, and verify_stale follows. As for the
// backup, the verify's entry itself is watched too: the installed verify raises
// no verify_not_installed, and once its entry is taken out of the sandboxed
// scheduler by hand, `status -json` carries verify_not_installed at once (its
// last run a day ago, so not verify_stale).
func TestStatusWarnsWhenAScheduledVerifyStops(t *testing.T) {
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

	// As for the backup, the verify's entry itself is watched: installed, no
	// WARN; taken out of the scheduler by hand, a WARN at once, before any
	// staleness.
	if codes := reasonCodes(24 * time.Hour); slices.Contains(codes, "verify_not_installed") {
		t.Errorf("an installed scheduled verify is reported as not installed; reason codes: %v", codes)
	}
	takeOutOfSandbox(t, schedule.DefaultNameFor(out)+"-verify")
	if codes := reasonCodes(24 * time.Hour); !slices.Contains(codes, "verify_not_installed") || slices.Contains(codes, "verify_stale") {
		t.Errorf("a scheduled verify taken out of the scheduler is not reported as such (its last run a day ago); reason codes: %v", codes)
	}
}

// covers: MA-458, R18, R14, S29
// Through the real binary (sandboxed scheduler), `status` reads the verify's own
// descriptor (owner ruling FB-4). Beside a backup and a weekly verify it prints
// a `Verify schedule:` line naming the verify, its state and cadence, and
// `status -json` carries `verify_schedule`. Once the backup is removed, the
// folder (no manifest, no run record yet) is still reported for the verify it
// records — never refused as "no archive" — and says no backup schedule is
// recorded. A verify descriptor cut short is a WARN
// (verify_descriptor_unreadable) naming the file and its remedy, and the line
// says it is unreadable.
func TestStatusNamesTheVerifySchedule(t *testing.T) {
	sandboxScheduler(t)
	out := t.TempDir()
	derived := schedule.DefaultNameFor(out) + "-verify"
	run := func(args ...string) string {
		t.Helper()
		code, stdout, stderr := runCLIOut(t, args...)
		if code != 0 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
		return stdout
	}
	run("schedule", "-install", "--", "-out", out, "-auto")
	run("schedule", "-interval", "weekly", "-at", "05:00", "-install", "--", "verify", "-out", out)
	type facet struct {
		Name, State, Interval, At string
	}
	statusJSON := func() (facet, []string) {
		t.Helper()
		var doc struct {
			VerifySchedule *facet   `json:"verify_schedule"`
			ReasonCodes    []string `json:"reason_codes"`
		}
		stdout := run("status", "-json", "-out", out)
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
			t.Fatalf("status -json is not valid JSON: %v\n%s", err, stdout)
		}
		if doc.VerifySchedule == nil {
			return facet{}, doc.ReasonCodes
		}
		return *doc.VerifySchedule, doc.ReasonCodes
	}
	line := `Verify schedule: "` + derived + `" installed · weekly at 05:00`

	if text := run("status", "-out", out); !strings.Contains(text, line) {
		t.Errorf("status does not name the verify schedule (want %q):\n%s", line, text)
	}
	if got, _ := statusJSON(); got != (facet{Name: derived, State: "installed", Interval: "weekly", At: "05:00"}) {
		t.Errorf("status -json verify_schedule = %+v, want the weekly verify %s", got, derived)
	}

	// Only the verify left: still reported, never "no archive".
	run("schedule", "-out", out, "-remove")
	text := run("status", "-out", out)
	if !strings.Contains(text, line) || !strings.Contains(text, "Schedule:   none recorded") {
		t.Errorf("with the backup removed, status does not report the verify alone:\n%s", text)
	}

	// Cut short: named with its remedy.
	path := filepath.Join(out, schedule.VerifyDescriptorName)
	if err := os.WriteFile(path, []byte("{\"version\":1,\"name\":"), 0o600); err != nil {
		t.Fatal(err)
	}
	text = run("status", "-out", out)
	for _, want := range []string{"Verify schedule: (descriptor present but unreadable", path, "delete it if no verify schedule"} {
		if !strings.Contains(text, want) {
			t.Errorf("status does not report the unreadable verify descriptor (lacks %q):\n%s", want, text)
		}
	}
	if got, codes := statusJSON(); got != (facet{}) || !slices.Contains(codes, "verify_descriptor_unreadable") {
		t.Errorf("status -json for an unreadable verify descriptor: verify_schedule=%+v, reason codes %v", got, codes)
	}
}

package main

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-72, R12, S28
// A flat job flag placed before `--` is explained by whether the job after `--`
// already carries it: a duplicate is dropped ("drop the -out before --"),
// while a genuinely misplaced flag is moved ("move ... after --") — friction
// #20.
func TestStrayFlagPhrasing(t *testing.T) {
	// The same flag appears after -- → duplicate: drop the one before --.
	msg := strayFlagRefusal([]string{"-out"}, []string{"-out", "/a", "-auto"})
	if !strings.Contains(msg, "drop the -out before --") {
		t.Errorf("duplicate-flag phrasing wrong: %q", msg)
	}
	if !strings.Contains(msg, "already carries it") {
		t.Errorf("duplicate-flag phrasing lacks the reason: %q", msg)
	}
	// The flag is not in the job → misplaced: move it after --.
	msg = strayFlagRefusal([]string{"-copy-first"}, []string{"-out", "/a", "-auto"})
	if !strings.Contains(msg, "move -copy-first after --") {
		t.Errorf("misplaced-flag phrasing wrong: %q", msg)
	}
}

// covers: MA-72, R16, S28
// jobUsesOutlook recognises the -outlook flag (but not -outlook-sync-wait), and
// the reminder printed after installing an -outlook schedule names the one-time
// "allow programmatic access" prompt an unattended run cannot answer (friction
// #10).
func TestOutlookReminderHelper(t *testing.T) {
	if !jobUsesOutlook([]string{"-out", "/a", "-outlook", "-outlook-sync-wait", "5m0s"}) {
		t.Error("jobUsesOutlook missed -outlook")
	}
	if jobUsesOutlook([]string{"-out", "/a", "-outlook-sync-wait", "5m0s"}) {
		t.Error("jobUsesOutlook matched -outlook-sync-wait alone")
	}
	if jobUsesOutlook([]string{"-out", "/a", "-auto"}) {
		t.Error("jobUsesOutlook matched a job without -outlook")
	}
	if !strings.Contains(outlookReminderText, "programmatic access") {
		t.Errorf("reminder text lacks the prompt name: %q", outlookReminderText)
	}
}

// covers: MA-75, R18, S29
// status falls back to the last-run record when a first run failed before any
// manifest or descriptor existed (reporting the failed run, RED, exit 0), and
// still refuses a directory that has none of the three.
func TestStatusFallsBackToLastRun(t *testing.T) {
	out := t.TempDir()
	if err := state.WriteLastRun(out, state.LastRun{
		Status:  state.RunFailed,
		Started: time.Now().Add(-time.Hour),
		Error:   "boom: the drive was not mounted",
	}); err != nil {
		t.Fatal(err)
	}
	stdout, err := exec.Command(testBin, "status", "-out", out).Output()
	if err != nil {
		t.Fatalf("status refused an archive with only a last-run record: %v", err)
	}
	s := string(stdout)
	if !strings.Contains(s, "FAILED") || !strings.Contains(s, "boom") {
		t.Errorf("status did not report the failed run:\n%s", s)
	}
	if !strings.Contains(s, "RED") {
		t.Errorf("a failed run should be RED:\n%s", s)
	}

	empty := t.TempDir()
	code, stderr := runCLI("status", "-out", empty)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("no manifest", "last-run record"))
}

// covers: MA-115, R18, S29
// `status -json` emits a valid, typed JSON document on a real archive and exits
// 0 (product nas-01).
func TestStatusJSON(t *testing.T) {
	out := t.TempDir()
	if code, stderr := runCLI("-input", "../../testdata/support.pst", "-out", out); code != 0 {
		t.Fatalf("export failed (%d): %s", code, stderr)
	}
	stdout, err := exec.Command(testBin, "status", "-json", "-out", out).Output()
	if err != nil {
		t.Fatalf("status -json exited non-zero: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout, &doc); err != nil {
		t.Fatalf("status -json did not emit valid JSON: %v\n%s", err, stdout)
	}
	for _, k := range []string{"version", "posture", "reasons", "messages", "indexed", "out"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("status -json missing key %q:\n%s", k, stdout)
		}
	}
	if p, _ := doc["posture"].(string); p == "" {
		t.Errorf("status -json empty posture:\n%s", stdout)
	}
}

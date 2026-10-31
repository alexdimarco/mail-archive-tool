//go:build darwin || windows

package main

import (
	"os/exec"
	"strings"
	"testing"
)

// covers: MA-936
// Hourly with the Thunderbird sync stays, with a note (owner ruling 5 of 2026-10-31, held
// finding effects-6): `mailarchive schedule -interval hourly` of a job with
// -thunderbird-sync prints that on an hour with no new mail Thunderbird stays open about 20
// minutes while the backup waits for mail ("about", not "up to": review finding
// behaviour-3). Positive twin first: the same job daily prints no
// such note; hourly without the sync prints none. Run through the real built program, on
// the preview path (nothing is installed). Build-tagged to the platforms where
// -thunderbird-sync is accepted (macOS, Windows). The -h text says it too.
func TestAnHourlyScheduleWithTheSyncPrintsTheNote(t *testing.T) {
	const note = "With Hourly and the Thunderbird sync, Thunderbird stays open for about 20 minutes in an hour with no new mail, while the backup waits for mail."
	out, store := t.TempDir(), t.TempDir()
	preview := func(label string, args ...string) string {
		t.Helper()
		cmd := exec.Command(testBin, append([]string{"schedule"}, args...)...)
		b, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: the preview was refused: %v", label, err)
		}
		if !strings.Contains(string(b), "NOT applied") {
			t.Fatalf("%s: not a preview:\n%s", label, b)
		}
		return string(b)
	}
	if got := preview("daily with the sync", "-interval", "daily", "--", "-out", out, "-input", store, "-thunderbird-sync"); strings.Contains(got, note) {
		t.Fatalf("positive twin: a daily schedule with the sync prints the hourly note:\n%s", got)
	}
	if got := preview("hourly without the sync", "-interval", "hourly", "-at", "00:15", "--", "-out", out, "-input", store); strings.Contains(got, note) {
		t.Errorf("an hourly schedule without the sync prints the note:\n%s", got)
	}
	if got := preview("hourly with the sync", "-interval", "hourly", "-at", "00:15", "--", "-out", out, "-input", store, "-thunderbird-sync"); !strings.Contains(got, note) {
		t.Errorf("an hourly schedule with the sync does not print %q:\n%s", note, got)
	}
	if got := preview("hourly with the sync, flat form", "-interval", "hourly", "-out", out, "-input", store, "-thunderbird-sync"); !strings.Contains(got, note) {
		t.Errorf("the flat form, hourly with the sync, does not print the note:\n%s", got)
	}

	// The -h text says it where -interval is described.
	cmd := exec.Command(testBin, "schedule", "-h")
	b, _ := cmd.CombinedOutput()
	if !strings.Contains(string(b), note) {
		t.Errorf("schedule -h does not carry the note:\n%s", b)
	}
}

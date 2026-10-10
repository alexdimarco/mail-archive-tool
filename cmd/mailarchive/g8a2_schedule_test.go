package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-455, MA-457, R14, R18, S14
// G8-AI2 (g8 adversarial pass, fresh-eyes re-check of its tombstone): the
// `-remove -- verify -out Y` hint offers the entry Y's verify descriptor
// records only when that name binds to Y. The earlier tombstone also planted a
// backup descriptor with the same name, which made the verify descriptor
// unreadable first and so never reached the hint; here the verify descriptor
// stands alone (recorded on "another host", which the reader lets through and
// only an operation that acts on the entry refuses). Positive twin first: Y's
// own custom-named verify descriptor is offered by name.
func TestVerifyRemoveHintNeverOffersAnotherArchivesEntry(t *testing.T) {
	sandboxScheduler(t)
	x, y := t.TempDir(), t.TempDir()
	xName := schedule.DefaultNameFor(x)
	if code, stdout, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", x, "-auto"); code != 0 {
		t.Fatalf("fixture: X's backup: exit %d: %s%s", code, stdout, stderr)
	}
	assure.Reached(t, schedule.Query(xName) == schedule.Installed, "fixture: X's backup installed")

	own := schedule.DescribeSpec(schedule.Spec{Name: "weekly-y-g8a2", Interval: schedule.Weekly, At: "05:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"verify", "-out", y}, Out: y})
	if err := schedule.WriteVerifyDescriptor(y, own); err != nil {
		t.Fatal(err)
	}
	_, stdout, stderr := runCLIOut(t, "schedule", "-remove", "--", "verify", "-out", y)
	if !strings.Contains(stdout+stderr, "weekly-y-g8a2") {
		t.Fatalf("the archive's own verify record is not offered by name:\n%s%s", stdout, stderr)
	}

	planted := schedule.DescribeSpec(schedule.Spec{Name: xName, Interval: schedule.Weekly, At: "05:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"verify", "-out", y}, Out: y})
	planted.Host = "elsewhere-g8a2"
	if err := schedule.WriteVerifyDescriptor(y, planted); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLIOut(t, "schedule", "-remove", "--", "verify", "-out", y)
	if strings.Contains(stdout+stderr, xName) {
		t.Errorf("the verify removal hint offers another archive's entry %s (exit %d):\n%s%s", xName, code, stdout, stderr)
	}
	if schedule.Query(xName) != schedule.Installed {
		t.Errorf("X's entry was removed")
	}
}

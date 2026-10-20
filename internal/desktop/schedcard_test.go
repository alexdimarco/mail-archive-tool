package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-691
// A local-files archive's Weekly-backup card shows Repair/Remove but NOT the interval/time
// inputs (those render only for the Microsoft 365 Install flow). The schedule JS (schedPost)
// must therefore read schedInterval/schedTime DEFENSIVELY — a missing input must not throw and
// silently kill the click. Shipped-code bug: Repair/Remove "did nothing" on a local archive
// because schedPost read $('schedInterval').value unconditionally (null.value → TypeError
// before the fetch). This pins the DOM shape the JS must handle.
func TestMA691_LocalArchiveScheduleCardHasNoIntervalInputs(t *testing.T) {
	out := tbArchive(t)
	if err := schedule.WriteDescriptor(out, schedule.Descriptor{
		Name: schedule.DefaultNameFor(out), Interval: "daily", At: "03:00",
		Job: []string{"run-job", filepath.Join(out, "job.json")},
	}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), Out: out, GOOS: "darwin"}))

	if !strings.Contains(page, `id="schedRemove"`) {
		t.Fatal("a local archive with a schedule must show the Remove button")
	}
	// The interval/time inputs and the Install button must be ABSENT — so schedPost can never
	// assume they exist.
	for _, bad := range []string{`id="schedInterval"`, `id="schedTime"`, `id="schedInstall"`} {
		if strings.Contains(page, bad) {
			t.Fatalf("a local archive's Weekly-backup card must not render %q (the schedule JS must handle its absence)", bad)
		}
	}
}

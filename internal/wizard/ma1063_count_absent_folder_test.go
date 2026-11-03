package wizard

import (
	"os"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1063, R36
// A scheduled run-job that finds its archive folder absent (the backup drive not plugged in, the
// share away) says nothing about the delete, so it leaves the count of runs refused for a delete
// this computer never began as it was, as the source-loss streak leaves itself on a run that says
// nothing about the source (docs/design-archive-delete.md §15.10 slice 6's "the same rule"; the
// slice-6 and G3 adversarial pass's MED-1). Refused runs 40 days apart with a drive-away run between
// them still pause on the run after the drive comes back. Positive twin first: the same 40 days with
// no absent run between pauses.
func TestMA1063_AnAbsentFolderLeavesTheRunJobsDeleteCount(t *testing.T) {
	// Positive twin: a refused run 40 days after the first stops this job's schedule.
	twin := newMA1058Rig(t, true)
	twin.seed(40)
	RunHeadless(twin.jobPath)
	if len(*twin.removed) != 1 || (*twin.removed)[0] != twin.name {
		t.Fatalf("positive twin: 40 days of refused runs did not stop the schedule: removed %v", *twin.removed)
	}

	g := newMA1058Rig(t, true)
	g.seed(40)
	first, ok := g.count()
	assure.Reached(t, ok, "the seeded count")

	// The drive is away: the run is refused for the absent folder and the count is untouched.
	away := g.out + "-unplugged"
	if err := os.Rename(g.out, away); err != nil {
		t.Fatal(err)
	}
	code := RunHeadless(g.jobPath)
	s, ok := g.count()
	assure.Refused(t, code, g.failLog(), assure.Code(1), assure.Names("is not present", g.out),
		assure.NoSideEffect(func() bool {
			return ok && s.ConsecutiveFails == first.ConsecutiveFails && s.FirstUnreachable.Equal(first.FirstUnreachable) && len(*g.removed) == 0
		}))

	// The drive is back, the marker still there: the refused run spans 40 days and pauses.
	if err := os.Rename(away, g.out); err != nil {
		t.Fatal(err)
	}
	RunHeadless(g.jobPath)
	if len(*g.removed) != 1 || (*g.removed)[0] != g.name {
		t.Fatalf("the refused run after the drive came back did not stop the schedule: removed %v, count %+v", *g.removed, s)
	}
	assure.Reached(t, g.paused(), "the auto-pause email")
	assure.Reached(t, strings.Contains(g.failLog(), "auto-paused the scheduled backup: "+AutoPauseDeleteWhy), "the pause told beside the job file")
	if time.Since(first.FirstUnreachable) < 39*24*time.Hour {
		t.Fatalf("the seeded count is not 40 days old: %v", first.FirstUnreachable)
	}
}

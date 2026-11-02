package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// countInstalls stands in for the scheduler's install of an entry, counting each.
func countInstalls(t *testing.T) *[]string {
	t.Helper()
	var installed []string
	orig := installEntry
	installEntry = func(s schedule.Spec) error { installed = append(installed, s.Name); return nil }
	t.Cleanup(func() { installEntry = orig })
	return &installed
}

// covers: MA-1036, R36
// The wizard's two installers refuse a folder whose delete did not finish (the 1.5.3 re-walk's
// guards pass, G1): the health view's "Repair the scheduled backup" (repairSchedule, which
// Resume uses too) and the "Yes — every day" offer after an export (installOffered, which the
// Desktop's Add another archive reaches). Before, each installed the scheduled backup the delete
// had stopped. Now each refuses in MA-1015's words, "…so its scheduled backup was not installed;
// nothing was changed.", before any question or file: no scheduler entry, no job file. Positive
// twins first: another folder, with that unfinished delete in the same log, gets its job file and
// its entry from the offer, and the same guard lets a repair of it through.
func TestWizardInstallsRefuseAFolderWhoseDeleteDidNotFinish(t *testing.T) {
	installed := countInstalls(t)
	removed := countRemovals(t)
	logPath := filepath.Join(t.TempDir(), archdelete.LogName)
	half := archiveFolder(t, "Half deleted", true)
	other := archiveFolder(t, "Other", false)
	startedLine(t, logPath, half)
	exe := filepath.Join(t.TempDir(), "mailarchive")

	// Positive twins.
	spec, err := installOffered(wizardChoice{out: other, auto: true}, schedule.Daily, "02:00", exe, logPath)
	otherJob, jerr := job.PathFor(schedule.DefaultNameFor(other))
	if jerr != nil {
		t.Fatal(jerr)
	}
	if _, serr := os.Stat(otherJob); err != nil || serr != nil || len(*installed) != 1 || spec.Name != schedule.DefaultNameFor(other) {
		t.Fatalf("positive twin: the offer for another folder: %v, job file %v, installed %v", err, serr, *installed)
	}
	d, derr := schedule.ReadDescriptor(half)
	if derr != nil {
		t.Fatal(derr)
	}
	otherSpec, serr := RepairSpec(other, schedule.Descriptor{Name: schedule.DefaultNameFor(other), Interval: "daily", At: "02:00"}, exe)
	if serr != nil {
		t.Fatal(serr)
	}
	if err := guardedInstall(logPath, other, otherSpec, nil); err != nil || len(*installed) != 2 {
		t.Fatalf("positive twin: a repair's install for another folder: %v (installed %v)", err, *installed)
	}
	*installed = nil

	halfJob, jerr := job.PathFor(schedule.DefaultNameFor(half))
	if jerr != nil {
		t.Fatal(jerr)
	}
	const words = "Delete not finished: click Delete again. This archive's delete did not finish, so its scheduled backup was not installed; nothing was changed."
	_, err = installOffered(wizardChoice{out: half, auto: true}, schedule.Daily, "02:00", exe, logPath)
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1), assure.Names(words),
		assure.NoSideEffect(func() bool {
			_, serr := os.Stat(halfJob)
			return len(*installed) == 0 && os.IsNotExist(serr)
		}))

	err = repairSchedule(half, d, logPath)
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1), assure.Names(words),
		assure.NoSideEffect(func() bool { return len(*installed) == 0 && len(*removed) == 0 }))
}

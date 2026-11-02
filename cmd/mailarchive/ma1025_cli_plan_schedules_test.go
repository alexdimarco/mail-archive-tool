package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-1025, R36
// delete-archive's plan at the terminal lists only the scheduled backups step 2 would stop, or
// says none (the 1.5.3 friction review's B1: it listed "Schedules to stop: NAME, NAME-verify" by
// hash whether or not either was installed). Positive twin first: an installed backup is listed.
func TestTheTerminalPlanListsOnlyInstalledBackups(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Installed")
	e.sched.installed[schedule.DefaultNameFor(dir)] = true
	a := &scripted{isAttended: true}
	if err := deleteArchive(a, dir, filepath.Join(t.TempDir(), "plan.txt"), "", io.Discard); err != nil {
		t.Fatalf("the positive twin's plan: %v", err)
	}
	said := strings.Join(a.said, "\n")
	if !strings.Contains(said, "  Scheduled backups it stops: the backup "+schedule.DefaultNameFor(dir)+".") || strings.Contains(said, "-verify") {
		t.Fatalf("the positive twin's plan:\n%s", said)
	}

	none := e.plainArchive("Nothing Installed")
	a = &scripted{isAttended: true}
	if err := deleteArchive(a, none, filepath.Join(t.TempDir(), "plan.txt"), "", io.Discard); err != nil {
		t.Fatalf("the plan: %v", err)
	}
	said = strings.Join(a.said, "\n")
	if !strings.Contains(said, "  Scheduled backups it stops: none.") || strings.Contains(said, schedule.DefaultNameFor(none)) || strings.Contains(said, "Schedules to stop") {
		t.Fatalf("nothing installed, the plan:\n%s", said)
	}
}

package schedule

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-863, R37
// Install returns a distinct error when the scheduler took the backup's entry (step 2a) but
// the archive's descriptor could not be written (step 2b) — design-desktop-settings-card
// section 9, R3C-13 — so the Desktop can undo a create or restore an edit, instead of an
// untyped error it can't tell from a refusal. The text is the one Install always gave. It runs
// against the crontab seams; no real scheduler is touched. Positive twin first: a writable
// archive folder is recorded and returns no error.
func TestInstallSaysWhenTheEntryIsInstalledButNotRecorded(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a descriptor write that fails is modelled with a read-only folder, for a user who is not root")
	}
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	crontab := ""
	crontabList = func() (string, error) { return crontab, nil }
	crontabInstall = func(c string) error { crontab = c; return nil }

	ok := t.TempDir()
	if err := installOn(backupSpecFor(ok, DefaultNameFor(ok)), "linux"); err != nil {
		t.Fatalf("the positive twin, a writable archive folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ok, DescriptorName)); err != nil {
		t.Fatalf("the positive twin recorded nothing: %v", err)
	}

	out := t.TempDir()
	if err := os.Chmod(out, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(out, 0o700) })
	err := installOn(backupSpecFor(out, DefaultNameFor(out)), "linux")
	if err == nil {
		t.Fatal("an install whose descriptor could not be written returned no error")
	}
	if !errors.Is(err, ErrNotRecorded) {
		t.Errorf("the step-2b failure is not ErrNotRecorded: %v", err)
	}
	var nr *NotRecordedError
	if !errors.As(err, &nr) || nr.Cause == nil {
		t.Errorf("the step-2b failure carries no cause: %#v", err)
	}
	if !strings.Contains(err.Error(), "is installed, but its descriptor could not be written") {
		t.Errorf("the text changed: %v", err)
	}
	if !strings.Contains(crontab, CronMarker(DefaultNameFor(out))) {
		t.Error("the entry was not installed, so this is not the step-2b case")
	}

	// A refusal before the scheduler (an invalid spec) is not ErrNotRecorded.
	bad := backupSpecFor(out, "")
	if err := installOn(bad, "linux"); err == nil || errors.Is(err, ErrNotRecorded) {
		t.Errorf("a refusal before anything was installed reads as not recorded: %v", err)
	}
}

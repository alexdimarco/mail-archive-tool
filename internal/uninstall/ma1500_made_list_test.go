package uninstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1500, R36
// Uninstall removes the stand-alone wizard's made-list and its lock file with the other settings
// files (docs/design-per-mailbox-archives.md §10, C10): both made by the made-list's own writer
// (job.RecordMade), ConfigTargets names them beside the settings file (the positive twin), a dry
// run lists them in the plan and removes nothing, a real run removes them, and the archive folder
// the list names is kept (R36).
func TestUninstallRemovesTheMadeListAndItsLock(t *testing.T) {
	origList, origRemove, origClear := listSchedules, removeSchedule, clearSignIn
	t.Cleanup(func() { listSchedules, removeSchedule, clearSignIn = origList, origRemove, origClear })
	listSchedules = func() []schedule.InstalledEntry { return nil }
	removeSchedule = func(schedule.Spec) error { return nil }
	clearSignIn = func(string, string) error { return nil }

	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "desktop-settings.json"), "x")
	kept := t.TempDir()
	writeFile(t, filepath.Join(kept, manifestName), `{"version":6,"stores":{}}`)
	made := filepath.Join(cfg, job.MadeListName)
	if err := job.RecordMade(made, kept, "imap.example.com"); err != nil {
		t.Fatal(err)
	}
	lock := job.MadeLockPath(made)
	// The set lock a set's run holds beside them (the adversarial pass's CON-03), made by its own
	// taker and released, as a finished set leaves it.
	release, err := job.HoldSetLock(made)
	if err != nil {
		t.Fatal(err)
	}
	release()
	files := []string{made, lock, filepath.Join(cfg, job.MadeSetLockName)}
	for _, f := range files {
		if _, err := os.Lstat(f); err != nil {
			t.Fatalf("the made-list's writer did not make %s: %v", f, err)
		}
	}

	targets, err := ConfigTargets(cfg, []string{kept})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range targets {
		got[p] = true
	}
	if !got[filepath.Join(cfg, "desktop-settings.json")] {
		t.Fatalf("the positive twin: the settings file is a target: %q", targets)
	}
	for _, f := range files {
		if !got[f] {
			t.Errorf("%s is not among uninstall's targets: %q", filepath.Base(f), targets)
		}
	}

	rep, err := Uninstall(cfg, []string{kept}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	listed := strings.Join(rep.ConfigFiles, "|")
	for _, f := range files {
		if !strings.Contains(listed, f) {
			t.Errorf("the plan does not list %s: %v", f, rep.ConfigFiles)
		}
		if _, err := os.Lstat(f); err != nil {
			t.Errorf("a dry run removed %s", f)
		}
	}
	if _, err := Uninstall(cfg, []string{kept}, "", false); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if _, err := os.Lstat(f); !os.IsNotExist(err) {
			t.Errorf("the real run left %s (%v)", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(kept, manifestName)); err != nil {
		t.Fatal("uninstall must NEVER delete an archive (R36)")
	}
}

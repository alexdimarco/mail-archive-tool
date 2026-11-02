package uninstall

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1003, R36
// Uninstall's plan names the deletion log among what it removes and says to copy it first
// (docs/design-archive-delete.md §8.1, §13 slice 5; O2-07): the dry run's Report carries the
// log's path when the settings folder holds one, and the sentence is the design's. Positive
// twin first: a settings folder with no deletions.log names none.
func TestUninstallPlanNamesTheDeletionLogAndSaysCopyItFirst(t *testing.T) {
	origList, origRemove, origClear := listSchedules, removeSchedule, clearSignIn
	defer func() { listSchedules, removeSchedule, clearSignIn = origList, origRemove, origClear }()
	listSchedules = func() []schedule.InstalledEntry { return nil }
	removeSchedule = func(schedule.Spec) error { t.Fatal("a dry run removed a schedule"); return nil }
	clearSignIn = func(string, string) error { t.Fatal("a dry run cleared the sign-in"); return nil }

	cfg := t.TempDir()
	writeFile(t, filepath.Join(cfg, "desktop-settings.json"), "{}")
	rep, err := Uninstall(cfg, nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ConfigFiles) != 1 || rep.DeletionsLog != "" {
		t.Fatalf("positive twin: no deletion log, none named: files %q, log %q", rep.ConfigFiles, rep.DeletionsLog)
	}

	log := filepath.Join(cfg, archdelete.LogName)
	writeFile(t, log, `{"event":"started"}`+"\n")
	rep, err = Uninstall(cfg, nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DeletionsLog != log {
		t.Fatalf("the plan must name the deletion log %s, got %q (files %q)", log, rep.DeletionsLog, rep.ConfigFiles)
	}
	// The sentence says where to put the copy (amended 2026-11-02, the 1.5.3 friction review's B11).
	if DeletionsLogNote != "Copy deletions.log first, somewhere you keep records (your Documents folder, for example), if you need it to check saved deletion records." {
		t.Fatalf("the plan's sentence is not the design's: %q", DeletionsLogNote)
	}
	if _, err := os.Stat(log); err != nil {
		t.Fatalf("a dry run removed the deletion log: %v", err)
	}
}

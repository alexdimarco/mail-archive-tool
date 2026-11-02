package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
)

// startedLine adds a delete's started line for folder, and no finished line, to the log at
// logPath: the delete of folder did not finish (§5.4).
func startedLine(t *testing.T, logPath, folder string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "w-" + filepath.Base(folder), "folder": folder, "row": folder, "kind": "plain"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr != nil || cerr != nil {
		t.Fatal(werr, cerr)
	}
	if u, err := archdelete.UnfinishedDelete(logPath, folder, ""); err != nil || u == nil {
		t.Fatalf("precondition: the log says the delete of %s did not finish: %v %v", folder, u, err)
	}
}

// archiveFolder is a folder holding a manifest; withSchedule adds its own scheduled backup's
// record, so a Pause would stop its live entry. (Only a Pause is asked of one with a record: a
// Resume of it would ask the real scheduler.)
func archiveFolder(t *testing.T, name string, withSchedule bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mailarchive-manifest.json"), []byte(`{"version":1,"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if withSchedule {
		if err := schedule.WriteDescriptor(dir, schedule.Descriptor{Name: schedule.DefaultNameFor(dir), Interval: "daily", At: "02:00",
			Job: []string{"mailarchive", "-out", dir}}); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// countRemovals stands in for the scheduler's removal of a live entry, counting each.
func countRemovals(t *testing.T) *[]string {
	t.Helper()
	var removed []string
	orig := removeLiveEntry
	removeLiveEntry = func(s schedule.Spec) error { removed = append(removed, s.Name); return nil }
	t.Cleanup(func() { removeLiveEntry = orig })
	return &removed
}

// covers: MA-1015, R36
// wizard.PauseArchive and wizard.ResumeArchive, which the terminal's `pause` and `resume` and the
// Desktop's Pause and Resume all reach, refuse a folder whose delete did not finish, in MA-1015's
// words, before they change anything (the 1.5.3 re-walk's adversarial pass, F1): Pause stops no
// live schedule and writes no pause marker into the half-deleted folder; Resume leaves the folder
// paused (its pause file and note as they were) and so puts back no scheduled backup. An empty log
// path asks MailArchive's own deletions.log. Positive twins first: another archive, with that
// unfinished delete in the same log, is paused (its live entry stopped) and resumed.
func TestPauseAndResumeRefuseAFolderWhoseDeleteDidNotFinish(t *testing.T) {
	removed := countRemovals(t)
	logPath := filepath.Join(t.TempDir(), archdelete.LogName)
	stopped := archiveFolder(t, "Stopped", true)
	parked := archiveFolder(t, "Parked", false)
	if err := seal.Seal(parked, "parked before the delete"); err != nil {
		t.Fatal(err)
	}
	startedLine(t, logPath, stopped)
	startedLine(t, logPath, parked)

	// Positive twins.
	other := archiveFolder(t, "Other", true)
	if err := PauseArchive(other, "parked", logPath); err != nil || !seal.IsSealed(other) || len(*removed) != 1 {
		t.Fatalf("positive twin: Pause of another archive: %v (removed %v)", err, *removed)
	}
	plain := archiveFolder(t, "Plain", false)
	if err := seal.Seal(plain, "parked"); err != nil {
		t.Fatal(err)
	}
	if err := ResumeArchive(plain, logPath); err != nil || seal.IsSealed(plain) {
		t.Fatalf("positive twin: Resume of another archive: %v", err)
	}
	*removed = nil

	err := PauseArchive(stopped, "parked", logPath)
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not paused; nothing was changed."),
		assure.NoSideEffect(func() bool { return !seal.IsSealed(stopped) && len(*removed) == 0 }))

	err = ResumeArchive(parked, logPath)
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not resumed; nothing was changed."),
		assure.NoSideEffect(func() bool { return seal.IsSealed(parked) && seal.Reason(parked) == "parked before the delete" }))

	// An empty log path is MailArchive's own deletions.log (this package's throwaway home).
	def, derr := archdelete.DefaultLogPath()
	if derr != nil {
		t.Fatal(derr)
	}
	t.Cleanup(func() { os.Remove(def) })
	fresh := archiveFolder(t, "Fresh", true)
	if err := PauseArchive(fresh, "parked", ""); err != nil || !seal.IsSealed(fresh) {
		t.Fatalf("positive twin: Pause with the default log holding nothing for it: %v", err)
	}
	if err := seal.Unseal(fresh); err != nil {
		t.Fatal(err)
	}
	*removed = nil
	startedLine(t, def, fresh)
	err = PauseArchive(fresh, "parked", "")
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again", "it was not paused"),
		assure.NoSideEffect(func() bool { return !seal.IsSealed(fresh) && len(*removed) == 0 }))
	startedLine(t, def, parked)
	err = ResumeArchive(parked, "")
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again", "it was not resumed"),
		assure.NoSideEffect(func() bool { return seal.IsSealed(parked) }))
}

// covers: MA-1015, R36
// A deletions.log that cannot be read (a folder where the file goes) refuses wizard.PauseArchive
// and wizard.ResumeArchive, naming it, since whether the folder's delete finished cannot be told;
// nothing is changed. Positive twin first: with the log readable, the same Pause and Resume work.
func TestPauseAndResumeRefuseADeletionLogTheyCannotRead(t *testing.T) {
	removed := countRemovals(t)
	logPath := filepath.Join(t.TempDir(), archdelete.LogName)
	a := archiveFolder(t, "A", true)
	b := archiveFolder(t, "B", false)
	if err := PauseArchive(a, "parked", logPath); err != nil || !seal.IsSealed(a) || len(*removed) != 1 {
		t.Fatalf("positive twin: Pause with a readable log: %v (removed %v)", err, *removed)
	}
	if err := seal.Seal(b, "parked"); err != nil {
		t.Fatal(err)
	}
	if err := ResumeArchive(b, logPath); err != nil || seal.IsSealed(b) {
		t.Fatalf("positive twin: Resume with a readable log: %v", err)
	}
	if err := seal.Unseal(a); err != nil {
		t.Fatal(err)
	}
	if err := seal.Seal(b, "parked"); err != nil {
		t.Fatal(err)
	}
	*removed = nil

	if err := os.Mkdir(logPath, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	err := PauseArchive(a, "parked", logPath)
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, "+logPath, "so it cannot tell whether a delete of this archive did not finish; nothing was changed. Check that file, then try again."),
		assure.NoSideEffect(func() bool { return !seal.IsSealed(a) && len(*removed) == 0 }))
	err = ResumeArchive(b, logPath)
	assure.Refused(t, refusedCode(err), errString(err), assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, "+logPath, "nothing was changed"),
		assure.NoSideEffect(func() bool { return seal.IsSealed(b) }))
}

// refusedCode is 1 for archdelete's typed refusal, 0 otherwise.
func refusedCode(err error) int {
	if archdelete.AsRefusal(err) != nil {
		return 1
	}
	return 0
}

package archdelete

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// addStarted appends a delete's started line for folder, and no finished line, to the log at
// logPath: the delete of folder did not finish (§5.4).
func addStarted(t *testing.T, logPath, folder string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "g-" + filepath.Base(folder), "folder": folder, "row": folder, "kind": "plain"})
	if err != nil {
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
	if u, err := UnfinishedDelete(logPath, folder, ""); err != nil || u == nil {
		t.Fatalf("precondition: the log says the delete of %s did not finish: %v %v", folder, u, err)
	}
}

func refusalCode(err error) int {
	if AsRefusal(err) != nil {
		return 1
	}
	return 0
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// covers: MA-1036, R36
// GuardedInstall, the one path the terminal's `schedule -install`, the wizard's repair and its
// "Yes — every day" offer (and so Resume and the Desktop's Add another archive) install a
// scheduled entry through, asks deletions.log before the install and refuses a folder whose delete
// did not finish, in MA-1015's words; nothing is installed (the 1.5.3 re-walk's guards pass, G1).
// It asks again after the install, so a delete that began while the entry went in — whose step 2
// may already have stopped the schedules, and so missed it — has the entry removed again (G4). A
// log that cannot be read refuses too. Positive twin first: another folder, with that unfinished
// delete in the same log, is installed once and never undone.
func TestGuardedInstallRefusesAndUndoesForADeleteNotFinished(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), LogName)
	half := filepath.Join(t.TempDir(), "Half deleted")
	other := filepath.Join(t.TempDir(), "Other")
	for _, d := range []string{half, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	addStarted(t, logPath, half)
	installs, undos := 0, 0
	install := func() error { installs++; return nil }
	undo := func() error { undos++; return nil }

	// Positive twin.
	if err := GuardedInstall(logPath, other, "its schedule", install, undo); err != nil || installs != 1 || undos != 0 {
		t.Fatalf("positive twin: install for another folder: %v (installs %d, undos %d)", err, installs, undos)
	}
	installs = 0

	// Before: refused, nothing installed.
	err := GuardedInstall(logPath, half, "its schedule", install, undo)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so its schedule was not installed; nothing was changed."),
		assure.NoSideEffect(func() bool { return installs == 0 && undos == 0 }))
	if r := AsRefusal(err); r == nil || r.Case != CaseUnfinished {
		t.Fatalf("the refusal's case: %+v", r)
	}

	// After: a delete of the folder begins while the entry goes in; the entry is removed again.
	raced := filepath.Join(t.TempDir(), "Raced")
	if err := os.MkdirAll(raced, 0o755); err != nil {
		t.Fatal(err)
	}
	racing := func() error { installs++; addStarted(t, logPath, raced); return nil }
	err = GuardedInstall(logPath, raced, "its scheduled backup", racing, undo)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so its scheduled backup was installed and then removed again; nothing was changed."),
		assure.NoSideEffect(func() bool { return installs == 1 && undos == 1 }))

	// The removal again fails: the refusal says the entry may still run, and how to stop it.
	installs, undos = 0, 0
	raced2 := filepath.Join(t.TempDir(), "Raced2")
	if err := os.MkdirAll(raced2, 0o755); err != nil {
		t.Fatal(err)
	}
	stuck := func() error { undos++; return errors.New("the scheduler said no") }
	err = GuardedInstall(logPath, raced2, "its schedule", func() error { installs++; addStarted(t, logPath, raced2); return nil }, stuck)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. A delete of this archive began while its schedule was being installed, and that schedule could not be removed again (the scheduler said no), so it may still run: click Delete again, which stops it."),
		assure.NoSideEffect(func() bool { return installs == 1 && undos == 1 }))

	// A log that cannot be read: refused before the install, naming it.
	installs, undos = 0, 0
	bad := filepath.Join(t.TempDir(), LogName)
	if err := os.Mkdir(bad, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	err = GuardedInstall(bad, other, "its schedule", install, undo)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, "+bad, "nothing was changed. Check that file, then try again."),
		assure.NoSideEffect(func() bool { return installs == 0 && undos == 0 }))
}

// covers: MA-1015, R36
// When MailArchive cannot find its settings folder (no home folder set), the deletion log's
// refusal says so and how to fix it, rather than naming a bare "deletions.log" and saying "Check
// that file" with no path to check (the 1.5.3 re-walk's guards pass, G7). Positive twin first: with
// the home folder set, the same check of a folder with nothing waiting lets it through.
func TestActionRefusalWithNoSettingsFolderSaysSo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "A")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ActionRefusal("", dir, "it was not paused"); err != nil {
		t.Fatalf("positive twin: the default log with nothing for the folder: %v", err)
	}
	for _, v := range []string{"HOME", "XDG_CONFIG_HOME", "AppData", "APPDATA", "USERPROFILE"} {
		t.Setenv(v, "")
	}
	if _, derr := DefaultLogPath(); derr == nil {
		t.Skip("this system finds a settings folder with no home folder set")
	}
	err := ActionRefusal("", dir, "it was not paused")
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("MailArchive could not find its settings folder, which holds its deletion log (",
			"so it cannot tell whether a delete of this archive did not finish; nothing was changed. Check that your home folder is set (HOME, or AppData on Windows), then try again."),
		assure.NoSideEffect(func() bool { _, serr := os.Stat(dir); return serr == nil }))
	if r := AsRefusal(err); r == nil || r.Case != CaseLogUnreadable {
		t.Fatalf("the refusal's case: %+v", r)
	}
}

package archdelete

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The G2 settle pass (MA-1045): the adversarial pass of MA-1044's backup guard, its findings
// fixed or encoded here. The probes it ran are kept as these guards.

// ma1045Half is a folder a delete stopped part way in (its manifest still there, removed last),
// a deletion log, and an mbox to back up.
func ma1045Half(t *testing.T) (half, logPath, mbox string) {
	t.Helper()
	logPath = filepath.Join(t.TempDir(), LogName)
	half = filepath.Join(t.TempDir(), "Half deleted")
	if err := os.MkdirAll(half, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(half, ".mailarchive-manifest.json"), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mbox = filepath.Join(t.TempDir(), "mail.mbox")
	if err := os.WriteFile(mbox, []byte("From a@b Mon Jan  1 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return
}

// ma1045Started appends a started line for folder, recording identity when given.
func ma1045Started(t *testing.T, logPath, folder string, id *Identity) {
	t.Helper()
	line := map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "s1045-" + filepath.Base(folder), "folder": folder, "row": folder, "kind": "plain"}
	if id != nil {
		line["identity"] = id
	}
	ma1044Line(t, logPath, line)
}

// ma1045Case is the case of err's refusal, or "" when it is none.
func ma1045Case(err error) Case {
	if r := AsRefusal(err); r != nil {
		return r.Case
	}
	return ""
}

// covers: MA-1045, R36
// A backup that starts while a delete of its folder is still RUNNING (the delete holds the
// archive's lock, its holder line naming a delete) is not told the delete "did not finish" and to
// "click Delete again" (the settle pass's LOW-1): it is refused as the busy archive it is, in
// msgBackupDeleting's words with the folder, matching lockfile.ErrHeld (kind archive_busy), so a
// scheduled run skips quietly naming the delete, as it did before MA-1044 (app.ScheduledLockSkip);
// the real engine writes nothing into the folder. Positive twins first: the same delete stopped
// part way (no lock held) is "Delete not finished…", and another program holding the lock (an
// export) while the delete is unfinished does not make it a running delete.
func TestABackupWhileADeleteRunsIsToldTheDeleteIsRunning(t *testing.T) {
	half, logPath, mbox := ma1045Half(t)
	ma1045Started(t, logPath, half, nil)
	unfinished := "Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: " + half + ")"

	// Positive twins.
	err := app.RefuseUnfinishedDelete(half, logPath)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(unfinished))
	if ma1045Case(err) != CaseUnfinished || errors.Is(err, lockfile.ErrHeld) {
		t.Fatalf("a delete stopped part way: case %q, held %v", ma1045Case(err), errors.Is(err, lockfile.ErrHeld))
	}
	backup, lerr := lockfile.AcquireAs(filepath.Join(half, lockName), "export")
	if lerr != nil {
		t.Fatal(lerr)
	}
	err = app.RefuseUnfinishedDelete(half, logPath)
	backup.Release()
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(unfinished))
	if ma1045Case(err) != CaseUnfinished {
		t.Fatalf("another program holding the lock: case %q", ma1045Case(err))
	}

	// The delete is running: it holds the lock.
	held, lerr := lockfile.AcquireAs(filepath.Join(half, lockName), "delete")
	if lerr != nil {
		t.Fatal(lerr)
	}
	defer held.Release()
	names := func() []string { n := ma1044Names(half); slices.Sort(n); return n }
	before := names()
	err = app.RefuseUnfinishedDelete(half, logPath)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("This archive is being deleted by another MailArchive window or terminal, so it was not backed up; nothing was changed. When that delete ends, see its result there. (Folder: "+half+")"),
		assure.Forbid("Delete again", "did not finish"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
	if c := ma1045Case(err); c != CaseBusy {
		t.Fatalf("the refusal's case: %q", c)
	}
	if !errors.Is(err, lockfile.ErrHeld) || app.FailureKind(err) != state.FailArchiveBusy || app.RefusedAsNoArchive(err) {
		t.Fatalf("a running delete is a held lock: ErrHeld %v, kind %q, taken back %v", errors.Is(err, lockfile.ErrHeld), app.FailureKind(err), app.RefusedAsNoArchive(err))
	}
	line, quiet := app.ScheduledLockSkip(half, err, time.Now())
	assure.Reached(t, quiet && strings.Contains(line, "This scheduled backup was skipped: a delete of this archive started at"), "a scheduled run skips quietly, naming the delete: "+line)

	// The real engine, scheduled.
	_, err = app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: half, Mode: export.Incremental, Unattended: true, DeletionsLog: logPath}, log.New(io.Discard, "", 0), nil)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("This archive is being deleted by another MailArchive window or terminal, so it was not backed up"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
}

// covers: MA-1044, MA-1045, R36
// Every spelling of the half-deleted folder is refused — a trailing separator, "..", "/.", a link
// to it, and upper case where the file system ignores case — while a folder with a similar name,
// a shorter one and a folder inside it are not (the settle pass's probe, kept as a guard).
// Positive twin first: the similar folders are backed up.
func TestTheBackupGuardRefusesEverySpellingOfTheFolder(t *testing.T) {
	half, logPath, _ := ma1045Half(t)
	id, ok := folderIdentity(half)
	if !ok {
		t.Fatal("precondition: the folder's identity")
	}
	ma1045Started(t, logPath, half, &id)

	// Positive twins.
	similar := filepath.Join(filepath.Dir(half), "Half deleted 2")
	prefix := filepath.Join(filepath.Dir(half), "Half")
	inside := filepath.Join(half, "sub")
	for _, p := range []string{similar, prefix} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{similar, prefix, inside} {
		if err := app.RefuseUnfinishedDelete(p, logPath); err != nil {
			t.Fatalf("positive twin: a different folder %s is refused: %v", p, err)
		}
	}

	spell := map[string]string{
		"trailing separator": half + string(filepath.Separator),
		"dot-dot":            filepath.Join(half, "..", filepath.Base(half)),
		"dot":                half + string(filepath.Separator) + ".",
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(half, link); err == nil {
		spell["link to it"] = link
	} else if runtime.GOOS != "windows" {
		t.Fatal(err)
	} // on Windows a link needs a privilege the run may not have
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		spell["upper case"] = filepath.Join(filepath.Dir(half), strings.ToUpper(filepath.Base(half)))
	}
	for _, p := range spell {
		err := app.RefuseUnfinishedDelete(p, logPath)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
			assure.Names("so it was not backed up; nothing was changed. (Folder: "+p+")"),
			assure.NoSideEffect(func() bool { return len(ma1044Names(half)) == 1 }))
		if c := ma1045Case(err); c != CaseUnfinished {
			t.Errorf("%s: case %q, want %q", p, c, CaseUnfinished)
		}
	}
}

// covers: MA-1044, MA-1045, R36
// A deletion log that cannot be trusted refuses every backup (fail closed, as ruled): a link, a
// folder, a file over the cap, one readable by others (off Windows), each as unreadable, kind
// deletion_log_unreadable; a torn "finished" line of the delete refuses too (the settle pass's
// probe, kept as a guard; LOW-3 records the cost for the operator). Positive twin first: the
// same log, whole and private, lets another folder through.
func TestTheBackupGuardFailsClosedOnALogItCannotTrust(t *testing.T) {
	half, logPath, _ := ma1045Half(t)
	ma1044Line(t, logPath, map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "x", "folder": "/elsewhere", "kind": "plain"})

	// Positive twin.
	if err := app.RefuseUnfinishedDelete(half, logPath); err != nil {
		t.Fatalf("positive twin: a whole, private log: %v", err)
	}

	cases := map[string]func(p string) error{
		"a link":   func(p string) error { return os.Symlink(logPath, p) },
		"a folder": func(p string) error { return os.Mkdir(p, 0o700) },
		"over the cap": func(p string) error {
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			terr := f.Truncate(logCap + 1)
			return errors.Join(terr, f.Close())
		},
	}
	if runtime.GOOS != "windows" {
		cases["readable by others"] = func(p string) error {
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				return err
			}
			return os.Chmod(p, 0o644)
		}
	}
	for name, mk := range cases {
		p := filepath.Join(t.TempDir(), LogName)
		if err := mk(p); err != nil {
			if name == "a link" && runtime.GOOS == "windows" {
				continue // a link needs a privilege the run may not have
			}
			t.Fatal(err)
		}
		err := app.RefuseUnfinishedDelete(half, p)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
			assure.Names("MailArchive could not read its deletion log, "+p, "(Folder: "+half+")"),
			assure.NoSideEffect(func() bool { return len(ma1044Names(half)) == 1 }))
		if c, k := ma1045Case(err), app.FailureKind(err); c != CaseLogUnreadable || k != state.FailDeletionLogUnreadable {
			t.Errorf("%s: case %q, kind %q: want %q, %q", name, c, k, CaseLogUnreadable, state.FailDeletionLogUnreadable)
		}
	}

	torn := filepath.Join(t.TempDir(), LogName)
	ma1045Started(t, torn, half, nil)
	f, err := os.OpenFile(torn, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, werr := f.WriteString(`{"at":"2026-11-02T10:00:02Z","event":"finished","run":"s1045-Half deleted","folder":"` + filepath.ToSlash(half) + `","outc`)
	if cerr := f.Close(); werr != nil || cerr != nil {
		t.Fatal(werr, cerr)
	}
	err = app.RefuseUnfinishedDelete(half, torn)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names("(Folder: "+half+")"),
		assure.NoSideEffect(func() bool { return len(ma1044Names(half)) == 1 }))
}

// covers: MA-1044
// OPEN GAP (the settle pass's MED-1; design-archive-delete §14): on macOS and Linux a removable or
// network volume's device number is handed out at mount, so after the drive is unplugged and
// plugged in again the half-deleted folder can have another one; its started line's identity
// then no longer matches, and the backup is let in. When it is closed (a volume identity that
// survives a remount, or a marker in the folder) remove the Skip: this becomes a guard.
func TestABackupIntoAHalfDeletedFolderOnARemountedDriveIsRefused(t *testing.T) {
	t.Skip("open gap MED-1 (G2 settle pass, MA-1044): a folder whose drive was plugged in again under another device number is not refused on macOS and Linux; closing it changes the delete's identity or adds a marker to its steps, which owes the 10-lens pre-code design review; recorded in design-archive-delete §14")
	if runtime.GOOS == "windows" {
		t.Skip("Windows keys by the volume serial number, which survives a remount")
	}
	half, logPath, mbox := ma1045Half(t)
	id, ok := folderIdentity(half)
	if !ok {
		t.Fatal("precondition: the folder's identity")
	}
	ma1045Started(t, logPath, half, &Identity{Volume: id.Volume + 1, FileID: id.FileID}) // as recorded before the remount
	_, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: half, Mode: export.Incremental, Unattended: true, DeletionsLog: logPath}, log.New(io.Discard, "", 0), nil)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names("Delete not finished: click Delete again."),
		assure.NoSideEffect(func() bool { return len(ma1044Names(half)) == 1 }))
}

// covers: MA-1044
// OPEN GAP (the settle pass's MED-2; design-archive-delete §14, for the operator): another
// computer's or another user account's backup into the same folder (a NAS share, a drive moved
// between computers) reads its own deletions.log, which never saw this delete, so it is let in.
// When it is closed (a marker the delete writes into the folder first and removes last) remove the
// Skip: this becomes a guard.
func TestAnotherComputersBackupIntoAHalfDeletedFolderIsRefused(t *testing.T) {
	t.Skip("open gap MED-2 (G2 settle pass, MA-1044): another computer's or account's backup reads its own deletions.log and is not refused; whether it is in G2's scope is the operator's call, and the fix (a marker in the folder) changes the delete's steps, which owes the 10-lens pre-code design review; recorded in design-archive-delete §14")
	half, logPath, mbox := ma1045Half(t)
	ma1045Started(t, logPath, half, nil)
	otherComputersLog := filepath.Join(t.TempDir(), LogName) // its own settings folder: no such delete
	_, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: half, Mode: export.Incremental, Unattended: true, DeletionsLog: otherComputersLog}, log.New(io.Discard, "", 0), nil)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names("Delete not finished: click Delete again."),
		assure.NoSideEffect(func() bool { return len(ma1044Names(half)) == 1 }))
}

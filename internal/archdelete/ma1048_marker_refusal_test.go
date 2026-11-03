package archdelete

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// The delete marker's refusals (docs/design-archive-delete.md §15.3, §15.4, §15.10 slice 2;
// MA-1048). Nothing writes the marker yet (slice 3): each test plants it by hand (plantMarker),
// as a delete stopped part way leaves it.

// ma1048Names is dir's names, sorted.
func ma1048Names(dir string) []string {
	n := ma1044Names(dir)
	slices.Sort(n)
	return n
}

// ma1048Half is an archive folder holding the marker, another folder beside it, and an mbox.
func ma1048Half(t *testing.T) (half, other, mbox string) {
	t.Helper()
	half, _, mbox = ma1045Half(t)
	plantMarker(t, half)
	other = filepath.Join(t.TempDir(), "Other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, ".mailarchive-manifest.json"), []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return half, other, mbox
}

// ma1048Backup is a scheduled backup of mbox into out by the real engine, with this package's
// check installed, asking the deletion log at logPath.
func ma1048Backup(out, logPath, mbox string) error {
	_, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: out, Mode: export.Incremental, Unattended: true, DeletionsLog: logPath}, log.New(io.Discard, "", 0), nil)
	return err
}

// covers: MA-1048, R36
// Every backup asks the marker, whatever this computer's deletion log knows (§15.3; MED-2): with
// another computer's or account's log (one that never saw the delete), with no log at all, and
// with a log that cannot be read, the engine's installed check refuses a folder holding the marker
// in the marker-only words (DM-11), kind delete_unfinished, taken back as no archive; the real
// app.Run writes nothing into the folder. When this computer's log knows the delete, MA-1044's
// words stay. A delete still running (it holds the lock, verb=delete) is the busy refusal, whether
// the log or the marker found it. Positive twin first: another folder, beside it, is backed up
// with the same logs.
func TestMA1048_EveryBackupRefusesTheMarkerWithNoLogLine(t *testing.T) {
	half, other, mbox := ma1048Half(t)
	anotherComputers := filepath.Join(t.TempDir(), LogName) // its own settings folder: no such delete
	unreadable := filepath.Join(t.TempDir(), LogName)
	if err := os.Mkdir(unreadable, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	thisComputers := filepath.Join(t.TempDir(), LogName)
	ma1045Started(t, thisComputers, half, nil)
	onlyMarker := delmark.MsgOnlyMarker + " (Folder: " + half + ")"

	// Positive twins.
	for _, lp := range []string{anotherComputers, ""} {
		if err := ma1048Backup(other, lp, mbox); err != nil {
			t.Fatalf("positive twin: a backup into the folder beside it (log %q): %v", lp, err)
		}
	}

	// Amended 2026-11-03 (slice 2b, the slice-2 pass's LOW-4 (a)): with the log unreadable the words
	// say so (delmark.LogUnreadableWords), never that the log "has no record of it"; still exact.
	unreadableWords := delmark.LogUnreadableWords(delmark.WhatNotBackedUp, "MailArchive's deletion log "+unreadable+" must be a regular file (not a symlink, pipe or device)") + " (Folder: " + half + ")"
	for name, lp := range map[string]string{"another computer's log": anotherComputers, "a log that cannot be read": unreadable} {
		err := app.RefuseUnfinishedDelete(half, lp)
		want := onlyMarker
		if lp == unreadable {
			want = unreadableWords
		}
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(want), assure.Forbid("Delete again", "could not read its deletion log"))
		if r := AsRefusal(err); r == nil || r.Case != CaseUnfinished {
			t.Errorf("%s: the refusal's case: %+v", name, r)
		}
		if app.FailureKind(err) != state.FailDeleteUnfinished || !app.RefusedAsNoArchive(err) || app.FoundSourceUnreachable(err) {
			t.Errorf("%s: kind %q, taken back %v, source unreachable %v", name, app.FailureKind(err), app.RefusedAsNoArchive(err), app.FoundSourceUnreachable(err))
		}
	}
	u, err := UnfinishedAt("", half, "")
	if err != nil || u == nil || !u.MarkerOnly || u.Marker != delmark.Regular || u.MarkerDetails.Run != "0123456789abcdef" {
		t.Fatalf("no log at all: the marker alone knows the delete: %+v %v", u, err)
	}

	// The real engine, scheduled, with another computer's log.
	before := ma1048Names(half)
	err = ma1048Backup(half, anotherComputers, mbox)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(onlyMarker),
		assure.NoSideEffect(func() bool { return slices.Equal(before, ma1048Names(half)) }))

	// This computer's log knows the delete: its own words.
	err = app.RefuseUnfinishedDelete(half, thisComputers)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+half+")"),
		assure.Forbid("another computer"))

	// The delete is still running: the busy refusal, with either log.
	held, lerr := lockfile.AcquireAs(filepath.Join(half, lockName), "delete")
	if lerr != nil {
		t.Fatal(lerr)
	}
	defer held.Release()
	for _, lp := range []string{anotherComputers, thisComputers} {
		err := app.RefuseUnfinishedDelete(half, lp)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
			assure.Names("This archive is being deleted by another MailArchive window or terminal, so it was not backed up"), assure.Forbid("did not finish"))
		if !errors.Is(err, lockfile.ErrHeld) || app.FailureKind(err) != state.FailArchiveBusy {
			t.Errorf("a running delete is a held lock: ErrHeld %v, kind %q", errors.Is(err, lockfile.ErrHeld), app.FailureKind(err))
		}
	}
}

// covers: MA-1048, R36
// The engine's own ask (app.RefuseDeleteMarker, which it asks where no check is installed; MA-1054)
// gives the very words and kind the installed check gives for a delete only the marker knows
// (DM-24): a regular marker, a name that is not a regular file, and a lookup that fails at a
// folder that is there. Positive twin first: both let a folder without the marker through.
func TestMA1048_TheEnginesFallbackSaysWhatTheInstalledCheckSays(t *testing.T) {
	half, other, _ := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)
	linked := filepath.Join(t.TempDir(), "Linked")
	if err := os.Mkdir(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, filepath.Join(t.TempDir(), "nothing"), filepath.Join(linked, delmark.Name))

	// Positive twins.
	if err := app.RefuseUnfinishedDelete(other, logPath); err != nil {
		t.Fatalf("positive twin: the installed check: %v", err)
	}
	if err := app.RefuseDeleteMarker(other, delmark.WhatNotBackedUp); err != nil {
		t.Fatalf("positive twin: the fallback: %v", err)
	}

	same := func(name, folder string) {
		t.Helper()
		installed := app.RefuseUnfinishedDelete(folder, logPath)
		restore := app.SetUnfinishedDeleteCheck(nil)
		fallback := app.RefuseUnfinishedDelete(folder, logPath)
		restore()
		if installed == nil || fallback == nil || installed.Error() != fallback.Error() || app.FailureKind(installed) != app.FailureKind(fallback) ||
			app.RefusedAsNoArchive(installed) != app.RefusedAsNoArchive(fallback) {
			t.Errorf("%s: installed %q (%s), fallback %q (%s)", name, errText(installed), app.FailureKind(installed), errText(fallback), app.FailureKind(fallback))
		}
	}
	same("a regular marker", half)
	same("a link at the name", linked)
	restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New("input/output error")}
	})
	same("a lookup that fails", other)
	restore()
}

// covers: MA-1048, R36
// Pause, Resume, a Switch, Save settings, Install and Repair ask the marker through ActionRefusal
// (§15.4, DM-23), and GuardedInstall with it: a folder holding it is refused in the marker-only
// words saying what was not done; a name that is not a regular file in its words; a lookup that
// fails at a folder that is there in the "could not check" words for each action (DM-23's list),
// case could-not-check — nothing installed. Positive twin first: another folder, with the same
// log, is let through and installed.
func TestMA1048_EveryActionRefusesTheMarkerInItsWords(t *testing.T) {
	half, other, _ := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)
	installs := 0
	install := func() error { installs++; return nil }
	undo := func() error { return nil }

	// Positive twins.
	if err := ActionRefusal(logPath, other, "it was not paused"); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if err := GuardedInstall(logPath, other, "its schedule", install, undo); err != nil || installs != 1 {
		t.Fatalf("positive twin: install %v (%d)", err, installs)
	}

	done := []string{"it was not paused", "it was not resumed", "it was not made the current archive",
		"these settings were not saved", "its scheduled backup was not installed", "its schedule was not installed"}
	for _, d := range done {
		err := ActionRefusal(logPath, half, d)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.OnlyMarkerWords(d)))
		if r := AsRefusal(err); r == nil || r.Case != CaseUnfinished {
			t.Errorf("%s: case %+v", d, r)
		}
	}
	err := GuardedInstall(logPath, half, "its schedule", install, undo)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.OnlyMarkerWords("its schedule was not installed")),
		assure.NoSideEffect(func() bool { return installs == 1 }))

	// A name that is not a regular file.
	if err := os.Remove(filepath.Join(half, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(half, delmark.Name), 0o755); err != nil {
		t.Fatal(err)
	}
	err = ActionRefusal(logPath, half, "it was not paused")
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.NotRegularWords("it was not paused")))

	// The lookup fails at a folder that is there.
	restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New("input/output error")}
	})
	defer restore()
	for _, d := range done {
		err := ActionRefusal(logPath, other, d)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
			assure.Names("MailArchive could not check this archive's folder for a delete that did not finish (input/output error), so "+d+"; nothing was changed. Check that the drive or folder can be read, then try again."))
		if r := AsRefusal(err); r == nil || r.Case != CaseCouldNotCheck {
			t.Errorf("%s: case %+v", d, r)
		}
	}
	err = GuardedInstall(logPath, other, "its schedule", install, undo)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names("could not check this archive's folder"),
		assure.NoSideEffect(func() bool { return installs == 1 }))
}

// covers: MA-1048, R36
// UnfinishedAt asks the marker first (§15.3; DM-21): a regular marker with no line in the log is a
// delete only the marker knows, its run and began read for words; the log's own line, when it has
// one, is returned with the marker beside it; a marker the folder does not hold leaves the log's
// answer as UnfinishedDelete gives it; a lookup that fails at a folder that is there is the typed
// error. The log-only asks stay log-only (DM-21): uninstall's PendingDeletes and Remove from list.
// Positive twin first: a folder without the marker and no line is nothing.
func TestMA1048_UnfinishedAtAsksTheMarkerFirst(t *testing.T) {
	half, other, _ := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)

	// Positive twin.
	if u, err := UnfinishedAt(logPath, other, ""); u != nil || err != nil {
		t.Fatalf("positive twin: %+v %v", u, err)
	}

	u, err := UnfinishedAt(logPath, half, "")
	if err != nil || u == nil || !u.MarkerOnly || u.Marker != delmark.Regular || u.MarkerDetails.Began != "2026-11-02T15:04:05Z" {
		t.Fatalf("a delete only the marker knows: %+v %v", u, err)
	}
	if p, err := PendingDeletes(logPath, ""); err != nil || len(p) != 0 {
		t.Fatalf("uninstall's ask stays log-only (DM-21): %q %v", p, err)
	}
	if err := UnlistRefusal(half, Deps{Log: logPath}); err != nil {
		t.Fatalf("Remove from list stays log-only (DM-21): %v", err)
	}

	ma1045Started(t, logPath, half, nil)
	u, err = UnfinishedAt(logPath, half, "")
	if err != nil || u == nil || u.MarkerOnly || u.Marker != delmark.Regular || u.Run != "s1045-Half deleted" {
		t.Fatalf("the log's line with the marker beside it: %+v %v", u, err)
	}

	ma1045Started(t, logPath, other, nil)
	u, err = UnfinishedAt(logPath, other, "")
	if err != nil || u == nil || u.MarkerOnly || u.Marker != delmark.Absent {
		t.Fatalf("the log alone: %+v %v", u, err)
	}

	restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New("input/output error")}
	})
	defer restore()
	var le *delmark.LookupError
	if u, err := UnfinishedAt(logPath, other, ""); u != nil || !errors.As(err, &le) {
		t.Fatalf("a lookup that fails: %+v %v", u, err)
	}
}

// covers: MA-1048, R36
// A dead drive's lookup is "no marker" through the installed check (DM-22): a scheduled backup into
// a folder whose drive is gone, its marker lookup failing with "device not ready" or "network name
// deleted", gets MA-689's archive-absent refusal, never "could not check"; at a folder that is
// there the same failure is "could not check", kind archive_unwritable, taken back as no archive.
// Positive twin first: with the lookup working, the folder beside it is backed up.
func TestMA1048_ADeadDrivesLookupGivesTheArchiveAbsentRefusal(t *testing.T) {
	_, other, mbox := ma1048Half(t)
	logPath := filepath.Join(t.TempDir(), LogName)
	if err := ma1048Backup(other, logPath, mbox); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	for _, cause := range []string{"The device is not ready.", "The specified network name is no longer available."} {
		restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
			return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New(cause)}
		})
		gone := filepath.Join(t.TempDir(), "unplugged", "Mail")
		err := ma1048Backup(gone, logPath, mbox)
		if k := app.FailureKind(err); err == nil || k != state.FailArchiveAbsent || strings.Contains(err.Error(), "could not check") {
			t.Errorf("%s: a dead drive: %v (kind %q), want MA-689's archive_absent", cause, err, k)
		}
		before := ma1048Names(other)
		err = ma1048Backup(other, logPath, mbox)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
			assure.Names("MailArchive could not check this archive's folder for a delete that did not finish ("+cause+"), so it was not backed up; nothing was changed. Check that the drive or folder can be read, then try again. (Folder: "+other+")"),
			assure.NoSideEffect(func() bool { return slices.Equal(before, ma1048Names(other)) }))
		if k := app.FailureKind(err); k != state.FailArchiveUnwritable || !app.RefusedAsNoArchive(err) {
			t.Errorf("%s: at a folder that is there: kind %q, taken back %v", cause, k, app.RefusedAsNoArchive(err))
		}
		restore()
	}
}

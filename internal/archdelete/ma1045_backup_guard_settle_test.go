package archdelete

import (
	"bytes"
	"context"
	"encoding/json"
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
	"mail-archive-tool/internal/delmark"
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

// ma1045StoppedInStep6 is a folder a real delete stopped in step 6 (design-archive-delete §15.7,
// DM-2 item 1, with the owner's ruling Q1): a removal fault on a file that is not the anchor, so
// the manifest, the delete's marker (written just before the first removal) and the lock file are
// all left — what a 1.5.6 delete stopped part way leaves. logPath is the deletion log of the
// computer that began it; mbox is an mbox to back up.
func ma1045StoppedInStep6(t *testing.T) (half, logPath, mbox string) {
	t.Helper()
	half = smallArchive(t, filepath.Join(t.TempDir(), "Half deleted"))
	writeFile(t, filepath.Join(half, "search.db"), "index")
	r := newRig(t, half)
	faultOn(t, nil, func(rel string) error {
		if rel == "search.db" {
			return inUseErr(rel)
		}
		return nil
	})
	if res := r.run(t, false); res.Status != StatusPartial || !res.Unfinished {
		t.Fatalf("precondition: the delete stops in step 6:\n%s", res.Message())
	}
	if !exists(filepath.Join(half, manifestName)) || !exists(filepath.Join(half, delmark.Name)) || exists(filepath.Join(half, filepath.FromSlash(stemA+".html"))) {
		t.Fatal("precondition: half deleted, the anchor and the marker both there")
	}
	return half, r.x.Log, ma1046Mbox(t)
}

// covers: MA-1044, MA-1046
// The remounted drive (the settle pass's MED-1; design-archive-delete §15.5, §15.7 with DM-2 items 1
// and 2): on macOS and Linux a removable or network volume's device number is handed out at mount,
// so after the drive is unplugged and plugged in again the half-deleted folder can have another
// one, and its started line's identity no longer matches. The marker refuses the backup whatever
// the device number (MA-1048), so this test asks more: that THIS computer's log still finds its
// delete — the device-and-file-number match alone finds no line (the remount is real), and the
// refusal is the log's own words, without the "begun on another computer" sentence — which needs
// §15.5's run-id route. Skipped until slice 5 builds that route (§15.7's DM-2 (b) correction, 3
// November 2026); every assertion is kept, nothing narrowed.
func TestABackupIntoAHalfDeletedFolderOnARemountedDriveIsRefused(t *testing.T) {
	t.Skip("open until slice 5 (design-archive-delete §15.5's run-id route; §15.7's DM-2 (b) correction, 3 November 2026): after a remount on macOS or Linux this computer's deletion log no longer finds its own delete of the folder by device and file number, so the refusal is the marker's \"begun on another computer\" words, not the log's (MED-1 of the G2 settle pass, MA-1044)")
	if runtime.GOOS == "windows" {
		t.Skip("Windows keys by the volume serial number, which survives a remount")
	}
	half, logPath, mbox := ma1045StoppedInStep6(t)
	ma1045Remount(t, logPath)
	if u, err := UnfinishedDelete(logPath, half, ""); err != nil || u != nil {
		t.Fatalf("precondition: the device and file number alone no longer find the delete: %+v %v", u, err)
	}
	names := func() []string { n := ma1044Names(half); slices.Sort(n); return n }
	before := names()
	_, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: half, Mode: export.Incremental, Unattended: true, DeletionsLog: logPath}, log.New(io.Discard, "", 0), nil)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names("Delete not finished: click Delete again."),
		assure.Forbid("another computer"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
}

// ma1045Remount rewrites the started lines of the log at path as recorded before a remount: the
// folder's device number one more than it is now.
func ma1045Remount(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []byte
	for _, b := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var l logLine
		if err := json.Unmarshal(b, &l); err != nil {
			t.Fatal(err)
		}
		if l.Event == evStarted && l.Identity != nil {
			l.Identity.Volume++
		}
		nb, err := json.Marshal(l)
		if err != nil {
			t.Fatal(err)
		}
		out = append(append(out, nb...), '\n')
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1044, MA-1046
// Another computer's or another user account's backup into a folder whose delete stopped in step 6
// (the settle pass's MED-2, closed by the delete's marker: design-archive-delete §15.2, §15.7 with
// DM-2 items 1 and 3, §15.10 slice 3): its own deletions.log — a brand-new one — never saw the delete,
// and the real engine refuses it on the marker the delete wrote, in the marker-only words in full
// (DM-11: what happened first, no "click Delete again" to a person who began no delete), nothing
// written into the folder: its sorted name list is the same after as before (DM-2 item 1, stricter
// than the one-name count of the hand-made fixture this replaced).
func TestAnotherComputersBackupIntoAHalfDeletedFolderIsRefused(t *testing.T) {
	half, _, mbox := ma1045StoppedInStep6(t)
	otherComputersLog := filepath.Join(t.TempDir(), LogName) // its own settings folder: no such delete
	names := func() []string { n := ma1044Names(half); slices.Sort(n); return n }
	before := names()
	_, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: half, Mode: export.Incremental, Unattended: true, DeletionsLog: otherComputersLog}, log.New(io.Discard, "", 0), nil)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"),
		assure.Forbid("click Delete again"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
	if app.FailureKind(err) != state.FailDeleteUnfinished {
		t.Fatalf("kind %q", app.FailureKind(err))
	}
}

// covers: MA-1044
// OPEN, a stated limit (design-archive-delete §15.6, DM-1 (b); §15.7, DM-2 item 4): a half-deleted
// folder WITHOUT its marker — removed by hand, by a sync conflict or an antivirus, hidden by a share
// that hides dot names, or lost to a power cut on FAT or exFAT (DM-12) — is refused only by the
// computer whose deletion log began the delete: another computer's or account's backup reads its
// own log and is let in. The original MED-2 body (a hand-written started line, no marker), kept so
// no property leaves the suite. Not the owner's Q1 case: a delete stopped before step 6's first
// removal leaves a whole archive that another computer backs up as intended (pinned by
// TestMA1046_EveryStopBeforeTheFirstRemovalLeavesNoMarker).
func TestAnotherComputersBackupIntoAHalfDeletedFolderWithoutItsMarkerIsRefused(t *testing.T) {
	t.Skip("open, a stated limit (design-archive-delete §15.6, DM-1 (b); §15.7, DM-2 item 4): a half-deleted folder whose marker is gone (removed by hand, a sync conflict, an antivirus, a share hiding dot names, a power cut on FAT or exFAT) is refused only by the computer whose deletion log began the delete; another computer's or account's backup is let in (MED-2 of the G2 settle pass, MA-1044)")
	half, logPath, mbox := ma1045Half(t)
	ma1045Started(t, logPath, half, nil)
	otherComputersLog := filepath.Join(t.TempDir(), LogName) // its own settings folder: no such delete
	_, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: half, Mode: export.Incremental, Unattended: true, DeletionsLog: otherComputersLog}, log.New(io.Discard, "", 0), nil)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names("Delete not finished: click Delete again."),
		assure.NoSideEffect(func() bool { return len(ma1044Names(half)) == 1 }))
}

// covers: MA-1044
// OPEN, a stated limit (design-archive-delete §15.6, DM-1 (b); §15.7, DM-2 item 4): the remounted
// drive's case for a half-deleted folder WITHOUT its marker — no marker names the delete, so even
// §15.5's run-id route (slice 5) cannot find this computer's line after the device number changed.
// The original MED-1 body, kept so no property leaves the suite.
func TestABackupIntoAHalfDeletedFolderWithoutItsMarkerOnARemountedDriveIsRefused(t *testing.T) {
	t.Skip("open, a stated limit (design-archive-delete §15.6, DM-1 (b); §15.7, DM-2 item 4): a half-deleted folder whose marker is gone, on a drive plugged in again under another device number (macOS, Linux), is not refused: no marker names the delete for §15.5's run-id route (MED-1 of the G2 settle pass, MA-1044)")
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

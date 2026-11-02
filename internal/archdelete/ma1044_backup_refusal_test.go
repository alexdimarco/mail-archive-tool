package archdelete

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// ma1044Line appends one deletions.log line to the log at logPath.
func ma1044Line(t *testing.T, logPath string, line map[string]any) {
	t.Helper()
	b, err := json.Marshal(line)
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
}

// ma1044Names is dir's names, or nil when it is not there.
func ma1044Names(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var n []string
	for _, e := range entries {
		n = append(n, e.Name())
	}
	return n
}

// covers: MA-1044, MA-1045, R36
// BackupRefusal is the engine's unfinished-delete check (installed in app when this package loads):
// app.RefuseUnfinishedDelete, which app.Run and app.RunGraph ask at a run's start and under its
// lock, refuses a folder whose delete did not finish in MA-1015's words for a backup, naming the
// folder, kind delete_unfinished (MA-1045; taken back as the emptied-folder guard's archive_absent
// is) and never a source-loss verdict; a log that cannot be read refuses in its words (fail
// closed), kind deletion_log_unreadable. A real app.Run into the half-deleted folder is refused with nothing
// written. A delete that finished leaves no refusal: a run into a fresh folder at that path makes
// its archive. Positive twin first: another folder, with that unfinished delete in the same log.
func TestBackupRefusalIsTheEnginesCheck(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), LogName)
	half := filepath.Join(t.TempDir(), "Half deleted")
	other := filepath.Join(t.TempDir(), "Other")
	for _, d := range []string{half, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".mailarchive-manifest.json"), []byte(`{"version":1,"records":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	addStarted(t, logPath, half)
	in := filepath.Join(t.TempDir(), "mail.mbox")
	if err := os.WriteFile(in, []byte("From a@b Mon Jan  1 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(out, deletionsLog string) error {
		_, err := app.Run(context.Background(), app.Options{Inputs: []string{in}, Out: out, Mode: export.Incremental, DeletionsLog: deletionsLog}, log.New(io.Discard, "", 0), nil)
		return err
	}

	// Positive twin.
	if err := app.RefuseUnfinishedDelete(other, logPath); err != nil {
		t.Fatalf("positive twin: another folder: %v", err)
	}
	if err := run(other, logPath); err != nil {
		t.Fatalf("positive twin: a backup into another folder: %v", err)
	}

	err := app.RefuseUnfinishedDelete(half, logPath)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+half+")"),
		assure.NoSideEffect(func() bool { return len(ma1044Names(half)) == 1 }))
	if r := AsRefusal(err); r == nil || r.Case != CaseUnfinished {
		t.Fatalf("the refusal's case: %+v", r)
	}
	if app.FailureKind(err) != state.FailDeleteUnfinished || !app.RefusedAsNoArchive(err) || app.FoundSourceUnreachable(err) {
		t.Fatalf("kind %q, taken back %v, source unreachable %v: want %s, taken back as no archive, and no source-loss verdict", app.FailureKind(err), app.RefusedAsNoArchive(err), app.FoundSourceUnreachable(err), state.FailDeleteUnfinished)
	}

	// The real engine, with this package's check installed.
	err = run(half, logPath)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("so it was not backed up; nothing was changed. (Folder: "+half+")"),
		assure.NoSideEffect(func() bool { n := ma1044Names(half); return len(n) == 1 && n[0] == ".mailarchive-manifest.json" }))

	// A log that cannot be read: refused, naming it.
	bad := filepath.Join(t.TempDir(), LogName)
	if err := os.Mkdir(bad, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	err = app.RefuseUnfinishedDelete(other, bad)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, "+bad, "nothing was changed. Check that file, then try again. (Folder: "+other+")"),
		assure.NoSideEffect(func() bool { return len(ma1044Names(other)) > 1 }))
	if r := AsRefusal(err); r == nil || r.Case != CaseLogUnreadable {
		t.Fatalf("the refusal's case: %+v", r)
	}
	if k := app.FailureKind(err); k != state.FailDeletionLogUnreadable || !app.RefusedAsNoArchive(err) {
		t.Fatalf("a log that cannot be read: kind %q, taken back %v: want %s, taken back as no archive", k, app.RefusedAsNoArchive(err), state.FailDeletionLogUnreadable)
	}

	// A delete that finished: a run into a fresh folder made at its path.
	done := filepath.Join(t.TempDir(), LogName)
	fresh := filepath.Join(t.TempDir(), "Deleted then made again")
	ma1044Line(t, done, map[string]any{"at": "2026-11-02T09:00:00Z", "event": "started", "run": "fin1044", "folder": fresh, "row": fresh, "kind": "plain"})
	ma1044Line(t, done, map[string]any{"at": "2026-11-02T09:00:01Z", "event": "folder removed", "run": "fin1044", "folder": fresh})
	ma1044Line(t, done, map[string]any{"at": "2026-11-02T09:00:02Z", "event": "finished", "run": "fin1044", "folder": fresh, "outcome": "complete"})
	if err := run(fresh, done); err != nil {
		t.Fatalf("a finished delete must not stop a backup into a fresh folder at its path: %v", err)
	}
	_, merr := os.Stat(filepath.Join(fresh, ".mailarchive-manifest.json"))
	assure.Reached(t, merr == nil, "the fresh folder's archive")
}

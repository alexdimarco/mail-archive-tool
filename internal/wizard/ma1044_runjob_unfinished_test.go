package wizard

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
)

// ma1044DefaultLog is MailArchive's own deletions.log in the package's throwaway home, put back as
// it was when the test ends.
func ma1044DefaultLog(t *testing.T) string {
	t.Helper()
	logPath, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	before, rerr := os.ReadFile(logPath)
	t.Cleanup(func() {
		os.RemoveAll(logPath)
		if rerr == nil {
			os.WriteFile(logPath, before, 0o600)
		}
	})
	return logPath
}

// ma1044Started appends a delete's started line for folder, and no finished line.
func ma1044Started(t *testing.T, logPath, folder string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "w1044-" + filepath.Base(folder), "folder": folder, "row": folder, "kind": "plain"})
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
	if u, err := archdelete.UnfinishedDelete(logPath, folder, ""); err != nil || u == nil {
		t.Fatalf("precondition: the log says the delete of %s did not finish: %v %v", folder, u, err)
	}
}

// ma1044Files is every file under dir with its bytes, the lock file aside (a released lock's
// known leftover).
func ma1044Files(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() || rel == lockfile.Name {
			return nil
		}
		b, rerr := os.ReadFile(p)
		out[rel] = string(b)
		return rerr
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func ma1044SameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// covers: MA-1044, R36
// A scheduled run-job (RunHeadless; the same path `mailarchive run-job FILE` takes by hand) into a
// folder whose delete did not finish is refused at its start, before its run log or any record is
// written into the folder (the 1.5.3 re-walk's guards pass, G2): exit 1, MA-1015's words for a
// backup with the folder, told beside the job file (its failure log), in the start-failure record
// and the alert streak (a failure notice), as the emptied-folder guard's refusal is; the folder is
// byte-identical. A delete that begins after the run's start and before the engine's lock is
// refused by the engine, and the run takes back the run log it made. A deletions.log that cannot
// be read refuses (fail closed). Positive twin first: another folder, with that unfinished delete
// in the same log, is backed up.
func TestMA1044_RunJobRefusesAFolderWhoseDeleteDidNotFinish(t *testing.T) {
	ma956JobEnv(t)
	var notices []string
	oldSend := sendNotice
	sendNotice = func(_, text string) { notices = append(notices, text) }
	t.Cleanup(func() { sendNotice = oldSend })
	logPath := ma1044DefaultLog(t)
	dir := t.TempDir()
	mbox := filepath.Join(dir, "Inbox")
	if err := os.WriteFile(mbox, []byte("From - Mon Jan 01 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	archive := func(name string) (string, string) {
		out := filepath.Join(dir, name)
		if _, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "mailarchive-"+name+".json")
		if err := job.Write(p, job.Job{Name: "mailarchive-" + name, Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
			t.Fatal(err)
		}
		return out, p
	}
	failLog := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, "mailarchive-"+name+".jobfail.log"))
		return string(b)
	}
	words := "Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed."

	half, halfJob := archive("half")
	other, otherJob := archive("other")
	// An earlier scheduled run left its run log in the folder: the refusal must not write to it.
	if code := RunHeadless(halfJob); code != 0 {
		t.Fatalf("the earlier scheduled run: exit %d %s", code, failLog("half"))
	}
	assure.Reached(t, fileThere(filepath.Join(half, "mailarchive.log")), "the earlier run's log")
	ma1044Started(t, logPath, half)

	// Positive twin.
	if code := RunHeadless(otherJob); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, failLog("other"))
	}
	assure.Reached(t, fileThere(filepath.Join(other, "mailarchive.log")), "the twin's run log")

	// At its start.
	before := ma1044Files(t, half)
	code := RunHeadless(halfJob)
	assure.Refused(t, code, failLog("half"), assure.Code(1),
		assure.Names(words+" (Folder: "+half+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }))
	crumb, _ := os.ReadFile(filepath.Join(must(failureDir()), lastFailureName))
	assure.Reached(t, strings.Contains(string(crumb), "so it was not backed up"), "the start-failure record names the refusal")
	assure.Reached(t, len(notices) == 1 && strings.Contains(notices[0], half), "the alert streak's failure notice for the folder")

	// A delete begins after the run's start (its check passed, its run log opened) and before the
	// engine's lock: the engine refuses it, and the run takes back the run log it made.
	raced, racedJob := archive("raced")
	before = ma1044Files(t, raced)
	old := headlessEngine
	t.Cleanup(func() { headlessEngine = old })
	reached := false
	headlessEngine = func(ctx context.Context, o app.Options, l *log.Logger, p app.ProgressFunc) (app.Result, error) {
		reached = true
		ma1044Started(t, logPath, raced)
		return old(ctx, o, l, p)
	}
	code = RunHeadless(racedJob)
	assure.Reached(t, reached, "the engine reached after the run's own start")
	assure.Refused(t, code, failLog("raced"), assure.Code(1),
		assure.Names(words+" (Folder: "+raced+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, raced)) }))
	headlessEngine = old

	// A deletions.log that cannot be read.
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	before = ma1044Files(t, other)
	code = RunHeadless(otherJob)
	assure.Refused(t, code, failLog("other"), assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, "+logPath, "Check that file, then try again. (Folder: "+other+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, other)) }))
}

// covers: MA-1044, R36
// The Desktop's Run now (RunJobInProcess) hands the engine the Desktop's own deletion log
// (RunNow.DeletionsLog, which the Desktop sets to its settings folder's), so the engine's asks at
// the run's start and under its lock read the log the Desktop's own Run now refusal (MA-987) reads,
// not MailArchive's default one. Positive twin first: with nothing in that log, Run now archives.
func TestMA1044_RunNowAsksTheDesktopsDeletionLog(t *testing.T) {
	ma956JobEnv(t)
	dir := t.TempDir()
	mbox := filepath.Join(dir, "Inbox")
	if err := os.WriteFile(mbox, []byte("From - Mon Jan 01 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "archive")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "mailarchive-archive.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-archive", Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
		t.Fatal(err)
	}
	desktopLog := filepath.Join(t.TempDir(), archdelete.LogName)
	quiet := log.New(io.Discard, "", 0)

	// Positive twin.
	if _, err := RunJobInProcess(context.Background(), context.Background(), jobPath, quiet, RunNow{DeletionsLog: desktopLog}); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	ma1044Started(t, desktopLog, out) // only the Desktop's log says so; MailArchive's default one is empty
	before := ma1044Files(t, out)
	_, err := RunJobInProcess(context.Background(), context.Background(), jobPath, quiet, RunNow{DeletionsLog: desktopLog})
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+out+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, out)) }))
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}

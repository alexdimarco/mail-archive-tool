package wizard

import (
	"context"
	"io"
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

// covers: MA-1045, R36
// The wizard's own export (the double-click wizard's first run, and the Desktop's Add another
// archive, which calls it in process) asks whether a delete of the chosen folder did not finish
// before it writes anything there (the G2 settle pass's MED-4): before, it opened its run log
// inside the folder and, on Windows, had Outlook write a plaintext PST of each account into
// <out>/_outlook-pst, all before the engine's first ask, and the engine's refusal then said
// "A log was written to: DIR/mailarchive.log". Now runExport refuses first, in MA-1044's words
// with the folder: no run log, no PST folder, the folder unchanged; and in its code the ask comes
// before both writes (the Outlook path runs only on Windows with Outlook, so its order is checked
// in the source, as addstopped_test.go checks runExport's). Positive twin first: the same ask lets
// another folder through.
func TestTheWizardsExportRefusesAFolderWhoseDeleteDidNotFinishBeforeItWrites(t *testing.T) {
	logPath := ma1044DefaultLog(t)
	half := filepath.Join(t.TempDir(), "Half deleted")
	other := filepath.Join(t.TempDir(), "Other")
	for _, d := range []string{half, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".mailarchive-manifest.json"), []byte(`{"version":1}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ma1044Started(t, logPath, half)

	// Positive twin.
	if err := app.RefuseUnfinishedDelete(other, ""); err != nil {
		t.Fatalf("positive twin: another folder: %v", err)
	}

	// The order in the code, for the Outlook path; checked first, so runExport is only called
	// below when it asks before its writes (it would otherwise open a dialog).
	src, rerr := os.ReadFile("wizard.go")
	if rerr != nil {
		t.Fatal(rerr)
	}
	code := assure.Reached(t, string(src), "wizard.go")
	body := code[strings.Index(code, "func runExport(c wizardChoice) error {"):]
	body = body[:strings.Index(body, "\n}\n")]
	ask := strings.Index(body, "app.RefuseUnfinishedDelete(c.out")
	logAt := strings.Index(body, "newLogger(c.out)")
	pstAt := strings.Index(body, "outlookcom.CreatePSTs(")
	if logAt < 0 || pstAt < 0 {
		t.Fatal("runExport no longer opens its log or exports PSTs where this test looks: update the test")
	}
	if ask < 0 || ask > logAt || ask > pstAt {
		t.Fatalf("runExport writes its run log (offset %d) or the Outlook PSTs (offset %d) before it asks for an unfinished delete (offset %d)", logAt, pstAt, ask)
	}

	before := ma1044Files(t, half)
	err := runExport(wizardChoice{out: half, outlookCOM: true, mode: export.Incremental})
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+half+")"),
		assure.Forbid("A log was written to"),
		assure.NoSideEffect(func() bool {
			_, lerr := os.Lstat(filepath.Join(half, "mailarchive.log"))
			_, perr := os.Lstat(filepath.Join(half, "_outlook-pst"))
			return os.IsNotExist(lerr) && os.IsNotExist(perr) && ma1044SameFiles(before, ma1044Files(t, half))
		}))
	if r := archdelete.AsRefusal(err); r == nil || r.Case != archdelete.CaseUnfinished {
		t.Fatalf("the refusal is archdelete's: %+v", r)
	}
}

// covers: MA-1045, R36
// A scheduled run-job that starts while a delete of its folder is still RUNNING (the delete holds
// the archive's lock) skips quietly, as a run that meets the lock did before MA-1044 (the G2 settle
// pass's LOW-1): exit 0, no failure log, no notice, nothing written into the folder — not "Delete
// not finished: click Delete again" in the middle of the delete. Positive twin first: with the
// lock released (the delete stopped part way) the same run is refused in MA-1044's words.
func TestARunJobWhileADeleteRunsSkipsQuietly(t *testing.T) {
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
	out := filepath.Join(dir, "deleting")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{mbox}, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "mailarchive-deleting.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-deleting", Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
		t.Fatal(err)
	}
	failLog := func() string {
		b, _ := os.ReadFile(filepath.Join(dir, "mailarchive-deleting.jobfail.log"))
		return string(b)
	}
	ma1044Started(t, logPath, out)

	// Positive twin: the delete stopped part way.
	before := ma1044Files(t, out)
	code := RunHeadless(jobPath)
	assure.Refused(t, code, failLog(), assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed. (Folder: "+out+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, out)) }))
	assure.Reached(t, len(notices) == 1, "the stopped delete's refusal is told by a notice")
	if err := os.Remove(filepath.Join(dir, "mailarchive-deleting.jobfail.log")); err != nil {
		t.Fatal(err)
	}
	notices = nil

	// The delete is running.
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "delete")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	code = RunHeadless(jobPath)
	if code != 0 || failLog() != "" || len(notices) != 0 || !ma1044SameFiles(before, ma1044Files(t, out)) {
		t.Fatalf("a run-job while a delete runs must skip quietly: exit %d, failure log %q, notices %q, folder unchanged %v",
			code, failLog(), notices, ma1044SameFiles(before, ma1044Files(t, out)))
	}
}

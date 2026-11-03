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
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
)

// ma1048Plant writes a regular delete marker into dir by hand, as a delete stopped part way leaves
// it (design-archive-delete §15.1; nothing writes it until slice 3).
func ma1048Plant(t *testing.T, dir string) {
	t.Helper()
	body := `{"what":"` + delmark.What + `","version":1,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z","note":"` + delmark.Note + `"}`
	if err := os.WriteFile(filepath.Join(dir, delmark.Name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1048, R36
// A scheduled run-job (RunHeadless) into a folder holding the delete marker, which this computer's
// deletion log knows nothing of (another computer or account began the delete; §15.3, MED-2), is
// refused at its start, before its run log or any record is written into the folder: exit 1, the
// marker-only words with the folder (DM-11), told beside the job file, in the start-failure record
// and by a failure notice; the folder unchanged. A delete still running there (it holds the lock,
// verb=delete) skips quietly. Positive twin first: another folder with the same empty log.
func TestMA1048_RunJobRefusesAFolderHoldingTheMarker(t *testing.T) {
	ma956JobEnv(t)
	var notices []string
	oldSend := sendNotice
	sendNotice = func(_, text string) { notices = append(notices, text) }
	t.Cleanup(func() { sendNotice = oldSend })
	ma1044DefaultLog(t) // MailArchive's own deletions.log: empty
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

	half, halfJob := archive("half")
	other, otherJob := archive("other")
	if code := RunHeadless(halfJob); code != 0 {
		t.Fatalf("the earlier scheduled run: exit %d %s", code, failLog("half"))
	}
	ma1048Plant(t, half)

	// Positive twin.
	if code := RunHeadless(otherJob); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, failLog("other"))
	}
	assure.Reached(t, fileThere(filepath.Join(other, "mailarchive.log")), "the twin's run log")

	before := ma1044Files(t, half)
	code := RunHeadless(halfJob)
	assure.Refused(t, code, failLog("half"), assure.Code(1),
		assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"), assure.Forbid("click Delete again"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }))
	crumb, _ := os.ReadFile(filepath.Join(must(failureDir()), lastFailureName))
	assure.Reached(t, strings.Contains(string(crumb), "Delete not finished: a delete of this archive was begun on another computer"), "the start-failure record names the refusal")
	assure.Reached(t, len(notices) == 1 && strings.Contains(notices[0], half), "the alert streak's failure notice for the folder")

	// The delete is still running: a quiet skip.
	os.Remove(filepath.Join(dir, "mailarchive-half.jobfail.log"))
	notices = nil
	held, err := lockfile.AcquireAs(filepath.Join(half, lockfile.Name), "delete")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	code = RunHeadless(halfJob)
	if code != 0 || failLog("half") != "" || len(notices) != 0 || !ma1044SameFiles(before, ma1044Files(t, half)) {
		t.Fatalf("a run-job while a delete runs must skip quietly: exit %d, failure log %q, notices %q", code, failLog("half"), notices)
	}
}

// covers: MA-1048, R36
// The wizard's own export (runExport: the double-click wizard's first run and the Desktop's Add
// another archive) refuses a folder holding the delete marker before it writes its run log or any
// PST, in the marker-only words with the folder; and the wizard's Pause and Resume refuse it in
// theirs (§15.4, through ActionRefusal). Positive twin first: the same asks let another folder
// through.
func TestMA1048_TheWizardsExportPauseAndResumeRefuseAFolderHoldingTheMarker(t *testing.T) {
	ma1044DefaultLog(t)
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
	ma1048Plant(t, half)

	// Positive twins.
	if err := app.RefuseUnfinishedDelete(other, ""); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if err := archdelete.ActionRefusal("", other, "it was not paused"); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	before := ma1044Files(t, half)
	err := runExport(wizardChoice{out: half, outlookCOM: true, mode: export.Incremental})
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1),
		assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"), assure.Forbid("A log was written to"),
		assure.NoSideEffect(func() bool {
			_, lerr := os.Lstat(filepath.Join(half, "mailarchive.log"))
			_, perr := os.Lstat(filepath.Join(half, "_outlook-pst"))
			return os.IsNotExist(lerr) && os.IsNotExist(perr) && ma1044SameFiles(before, ma1044Files(t, half))
		}))

	err = PauseArchive(half, "", "")
	rc = 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1), assure.Names(delmark.OnlyMarkerWords("it was not paused")),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }))
	err = ResumeArchive(half, "")
	rc = 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1), assure.Names(delmark.OnlyMarkerWords("it was not resumed")),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }))
}

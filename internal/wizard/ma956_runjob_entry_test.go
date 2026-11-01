package wizard

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/outlookcom"
)

// The run-job fixes from slice 2's adversarial pass (design-archive-delete §7; findings
// del-s2-A1-01, A2-01, A1-02, A2-02, A2-03, A1-05).

// ma956JobEnv stubs the notice and puts the job failure logs in a temp folder.
func ma956JobEnv(t *testing.T) {
	t.Helper()
	old := notify
	notify = func(string, ...zenity.Option) error { return nil }
	oldDir := failureDir
	fdir := t.TempDir()
	failureDir = func() (string, error) { return fdir, nil }
	t.Cleanup(func() { notify = old; failureDir = oldDir })
}

// ma956FakeOutlook stands in for the Outlook app's export (outlookcom.CreatePSTs): it makes
// the PST folder as the real one does — only the folder itself for a scheduled run
// (ParentMustExist) — and writes one real PST there. It records the options it was given.
func ma956FakeOutlook(t *testing.T, got *[]outlookcom.Options) {
	t.Helper()
	pst, err := os.ReadFile(filepath.Join("..", "..", "testdata", "support.pst"))
	if err != nil {
		t.Fatal(err)
	}
	old := headlessCreatePSTs
	t.Cleanup(func() { headlessCreatePSTs = old })
	headlessCreatePSTs = func(dir string, o outlookcom.Options, _ *log.Logger) ([]outlookcom.Store, []outlookcom.Skip, error) {
		*got = append(*got, o)
		mk := os.MkdirAll
		if o.ParentMustExist {
			mk = func(p string, m os.FileMode) error { return os.Mkdir(p, m) }
		}
		if err := mk(dir, 0o755); err != nil && !os.IsExist(err) {
			return nil, nil, err
		}
		p := filepath.Join(dir, "Mailbox.pst")
		if err := os.WriteFile(p, pst, 0o600); err != nil {
			return nil, nil, err
		}
		return []outlookcom.Store{{Name: "Mailbox", Path: p}}, nil, nil
	}
}

func ma956Names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range entries {
		n = append(n, e.Name())
	}
	slices.Sort(n)
	return n
}

// covers: MA-956, R36
// A scheduled Outlook run-job that found its archive at its entry check, exported the
// mailbox to PSTs in the folder (minutes, with no lock), and reaches the engine after a
// delete that could not remove what it holds open (Windows: its run log, the PSTs) is
// refused under its lock — not rebuilt among the person's files — and takes back what it
// wrote there: the PSTs (a plaintext copy of the mailbox), the PST folder and the run log it
// made; the refusal goes to the job's failure log beside the job file, not into the folder.
// The PST export never makes the archive folder itself (ParentMustExist). Positive twin
// first: the same job into an empty folder archives the PST and reclaims it.
func TestMA956_RunJobOutlookRefusedAfterADeleteTakesBackItsWrites(t *testing.T) {
	ma956JobEnv(t)
	var pstOpts []outlookcom.Options
	ma956FakeOutlook(t, &pstOpts)
	dir := t.TempDir()
	jobFor := func(name, out string) string {
		p := filepath.Join(dir, name+".json")
		if err := job.Write(p, job.Job{Name: name, Out: out, Mode: "incremental", Outlook: true, OutlookSyncWait: "0s"}); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Positive twin.
	first := filepath.Join(dir, "first")
	if err := os.Mkdir(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(jobFor("mailarchive-first", first)); code != 0 {
		b, _ := os.ReadFile(filepath.Join(dir, "mailarchive-first.jobfail.log"))
		t.Fatalf("a first scheduled Outlook run into an empty folder: exit %d %s", code, b)
	}
	assure.Reached(t, fileThere(filepath.Join(first, ".mailarchive-manifest.json")), "the first run's archive")
	if fileThere(filepath.Join(first, "_outlook-pst")) {
		t.Error("a successful run reclaims its PSTs")
	}
	assure.Reached(t, len(pstOpts) == 1 && pstOpts[0].ParentMustExist, "the scheduled export makes only its own folder")

	src := filepath.Join(t.TempDir(), "Inbox")
	if err := os.WriteFile(src, []byte("From - Mon Jan 01 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mine := []byte("my own notes\n")
	old := headlessEngine
	t.Cleanup(func() { headlessEngine = old })

	// refusedAfterDelete makes an archive at a terminal (no run log of the job's yet), with the
	// person's file beside it when persons; runs the scheduled Outlook job, whose engine is
	// reached after a delete that could not remove the run's open log or the PSTs Outlook is
	// writing (Windows); and checks the refusal and that the folder holds only what it held
	// besides the archive (and the run's own lock file, the known leftover: lockfile Release).
	refusedAfterDelete := func(name string, persons bool, why ...string) {
		kept := filepath.Join(dir, name)
		if err := os.Mkdir(kept, 0o700); err != nil {
			t.Fatal(err)
		}
		want := []string{}
		if persons {
			if err := os.WriteFile(filepath.Join(kept, "notes.txt"), mine, 0o600); err != nil {
				t.Fatal(err)
			}
			want = []string{"notes.txt"}
		}
		if _, err := app.Run(context.Background(), app.Options{Inputs: []string{src}, Out: kept, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
			t.Fatal(err)
		}
		reached := false
		headlessEngine = func(ctx context.Context, o app.Options, l *log.Logger, p app.ProgressFunc) (app.Result, error) {
			reached = true
			for _, n := range ma956Names(t, kept) {
				if n != "notes.txt" && n != "mailarchive.log" && n != "_outlook-pst" {
					if err := os.RemoveAll(filepath.Join(kept, n)); err != nil {
						t.Fatal(err)
					}
				}
			}
			return old(ctx, o, l, p)
		}
		code := RunHeadless(jobFor("mailarchive-"+name, kept))
		assure.Reached(t, reached, "the engine reached after the PST export")
		failLog, _ := os.ReadFile(filepath.Join(dir, "mailarchive-"+name+".jobfail.log"))
		rc := 0
		if code != 0 {
			rc = 1
		}
		assure.Refused(t, rc, string(failLog), assure.Code(1),
			assure.Names(append([]string{kept, "remove its scheduled backup"}, why...)...),
			assure.Forbid("notes.txt"),
			assure.NoSideEffect(func() bool {
				left := slices.DeleteFunc(ma956Names(t, kept), func(n string) bool { return n == ".mailarchive.lock" })
				got, _ := os.ReadFile(filepath.Join(kept, "notes.txt"))
				return slices.Equal(left, want) && (!persons || bytes.Equal(got, mine))
			}))
	}
	// The delete kept the folder for the person's file: refused as a folder of the person's files.
	refusedAfterDelete("persons", true, "holds no MailArchive archive")
	// Only the run's own files are left (they were held open): refused because the run found an
	// archive at its entry and finds none under its lock (del-s2-A1-01, A2-01).
	refusedAfterDelete("ownonly", false, "held a MailArchive archive when this backup started", "holds none now")
}

// covers: MA-956, R36
// Run now (wizard.RunJobInProcess) runs as the scheduled run-job does (optsForJob): the
// run-job's own files in the folder — its mailarchive.log and the scheduler's stderr file —
// are MailArchive's, so a folder holding only those (no descriptor) is archived into by Run
// now as by the schedule (del-s2-A1-05); a folder of the person's files is refused by both.
func TestMA956_RunNowCountsTheRunJobsOwnFiles(t *testing.T) {
	ma956JobEnv(t)
	dir := t.TempDir()
	mbox := filepath.Join(dir, "Inbox")
	if err := os.WriteFile(mbox, []byte("From - Mon Jan 01 00:00:00 2024\r\nFrom: a@b\r\nSubject: x\r\nMessage-ID: <x@y>\r\n\r\nbody\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	jobFor := func(name, out string) string {
		p := filepath.Join(dir, name+".json")
		if err := job.Write(p, job.Job{Name: name, Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	logs := t.TempDir()
	for _, n := range []string{"mailarchive.log", "mailarchive.stderr.log"} {
		if err := os.WriteFile(filepath.Join(logs, n), []byte("an earlier refused run\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	quiet := log.New(io.Discard, "", 0)
	if _, err := RunJobInProcess(context.Background(), context.Background(), jobFor("mailarchive-logs", logs), quiet, RunNow{}); err != nil {
		t.Fatalf("Run now into a folder holding only the run-job's own logs must work: %v", err)
	}
	assure.Reached(t, fileThere(filepath.Join(logs, ".mailarchive-manifest.json")), "Run now's archive")

	kept := t.TempDir()
	if err := os.WriteFile(filepath.Join(kept, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunJobInProcess(context.Background(), context.Background(), jobFor("mailarchive-kept", kept), quiet, RunNow{})
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errString(err), assure.Code(1), assure.Names(kept, "holds no MailArchive archive", "Make a new archive…"),
		assure.NoSideEffect(func() bool { return slices.Equal(ma956Names(t, kept), []string{"notes.txt"}) }))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

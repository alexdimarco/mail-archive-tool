package wizard

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
)

// covers: MA-1471, R18
// The scheduled run (RunHeadless) and Run now (RunJobInProcess) of an "All of them" job skip a
// gone account folder through the same rule: the scheduled run exits 0 and records the
// mailbox_gone warning, and Run now's note names the skipped mailbox. Notifications are stood in.
// Positive twin first: with both mailboxes there each run ends ok and skips nothing.
func TestRunNowAndTheScheduledRunSkipAGoneMailbox(t *testing.T) {
	old := notify
	notify = func(string, ...zenity.Option) error { return nil }
	t.Cleanup(func() { notify = old })
	fdir := t.TempDir()
	oldDir := failureDir
	failureDir = func() (string, error) { return fdir, nil }
	t.Cleanup(func() { failureDir = oldDir })

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	imap := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail")
	a, b := filepath.Join(imap, "imap.qza.example"), filepath.Join(imap, "imap.qzb.example")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		box := "From - Mon Mar  3 09:00:00 2025\nFrom: x@example.com\nSubject: s\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + filepath.Base(d) + "@ex>\n\nbody\n\n"
		if err := os.WriteFile(filepath.Join(d, "INBOX"), []byte(box), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "INBOX.msf"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "arch")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{a, b}, Auto: true, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(t.TempDir(), "mailarchive-all.json")
	if err := job.Write(jobPath, job.Job{Name: "mailarchive-all", Out: out, Mode: "incremental", Inputs: []string{a, b}, Auto: true}); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("positive twin: the scheduled run with both there exit %d", code)
	}
	if note, err := RunJobInProcess(context.Background(), context.Background(), jobPath, log.New(io.Discard, "", 0), RunNow{}); err != nil || strings.Contains(note, "Not found") {
		t.Fatalf("positive twin: Run now with both there: %v\n%s", err, note)
	}

	os.RemoveAll(b)
	if code := RunHeadless(jobPath); code != 0 {
		t.Fatalf("the scheduled run with one account folder gone must skip it, exit %d", code)
	}
	lr, _, _ := state.ReadLastRun(out)
	if lr.Status != state.RunOK || len(lr.Warnings) != 1 || lr.Warnings[0].Code != state.MailboxGoneCode || lr.Warnings[0].Path != b {
		t.Fatalf("the scheduled run's record: %+v", lr)
	}
	note, err := RunJobInProcess(context.Background(), context.Background(), jobPath, log.New(io.Discard, "", 0), RunNow{})
	if err != nil || !strings.Contains(note, "Not found, so skipped: 1 mailbox.") || !strings.Contains(note, "\n"+b+"\n") {
		t.Fatalf("Run now must skip and name the gone mailbox: %v\n%s", err, note)
	}
}

// covers: MA-1471, MA-1478, X6
// The end-of-run summaries say the mailboxes a run that finished skipped, from Result.Skipped
// (docs/design-1.7.5-fixes.md §1.3): the wizard's summary, the plaintext Run now note and the
// encrypted one (the wizard's encrypted summary and the Desktop's encrypted Run now note, in memory
// only, MA-1478) each say how many, that the other mailboxes were backed up, each "NAME (PATH)",
// and what to do if one was not removed on purpose, in proper singular and plural (MA-1161's rule).
// Positive twin first: a run that skipped nothing says none of it.
func TestEverySummarySaysTheSkippedMailboxes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	imap := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail")
	b, c := filepath.Join(imap, "imap.qzb.example"), filepath.Join(imap, "imap.qzc.example")
	summaries := func(r app.Result) map[string]string {
		return map[string]string{
			"the wizard's summary":       exportSummaryFor(t.TempDir(), false, r, "darwin"),
			"the Run now note":           runNowSummaryFor(r, false),
			"the encrypted Run now note": encryptedSummaryFor(r, false),
		}
	}
	for name, s := range summaries(app.Result{}) {
		if strings.Contains(s, "Not found, so skipped") {
			t.Fatalf("positive twin: %s says a skip when there was none:\n%s", name, s)
		}
	}
	one := app.Result{Skipped: []app.SkippedInput{{Path: b, Name: "qzsam@example.com"}}}
	for name, s := range summaries(one) {
		for _, want := range []string{
			"Not found, so skipped: 1 mailbox. It is no longer on this computer, and the mail already saved from it stays in the archive; the other mailboxes were backed up:\nqzsam@example.com (" + b + ")\nIf you didn't remove it on purpose, add its account back in Thunderbird.",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("%s lacks the skip:\n%s\nwant\n%s", name, s, want)
			}
		}
	}
	two := app.Result{Skipped: []app.SkippedInput{{Path: b, Name: "qzsam@example.com"}, {Path: c}}}
	for name, s := range summaries(two) {
		want := "Not found, so skipped: 2 mailboxes. They are no longer on this computer, and the mail already saved from them stays in the archive; the other mailboxes were backed up:\nqzsam@example.com (" + b + ")\n" + c + "\nIf you didn't remove one on purpose, add its account back in Thunderbird."
		if !strings.Contains(s, want) {
			t.Errorf("%s lacks the two skips:\n%s\nwant\n%s", name, s, want)
		}
	}
}

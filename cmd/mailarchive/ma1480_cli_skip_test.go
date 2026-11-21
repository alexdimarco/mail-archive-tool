package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// cliStore makes dir a Thunderbird store holding one message.
func cliStore(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	box := "From - Mon Mar  3 09:00:00 2025\nFrom: x@example.com\nSubject: s\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + filepath.Base(dir) + "@ex>\n\nbody\n\n"
	if err := os.WriteFile(filepath.Join(dir, "INBOX"), []byte(box), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "INBOX.msf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// cliRun runs the command with args, its home where the Thunderbird profile is, and returns its
// exit code and everything it printed.
func cliRun(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(testBin, args...)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, string(out)
	case errors.As(err, &ee):
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("run %v: %v", args, err)
	return -1, ""
}

// covers: MA-1480, MA-1471, R12
// The command line with -auto and -input (docs/design-1.7.5-fixes.md §1.5): `export -auto -input X`
// with X gone where the run's rule skips it exits 0 where 1.7.4 refused, and the skip is reported
// once, by the run (its log line and its summary, from the same discovery), never by the up-front
// check; -list prints the stores found, then "Not found, so skipped: PATH"; a -follow-links opt-in
// is checked against the inputs found plus the skipped ones, so one naming a skipped input is not
// refused, while one naming nothing found or skipped still is. Positive twins first: with both
// mailboxes there the export and an opt-in for a found input pass.
func TestTheCommandLineSkipsAGoneMailboxOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	imap := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail")
	a, b := filepath.Join(imap, "imap.qza.example"), filepath.Join(imap, "imap.qzb.example")
	cliStore(t, a)
	cliStore(t, b)
	out := filepath.Join(t.TempDir(), "arch")
	if code, printed := cliRun(t, "-auto", "-input", a, "-input", b, "-out", out); code != 0 {
		t.Fatalf("positive twin: both there: exit %d\n%s", code, printed)
	}
	if code, printed := cliRun(t, "-auto", "-input", a, "-input", b, "-out", out, "-follow-links", a); code != 0 {
		t.Fatalf("positive twin: an opt-in for a found input: exit %d\n%s", code, printed)
	}

	os.RemoveAll(b)
	code, printed := cliRun(t, "-auto", "-input", a, "-input", b, "-out", out)
	if code != 0 {
		t.Fatalf("export -auto -input with one gone where the run skips it must exit 0: %d\n%s", code, printed)
	}
	if n := strings.Count(printed, "Not found, so skipped: 1 mailbox."); n != 1 {
		t.Fatalf("the summary must report the skip once, got %d:\n%s", n, printed)
	}
	if n := strings.Count(printed, "Not found, so skipped: "+b+". It is no longer on this computer"); n != 1 {
		t.Fatalf("the run's log line must say it once, got %d:\n%s", n, printed)
	}

	// Amended 2026-11-20 by the 1.7.5 friction review (F14): -list is a preview, so it says "Not
	// found, would be skipped: PATH" (it said "Not found, so skipped: PATH", though nothing ran).
	// Pinned at the same strength, word for word, with the old words forbidden.
	code, printed = cliRun(t, "-auto", "-input", a, "-input", b, "-out", out, "-list")
	if code != 0 || !strings.Contains(printed, a) || !strings.Contains(printed, "Not found, would be skipped: "+b+"\n") || strings.Contains(printed, "so skipped") {
		t.Fatalf("-list must print the found stores then the one it would skip: exit %d\n%s", code, printed)
	}

	if code, printed := cliRun(t, "-auto", "-input", a, "-input", b, "-out", out, "-follow-links", b); code != 0 || strings.Contains(printed, "is not one of the inputs being archived") {
		t.Fatalf("an opt-in naming a skipped input must not be refused: exit %d\n%s", code, printed)
	}
	nothing := filepath.Join(t.TempDir(), "nothing")
	before, _ := os.ReadFile(filepath.Join(out, ".mailarchive-lastrun.json"))
	code, printed = cliRun(t, "-auto", "-input", a, "-input", b, "-out", out, "-follow-links", nothing)
	assure.Refused(t, code, printed, assure.Code(1), assure.Names("-follow-links "+nothing, "is not one of the inputs being archived"),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(out, ".mailarchive-lastrun.json"))
			return bytes.Equal(before, after)
		}))
}

// covers: MA-1480, MA-1471, MA-1288
// The command line's summary of a run that skipped: on a terminal each skipped mailbox by its
// account's name and path; in a log file by its path alone, never the name (MA-1288). Positive
// twin first: the terminal names it.
func TestTheSummarySaysTheSkippedMailboxByNameOnlyOnScreen(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	p := filepath.Join(home, ".thunderbird", "abcd1234.default-release", "ImapMail", "imap.qzb.example")
	r := app.Result{Skipped: []app.SkippedInput{{Path: p, Name: "qzsam@qz.example"}}}
	var screen bytes.Buffer
	onScreen := summaryOnScreen
	summaryOnScreen = func(*log.Logger) bool { return true }
	printSummaryFor(log.New(&screen, "", 0), r, false, false, t.TempDir(), "Done.", "darwin")
	summaryOnScreen = onScreen
	for _, want := range []string{"Not found, so skipped: 1 mailbox. It is no longer on this computer, and the mail already saved from it stays in the archive; the other mailboxes were backed up:",
		"qzsam@qz.example (" + p + ")", "If you didn't remove it on purpose, add its account back in Thunderbird."} {
		if !strings.Contains(screen.String(), want) {
			t.Fatalf("positive twin: the terminal lacks %q:\n%s", want, screen.String())
		}
	}
	var file bytes.Buffer
	printSummaryFor(log.New(&file, "", 0), r, false, false, t.TempDir(), "Done.", "darwin")
	if strings.Contains(file.String(), "qzsam") || !strings.Contains(file.String(), "\n"+p+"\n") {
		t.Fatalf("a log says the path alone:\n%s", file.String())
	}
}

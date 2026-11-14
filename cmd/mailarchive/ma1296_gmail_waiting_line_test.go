package main

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1296, R45
// A command-line user learns that the Gmail question waits (the B3 friction review's F8, UX
// contract X6): `status` says it for each Gmail store the last backup found off, and the summary
// of a run with nobody to ask says it too. Positive twin first: an archive with nothing waiting
// has no such line. A Not now kept on this computer, or a yes kept for that store, silences
// status; an encrypted run's summary never says it (O1).
func TestStatusAndAnUnattendedRunSayTheGmailQuestionWaits(t *testing.T) {
	line := state.GmailWaitingLine("imap.gmail.com")
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "archive")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the run: exit %d, %s", code, stderr)
	}
	if got, _ := statusOf(t, dir); strings.Contains(got, "not switched on") {
		t.Fatalf("positive twin: status says the question waits:\n%s", got)
	}
	lr, _, _ := state.ReadLastRun(dir)
	lr.GmailOff = []string{"imap.gmail.com"}
	if err := state.WriteLastRun(dir, lr); err != nil {
		t.Fatal(err)
	}
	if got, code := statusOf(t, dir); code != 0 || !strings.Contains(got, line) {
		t.Fatalf("status (exit %d) does not say %q:\n%s", code, line, got)
	}
	if err := gmailanswer.KeepHere(dir, gmailanswer.Answer{Yes: []string{"imap.gmail.com"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := statusOf(t, dir); strings.Contains(got, line) {
		t.Fatalf("status says the question waits after a yes kept here:\n%s", got)
	}
	if err := gmailanswer.KeepHere(dir, gmailanswer.Answer{NotNow: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := statusOf(t, dir); strings.Contains(got, line) {
		t.Fatalf("status says the question waits after Not now:\n%s", got)
	}

	// The summary of a run with nobody to ask.
	r := app.Result{}
	r.Gmail = app.GmailOutcome{Off: []string{"imap.gmail.com"}}
	var buf bytes.Buffer
	printSummaryFor(log.New(&buf, "", 0), r, false, false, dir, "Done.", "darwin")
	if !strings.Contains(buf.String(), line) {
		t.Fatalf("an unattended run's summary does not say %q:\n%s", line, buf.String())
	}
	buf.Reset()
	r.Gmail.Encrypted = true
	printSummaryFor(log.New(&buf, "", 0), r, false, false, dir, "Done.", "darwin")
	if strings.Contains(buf.String(), "Gmail") {
		t.Fatalf("an encrypted run's summary names Gmail:\n%s", buf.String())
	}
}

package app

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-1236, R45, R37
// The Gmail switch on an encrypted archive writes nothing beside it that says the archive holds
// a Gmail account (O1, GD-3; docs/design-gmail-dedup.md §7.4 fold c and §8.1 fold). Positive
// twin first: a plaintext archive's first run of a Gmail store logs the switch. An encrypted
// archive's first run of the same store switches it on inside the sealed record, and yet its
// plain log carries no switch line, its last-run record no Gmail field, and the run carries no
// Gmail counts. A later unattended run of an existing encrypted archive whose store is off keeps
// no waiting question beside it either; its question (in the run's outcome, and as a terminal
// asks it) carries the encrypted archive's extra sentence.
func TestTheGmailSwitchOnAnEncryptedArchiveSaysNothingBesideIt(t *testing.T) {
	const switchWords = "each email is stored once"
	run := func(opts Options) (Result, string) {
		t.Helper()
		var buf bytes.Buffer
		opts.Mode = export.Incremental
		res, err := Run(context.Background(), opts, log.New(&buf, "", 0), nil)
		if err != nil {
			t.Fatalf("run of %v: %v", opts.Inputs, err)
		}
		return res, buf.String()
	}
	noGmailBeside := func(what, out string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(out, ".mailarchive-lastrun.json"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToLower(string(b)), "gmail") {
			t.Fatalf("%s: the last-run record beside the encrypted archive names Gmail: %s", what, b)
		}
	}

	_, store := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	if _, logged := run(Options{Inputs: []string{store}, Out: tmpDir(t)}); !strings.Contains(logged, switchWords) {
		t.Fatalf("positive twin: a plaintext archive's switch is not logged: %q", logged)
	}

	out, key := encryptedOut(t)
	res, logged := run(Options{Inputs: []string{store}, Out: out, Key: key})
	if len(res.Gmail.Switched) != 1 {
		t.Fatalf("a new encrypted Gmail archive is not switched on: %+v", res.Gmail)
	}
	if m := loadSealed(t, out, key); !m.DedupOn(res.Gmail.Switched[0]) || m.Version != 7 {
		t.Fatalf("the sealed record: switched %v, version %d; want on and 7", m.DedupOn(res.Gmail.Switched[0]), m.Version)
	}
	if strings.Contains(logged, switchWords) {
		t.Fatalf("the encrypted archive's plain log names the switch: %q", logged)
	}
	if res.Gmail.Counts.Line() != "" {
		t.Fatalf("an encrypted run carries the Gmail counts %q", res.Gmail.Counts.Line())
	}
	noGmailBeside("a new archive", out)

	// An existing encrypted archive whose Gmail store is off: an unattended run keeps no
	// waiting question beside it.
	out, key = encryptedOut(t)
	tbCheckOff = true
	run(Options{Inputs: []string{store}, Out: out, Key: key})
	tbCheckOff = false
	res, _ = run(Options{Inputs: []string{store}, Out: out, Key: key, Unattended: true})
	if len(res.Gmail.Off) != 1 {
		t.Fatalf("fixture: the existing encrypted archive's store is not off: %+v", res.Gmail)
	}
	noGmailBeside("an existing archive", out)
	// The question for an encrypted archive carries its extra sentence (§7.4 F1 note), in the
	// run's outcome and at a terminal.
	if want := state.GmailSwitchQuestion(true); res.Gmail.Question != want || want == state.GmailQuestion {
		t.Fatalf("the encrypted archive's question %q, want %q", res.Gmail.Question, want)
	}
	var asked []string
	run(Options{Inputs: []string{store}, Out: out, Key: key, AskGmail: func(q string) bool { asked = append(asked, q); return false }})
	if len(asked) != 1 || asked[0] != state.GmailSwitchQuestion(true) {
		t.Fatalf("the terminal asked %q, want the encrypted question once", asked)
	}
}

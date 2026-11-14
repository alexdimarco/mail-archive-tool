package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1303, R45
// At a terminal only a typed line is an answer (the B3 adversarial pass's A4, turned from its
// probes TestB3AdvEndOfInputIsARememberedNotNow and TestB3AdvInterruptDoesNotEndTheWait): the end
// of the input (Ctrl-D) is no answer, said as such, Not now for that backup only and never
// remembered; Ctrl-C (the run's interrupt) ends the question at once, says nothing changed and
// nothing is remembered, and the run then stops as an interrupted run does (MA-1303's run test).
// An encrypted archive's Not now says what happens: the next terminal backup asks again, since
// nothing beside it may be kept (the B3 UX walk's U5). Positive twin first: Enter typed is an
// answered Not now, remembered for the archive.
func TestOnlyATypedLineIsAnAnswerAndCtrlCStopsTheQuestion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	oldTerm, oldIn := gmailAtTerminal, gmailInput
	t.Cleanup(func() { gmailAtTerminal, gmailInput = oldTerm, oldIn })
	gmailAtTerminal = func() bool { return true }
	ask := func(out string, in io.Reader) {
		t.Helper()
		gmailInput = func() io.Reader { return in }
		opts := app.Options{Out: out}
		setGmailQuestion(&opts, false)
		if opts.AskGmail(state.GmailSwitchQuestion(false)) {
			t.Fatal("not a yes, yet Yes")
		}
	}

	// Positive twin: Enter is an answered Not now, remembered.
	typed := filepath.Join(home, "typed")
	runningRecord(t, typed)
	ask(typed, strings.NewReader("\n"))
	if !gmailanswer.Read(typed).NotNow {
		t.Fatal("positive twin: a typed Enter is not remembered as Not now")
	}

	// The end of the input: no answer, not remembered.
	var w bytes.Buffer
	if yes, answered := askGmailAnswer(strings.NewReader(""), &w, state.GmailSwitchQuestion(false)); yes || answered || !strings.Contains(w.String(), gmailNoAnswer) || strings.Contains(w.String(), gmailSaidNotNow) {
		t.Fatalf("the end of the input: yes %v answered %v, said %q", yes, answered, w.String())
	}
	eof := filepath.Join(home, "eof")
	runningRecord(t, eof)
	ask(eof, strings.NewReader(""))
	if gmailanswer.Read(eof).NotNow {
		t.Fatal("the end of the input is remembered as Not now on this computer")
	}

	// Ctrl-C: the question ends at once, well before the time limit.
	old := gmailAnswerWait
	gmailAnswerWait = time.Minute
	t.Cleanup(func() { gmailAnswerWait = old })
	pr, pw := io.Pipe()
	defer pw.Close()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	w.Reset()
	start := time.Now()
	yes, answered := askGmailAnswerCtx(ctx, pr, &w, state.GmailSwitchQuestion(false), false)
	if yes || answered || time.Since(start) > 10*time.Second || !strings.Contains(w.String(), gmailStopped) {
		t.Fatalf("Ctrl-C: yes %v answered %v after %v, said %q", yes, answered, time.Since(start), w.String())
	}
	stopped := filepath.Join(home, "stopped")
	runningRecord(t, stopped)
	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	gmailInput = func() io.Reader { return pr2 }
	ctx2, cancel2 := context.WithCancel(context.Background())
	opts := app.Options{Out: stopped}
	setGmailQuestionCtx(ctx2, &opts, false)
	time.AfterFunc(50*time.Millisecond, cancel2)
	if opts.AskGmail(state.GmailSwitchQuestion(false)) || gmailanswer.Read(stopped).NotNow {
		t.Fatal("Ctrl-C was taken as yes, or remembered as Not now")
	}

	// An encrypted archive's Not now says the next terminal backup asks again.
	w.Reset()
	if _, answered := askGmailAnswerCtx(context.Background(), strings.NewReader("\n"), &w, state.GmailSwitchQuestion(true), true); !answered ||
		!strings.Contains(w.String(), gmailSaidNotNowEncrypted) || strings.Contains(w.String(), "will not ask again") {
		t.Fatalf("an encrypted archive's Not now says %q", w.String())
	}
	enc := filepath.Join(home, "enc")
	gmailInput = func() io.Reader { return strings.NewReader("\n") }
	eopts := app.Options{Out: enc, Key: &vault.Key{}}
	setGmailQuestion(&eopts, false)
	eopts.AskGmail(state.GmailSwitchQuestion(true))
	if p, _ := gmailanswer.Path(enc); fileExists(p) {
		t.Fatal("an encrypted archive's Not now was kept on this computer")
	}
}

// covers: MA-1306, R45
// The store name in `status`'s waiting line and in the summary of a run with nobody to ask is
// shown with control characters (C0, DEL, C1, such as U+009B, a one-character terminal escape)
// and bidirectional overrides (U+202E) taken out, as every other untrusted name the command line
// prints (the B3 adversarial pass's A5, turned from its probe
// TestB3AdvStatusWaitingLinePrintsControls). Positive twin first: a plain store name is shown as
// it is.
func TestTheGmailWaitingLineShowsNoControlCharacters(t *testing.T) {
	sum := func(tok string) string {
		r := app.Result{}
		r.Gmail = app.GmailOutcome{Off: []string{tok}}
		var buf bytes.Buffer
		printSummaryFor(log.New(&buf, "", 0), r, false, false, t.TempDir(), "Done.", "darwin")
		return buf.String()
	}
	if got := sum("imap.gmail.com"); !strings.Contains(got, state.GmailWaitingLine("imap.gmail.com")) {
		t.Fatalf("positive twin: %q", got)
	}
	for _, tok := range []string{"a\u009b2Jb", "imap.‮gmail.com"} {
		if got := sum(tok); !state.SafeToken(tok) || strings.ContainsAny(got, "\u009b‮") || !strings.Contains(got, "Gmail: not switched on for ") {
			t.Fatalf("the summary prints the store name %q raw (or not at all): %q", tok, got)
		}
		if shown := gmailStoreShown(tok); strings.ContainsAny(shown, "\u009b‮") {
			t.Fatalf("the shown name keeps a control: %q", shown)
		}
	}
}

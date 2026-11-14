package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1293, R45
// The terminal's Gmail question after the B3 friction review (F4, F7, F13, F14). It shows the
// heading, then the question's words, then how to answer. Nobody answering within the time limit
// is Not now for this run (so a backup never holds the archive's lock waiting on a question), and
// is not remembered; an answered Not now is remembered on this computer (in the Gmail answer file,
// even with no job file), so the next terminal backup asks nothing, and the words say so; it is
// never remembered for an encrypted archive (nothing beside it may say it holds a Gmail account).
// A yes kept on this computer (the Desktop card's, for an archive with no job file) is carried
// into the run. The `-gmail-folders` help says it answers the question without asking and that it
// cannot be undone. Positive twin first: an answer given in time is taken as given.
func TestTheTerminalQuestionHasATimeLimitAndANotNowIsRemembered(t *testing.T) {
	// Positive twin: answered in time.
	var w bytes.Buffer
	if !askGmail(strings.NewReader("y\n"), &w, state.GmailSwitchQuestion(false)) {
		t.Fatal("positive twin: y in time is not Yes")
	}
	s := w.String()
	if !strings.Contains(s, state.GmailQuestionHeading+"\n"+state.GmailQuestion+"\n"+gmailHowToAnswer) {
		t.Fatalf("the terminal does not show the heading, the question and how to answer: %q", s)
	}

	// Nobody answers: Not now once the limit passes, said so, and the read does not hold the run.
	old := gmailAnswerWait
	gmailAnswerWait = 50 * time.Millisecond
	t.Cleanup(func() { gmailAnswerWait = old })
	pr, pw := io.Pipe()
	defer pw.Close()
	w.Reset()
	start := time.Now()
	yes, answered := askGmailAnswer(pr, &w, state.GmailSwitchQuestion(false))
	if yes || answered || time.Since(start) > 5*time.Second {
		t.Fatalf("no answer: yes %v, answered %v after %v", yes, answered, time.Since(start))
	}
	if !strings.Contains(w.String(), gmailNoAnswer) {
		t.Fatalf("no answer is not said: %q", w.String())
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	out := filepath.Join(home, "archive")
	oldTerm := gmailAtTerminal
	gmailAtTerminal = func() bool { return true }
	t.Cleanup(func() { gmailAtTerminal = oldTerm })
	oldIn := gmailInput
	t.Cleanup(func() { gmailInput = oldIn })

	// An answered Not now, with no job file: remembered here; the next run asks nothing.
	gmailInput = func() io.Reader { return strings.NewReader("\n") }
	opts := app.Options{Out: out}
	setGmailQuestion(&opts, false)
	if opts.AskGmail == nil || opts.GmailNotNow {
		t.Fatal("a person at a terminal with nothing kept is not asked")
	}
	if opts.AskGmail(state.GmailSwitchQuestion(false)) {
		t.Fatal("Enter is not Not now")
	}
	if a := gmailanswer.Read(out); !a.NotNow {
		t.Fatal("an answered Not now is not remembered on this computer")
	}
	opts = app.Options{Out: out}
	setGmailQuestion(&opts, false)
	if !opts.GmailNotNow {
		t.Fatal("the next terminal backup does not honour the Not now kept here")
	}
	if !strings.Contains(gmailSaidNotNow, "will not ask again on this computer") {
		t.Fatalf("the Not now words still promise to ask again: %q", gmailSaidNotNow)
	}

	// No answer in time: not remembered.
	out2 := filepath.Join(home, "archive2")
	gmailInput = func() io.Reader { r, _ := io.Pipe(); return r }
	opts = app.Options{Out: out2}
	setGmailQuestion(&opts, false)
	if opts.AskGmail(state.GmailSwitchQuestion(false)) || gmailanswer.Read(out2).NotNow {
		t.Fatal("no answer in time was taken as yes, or remembered as Not now")
	}

	// An encrypted archive's Not now is never remembered beside it.
	out3 := filepath.Join(home, "archive3")
	gmailInput = func() io.Reader { return strings.NewReader("n\n") }
	opts = app.Options{Out: out3, Key: &vault.Key{}}
	setGmailQuestion(&opts, false)
	if opts.AskGmail(state.GmailSwitchQuestion(true)) {
		t.Fatal("n is not Not now")
	}
	if p, _ := gmailanswer.Path(out3); fileExists(p) {
		t.Fatal("an encrypted archive's Not now was written to this computer's Gmail answer file")
	}

	// A yes kept here (the Desktop card's, no job file) is carried into the terminal's run.
	out4 := filepath.Join(home, "archive4")
	if err := gmailanswer.Keep(out4, gmailanswer.Answer{Yes: []string{"gmail-store"}}); err != nil {
		t.Fatal(err)
	}
	opts = app.Options{Out: out4}
	setGmailQuestion(&opts, false)
	if len(opts.GmailYesStores) != 1 || opts.GmailYesStores[0] != "gmail-store" {
		t.Fatalf("the yes kept here is not carried: %v", opts.GmailYesStores)
	}

	// The flag's help.
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	exportFlags(fs)
	help := fs.Lookup("gmail-folders").Usage
	for _, must := range []string{"Say yes to the Gmail question", state.GmailQuestionHeading, "This cannot be undone"} {
		if !strings.Contains(help, must) {
			t.Fatalf("the -gmail-folders help does not say %q: %q", must, help)
		}
	}
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Lstat(p)
	return err == nil
}

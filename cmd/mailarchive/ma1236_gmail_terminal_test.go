package main

import (
	"bytes"
	"flag"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	jobfile "mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1236, R45
// The Gmail question at a terminal (slice B3's terminal surface): the question's exact words,
// then how to answer; "y" or "yes" in any case is Yes and says what follows; an empty line, any
// other answer or the end of the input is Not now and says nothing changed and how to say yes
// later. The run's options: a person at a terminal gets the question; a scheduled run
// (-unattended) and a run whose input or output is not a terminal never do; a Not now this
// computer keeps for the archive (the Desktop's, in its job file) is passed to the run, which
// then asks nothing; `-gmail-folders` is the yes for scripts. Positive twin first (a person at
// a terminal, nothing kept, is asked).
func TestTheTerminalAsksTheGmailQuestionOnlyOfAPerson(t *testing.T) {
	for _, c := range []struct {
		in  string
		yes bool
	}{{"y\n", true}, {"YES\n", true}, {" yes \n", true}, {"\n", false}, {"n\n", false}, {"maybe\n", false}, {"", false}} {
		var w bytes.Buffer
		got := askGmail(strings.NewReader(c.in), &w, state.GmailSwitchQuestion(false))
		if got != c.yes {
			t.Fatalf("answer %q: yes=%v, want %v", c.in, got, c.yes)
		}
		s := w.String()
		if !strings.Contains(s, state.GmailQuestion+"\n"+gmailHowToAnswer) {
			t.Fatalf("answer %q: the question's words are not shown: %q", c.in, s)
		}
		want := gmailSaidNotNow
		if c.yes {
			want = gmailSaidYes
		}
		if !strings.Contains(s, want) {
			t.Fatalf("answer %q: %q does not say %q", c.in, s, want)
		}
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	out := filepath.Join(home, "archive")
	atTerminal := true
	old := gmailAtTerminal
	gmailAtTerminal = func() bool { return atTerminal }
	t.Cleanup(func() { gmailAtTerminal = old })

	opts := app.Options{Out: out}
	setGmailQuestion(&opts, false)
	if opts.AskGmail == nil || opts.GmailNotNow {
		t.Fatalf("a person at a terminal, nothing kept: asked %v, not now %v", opts.AskGmail != nil, opts.GmailNotNow)
	}
	opts = app.Options{Out: out}
	setGmailQuestion(&opts, true)
	if opts.AskGmail != nil {
		t.Fatal("a scheduled run is asked")
	}
	atTerminal = false
	opts = app.Options{Out: out}
	setGmailQuestion(&opts, false)
	if opts.AskGmail != nil {
		t.Fatal("a run with no terminal is asked")
	}
	atTerminal = true
	p, err := jobfile.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := jobfile.Write(p, jobfile.Job{Name: "a", Out: out, Mode: "incremental", GmailNotNow: true}); err != nil {
		t.Fatal(err)
	}
	opts = app.Options{Out: out}
	setGmailQuestion(&opts, false)
	if !opts.GmailNotNow {
		t.Fatal("the Not now kept in the archive's job file is not passed to the run")
	}

	for _, c := range []struct {
		args []string
		yes  bool
	}{{[]string{"-out", out}, false}, {[]string{"-out", out, "-gmail-folders"}, true}} {
		fs := flag.NewFlagSet("export", flag.ContinueOnError)
		o := exportFlags(fs)
		if err := fs.Parse(c.args); err != nil {
			t.Fatal(err)
		}
		if got := exportOptions(o, nil, export.Incremental, time.Time{}).GmailYes; got != c.yes {
			t.Fatalf("%q: GmailYes %v, want %v", c.args, got, c.yes)
		}
	}
}

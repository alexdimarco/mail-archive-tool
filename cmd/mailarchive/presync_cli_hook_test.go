package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
)

// cliFake is the scripted pre-sync the presyncjob seam takes (C-41): it records
// the Config and returns a chosen result, and starts nothing.
type cliFake struct {
	cfg presync.Config
	res presync.Result
}

func (f *cliFake) run(_ context.Context, cfg presync.Config) presync.Result {
	f.cfg = cfg
	return f.res
}

// covers: MA-568, MA-901, MA-898
// The command line's -thunderbird-sync runs on the shared hook
// (design-presync-attended §2.2, T20, slice AS3): through the presyncjob seam, the
// session is handed the named profile, the command line's words and stderr for
// its narration, and is never attended (R38: no flag turns it on before slice
// AS10). B6 now reaches the command line too — a side outcome becomes a warning
// beside the main one — and so do the session's Release and its outcome, which
// the command line's own copy of the hook dropped. Where no adapter exists, the
// flag is refused, so the archive runs without the sync and says why.
func TestCLIHookIsTheSharedHook(t *testing.T) {
	if !presyncjob.Supported() {
		var opts app.Options
		err := setPresyncHook(&opts, "")
		if err == nil || !strings.Contains(err.Error(), "only available on macOS and Windows") || opts.BeforeRead != nil {
			t.Fatalf("no adapter: err %v, hook installed %v; want the refusal and no hook", err, opts.BeforeRead != nil)
		}
	}

	released := false
	fake := &cliFake{res: presync.Result{
		Outcome: presync.OutcomeQuitNotPermitted,
		Side:    []presync.Outcome{presync.OutcomeStateUnreadable},
		Release: func() { released = true },
		Left:    presync.LeftOpen,
	}}
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	defer restore()
	r := presync.OverrideTablesForTest(map[string]bool{"darwin": true, "windows": true}, map[string]bool{"darwin": true, "windows": true})
	defer r()

	var opts app.Options
	if err := setPresyncHook(&opts, "/profiles/x"); err != nil || opts.BeforeRead == nil {
		t.Fatalf("setPresyncHook: err %v, hook installed %v", err, opts.BeforeRead != nil)
	}
	br := opts.BeforeRead(context.Background(), []string{"/in/a"})
	c := fake.cfg
	if c.Attended || c.NeverQuit || c.EndWait != nil {
		t.Errorf("the command line's session was handed Attended %v, NeverQuit %v, EndWait set %v", c.Attended, c.NeverQuit, c.EndWait != nil)
	}
	if c.Profile != "/profiles/x" || !c.ProfileNamed || c.Surface != presync.SurfaceCLI || c.Log != os.Stderr || len(c.Inputs) != 1 {
		t.Errorf("the session was handed %+v", c)
	}
	if len(br.Warnings) != 2 || br.Warnings[0].Code != presync.OutcomeQuitNotPermitted.Code || br.Warnings[1].Code != presync.OutcomeStateUnreadable.Code {
		t.Errorf("warnings = %+v, want the main warning then the B6 side warning", br.Warnings)
	}
	if br.Release == nil {
		t.Fatal("the session's Release was dropped")
	}
	br.Release()
	if !released {
		t.Error("the Release handed to the run is not the session's")
	}
	if br.Outcome == nil || br.Outcome.Name != "quit-not-permitted" || br.Outcome.Left != "left-open" || br.Outcome.Line == "" {
		t.Errorf("outcome = %+v, want the session's", br.Outcome)
	}
}

// covers: MA-561
// The command line's hook turns a WARN outcome into one RunWarning carrying its
// code and the what/why/do-this message; an ok or note outcome adds none.
func TestPresyncWarningsMapping(t *testing.T) {
	fake := &cliFake{}
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	defer restore()
	var opts app.Options
	if err := setPresyncHook(&opts, ""); err != nil || opts.BeforeRead == nil {
		t.Fatalf("setPresyncHook: err %v, hook installed %v", err, opts.BeforeRead != nil)
	}
	warningsFor := func(o presync.Outcome) []string {
		fake.res = presync.Result{Outcome: o}
		var codes []string
		for _, w := range opts.BeforeRead(context.Background(), nil).Warnings {
			if w.Message == "" {
				t.Errorf("%s: a warning with no message", o.Name)
			}
			codes = append(codes, w.Code)
		}
		return codes
	}
	if w := warningsFor(presync.OutcomeQuitNotPermitted); len(w) != 1 || w[0] != presync.OutcomeQuitNotPermitted.Code {
		t.Errorf("warn mapping = %v, want one quit-not-permitted warning", w)
	}
	if w := warningsFor(presync.OutcomeOK); len(w) != 0 {
		t.Errorf("ok outcome produced warnings: %v", w)
	}
	if w := warningsFor(presync.OutcomeAlreadyRunning); len(w) != 0 {
		t.Errorf("a note outcome produced warnings: %v", w)
	}
}

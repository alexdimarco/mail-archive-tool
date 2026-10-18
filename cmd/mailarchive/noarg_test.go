package main

import (
	"bytes"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-620, S59
// §4.3's no-argument rule as a pure table: every row, every OS, inputs injected.
// SE1 constants — Windows no-console → desktop (W3); macOS bundle-without-terminal
// → wizard (M1); every no-argument Linux launch → usage (L1 dormant until L-L1,
// L2 otherwise). The console build double-clicked (W2) is usage with the wait.
//
// Prove-fail (§9.4): changing defaultVerb's macOS arm from `inBundle && noTerminal`
// to `inBundle` reddens the "M2 bundle from a terminal" row; merging W2 into W1
// (dropping the consoleIsOwn branch) reddens the "W2 own console" row.
func TestMA620_DefaultVerbTable(t *testing.T) {
	cases := []struct {
		name                                  string
		goos                                  string
		console, own, bundle, noTerm, display bool
		want                                  noArgPlan
	}{
		{"W1 shell-shared console", "windows", true, false, false, false, false, noArgPlan{verb: "usage"}},
		{"W2 own console (double-click)", "windows", true, true, false, false, false, noArgPlan{verb: "usage", consoleWait: true}},
		{"W3 no console (any own name)", "windows", false, false, false, false, false, noArgPlan{verb: "desktop"}},
		{"M1 bundle, no terminal", "darwin", false, false, true, true, false, noArgPlan{verb: "wizard"}},
		{"M2 bundle from a terminal", "darwin", false, false, true, false, false, noArgPlan{verb: "usage"}},
		{"M2 PATH symlink from a terminal", "darwin", false, false, false, false, false, noArgPlan{verb: "usage"}},
		{"M2 no terminal, outside a bundle", "darwin", false, false, false, true, false, noArgPlan{verb: "usage"}},
		{"L1 graphical, no terminal (dormant in SE1)", "linux", false, false, false, true, true, noArgPlan{verb: "usage"}},
		{"L2 a terminal", "linux", false, false, false, false, false, noArgPlan{verb: "usage"}},
		{"L2 no terminal, no display", "linux", false, false, false, true, false, noArgPlan{verb: "usage"}},
		{"other OS", "plan9", false, false, false, true, true, noArgPlan{verb: "usage"}},
	}
	for _, c := range assure.Reached(t, cases, "defaultVerb table rows") {
		got := defaultVerb(c.goos, c.console, c.own, c.bundle, c.noTerm, c.display)
		if got != c.want {
			t.Errorf("%s: defaultVerb(%q, console=%v own=%v bundle=%v noTerm=%v display=%v) = %+v, want %+v",
				c.name, c.goos, c.console, c.own, c.bundle, c.noTerm, c.display, got, c.want)
		}
	}
}

// covers: MA-620, S59
// The console build double-clicked (W2): the pointer is written first, then
// today's usage refusal (unchanged), then "Press Enter to close this window."
// and exactly one line is read; EOF ends the wait without blocking; the pointer
// names the sibling file when there is one and only the shortcut when there is
// not; and the console build opens no GUI (launchDesktop/runWizard are never
// called — it would bind the console to the server, §4.3). The plain usage cases
// (W1/M2/L2) write nothing and fall through (handled=false), so the pointer and
// the wait happen in the W2 case only.
func TestMA620_ConsoleBuildPointerAndWait(t *testing.T) {
	w2 := defaultVerb("windows", true, true, false, false, false)
	if !w2.consoleWait || w2.verb != "usage" {
		t.Fatalf("W2 plan = %+v, want {usage, consoleWait}", w2)
	}

	run := func(plan noArgPlan, sibling, stdin string) (code int, handled bool, out string, reads int, launched, wizarded bool) {
		var buf bytes.Buffer
		cr := &countingReader{r: strings.NewReader(stdin)}
		code, handled = runNoArgWith(noArgEnv{
			stderr:        &buf,
			stdin:         cr,
			self:          "mailarchive.exe",
			sibling:       sibling,
			launchDesktop: func([]string) error { launched = true; return nil },
			runWizard:     func() int { wizarded = true; return 0 },
			usageRefusal:  func() int { buf.WriteString("<<REFUSAL>>"); return 1 },
		}, plan)
		return code, handled, buf.String(), int(cr.calls.Load()), launched, wizarded
	}

	// W2, no sibling: the pointer names only the shortcut.
	code, handled, out, reads, launched, wizarded := run(w2, "", "\n")
	if code != 1 || !handled {
		t.Errorf("W2: code=%d handled=%v, want 1/true", code, handled)
	}
	if launched || wizarded {
		t.Errorf("W2 opened a GUI (launchDesktop=%v runWizard=%v) — the console build must not (§4.3)", launched, wizarded)
	}
	if reads == 0 {
		t.Error("W2 did not read a line for the Enter wait")
	}
	iPtr := strings.Index(out, "This is the command-line program")
	iRef := strings.Index(out, "<<REFUSAL>>")
	iWait := strings.Index(out, "Press Enter to close this window.")
	switch {
	case iPtr < 0:
		t.Errorf("W2 wrote no pointer; got %q", out)
	case iRef < 0:
		t.Errorf("W2 did not run today's refusal; got %q", out)
	case iWait < 0:
		t.Errorf("W2 wrote no Enter prompt; got %q", out)
	case !(iPtr < iRef && iRef < iWait):
		t.Errorf("W2 order wrong (pointer=%d refusal=%d wait=%d); got %q", iPtr, iRef, iWait, out)
	}
	if strings.Contains(out, "double-click the MailArchive Desktop shortcut") == false {
		t.Errorf("W2 with no sibling must name only the shortcut; got %q", out)
	}

	// W2 with a sibling: the pointer names the sibling file.
	_, _, out, _, _, _ = run(w2, "mailarchivew.exe", "\n")
	if !strings.Contains(out, "double-click mailarchivew.exe or the MailArchive Desktop shortcut") {
		t.Errorf("W2 with a sibling must name it; got %q", out)
	}

	// EOF ends the wait without blocking (a redirected or empty stdin).
	code, handled, out, reads, _, _ = run(w2, "", "")
	if code != 1 || !handled || reads == 0 {
		t.Errorf("W2 with EOF stdin: code=%d handled=%v reads=%d, want 1/true/>0", code, handled, reads)
	}

	// The pointer and the wait happen in W2 only: a shell-shared console (W1) and
	// a macOS terminal (M2) write nothing and fall through to main's usage path.
	for _, plan := range []noArgPlan{
		defaultVerb("windows", true, false, false, false, false), // W1
		defaultVerb("darwin", false, false, false, false, false), // M2
	} {
		code, handled, out, reads, launched, wizarded = run(plan, "", "")
		if handled || code != 0 || out != "" || reads != 0 || launched || wizarded {
			t.Errorf("plain usage %+v: code=%d handled=%v out=%q reads=%d — want fall-through, no output, no read",
				plan, code, handled, out, reads)
		}
	}
}

// covers: MA-620, S59
// The no-argument rule is reached under the program's own names (including a
// suffixed release name and a renamed copy) with an empty argument list, and
// never for a former name (which routes by §4.1 rule 6) or for an own name that
// carries an explicit verb.
func TestMA620_NoArgRouting(t *testing.T) {
	ownNames := []string{"mailarchive", "mailarchivew", "mailarchive.exe", "mailarchivew-windows-amd64", "MailArchive (1)"}
	for _, base := range assure.Reached(t, ownNames, "own names") {
		if got := resolveInvocation(base, nil); got.verb != "no-arg" {
			t.Errorf("own name %q with no args: verb=%q, want no-arg (§4.3/MA-620)", base, got.verb)
		}
	}

	// Former names with no args route by rule 6, not to the no-argument rule.
	if got := resolveInvocation("mailarchive-desktop", nil); got.verb != "desktop" {
		t.Errorf("mailarchive-desktop (former) with no args: verb=%q, want desktop (rule 6)", got.verb)
	}
	if got := resolveInvocation("mailarchive-gui", nil); got.verb != "wizard" {
		t.Errorf("mailarchive-gui (former) with no args: verb=%q, want wizard (rule 6)", got.verb)
	}

	// An own name that carries a verb is that verb, not the no-argument rule.
	if got := resolveInvocation("mailarchive", []string{"wizard"}); got.verb != "wizard" {
		t.Errorf("own name with an explicit verb: verb=%q, want wizard", got.verb)
	}
}

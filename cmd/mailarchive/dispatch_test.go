package main

import (
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

// eqArgs compares two argument slices, treating nil and empty as equal.
func eqArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// captureExportUsage renders the root usage (exportUsage) to a string by
// redirecting the stderr it writes to.
func captureExportUsage(t *testing.T) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	exportUsage(flag.NewFlagSet("mailarchive", flag.ContinueOnError))()
	os.Stderr = old
	_ = w.Close()
	b, _ := io.ReadAll(r)
	_ = r.Close()
	return string(b)
}

// withStderrDiscarded runs f with stderr sent to the null device, so a handler
// that prints a refusal/usage does not clutter the test log.
func withStderrDiscarded(t *testing.T, f func()) {
	t.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		f()
		return
	}
	old := os.Stderr
	os.Stderr = devnull
	defer func() { os.Stderr = old; _ = devnull.Close() }()
	f()
}

// covers: MA-610, R33, S59
// Every GUI surface is a verb of the one program: desktop/wizard/run-job route
// in-process to desktop.Launch/wizard.Run/wizard.RunHeadless; the legacy schedule
// argv (-job FILE in exactly two arguments, a -capture-first list) reach run-job
// and desktop; and the root usage lists the three verbs. Routing is asserted
// purely; the handlers are proven reached through injectable seams with NO
// process started (that no copy is ever started is MA-612's walk).
func TestMA610_EverySurfaceIsAVerb(t *testing.T) {
	cases := []struct {
		name string
		base string
		args []string
		want verbRoute
	}{
		{"desktop verb", "mailarchive", []string{"desktop", "-out", "A"}, verbRoute{verb: "desktop", args: []string{"-out", "A"}}},
		{"wizard verb", "mailarchive", []string{"wizard"}, verbRoute{verb: "wizard"}},
		{"run-job verb", "mailarchive", []string{"run-job", "job.json"}, verbRoute{verb: "run-job", args: []string{"job.json"}}},
		{"legacy -job FILE (two args)", "mailarchive", []string{"-job", "job.json"}, verbRoute{verb: "run-job", args: []string{"job.json"}}},
		{"legacy -capture-first", "mailarchive", []string{"-capture", "-out", "A"}, verbRoute{verb: "desktop", args: []string{"-capture", "-out", "A"}}},
		{"an export list is not a verb", "mailarchive", []string{"-out", "A", "-auto"}, verbRoute{verb: "", args: []string{"-out", "A", "-auto"}}},
	}
	for _, c := range cases {
		got := resolveInvocation(c.base, c.args)
		if got.verb != c.want.verb || got.usage != c.want.usage || !eqArgs(got.args, c.want.args) {
			t.Errorf("%s: resolveInvocation(%q, %v) = %+v, want %+v", c.name, c.base, c.args, got, c.want)
		}
	}

	// The handlers are REACHED in-process: swap the dispatch seams for recorders
	// and confirm the right one fires with the right args, with nothing executed.
	origD, origW, origJ := launchDesktop, runWizard, runHeadlessJob
	defer func() { launchDesktop, runWizard, runHeadlessJob = origD, origW, origJ }()

	var desktopArgs []string
	var wizardRan bool
	var jobPath string
	launchDesktop = func(a []string) error { desktopArgs = a; return nil }
	runWizard = func() int { wizardRan = true; return 0 }
	runHeadlessJob = func(p string) int { jobPath = p; return 0 }

	if code, handled := runResolvedVerb("mailarchive", []string{"desktop", "-out", "A"}); !handled || code != 0 {
		t.Fatalf("desktop verb: handled=%v code=%d", handled, code)
	}
	if !eqArgs(desktopArgs, []string{"-out", "A"}) {
		t.Errorf("desktop.Launch reached with args %v, want [-out A]", desktopArgs)
	}
	if _, handled := runResolvedVerb("mailarchive", []string{"wizard"}); !handled || !wizardRan {
		t.Error("wizard verb did not reach wizard.Run in-process")
	}
	if _, handled := runResolvedVerb("mailarchive", []string{"run-job", "job.json"}); !handled || jobPath != "job.json" {
		t.Errorf("run-job verb did not reach wizard.RunHeadless(%q); got %q", "job.json", jobPath)
	}
	if _, handled := runResolvedVerb("mailarchive", []string{"-out", "A"}); handled {
		t.Error("an export invocation must fall through to the CLI dispatch, not a GUI verb")
	}

	usage := captureExportUsage(t)
	for _, v := range []string{"desktop", "wizard", "run-job"} {
		if !strings.Contains(usage, "mailarchive "+v) {
			t.Errorf("root usage does not list the %q verb:\n%s", v, usage)
		}
	}
}

// covers: MA-611, R33, S59
// The legacy spellings are recognised only in their exact shape and only under
// the program's own names (mailarchive, mailarchivew); every other flag-first
// list reaches export unchanged. Under a former name (§4.1 rule 6, invokedBase
// injected): mailarchive-desktop reaches desktop with the same argv;
// mailarchive-gui maps -job F (and its variants) to run-job, no -job to wizard,
// and an unknown flag to today's usage refusal and exit code.
func TestMA611_LegacySpellingsExactShapeAndOwnNamesOnly(t *testing.T) {
	cases := []struct {
		name string
		base string
		args []string
		want verbRoute
	}{
		// Former name mailarchive-desktop: any argv → desktop, same argv (rule 6).
		{"desktop former: -copy-link", "mailarchive-desktop", []string{"-copy-link"}, verbRoute{verb: "desktop", args: []string{"-copy-link"}}},
		{"desktop former: -no-browser", "mailarchive-desktop", []string{"-no-browser"}, verbRoute{verb: "desktop", args: []string{"-no-browser"}}},
		{"desktop former: --capture X", "mailarchive-desktop", []string{"--capture", "-out", "A"}, verbRoute{verb: "desktop", args: []string{"--capture", "-out", "A"}}},
		{"desktop former: -config X -capture", "mailarchive-desktop", []string{"-config", "c.json", "-capture"}, verbRoute{verb: "desktop", args: []string{"-config", "c.json", "-capture"}}},
		{"desktop former: -capture=true", "mailarchive-desktop", []string{"-capture=true"}, verbRoute{verb: "desktop", args: []string{"-capture=true"}}},
		{"desktop former: empty", "mailarchive-desktop", nil, verbRoute{verb: "desktop"}},
		{"desktop former: .exe suffix", "mailarchive-desktop.exe", []string{"-capture"}, verbRoute{verb: "desktop", args: []string{"-capture"}}},
		// Former name mailarchive-gui: -job → run-job F; no -job → wizard; unknown → usage.
		{"gui former: -job F", "mailarchive-gui", []string{"-job", "j.json"}, verbRoute{verb: "run-job", args: []string{"j.json"}}},
		{"gui former: -job=F", "mailarchive-gui", []string{"-job=j.json"}, verbRoute{verb: "run-job", args: []string{"j.json"}}},
		{"gui former: --job F", "mailarchive-gui", []string{"--job", "j.json"}, verbRoute{verb: "run-job", args: []string{"j.json"}}},
		{"gui former: --job=F", "mailarchive-gui", []string{"--job=j.json"}, verbRoute{verb: "run-job", args: []string{"j.json"}}},
		{"gui former: no -job → wizard", "mailarchive-gui", nil, verbRoute{verb: "wizard"}},
		{"gui former: unknown flag → usage", "mailarchive-gui", []string{"-nope"}, verbRoute{usage: true}},
		{"gui former: .exe suffix", "mailarchive-gui.exe", []string{"-job", "j.json"}, verbRoute{verb: "run-job", args: []string{"j.json"}}},
		// Own names: the former-name desktop lists are UNCHANGED by rule 6.
		{"own: -copy-link → export", "mailarchive", []string{"-copy-link"}, verbRoute{verb: "", args: []string{"-copy-link"}}},
		{"own: --capture → export", "mailarchive", []string{"--capture"}, verbRoute{verb: "", args: []string{"--capture"}}},
		{"own: -no-browser → export", "mailarchivew", []string{"-no-browser"}, verbRoute{verb: "", args: []string{"-no-browser"}}},
		// Own names: the legacy forms are recognised only in their exact shape.
		{"own: -job F (two args) → run-job", "mailarchive", []string{"-job", "j.json"}, verbRoute{verb: "run-job", args: []string{"j.json"}}},
		{"own: -job F X (three args) → export", "mailarchive", []string{"-job", "j.json", "x"}, verbRoute{verb: "", args: []string{"-job", "j.json", "x"}}},
		{"own: -capture first → desktop", "mailarchivew", []string{"-capture"}, verbRoute{verb: "desktop", args: []string{"-capture"}}},
		// A non-own, non-former name does not get the legacy own-name forms.
		{"other name: -job F → export", "weirdname", []string{"-job", "j.json"}, verbRoute{verb: "", args: []string{"-job", "j.json"}}},
	}
	for _, c := range cases {
		got := resolveInvocation(c.base, c.args)
		if got.verb != c.want.verb || got.usage != c.want.usage || !eqArgs(got.args, c.want.args) {
			t.Errorf("%s: resolveInvocation(%q, %v) = %+v, want %+v", c.name, c.base, c.args, got, c.want)
		}
	}

	// The unknown-flag former-name case is today's usage refusal with exit 2.
	withStderrDiscarded(t, func() {
		if code, handled := runResolvedVerb("mailarchive-gui", []string{"-nope"}); !handled || code != 2 {
			t.Errorf("mailarchive-gui unknown flag: code=%d handled=%v, want 2/true", code, handled)
		}
	})

	// invokedBase returns a non-empty base name (its GOOS-specific source is the
	// unresolved os.Executable() / os.Args[0]; the routing above injects it).
	if invokedBase() == "" {
		t.Error("invokedBase returned an empty base name")
	}
}

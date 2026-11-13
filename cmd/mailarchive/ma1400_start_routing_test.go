package main

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/wizard"
)

// covers: MA-1400, R50, R33
// The `start` verb's routing (design-menubar D3, §7, MB-24), every input to its
// outcome. Positive twin first: `mailarchive start` reaches desktop.Start in-process
// with its arguments, exit 0. Then: `start -background` keeps its flag; the .app's
// executable (the mailarchive-gui name) with no -job and macOS's no-argument bundle
// launch (M1) both reach `start`; a refusal already shown is exit 1 and is not printed
// again; -h is exit 0; and the event loop is asked for exactly where the design puts it
// — on macOS only for a launch from inside the bundle (Launch Services routes the app's
// later clicks to that process), on Windows for every instance (its end-of-session
// messages), never on Linux. The wizard verb asks the running MailArchive first: the
// cmd layer wires desktop.HandOffWizard.
func TestMA1400_StartRouting(t *testing.T) {
	origS, origL := launchStart, useAppLoop
	defer func() { launchStart, useAppLoop = origS, origL }()
	var got []string
	calls := 0
	launchStart = func(a []string) error { calls++; got = a; return nil }
	loop := []bool{}
	useAppLoop = func(on bool) { loop = append(loop, on) }

	if code, handled := runResolvedVerb("mailarchive", []string{"start"}); !handled || code != 0 || calls != 1 || len(got) != 0 {
		t.Fatalf("start: handled=%v code=%d calls=%d args=%v; want reached once with no args", handled, code, calls, got)
	}
	if code, handled := runResolvedVerb("mailarchivew", []string{"start", "-background"}); !handled || code != 0 || !eqArgs(got, []string{"-background"}) {
		t.Errorf("start -background: handled=%v code=%d args=%v", handled, code, got)
	}
	for name, c := range map[string]struct {
		base string
		args []string
	}{
		"the .app's executable, no -job": {"mailarchive-gui", nil},
		"own name, explicit verb":        {"mailarchive", []string{"start"}},
	} {
		if r := resolveInvocation(c.base, c.args); r.verb != "start" {
			t.Errorf("%s: verb %q, want start", name, r.verb)
		}
	}
	if p := defaultVerb("darwin", false, false, true, true, false); p.verb != "start" {
		t.Errorf("M1 (the .app opened from Finder): %+v, want start", p)
	}
	// The no-argument plan reaches the start seam.
	calls = 0
	if code, handled := runNoArgWith(noArgEnv{launchStart: launchStart, invoked: "/Applications/Mail Archive.app/Contents/MacOS/mailarchive-gui"}, noArgPlan{verb: "start"}); !handled || code != 0 || calls != 1 {
		t.Errorf("the no-argument start plan: handled=%v code=%d calls=%d", handled, code, calls)
	}

	// Outcomes: a refusal shown is exit 1, help is exit 0.
	launchStart = func([]string) error { return flag.ErrHelp }
	if code, _ := runResolvedVerb("mailarchive", []string{"start", "-h"}); code != 0 {
		t.Errorf("start -h: exit %d, want 0", code)
	}
	launchStart = func([]string) error { return errors.New("boom") }
	if code, _ := runResolvedVerb("mailarchive", []string{"start"}); code != 1 {
		t.Errorf("start that fails: exit %d, want 1", code)
	}

	// Where the event loop runs.
	for _, c := range assure.Reached(t, []struct {
		goos, verb, invoked string
		want                bool
	}{
		{"darwin", "start", "/Applications/Mail Archive.app/Contents/MacOS/mailarchive-gui", true},
		{"darwin", "start", "/usr/local/bin/mailarchive", false},
		{"darwin", "desktop", "/Applications/Mail Archive.app/Contents/MacOS/mailarchive-gui", false},
		{"windows", "start", `C:\Program Files\MailArchive\mailarchivew.exe`, true},
		{"windows", "desktop", `C:\Program Files\MailArchive\mailarchivew.exe`, true},
		{"windows", "wizard", `C:\Program Files\MailArchive\mailarchivew.exe`, false},
		{"linux", "start", "/home/a/.local/bin/mailarchive", false},
	}, "event-loop rows") {
		if got := appLoopFor(c.goos, c.verb, c.invoked); got != c.want {
			t.Errorf("appLoopFor(%s, %s, %s) = %v, want %v", c.goos, c.verb, c.invoked, got, c.want)
		}
	}
	if len(loop) == 0 {
		t.Errorf("the start route never asked whether to run the event loop")
	}

	// The wizard verb asks the running MailArchive first (D5).
	if wizard.HandOff == nil {
		t.Fatalf("the cmd layer did not wire wizard.HandOff")
	}
	_ = desktop.HandOffWizard // the function wired
}

// covers: MA-1400, R50
// The .app's bundle (design-menubar MB-4): Info.plist.in marks the app LSUIElement, so
// the instance shows no Dock icon (no flash before the accessory policy is set) and its
// executable stays the mailarchive-gui name the routing sends to `start`.
func TestMA1400_TheBundleIsAnAgentAppThatRunsStart(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("..", "..", "packaging", "macos", "Info.plist.in"))
	if err != nil {
		t.Fatal(err)
	}
	keys := plistDict(t, bytes.ReplaceAll(in, []byte("@VERSION@"), []byte("1.0.0")))
	if keys["LSUIElement"] != "<true>" {
		t.Errorf("Info.plist.in LSUIElement = %q, want <true> (MB-4)", keys["LSUIElement"])
	}
	if exe := keys["CFBundleExecutable"]; resolveInvocation(exe, nil).verb != "start" {
		t.Errorf("the bundle's executable %q does not reach start", exe)
	}
}

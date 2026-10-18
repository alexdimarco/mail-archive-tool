package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/wizard"
)

// R33 — one program. Every GUI surface is a verb of this one executable, reached
// IN-PROCESS and never by starting a copy of ourselves (MA-612): `desktop` →
// desktop.Launch, `wizard` → wizard.Run, `run-job` → wizard.RunHeadless. The
// schedules and shortcuts the historical binaries installed keep working two
// ways — the legacy argv forms under our own name, and the former invoked names
// (mailarchive-desktop / mailarchive-gui) via §4.1 rule 6. resolveInvocation is
// PURE (invokedBase injected) so MA-610/MA-611 can table-test every case without
// a real executable; runResolvedVerb runs the chosen handler.

// Dispatch seams: the in-process entry points, as vars so MA-610 can prove the
// router reaches each one (with the right args) without launching a GUI, and so
// MA-612's walk sees no process start here.
var (
	launchDesktop  = desktop.Launch
	runWizard      = wizard.Run
	runHeadlessJob = wizard.RunHeadless
)

// verbRoute is where one invocation is routed.
type verbRoute struct {
	verb  string   // "desktop" | "wizard" | "run-job" | "" (a CLI verb or export)
	args  []string // the arguments to hand the verb's handler
	usage bool     // mailarchive-gui former name with an unknown flag → today's usage refusal
}

// invokedBase is the base name the program was invoked under: the unresolved
// os.Executable() on Windows and macOS, and the base of os.Args[0] on Linux
// (§4.1 rule 6 / MA-611). Impure; the routing that consumes it is pure.
func invokedBase() string {
	if runtime.GOOS == "linux" {
		return filepath.Base(os.Args[0])
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Base(exe)
	}
	return filepath.Base(os.Args[0])
}

// resolveInvocation maps the invoked base name and raw args to a routing
// decision. Pure, so every MA-610/MA-611 case is table-testable.
func resolveInvocation(invokedBase string, args []string) verbRoute {
	base := strings.TrimSuffix(strings.ToLower(invokedBase), ".exe")
	switch base {
	case "mailarchive-desktop":
		// Former name: every argv reaches the dashboard unchanged (§4.1 rule 6).
		return verbRoute{verb: "desktop", args: args}
	case "mailarchive-gui":
		// Former name: -job F → run-job F; no -job → wizard; unknown flag → usage.
		return resolveGUIFormerName(args)
	}

	// An explicit GUI verb is a verb of the one program under our own name.
	if len(args) > 0 {
		switch args[0] {
		case "desktop":
			return verbRoute{verb: "desktop", args: args[1:]}
		case "wizard":
			return verbRoute{verb: "wizard", args: args[1:]}
		case "run-job":
			return verbRoute{verb: "run-job", args: args[1:]}
		}
	}

	// The legacy schedule argv is recognised only in its exact shape and only
	// under our own names (MA-611); anything else is a CLI verb or export.
	if base == "mailarchive" || base == "mailarchivew" {
		if len(args) == 2 && args[0] == "-job" {
			return verbRoute{verb: "run-job", args: args[1:]} // -job FILE (exactly two arguments)
		}
		if len(args) > 0 && args[0] == "-capture" {
			return verbRoute{verb: "desktop", args: args} // a -capture-first list → desktop -capture …
		}
	}
	return verbRoute{verb: "", args: args}
}

// resolveGUIFormerName maps the args seen under the mailarchive-gui former name,
// mirroring that binary's only flag (-job): -job/--job F (or =F) runs the saved
// job; no -job opens the wizard; any other flag is today's usage refusal.
func resolveGUIFormerName(args []string) verbRoute {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-job" || a == "--job":
			if i+1 < len(args) {
				return verbRoute{verb: "run-job", args: []string{args[i+1]}}
			}
			return verbRoute{usage: true} // -job with no value is a flag error
		case strings.HasPrefix(a, "-job=") || strings.HasPrefix(a, "--job="):
			return verbRoute{verb: "run-job", args: []string{a[strings.IndexByte(a, '=')+1:]}}
		case strings.HasPrefix(a, "-"):
			return verbRoute{usage: true} // any other flag → today's usage refusal
		default:
			// A bare positional with no -job seen: the historical flag.Parse stops
			// here with jobPath unset, so the wizard opens.
			return verbRoute{verb: "wizard"}
		}
	}
	return verbRoute{verb: "wizard"} // empty argv → the wizard
}

// runResolvedVerb runs a resolved GUI verb (desktop / wizard / run-job) or the
// former-name usage refusal in-process and returns (exit code, true). It returns
// (0, false) when the invocation is a CLI verb or export, so main falls through
// to that dispatch. It NEVER starts a copy of the program (R33, MA-612).
func runResolvedVerb(base string, args []string) (code int, handled bool) {
	r := resolveInvocation(base, args)
	switch {
	case r.usage:
		// mailarchive-gui former name, unknown flag: reproduce that binary's flag
		// error + usage and exit 2 (MA-611, "today's usage refusal and exit code").
		fs := flag.NewFlagSet("mailarchive-gui", flag.ContinueOnError)
		fs.String("job", "", "run this saved job headlessly (no dialogs); what the installed schedule uses")
		_ = fs.Parse(args)
		return 2, true
	case r.verb == "desktop":
		if err := launchDesktop(r.args); err != nil {
			// A refusal the desktop already showed (its dialog and stderr) is not
			// reprinted here, so it reaches each channel once (§4.1 rule 4, MA-636).
			if !desktop.RefusalShown(err) {
				fmt.Fprintln(os.Stderr, "mailarchive: "+err.Error())
			}
			return 1, true
		}
		return 0, true
	case r.verb == "wizard":
		return runWizard(), true
	case r.verb == "run-job":
		if len(r.args) == 0 {
			fmt.Fprintln(os.Stderr, "mailarchive: run-job needs a saved job file path")
			return 2, true
		}
		return runHeadlessJob(r.args[0]), true
	default:
		return 0, false
	}
}

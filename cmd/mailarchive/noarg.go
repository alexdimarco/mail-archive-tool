package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"

	"mail-archive-tool/internal/desktop"
)

// §4.3 / MA-620 — what a launch with no argument does. Under the program's own
// names (not a former name; those are rule 6, §4.1) an empty argument list is
// decided by defaultVerb, a PURE function of the OS and five read-once inputs,
// so every row of §4.3 is table-testable on any OS (the inputs are injected).
// The impure reading of those inputs happens in runNoArg, once, per OS.

// noArgInputs are the five facts §4.3 reads once about how the program was
// started. Each is meaningful on some platforms and false elsewhere (a Windows
// console fact is false on macOS; a macOS bundle fact is false on Windows).
type noArgInputs struct {
	consoleAttached bool // Windows: GetConsoleWindow() != 0
	consoleIsOwn    bool // Windows: GetConsoleProcessList returns 1 (this program is the console's only process)
	inBundle        bool // macOS: the unresolved invoked path contains /Contents/MacOS/
	noTerminal      bool // macOS/Linux: no stream is a terminal and /dev/tty cannot be opened
	hasDisplay      bool // Linux: DISPLAY or WAYLAND_DISPLAY is present
}

// noArgPlan is what defaultVerb decides: a verb to run in-process, or the usage
// refusal. consoleWait marks the one case (W2) that first writes the console
// pointer and then waits for Enter around that refusal.
type noArgPlan struct {
	verb        string // "desktop" | "wizard" | "usage"
	consoleWait bool   // W2 only: write the X2 pointer and wait for Enter around the refusal
}

// defaultVerb is §4.3's table as a pure function. SE1 constants: the Windows
// no-console verb is `desktop` (W3); the macOS bundle-without-terminal verb is
// `wizard` (M1, Q2-mac's interim); the Linux graphical row (L1) is DORMANT until
// lab L-L1 passes both ways (Q4 answered "honour", 13.44), so in SE1/SE2 every
// no-argument Linux launch is today's usage refusal. Each verb that a later
// slice changes (M1→desktop at the macOS flip, L1 active after L-L1) is changed
// only by that slice (SC16, SC18, SC19).
//
// Prove-fail (§9.4): changing `inBundle && noTerminal` to `inBundle` reddens the
// macOS bundle-from-Terminal row; merging the consoleIsOwn branch into the
// shared-console branch reddens W2.
func defaultVerb(goos string, consoleAttached, consoleIsOwn, inBundle, noTerminal, hasDisplay bool) noArgPlan {
	switch goos {
	case "windows":
		switch {
		case !consoleAttached:
			return noArgPlan{verb: "desktop"} // W3: no console, any own name (suffixed or renamed included)
		case consoleIsOwn:
			return noArgPlan{verb: "usage", consoleWait: true} // W2: the console build double-clicked
		default:
			return noArgPlan{verb: "usage"} // W1: a console shared by a shell/script
		}
	case "darwin":
		if inBundle && noTerminal {
			return noArgPlan{verb: "wizard"} // M1: Finder/Dock/open of the .app
		}
		return noArgPlan{verb: "usage"} // M2: a terminal on any stream, or a path outside a bundle
	case "linux":
		// L1 (noTerminal && hasDisplay → wizard) is dormant until L-L1; L2 is
		// every other case. Both are today's usage refusal in SE1/SE2. The
		// inputs are read all the same, so the active L1 row is a one-line change
		// in this branch for the slice that lands it.
		_ = hasDisplay
		return noArgPlan{verb: "usage"}
	default:
		return noArgPlan{verb: "usage"}
	}
}

// noArgEnv holds the seams runNoArgWith needs, so the W2 pointer-and-wait can be
// driven on any OS by an injected reader and captured writer (MA-620's
// injected-reader test), and so the test can prove the sibling is never started.
type noArgEnv struct {
	stderr        io.Writer
	stdin         io.Reader
	self          string // the program's own invoked name, for the pointer's "run <self> -h"
	sibling       string // the GUI sibling file to name in the pointer, or "" (name only the shortcut)
	launchDesktop func([]string) error
	runWizard     func() int
	usageRefusal  func() int // today's usage refusal: text and exit code unchanged
}

// runNoArg reads the five inputs once for this OS, asks defaultVerb what to do,
// and runs it. It returns (code, true) when it handled the launch in-process (a
// GUI verb, or W2's pointer-and-wait), and (0, false) for the plain usage cases
// (W1/M2/L2), so main reproduces today's usage refusal through its own default
// path — text and exit code unchanged.
func runNoArg() (code int, handled bool) {
	in := readNoArgInputs(invokedPath())
	plan := defaultVerb(runtime.GOOS, in.consoleAttached, in.consoleIsOwn, in.inBundle, in.noTerminal, in.hasDisplay)
	return runNoArgWith(noArgEnv{
		stderr:        os.Stderr,
		stdin:         os.Stdin,
		self:          invokedBase(),
		sibling:       guiSibling(), // "" in SE1: the §4.6.1 sibling finder (passing §4.6.0) lands in SE2
		launchDesktop: launchDesktop,
		runWizard:     runWizard,
		usageRefusal:  usageRefusal,
	}, plan)
}

// runNoArgWith executes a decided plan with injected seams. The W2 case writes
// the pointer, runs today's refusal (unchanged), then "Press Enter to close this
// window." and reads one line (EOF ends the wait, so a redirected stdin never
// blocks). It never starts the sibling (§5.4 item 1, R33, MA-612).
func runNoArgWith(env noArgEnv, plan noArgPlan) (code int, handled bool) {
	switch plan.verb {
	case "desktop":
		if err := env.launchDesktop(nil); err != nil {
			// A refusal the desktop already showed is not reprinted (§4.1 rule 4, MA-636).
			if !desktop.RefusalShown(err) {
				fmt.Fprintln(env.stderr, "mailarchive: "+err.Error())
			}
			return 1, true
		}
		return 0, true
	case "wizard":
		return env.runWizard(), true
	default: // "usage"
		if !plan.consoleWait {
			// W1 / M2 / L2: let main's default path print today's usage refusal.
			return 0, false
		}
		// W2: the console build double-clicked (Explorer is the console's only
		// other owner). Point at the GUI, show today's refusal, then wait.
		fmt.Fprintln(env.stderr, consolePointer(env.self, env.sibling))
		code = env.usageRefusal()
		fmt.Fprintln(env.stderr, "Press Enter to close this window.")
		readOneLine(env.stdin)
		return code, true
	}
}

// consolePointer is the X2 pointer the console build writes before its refusal
// when it was double-clicked (§4.3, §6). It names the GUI sibling file when
// there is one, and only the MailArchive Desktop shortcut when there is not.
func consolePointer(self, sibling string) string {
	if sibling != "" {
		return "This is the command-line program. To open MailArchive Desktop, double-click " + sibling +
			" or the MailArchive Desktop shortcut. For the commands, open a terminal and run " + self + " -h."
	}
	return "This is the command-line program. To open MailArchive Desktop, double-click the MailArchive Desktop" +
		" shortcut. For the commands, open a terminal and run " + self + " -h."
}

// readOneLine reads a single line, returning at a newline or at EOF, and never
// reads a second. It is only the wait's blocking read; the content is discarded.
func readOneLine(r io.Reader) {
	if r == nil {
		return
	}
	_, _ = bufio.NewReader(r).ReadString('\n')
}

// usageRefusal reproduces today's no-argument usage refusal exactly — the text
// and exit code main prints when `mailarchive` runs with no arguments
// (runExport(nil) → "-out is required …"). W2 calls it between the pointer and
// the wait, so the refusal line the owner reads is the one they read today.
func usageRefusal() int {
	err := runExport(nil)
	if err == nil {
		return 0
	}
	var ee *exitError
	if !errors.As(err, &ee) {
		fmt.Fprintln(os.Stderr, "mailarchive: "+err.Error())
	}
	return exitCode(err)
}

// guiSibling returns the GUI file to name in the W2 pointer, or "" to name only
// the shortcut. In SE1 it is always "": the sibling finder of §4.6.1 (which must
// pass the §4.6.0 build-identity test) is built in SE2 and wired here then, so
// until then the console build points at the shortcut alone — a valid branch of
// §4.3 ("or only the shortcut when there is none").
func guiSibling() string { return "" }

// invokedPath is the unresolved path the program was invoked with (os.Args[0]).
// It feeds the macOS inBundle input (§4.3): a Finder/Dock/open launch of the
// .app passes the path under Contents/MacOS/, while a PATH symlink typed in a
// terminal does not, so the symlink case stays M2 (usage).
func invokedPath() string {
	if len(os.Args) > 0 {
		return os.Args[0]
	}
	return ""
}

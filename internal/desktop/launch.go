package desktop

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/browser"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/wizard"
)

// launchEnv is what a launch needs from its surroundings: the per-account state
// directory, the logon session, the browser opener, the dialogs a windowsgui
// binary shows in place of a console, and what is listening at MailArchive's
// former address. The binary uses osEnv; a test injects each, so a launch runs
// in-process with no browser, no dialog and no real account state.
type launchEnv struct {
	Stdout, Stderr io.Writer
	StateDir       func() (string, error)
	Session        string
	Open           func(url string) error
	// ShowRefusal shows a refusal where the user will see it — a dialog (the binary
	// has no console) and stderr — in the words of design §7.6 [CC20].
	ShowRefusal func(text string)
	// ShowLink shows a fresh link in a dialog with a Copy button, for a browser that
	// did not open [CC20].
	ShowLink func(link string)
	// ShowNotice shows an informational notice — stderr and a dialog — for a handoff
	// to a holder of another version; unlike a refusal, the launcher exits 0
	// (§4.5.3, MA-637).
	ShowNotice func(text string)
	// Version is this program's build version, stamped into the instance file and
	// compared on handoff to tell a holder of another version (§4.5.3, MA-637).
	Version string
	// Probe says what answers at 127.0.0.1:8097 (instance.FormerProgram,
	// instance.FormerPreUpgrade, or "" for nothing); EndOld ends an older instance of
	// this account found there and reports what it ended — the pid and program path,
	// or (0, "") for nothing (§4.5.2, MA-638) [CC22, CC23].
	Probe  func() string
	EndOld func() (pid int, program string)
	// WatchEvery is how often the lock holder re-checks its lock and instance file
	// (a minute); Window how long a launch keeps trying an instance that does not
	// answer (15 s) [CC21].
	WatchEvery, Window time.Duration
	// Ready, when set, is told once an instance is serving: how a test finds it.
	Ready func(instance.Info)
	// ExportDir is the per-user, local state directory the Extraction workspace keeps its
	// run records and journal in (StateDir); nil or "" leaves the workspace able
	// to preview but not to export, and writes nothing — what a test that runs a launch
	// in-process wants.
	ExportDir func() string
	// WorkspaceOff reports the machine policy that turns the Extraction workspace off
	// (Windows: an administrator's HKLM value); nil is "not set".
	WorkspaceOff func() bool

	// GUISession is this process's sign-in session (instance.GUISession): a launch
	// sends it in the hand-off, and the instance compares a launcher's with its own
	// (R50, the fold's third contradiction).
	GUISession string
	// AllowForeground lets the process pid bring a window to the front (Windows'
	// AllowSetForegroundWindow, design-menubar §7.3); a launch calls it, before a
	// hand-off, only for the pid the lock's holder record confirms (MB-17). nil where the
	// operating system has no such rule.
	AllowForeground func(pid int)
	// Loop, when set, runs the operating system's event loop for the instance (the
	// AppKit run loop on macOS's main thread; a hidden window's message loop on Windows)
	// around the serving, so the instance answers macOS's reopen and terminate requests
	// and Windows' end of session (design-menubar §7.2, §8, MB-2). nil serves directly.
	Loop AppLoop
	// BundleVersion is the version of the program on disk, for MB-14's line on macOS's
	// reopen; nil where there is no bundle to read.
	BundleVersion func() string
	// Tell shows a hand-off's outcome in this launch's own session (the choice or the
	// wizard already open, another or an unknown sign-in, a copy closing), headed
	// "MailArchive" (the S1 friction review, F6). nil uses ShowNotice.
	Tell func(text string)
}

// tell shows text through Tell, or ShowNotice when no Tell is set.
func (e *launchEnv) tell(text string) {
	if e.Tell != nil {
		e.Tell(text)
		return
	}
	e.ShowNotice(text)
}

// closingWait is how long a launch keeps waiting for a running copy that is closing
// (its instance file says so, or it answered "closing"): the drain's bound, the servers'
// shutdown and a margin, so a launch during a stop becomes the next copy instead of
// telling the person to kill the one that is protecting its work (A7).
var closingWait = DrainBudget + 10*time.Second

// errClosing is a hand-off attempt that met a copy which is closing: not handled yet.
var errClosing = errors.New("the running MailArchive is closing")

// AppLoop runs serve while the operating system's event loop for the instance runs on
// the calling thread, routing the system's requests to ev, and returns serve's error.
// It returns once serve has returned.
type AppLoop func(serve func() error, ev LoopEvents) error

// LoopEvents are what the operating system's loop reports to the instance.
type LoopEvents struct {
	// Reopen: macOS opened the app again while it runs (Launch Services' reopen).
	Reopen func()
	// EndSession: sign-out, shut down, an installer or Restart Manager asks the
	// process to end. It stops without asking and waits for running work up to bound
	// (MB-2 items 1 and 3), then returns.
	EndSession func(bound time.Duration)
	// Busy reports whether the instance runs work now, an open wizard included (Windows
	// blocks the shutdown with a reason while it does, and an installer's close request
	// is refused: the S1 fix round, A2).
	Busy func() bool
	// Close: a close request from outside (Windows' WM_CLOSE, what `taskkill` without /F
	// sends), or the loss of the event loop: the one stop, asking nothing; the serving
	// then waits for running work (the S1 fix round, A1). It returns at once.
	Close func()
}

// appLoop is the platform's AppLoop, set by loop_darwin.go and loop_windows.go; the
// command layer turns it on with UseAppLoop for a launch the operating system started
// as the app (macOS: from the bundle). nil elsewhere.
var appLoop AppLoop

// appLoopOn is whether osEnv uses appLoop (UseAppLoop).
var appLoopOn bool

// UseAppLoop turns the platform's event loop on for this process's instance: on macOS
// for a launch from the .app bundle (Launch Services routes the app's later clicks to
// it), on Windows for every instance (its end-of-session messages).
func UseAppLoop(on bool) { appLoopOn = on }

func osEnv() *launchEnv {
	env := &launchEnv{
		Stdout: os.Stdout, Stderr: os.Stderr, StateDir: instance.Dir, Session: instance.LogonSession(),
		Open: browser.Open, ShowRefusal: showRefusal, ShowLink: showLink, ShowNotice: showNotice,
		Version:    buildVersion(),
		Probe:      func() string { return probeFormer(instance.FormerAddress, 800*time.Millisecond) },
		EndOld:     func() (int, string) { return endPreUpgrade(instance.FormerAddress) },
		WatchEvery: time.Minute, Window: instance.DefaultWait,
		ExportDir: StateDir, WorkspaceOff: workspacePolicyOff,
		GUISession: instance.GUISession(), AllowForeground: allowForeground,
		BundleVersion: bundleVersionOnDisk, Tell: tellNotice,
	}
	if appLoopOn {
		env.Loop = appLoop
	}
	return env
}

// refusedError is a refusal that has already been shown to the user.
type refusedError struct{ text string }

func (e *refusedError) Error() string { return e.text }

// RefusalShown reports whether err is a refusal Launch has already shown the user
// (via a dialog, and stderr). A caller that prints errors itself must not print
// this one again: the windowsgui binary has no console to repeat it to, and the
// one program's dispatcher keeps one refusal voice (X2).
func RefusalShown(err error) bool {
	var r *refusedError
	return errors.As(err, &r)
}

func refuse(env *launchEnv, text string) error {
	env.ShowRefusal(text)
	return &refusedError{text}
}

// The `start` verb's terminal words (the S1 friction review, F14): what it is, and its
// one refusal, which the dispatcher prints after "mailarchive: " (so no second prefix).
const (
	startUsageText = "Usage: mailarchive start [flags]\n\nOpens MailArchive as its app icon does: it asks whether to add or back up mail step by step or to open MailArchive Desktop in your browser, or, when MailArchive already runs for your account, asks that copy to.\n\nFlags:"
	startArgsText  = "start takes no arguments, only flags (got %q); run mailarchive start -h"
)

// The §7.6 refusal texts of the desktop (what / why / what to do).
func portBusyText(addr string) string {
	return fmt.Sprintf("mailarchive desktop: -addr %s is in use by another program. Why: the dashboard needs its own port. Do this: leave -addr unset (any free port is used) or choose a free port.", addr)
}

func wedgedText(pid int) string {
	who := "quit it in Task Manager (or `kill` it)"
	if pid > 0 {
		who = fmt.Sprintf("quit it in Task Manager (pid %d; or `kill %d`)", pid, pid)
	}
	return "MailArchive Desktop is already running for your account but is not responding. Do this: " + who + ", then start it again."
}

func otherConfigText(path string) string {
	return fmt.Sprintf("MailArchive Desktop is already running for your account with another archive or configuration (%s). Do this: use that window, or quit it first.", path)
}

// otherVersion reports whether a handoff should name the holder: its version is
// absent (a pre-SE1 instance), or it differs from this program's and neither is a
// 'dev' build (whose version is not comparable). Version, not base name, so a
// renamed file and every in-place upgrade are caught (§4.5.3, MA-637).
func otherVersion(holder, mine string) bool {
	if holder == "" {
		return true
	}
	if holder == mine || holder == "dev" || mine == "dev" {
		return false
	}
	return true
}

// otherVersionText is the §4.5.3 notice naming the holder's program (control-
// stripped) and pid; the launcher shows it and still exits 0.
func otherVersionText(exe string, pid int) string {
	return fmt.Sprintf("Another version of MailArchive Desktop is still running for this account (%s, process %d), so the dashboard that opened is that version's. Do this: quit it (on Windows, end process %d in Task Manager; elsewhere, `kill %d`), or sign out and back in, then open MailArchive Desktop again.", stripControl(exe), pid, pid, pid)
}

// stripControl removes control characters from a path before it is shown, so a
// crafted executable path cannot inject terminal escapes or dialog markup.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func noStateDirText(err error) string {
	return fmt.Sprintf("MailArchive Desktop cannot start: it has no private folder of your account to keep its own state in (%v). Do this: sign out and back in so your profile folder is set, then start MailArchive again.", err)
}

// launchOpts is what a launch was asked for.
type launchOpts struct {
	addr, readerAddr string
	addrSet          bool
	out, cfgPath     string // absolute; out is "" when not given
	noBrowser        bool
	copyLink         bool
	dcfg             Config
	// start is the `start` verb (design-menubar D3): an instance it starts shows the
	// launcher choice instead of opening the browser, and a launch that finds one
	// running hands off `choose`; background (`start -background`, the sign-in launch)
	// shows nothing, and does nothing when one runs.
	start, background bool
}

// desktopOpens reports whether this launch, once it is the instance, opens or shows
// a Desktop page itself (the desktop verb with no -no-browser, a -copy-link, or a
// `start` where no dialog can show): such a run never ends as idle (F2).
func (o *launchOpts) desktopOpens() bool {
	switch {
	case o.start && o.background:
		return false
	case o.start:
		return !canShowDialogs()
	}
	return o.copyLink || !o.noBrowser
}

// launch is MailArchive Desktop (design §4.C4): a scheduled `-capture` runs one
// headless capture and returns; otherwise the account's lock decides — the holder
// is the account's one instance of this configuration, and serves; any other
// launch hands off to that instance after it has proven itself with its key, so a
// listener that is not it, whatever port it holds, is never opened.
func launch(ctx context.Context, args []string, env *launchEnv) error {
	return launchMode(ctx, args, env, false)
}

// launchStart is the `start` verb (design-menubar §6, §7): what every app icon and
// shortcut runs. It is the desktop launch with the launcher choice in place of the
// browser, and `-background` for the sign-in launch.
func launchStart(ctx context.Context, args []string, env *launchEnv) error {
	return launchMode(ctx, args, env, true)
}

func launchMode(ctx context.Context, args []string, env *launchEnv, start bool) error {
	name := "mailarchive desktop"
	if start {
		name = "mailarchive start"
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if env.Stderr != nil {
		fs.SetOutput(env.Stderr)
	}
	if start {
		// What `start` is, before its flags (the S1 friction review, F14).
		fs.Usage = func() {
			fmt.Fprintln(fs.Output(), startUsageText)
			fs.PrintDefaults()
		}
	}
	addr := fs.String("addr", "127.0.0.1:0", "loopback address for the dashboard (must be loopback; port 0 takes any free port)")
	readerAddr := fs.String("reader-addr", "127.0.0.1:0", "loopback address for the archive reader (must be loopback; port 0 takes any free port)")
	out := fs.String("out", "", "archive directory to manage")
	cfg := fs.String("config", "", "Graph config file (default: under your OS config dir)")
	noWorkspace := fs.Bool("no-workspace", false, "turn the Extraction workspace off: its pages and endpoints are not served and the Export group is hidden (an administrator can also set the machine policy)")
	var capture, noBrowser, copyLink, background *bool
	var logPath *string
	never := false
	if start {
		background = fs.Bool("background", false, "start MailArchive with no window (the sign-in launch); when it already runs, do nothing")
		capture, noBrowser, copyLink = &never, &never, &never
		logPath = new(string)
	} else {
		capture = fs.Bool("capture", false, "run ONE scheduled headless capture (no dashboard) and exit — what the scheduled backup runs")
		logPath = fs.String("log", "", "capture mode: append the run log here (default: desktop-capture.log under your OS config dir)")
		noBrowser = fs.Bool("no-browser", false, "do not open the dashboard in a browser on start (use for a background/Startup launch)")
		copyLink = fs.Bool("copy-link", false, "show a fresh one-time link in a dialog, to copy into a browser, instead of opening one (for a default browser that cannot be opened)")
		background = &never
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if start && fs.NArg() > 0 {
		return fmt.Errorf(startArgsText, fs.Arg(0))
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	cfgPath := *cfg
	if cfgPath == "" {
		p, err := graphconfig.DefaultConfigPath()
		if err != nil {
			return err
		}
		cfgPath = p
	}
	store, _ := graphconfig.DefaultSecretStore() // informational shell tolerates a nil store
	settingsPath := settingsPathFor(cfgPath)
	exe, _ := os.Executable() // recorded in a scheduled task so the weekly backup runs this binary
	dcfg := Config{Out: *out, ConfigPath: cfgPath, Store: store, SettingsPath: settingsPath, Exe: exe, Version: buildVersion(), LastArchive: OfferedArchive}
	dcfg.IndexPath = indexPathFor(cfgPath) // the Desktop maintains the known-archives index beside desktop-settings.json (SD-6)
	dcfg.WorkspaceOff = *noWorkspace || (env.WorkspaceOff != nil && env.WorkspaceOff())
	if env.ExportDir != nil {
		dcfg.StateDir = env.ExportDir()
	}

	// Scheduled headless capture (DC1): no server, no prompt — read the saved
	// config + sign-in and run one incremental capture, logging to a file.
	if *capture {
		lp := *logPath
		if lp == "" {
			lp = filepath.Join(filepath.Dir(cfgPath), "desktop-capture.log")
		}
		// A log inside an encrypted archive is never opened (phase2a §4) — a schedule the
		// Desktop installed points its log into the archive. The capture is refused and
		// recorded like any scheduled refusal: as one without the key when the archive
		// itself is encrypted. Like every failure exit of a scheduled capture but the paused
		// refusal, it goes through the alert streak (R3C-10, MA-879): a capture refused here
		// every run is told once, never silently.
		if lerr := app.RefuseLogInside(lp); lerr != nil {
			out := dcfg.effectiveOut()
			if perr := seal.RefuseWrite(out); perr != nil {
				return perr // paused: no record, no alert (S2-C2)
			}
			if kerr := app.RefuseKeyless("capture into it", out); kerr != nil {
				lerr = kerr
			}
			app.RecordRefusedRun(app.Options{Out: out, Unattended: true}, lerr)
			dcfg.reportRun(backupalert.KeyFor(out), out, alert.ResultFailed, lerr, 0, nil)
			return lerr
		}
		return scheduledCapture(dcfg, lp, filepath.Join(filepath.Dir(cfgPath), "desktop-capture.log"), env.Stderr)
	}

	// Both surfaces are control/reader surfaces served only on this machine; never
	// the network (design P2/DC7).
	if !Loopback(*addr) {
		return fmt.Errorf("-addr %s is not a loopback address: the dashboard is served only on 127.0.0.1/localhost", *addr)
	}
	if !Loopback(*readerAddr) {
		return fmt.Errorf("-reader-addr %s is not a loopback address: the archive reader is served only on 127.0.0.1/localhost", *readerAddr)
	}

	o := &launchOpts{addr: *addr, readerAddr: *readerAddr, addrSet: set["addr"], noBrowser: *noBrowser, copyLink: *copyLink, dcfg: dcfg,
		start: start, background: *background}
	o.cfgPath, _ = filepath.Abs(cfgPath)
	if *out != "" {
		o.out, _ = filepath.Abs(*out)
	}
	dir, err := env.StateDir()
	if err != nil {
		return refuse(env, noStateDirText(err))
	}
	return runInstance(ctx, dir, o, env)
}

// runInstance takes the account's lock and serves, or reaches the instance that
// holds it. A launch that finds the lock held keeps trying for up to env.Window —
// the instance may be starting, its instance file may have just been rewritten —
// and, if the lock falls free meanwhile, becomes the instance itself [CC21].
func runInstance(ctx context.Context, dir string, o *launchOpts, env *launchEnv) error {
	deadline := time.Now().Add(env.Window)
	closing := false
	for {
		h, err := instance.Acquire(dir, env.Session)
		if err == nil {
			return serveInstance(ctx, h, o, env)
		}
		if !instance.IsHeld(err) {
			return refuse(env, noStateDirText(err))
		}
		handled, herr := handoff(ctx, dir, o, env, deadline)
		if handled {
			return herr
		}
		if errors.Is(herr, errClosing) && !closing {
			// The running copy is closing and waits for its work: wait for it to end,
			// then become the next copy (A7).
			closing = true
			if d := time.Now().Add(closingWait); d.After(deadline) {
				deadline = d
			}
		}
		if time.Now().After(deadline) {
			if closing {
				env.tell(closingText)
				return nil
			}
			pid := instance.HolderPID(dir, env.Session)
			return refuse(env, wedgedText(pid))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// handoff is one attempt to reach the running instance (§4.C4 step 3). It reports
// handled when it has an outcome — the instance opened its browser (or said it was
// busy opening one), a link was shown, or the launch was refused for another
// configuration — and not handled when there is nothing yet to go on (no instance
// file, one that does not match the lock's holder, an instance that did not prove
// itself in time), so the caller tries again until its deadline.
func handoff(ctx context.Context, dir string, o *launchOpts, env *launchEnv, deadline time.Time) (handled bool, err error) {
	info, rerr := instance.Read(instance.FilePathIn(dir, env.Session))
	if rerr != nil {
		return false, nil
	}
	// The file must be the lock holder's: a file left by another process, or
	// forged, does not name who holds the lock.
	if pid := instance.HolderPID(dir, env.Session); pid != 0 && pid != info.PID {
		return false, nil
	}
	if info.Closing {
		// It is stopping and waits for its work (A7): not handled yet, keep waiting.
		return false, errClosing
	}
	if !samePathLaunch(info.Config, o.cfgPath) {
		return true, refuse(env, otherConfigText(info.Config))
	}
	if o.out != "" && info.Out != "" && !samePathLaunch(info.Out, o.out) {
		return true, refuse(env, otherConfigText(info.Out))
	}
	key, kerr := info.KeyBytes()
	if kerr != nil {
		return false, nil
	}
	window := time.Until(deadline)
	if window < 0 {
		window = 0
	}
	stillHeld := func() bool { return instance.LockIsHeld(dir, env.Session) }
	if o.start {
		// The `start` verb (design-menubar §7): a sign-in launch does nothing when
		// MailArchive already runs; any other asks the running copy for the choice, in
		// the second request form when its file says it knows it (MB-3), else today's
		// open with the other-version notice below.
		if o.background {
			return true, nil
		}
		if info.HasAct(instance.ActChoose) {
			done, herr := handoffAct(ctx, dir, info, key, instance.ActChoose, env, window, stillHeld)
			if done {
				return true, herr
			}
			if herr != nil {
				// Not reached yet, or closing: the caller tries again until its deadline.
				return false, closingOrNil(herr)
			}
			// Refused once (MB-3 item 2) or "can't show here" (MB-9): Desktop opens.
		}
	}
	quiet := o.noBrowser || o.copyLink
	var rep instance.Reply
	var cerr error
	if info.HasAct(instance.ActOpen) {
		// The second form's open carries this launch's sign-in, so a copy of another or
		// an unknown sign-in opens nothing on its screen (R50, for `desktop` too: the S1
		// adversarial pass, A3); a copy that refuses the form gets the first form once.
		ar := instance.ActRequest{Version: env.Version, Session: env.GUISession}
		rep, cerr = instance.ReachOpen(ctx, info.Dashboard, key, quiet, ar, window, stillHeld)
		switch {
		case errors.Is(cerr, instance.ErrRefused):
			rep, cerr = instance.Reach(ctx, info.Dashboard, key, quiet, window, stillHeld)
		case cerr != nil:
			return false, nil
		case rep.Reason == instance.ReasonClosing:
			return false, errClosing
		case rep.Reason == instance.ReasonOtherSession:
			env.tell(otherSessionText)
			return true, nil
		case rep.Reason == instance.ReasonUnknownSession:
			env.tell(unknownSessionText)
			return true, nil
		case rep.Reason != "":
			// No other reason answers an open; a squatter cannot forge one (MAC).
			return false, nil
		}
	} else {
		rep, cerr = instance.Reach(ctx, info.Dashboard, key, quiet, window, stillHeld)
	}
	if cerr != nil {
		return false, nil
	}
	switch {
	case rep.Busy, rep.Opened:
	case rep.Link != "" && (o.copyLink || !o.noBrowser):
		// The instance could not open a browser, or was asked not to and the
		// caller wants a link: show it where it can be copied [CC20].
		env.ShowLink(rep.Link)
	}
	// After a verified handoff (Busy, Opened or a Link), name a holder of another
	// or no version, and still exit 0 (§4.5.3, MA-637).
	if (rep.Busy || rep.Opened || rep.Link != "") && otherVersion(info.Version, env.Version) {
		env.ShowNotice(otherVersionText(info.Exe, info.PID))
	}
	return true, nil
}

// closingOrNil keeps errClosing and drops any other not-yet-reached error.
func closingOrNil(err error) error {
	if errors.Is(err, errClosing) {
		return errClosing
	}
	return nil
}

// scheduledCapture runs one scheduled capture (HeadlessCapture) logging to lp, the log its
// schedule names — which a schedule the Desktop installed keeps in the archive folder — with
// the emptied-folder guard (design-archive-delete §7, MA-956) kept out of a refused folder:
//   - a folder the guard refuses gets no log: when lp is in it, the capture logs to
//     settingsLog (desktop-capture.log beside the Desktop's settings) instead, so the refusal
//     leaves a trace there as run-job's leaves one beside its job file; a log elsewhere (after
//     "Save and keep" moved the archive) is opened as given and gets the refusal (del-s2-A3-01);
//   - a capture refused later because its folder holds no archive (under its lock, or the folder
//     gone) closes a log it opened in that folder, removes it when the capture made it, and
//     logs the refusal to settingsLog (del-s2-A2-03);
//   - whether the folder held an archive before the log was opened goes to the engine
//     (CaptureArchiveAtEntry), so a delete between the two is refused under the lock even
//     when it leaves only that log (slice 2's re-verification, item 1).
func scheduledCapture(dcfg Config, lp, settingsLog string, stderr io.Writer) error {
	dcfg.CaptureLog = lp
	out := dcfg.effectiveOut()
	dcfg.CaptureArchiveAtEntry = out != "" && app.ArchiveHere(out, "")
	logPath := lp
	// Nor does a folder whose delete did not finish get the log (MA-1044): the capture refuses it.
	if out != "" && app.PathIn(lp, out) && (app.RefuseEmptiedFolder(out, "", dcfg.captureOwnFiles()...) != nil ||
		app.RefuseUnfinishedDelete(out, dcfg.deletionsLogPath()) != nil) {
		logPath = settingsLog
	}
	_, serr := os.Lstat(logPath)
	made := errors.Is(serr, fs.ErrNotExist)
	var w io.Writer = stderr
	var f *os.File
	if of, ferr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); ferr == nil {
		f = of
		defer f.Close()
		w = io.MultiWriter(stderr, f)
	}
	err := startCapture(dcfg, w)
	if f != nil && out != "" && app.RefusedAsNoArchive(err) && app.PathIn(logPath, out) {
		_ = f.Close()
		if made {
			_ = os.Remove(logPath)
		}
		if sf, ferr := os.OpenFile(settingsLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); ferr == nil {
			log.New(sf, "", log.LstdFlags).Printf("FAILED: %v", err)
			_ = sf.Close()
		}
	}
	return err
}

// startCapture is scheduledCapture's HeadlessCapture; a var so a test can delete the archive
// between the log's opening and the capture's start.
var startCapture = HeadlessCapture

func samePathLaunch(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// serveInstance is the account's instance (§4.C4 step 2): it has the lock, so any
// instance file there is stale; it binds the dashboard (any free port unless -addr
// names one, in which case a busy port is a refusal), notes what holds
// MailArchive's former address, binds the reader on a free port of its own, publishes
// its address and key in the 0600 instance file, serves, and opens the browser at
// its own one-time link unless it was told not to.
func serveInstance(ctx context.Context, h *instance.Holder, o *launchOpts, env *launchEnv) error {
	// Closed once: by the serving itself when it ends (so a macOS terminate request,
	// which ends the process inside the event loop, still lets go of the instance file
	// and the lock first), or here.
	closeHolder := sync.OnceFunc(h.Close)
	defer closeHolder()
	h.RemoveStale()

	ln, err := net.Listen("tcp", o.addr)
	if err != nil {
		if o.addrSet && isAddrInUse(err) {
			return refuse(env, portBusyText(o.addr))
		}
		return err
	}
	// MailArchive's former address, probed only when the dashboard's own address was
	// left to us: an older instance there is ended if it is this account's, and
	// whatever else holds it is named, never silently [CC22, CC23, ruling OQ-9].
	former := ""
	var ended *instance.Ended
	if !o.addrSet {
		former = env.Probe()
		if former == instance.FormerPreUpgrade {
			if pid, program := env.EndOld(); pid != 0 {
				former = instance.FormerEnded
				ended = &instance.Ended{PID: pid, Program: stripControl(program)}
			}
			// Could not end it: keep FormerPreUpgrade, so the header and status show
			// the pre-upgrade notice until the owner signs out (§4.5.2, CC22).
		}
	}
	readerLn, err := net.Listen("tcp", o.readerAddr)
	if err != nil {
		ln.Close()
		return fmt.Errorf("archive reader could not bind %s: %w", o.readerAddr, err)
	}

	sess := localweb.NewSession()
	origin := "http://" + ln.Addr().String()
	dcfg := o.dcfg
	// The one write before any Save (design-desktop-settings-card section 8, rev4.1): a
	// BackupFor an earlier version recorded for a run-job's folder is cleared at start, so
	// Remove or a capture install on another archive can't delete that run-job's entry.
	if cleared, err := ClearRunJobBackupFor(dcfg.SettingsPath); err != nil {
		fmt.Fprintln(env.Stderr, "mailarchive desktop: the settings file still names another archive's backup, which could not be cleared: "+err.Error())
	} else if cleared {
		fmt.Fprintln(env.Stderr, "mailarchive desktop: the settings file no longer names another archive's scheduled backup as this one's; that backup keeps running as it was.")
	}
	// The workspace's Graph session and every preview and export run on this process's own
	// life: cancelling ctx (Ctrl-C, a logoff) drains them, and the process waits for the
	// running export to finalize (its manifest says cancelled, its marker CANCELLED) before it
	// lets go of the account's lock, within DrainBudget (XC6, MA-514).
	life := NewLifecycle(ctx)
	dcfg.Lifecycle = life
	// The encrypted archives opened here lock first at Shut down — before any other
	// drain, so a slow export's cannot use the budget up — and refuse every unlock
	// after; jobs keep their own leases (phase3 §3.2; the P3-3 plan, 3c).
	dcfg.Keys = newArchiveKeys(dcfg.Store, readerLockedNote)
	life.OnDrain(func(context.Context) error { dcfg.Keys.lockAll(true); return nil })
	dcfg.Keys.startReaper(life.Context().Done())
	dcfg.ReaderURL = "http://" + readerLn.Addr().String() + sess.Prefix()
	dcfg.Sessions = sess.Opened
	dcfg.Former = former
	dcfg.Ended = ended
	dcfg.NewLink = func() string { return sess.Link(origin, localweb.LinkTTL) }
	dcfg.WizardLock = func() (func(), bool) { return wizard.LockWizardIn(h.Dir) }
	// The reader is made first so a Delete can close its index of the archive it deletes
	// (design-archive-delete §9 "Open files", MA-974); it is given the finished config below.
	rmx := &readerMux{}
	dcfg.ReaderHold = rmx.holdFor

	keyHex, key := instance.NewKey()
	handoffH := &instance.Handoff{
		Key: key, Open: env.Open, Redeemed: sess.WaitRedeemed,
		Mint: func(ttl time.Duration) (string, string) {
			t := sess.Mint(ttl)
			return origin + localweb.OpenPath(t), t
		},
	}
	var d *dashboard
	dcfg.made = func(x *dashboard) {
		d = x
		if instanceDashboardMade != nil {
			instanceDashboardMade(x)
		}
	}
	// The wizard this process runs exports on its life, and every stop waits for it
	// (design-menubar MB-2 item 4).
	wizard.BindExports(life.Context())
	life.OnDrain(wizard.WaitExports)
	// The launcher choice and the in-process wizard (design-menubar §6, slice S1).
	// desktopAsked records that a Desktop page was opened or asked for in this run, so a
	// browser still on its way never lets the instance end as idle (F2).
	var desktopAsked atomic.Bool
	openDesktop := func() {
		desktopAsked.Store(true)
		if env.Open(sess.Link(origin, localweb.OpenerTTL)) != nil {
			env.ShowLink(sess.Link(origin, localweb.LinkTTL))
		}
	}
	f := &front{
		life: life, dir: h.Dir, session: env.GUISession, version: env.Version, open: openDesktop,
		task:     func() string { return d.nowTask() },
		question: func() string { return d.quitQuestion() },
		idle: func() bool {
			n, _ := sess.Opened()
			return n == 0 && !desktopAsked.Load() && d.quitQuestion() == ""
		},
		hasArchives:   dcfg.hasAnyArchive,
		bundleVersion: env.BundleVersion,
	}
	handoffH.Act = f.act
	handoffH.Gate = f.gate
	handoffH.Quit = f.quitRequest
	// Where one copy serves every sign-in of the account, a first-form open (it carries no
	// sign-in) gets a link for the launch's own session, never this copy's browser (B9).
	handoffH.FirstFormQuiet = !sessionsSeparate
	// DisableGeneralOptionsHandler: net/http would answer OPTIONS * before the Host guard (AC8).
	dash := &http.Server{Handler: dashboardHandler(dcfg, ln.Addr(), sess, handoffH), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, DisableGeneralOptionsHandler: true}
	rmx.cfg = dcfg
	var rm http.Handler = rmx
	reader := &http.Server{Handler: readerHandler(rm, readerLn.Addr(), sess), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, DisableGeneralOptionsHandler: true}

	exe, _ := os.Executable()
	info := instance.Info{
		PID: os.Getpid(), Exe: exe, Started: time.Now(), Config: o.cfgPath, Out: o.out, Session: env.Session,
		Dashboard: ln.Addr().String(), Reader: readerLn.Addr().String(), Key: keyHex, Former: former, Version: env.Version, Ended: ended,
		Acts: instance.KnownActs, Icon: instance.IconNotYet,
	}
	if err := h.Publish(info); err != nil {
		ln.Close()
		readerLn.Close()
		return fmt.Errorf("could not record this instance: %w", err)
	}
	stopWatch := h.Watch(env.WatchEvery, func(s string) { fmt.Fprintln(env.Stderr, "mailarchive desktop: "+s) })
	defer stopWatch()
	// Once a stop begins, the instance file says so, so a launch during the drain waits
	// for this copy to end instead of calling it "not responding" (A7).
	go func() {
		<-life.Context().Done()
		h.MarkClosing()
	}()
	if o.desktopOpens() {
		desktopAsked.Store(true) // the desktop verb opens its page itself (F2)
	}

	serve := func() error {
		defer closeHolder()
		defer stopWatch()
		return serveUntilStopped(ln, readerLn, dash, reader, rm, sess, origin, info, o, env, life, f)
	}
	if env.Loop == nil {
		return serve()
	}
	served := make(chan struct{})
	return env.Loop(func() error { defer close(served); return serve() }, LoopEvents{
		Reopen: f.reopen,
		// Sign-out, an installer, Restart Manager: stop without asking and wait for
		// running work up to the bound (MB-2 items 1 and 3).
		EndSession: func(bound time.Duration) {
			life.Stop()
			select {
			case <-served:
			case <-time.After(bound):
			}
		},
		Busy:  func() bool { return d.quitQuestion() != "" },
		Close: life.Stop,
	})
}

// serveUntilStopped serves the dashboard and the reader until the lifecycle stops (or a
// server fails), then shuts both down and waits for the drain: the second half of
// serveInstance, run directly or inside the operating system's event loop.
func serveUntilStopped(ln, readerLn net.Listener, dash, reader *http.Server, rm http.Handler, sess *localweb.Session, origin string, info instance.Info, o *launchOpts, env *launchEnv, life *Lifecycle, f *front) error {
	errc := make(chan error, 2)
	go func() { errc <- dash.Serve(ln) }()
	go func() { errc <- reader.Serve(readerLn) }()

	fmt.Fprintf(env.Stdout, "MailArchive Desktop is running: dashboard on %s and archive reader on %s, each opened only through a private link.\n", info.Dashboard, info.Reader)
	fmt.Fprintf(env.Stdout, "Config: %s\n", o.cfgPath)
	fmt.Fprintln(env.Stdout, "If your browser does not open, run: mailarchive desktop -copy-link")
	fmt.Fprintln(env.Stdout, "Press Ctrl-C to stop.")

	// Open the dashboard in the OS default browser at this process's own one-time
	// link. The binary is built -ldflags -H=windowsgui (no console), so a
	// Desktop/Start-menu shortcut click would otherwise start a silent, invisible
	// server with nothing to see (review finding R1); when the opener cannot start
	// the browser, the link is shown in a dialog instead of being lost [CC20].
	// -no-browser (the Startup launch) suppresses it; -copy-link shows the link
	// without opening anything.
	switch {
	case o.start && o.background:
		// The sign-in launch: nothing shows (design-menubar §7.3).
	case o.start && canShowDialogs():
		// An app icon or shortcut: the launcher choice in place of the browser (§6.1).
		f.offerChoice(false, "")
	case o.copyLink:
		go env.ShowLink(sess.Link(origin, localweb.LinkTTL))
	case !o.noBrowser:
		// The desktop verb, and `start` where no dialog can show (Linux with no dialog
		// tool): Desktop opens directly (§6.1).
		if env.Open(sess.Link(origin, localweb.OpenerTTL)) != nil {
			go env.ShowLink(sess.Link(origin, localweb.LinkTTL))
		}
	}
	if env.Ready != nil {
		env.Ready(info)
	}

	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-life.Context().Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = reader.Shutdown(shutCtx)
		if c, ok := rm.(io.Closer); ok {
			_ = c.Close() // release the reader's open index handle on shutdown
		}
		err := dash.Shutdown(shutCtx)
		// the running export finalizes before the process lets go (its own budget)
		select {
		case <-life.Drained():
		case <-time.After(DrainBudget + 5*time.Second):
		}
		return err
	}
}

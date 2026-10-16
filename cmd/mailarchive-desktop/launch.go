package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/browser"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/localweb"
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
	// Probe says what answers at 127.0.0.1:8097 (instance.FormerProgram,
	// instance.FormerPreUpgrade, or "" for nothing); EndOld ends an older instance of
	// this account found there and reports whether it did [CC22, CC23].
	Probe  func() string
	EndOld func() bool
	// WatchEvery is how often the lock holder re-checks its lock and instance file
	// (a minute); Window how long a launch keeps trying an instance that does not
	// answer (15 s) [CC21].
	WatchEvery, Window time.Duration
	// Ready, when set, is told once an instance is serving: how a test finds it.
	Ready func(instance.Info)
	// ExportDir is the per-user, local state directory the Extraction workspace keeps its
	// run records and journal in (desktop.StateDir); nil or "" leaves the workspace able
	// to preview but not to export, and writes nothing — what a test that runs a launch
	// in-process wants.
	ExportDir func() string
	// WorkspaceOff reports the machine policy that turns the Extraction workspace off
	// (Windows: an administrator's HKLM value); nil is "not set".
	WorkspaceOff func() bool
}

func osEnv() *launchEnv {
	return &launchEnv{
		Stdout: os.Stdout, Stderr: os.Stderr, StateDir: instance.Dir, Session: instance.LogonSession(),
		Open: browser.Open, ShowRefusal: showRefusal, ShowLink: showLink,
		Probe:      func() string { return probeFormer(instance.FormerAddress, 800*time.Millisecond) },
		EndOld:     func() bool { return endPreUpgrade(instance.FormerAddress) },
		WatchEvery: time.Minute, Window: instance.DefaultWait,
		ExportDir: desktop.StateDir, WorkspaceOff: workspacePolicyOff,
	}
}

// refusedError is a refusal that has already been shown to the user.
type refusedError struct{ text string }

func (e *refusedError) Error() string { return e.text }

func refuse(env *launchEnv, text string) error {
	env.ShowRefusal(text)
	return &refusedError{text}
}

// The §7.6 refusal texts of the desktop (what / why / what to do).
func portBusyText(addr string) string {
	return fmt.Sprintf("mailarchive-desktop: -addr %s is in use by another program. Why: the dashboard needs its own port. Do this: leave -addr unset (any free port is used) or choose a free port.", addr)
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
	dcfg             desktop.Config
}

// launch is MailArchive Desktop (design §4.C4): a scheduled `-capture` runs one
// headless capture and returns; otherwise the account's lock decides — the holder
// is the account's one instance of this configuration, and serves; any other
// launch hands off to that instance after it has proven itself with its key, so a
// listener that is not it, whatever port it holds, is never opened.
func launch(ctx context.Context, args []string, env *launchEnv) error {
	fs := flag.NewFlagSet("mailarchive-desktop", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:0", "loopback address for the dashboard (must be loopback; port 0 takes any free port)")
	readerAddr := fs.String("reader-addr", "127.0.0.1:0", "loopback address for the archive reader (must be loopback; port 0 takes any free port)")
	out := fs.String("out", "", "archive directory to manage")
	cfg := fs.String("config", "", "Graph config file (default: under your OS config dir)")
	capture := fs.Bool("capture", false, "run ONE scheduled headless capture (no dashboard) and exit — what the weekly backup runs")
	logPath := fs.String("log", "", "capture mode: append the run log here (default: desktop-capture.log under your OS config dir)")
	noBrowser := fs.Bool("no-browser", false, "do not open the dashboard in a browser on start (use for a background/Startup launch)")
	copyLink := fs.Bool("copy-link", false, "show a fresh one-time link in a dialog, to copy into a browser, instead of opening one (for a default browser that cannot be opened)")
	noWorkspace := fs.Bool("no-workspace", false, "turn the Extraction workspace off: its pages and endpoints are not served and the Export group is hidden (an administrator can also set the machine policy)")
	if err := fs.Parse(args); err != nil {
		return err
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
	settingsPath := filepath.Join(filepath.Dir(cfgPath), "desktop-settings.json")
	exe, _ := os.Executable() // recorded in a scheduled task so the weekly backup runs this binary
	dcfg := desktop.Config{Out: *out, ConfigPath: cfgPath, Store: store, SettingsPath: settingsPath, Exe: exe, Version: buildVersion(), LastArchive: desktop.OfferedArchive}
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
		var w io.Writer = env.Stderr
		if f, ferr := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); ferr == nil {
			defer f.Close()
			w = io.MultiWriter(env.Stderr, f)
		}
		return desktop.HeadlessCapture(dcfg, w)
	}

	// Both surfaces are control/reader surfaces served only on this machine; never
	// the network (design P2/DC7).
	if !desktop.Loopback(*addr) {
		return fmt.Errorf("-addr %s is not a loopback address: the dashboard is served only on 127.0.0.1/localhost", *addr)
	}
	if !desktop.Loopback(*readerAddr) {
		return fmt.Errorf("-reader-addr %s is not a loopback address: the archive reader is served only on 127.0.0.1/localhost", *readerAddr)
	}

	o := &launchOpts{addr: *addr, readerAddr: *readerAddr, addrSet: set["addr"], noBrowser: *noBrowser, copyLink: *copyLink, dcfg: dcfg}
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
		if time.Now().After(deadline) {
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
	if !samePath(info.Config, o.cfgPath) {
		return true, refuse(env, otherConfigText(info.Config))
	}
	if o.out != "" && info.Out != "" && !samePath(info.Out, o.out) {
		return true, refuse(env, otherConfigText(info.Out))
	}
	key, kerr := info.KeyBytes()
	if kerr != nil {
		return false, nil
	}
	quiet := o.noBrowser || o.copyLink
	window := time.Until(deadline)
	if window < 0 {
		window = 0
	}
	rep, cerr := instance.Reach(ctx, info.Dashboard, key, quiet, window, func() bool {
		return instance.LockIsHeld(dir, env.Session)
	})
	if cerr != nil {
		return false, nil
	}
	switch {
	case rep.Busy, rep.Opened:
		return true, nil
	case rep.Link != "" && (o.copyLink || !o.noBrowser):
		// The instance could not open a browser, or was asked not to and the
		// caller wants a link: show it where it can be copied [CC20].
		env.ShowLink(rep.Link)
	}
	return true, nil
}

func samePath(a, b string) bool {
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
	defer h.Close()
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
	if !o.addrSet {
		former = env.Probe()
		if former == instance.FormerPreUpgrade && env.EndOld() {
			former = ""
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
	// The workspace's Graph session and every preview and export run on this process's own
	// life: cancelling ctx (Ctrl-C, a logoff) drains them, and the process waits for the
	// running export to finalize (its manifest says cancelled, its marker CANCELLED) before it
	// lets go of the account's lock, within desktop.DrainBudget (XC6, MA-514).
	life := desktop.NewLifecycle(ctx)
	dcfg.Lifecycle = life
	dcfg.ReaderURL = "http://" + readerLn.Addr().String() + sess.Prefix()
	dcfg.Sessions = sess.Opened
	dcfg.Former = former
	dcfg.NewLink = func() string { return sess.Link(origin, localweb.LinkTTL) }

	keyHex, key := instance.NewKey()
	handoffH := &instance.Handoff{
		Key: key, Open: env.Open, Redeemed: sess.WaitRedeemed,
		Mint: func(ttl time.Duration) (string, string) {
			t := sess.Mint(ttl)
			return origin + localweb.OpenPath(t), t
		},
	}
	// DisableGeneralOptionsHandler: net/http would answer OPTIONS * before the Host guard (AC8).
	dash := &http.Server{Handler: dashboardHandler(dcfg, ln.Addr(), sess, handoffH), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, DisableGeneralOptionsHandler: true}
	rm := desktop.ReaderHandler(dcfg)
	reader := &http.Server{Handler: readerHandler(rm, readerLn.Addr(), sess), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, DisableGeneralOptionsHandler: true}

	exe, _ := os.Executable()
	info := instance.Info{
		PID: os.Getpid(), Exe: exe, Started: time.Now(), Config: o.cfgPath, Out: o.out, Session: env.Session,
		Dashboard: ln.Addr().String(), Reader: readerLn.Addr().String(), Key: keyHex, Former: former,
	}
	if err := h.Publish(info); err != nil {
		ln.Close()
		readerLn.Close()
		return fmt.Errorf("could not record this instance: %w", err)
	}
	stopWatch := h.Watch(env.WatchEvery, func(s string) { fmt.Fprintln(env.Stderr, "mailarchive-desktop: "+s) })
	defer stopWatch()

	errc := make(chan error, 2)
	go func() { errc <- dash.Serve(ln) }()
	go func() { errc <- reader.Serve(readerLn) }()

	fmt.Fprintf(env.Stdout, "MailArchive Desktop is running: dashboard on %s and archive reader on %s, each opened only through a private link.\n", info.Dashboard, info.Reader)
	fmt.Fprintf(env.Stdout, "Config: %s\n", o.cfgPath)
	fmt.Fprintln(env.Stdout, "If your browser does not open, run: mailarchive-desktop -copy-link")
	fmt.Fprintln(env.Stdout, "Press Ctrl-C to stop.")

	// Open the dashboard in the OS default browser at this process's own one-time
	// link. The binary is built -ldflags -H=windowsgui (no console), so a
	// Desktop/Start-menu shortcut click would otherwise start a silent, invisible
	// server with nothing to see (review finding R1); when the opener cannot start
	// the browser, the link is shown in a dialog instead of being lost [CC20].
	// -no-browser (the Startup launch) suppresses it; -copy-link shows the link
	// without opening anything.
	switch {
	case o.copyLink:
		go env.ShowLink(sess.Link(origin, localweb.LinkTTL))
	case !o.noBrowser:
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
	case <-ctx.Done():
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
		case <-time.After(desktop.DrainBudget + 5*time.Second):
		}
		return err
	}
}

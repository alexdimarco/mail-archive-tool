// Command mailarchive-desktop is the in-process dashboard-and-launcher: a
// loopback-only local web control panel for capturing, browsing, and scheduling a
// Microsoft 365 archive, calling the engine's internal packages directly (never a
// child binary). See docs/design-mailarchive-desktop.md.
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
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/graphconfig"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "mailarchive-desktop: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("mailarchive-desktop", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8097", "loopback address for the dashboard (must be loopback)")
	readerAddr := fs.String("reader-addr", "127.0.0.1:8099", "loopback address for the archive reader (must be loopback)")
	out := fs.String("out", "", "archive directory to manage")
	cfg := fs.String("config", "", "Graph config file (default: under your OS config dir)")
	capture := fs.Bool("capture", false, "run ONE scheduled headless capture (no dashboard) and exit — what the weekly backup runs")
	logPath := fs.String("log", "", "capture mode: append the run log here (default: desktop-capture.log under your OS config dir)")
	noBrowser := fs.Bool("no-browser", false, "do not open the dashboard in a browser on start (use for a background/Startup launch)")
	if err := fs.Parse(args); err != nil {
		return err
	}
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
	dcfg := desktop.Config{Out: *out, ConfigPath: cfgPath, Store: store, SettingsPath: settingsPath, Exe: exe}

	// Scheduled headless capture (DC1): no server, no prompt — read the saved
	// config + sign-in and run one incremental capture, logging to a file.
	if *capture {
		lp := *logPath
		if lp == "" {
			lp = filepath.Join(filepath.Dir(cfgPath), "desktop-capture.log")
		}
		var w io.Writer = os.Stderr
		if f, ferr := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); ferr == nil {
			defer f.Close()
			w = io.MultiWriter(os.Stderr, f)
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
	dcfg.ReaderURL = "http://" + *readerAddr + "/"
	dashURL := "http://" + *addr + "/"

	// Bind the dashboard port FIRST. If it is already taken, an instance is already
	// running (e.g. the Startup shortcut started one at login). Don't fail — just
	// open the browser to the running instance and exit, so a second shortcut click
	// brings up the page instead of erroring (review finding R1 / README-RMM.md).
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		if isAddrInUse(err) {
			if *noBrowser {
				fmt.Printf("MailArchive Desktop is already running at %s\n", dashURL)
			} else {
				fmt.Printf("MailArchive Desktop is already running — opening %s\n", dashURL)
				openBrowser(dashURL)
			}
			return nil
		}
		return err
	}
	readerLn, err := net.Listen("tcp", *readerAddr)
	if err != nil {
		_ = ln.Close()
		return fmt.Errorf("archive reader could not bind %s: %w", *readerAddr, err)
	}

	dash := &http.Server{Handler: desktop.DashboardHandler(dcfg), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	reader := &http.Server{Handler: desktop.ReaderHandler(dcfg), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 2)
	go func() { errc <- dash.Serve(ln) }()
	go func() { errc <- reader.Serve(readerLn) }()

	fmt.Printf("MailArchive Desktop — open this page in a browser:\n  %s\n", dashURL)
	fmt.Printf("Archive reader: http://%s/\n", *readerAddr)
	fmt.Printf("Config: %s\n", cfgPath)
	fmt.Println("Press Ctrl-C to stop.")

	// Open the dashboard in the OS default browser. The binary is built
	// -ldflags -H=windowsgui (no console), so a Desktop/Start-menu shortcut click
	// would otherwise start a silent, invisible server with nothing to see
	// (review finding R1). The listener is already bound above, so the browser
	// never races a cold port. -no-browser (the Startup launch) suppresses it.
	if !*noBrowser {
		openBrowser(dashURL)
	}

	select {
	case err := <-errc:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = reader.Shutdown(shutCtx)
		return dash.Shutdown(shutCtx)
	}
}

// isAddrInUse reports whether a net.Listen error is "address already in use" —
// i.e. another instance already holds the port. errors.Is on syscall.EADDRINUSE
// covers Linux/macOS/Windows; the string fallback guards any wrapper that does
// not compare equal (incl. the Windows WSAEADDRINUSE message text).
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "address already in use") ||
		strings.Contains(msg, "only one usage of each socket address")
}

// openBrowserArgv is the detached command that opens a URL in the OS default
// browser. A pure function of GOOS so a test can assert the argv without a
// display. It mirrors cmd/mailarchive-gui's openPathArgv — the same OS handlers
// open a URL as open a path.
func openBrowserArgv(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

// openBrowser opens url in the OS default browser, detached and best-effort: the
// windowsgui binary has no console to show a launch error, and the URL is already
// printed, so a failure is silent. It is a var so a test can observe the launch
// without spawning a real browser.
var openBrowser = func(url string) {
	name, args := openBrowserArgv(runtime.GOOS, url)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err == nil && cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

// Command mailarchive-desktop is the in-process dashboard-and-launcher: a
// loopback-only local web control panel for capturing, browsing, and scheduling a
// Microsoft 365 archive, calling the engine's internal packages directly (never a
// child binary). See docs/design-mailarchive-desktop.md. Each account has one
// instance of it, reached through a private one-time link and verified by a
// challenge-response before a launch hands off to it (design-local-confidentiality
// §4.C2–C4).
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"mail-archive-tool/internal/browser"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/localweb"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		// A refusal has already been shown (a dialog, and stderr): the windowsgui
		// binary has no console to print it to again.
		var r *refusedError
		if !errors.As(err, &r) {
			fmt.Fprintf(os.Stderr, "mailarchive-desktop: %v\n", err)
		}
		os.Exit(1)
	}
}

// run is the binary's entry: it launches with the operating system's own
// surroundings, until the process is interrupted.
func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return launch(ctx, args, osEnv())
}

// buildVersion is the version the binary was built as, for the manifest of an export;
// "dev" for a build with no module version.
func buildVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// dashboardHandler is the dashboard as this binary serves it on the listener at,
// and readerHandler the embedded archive reader rm: each behind the local-surface
// guard (design-local-confidentiality §4.C1, P6) and the one session s of this
// process (P7: the reader's port takes the cookie the dashboard's link gave, since
// a cookie for host 127.0.0.1 goes to every port). handoff, for the dashboard,
// answers a later launch of this account (P8). Built in one place so the
// structural test (MA-378) probes exactly what the binary serves.
func dashboardHandler(dcfg desktop.Config, at net.Addr, s *localweb.Session, handoff http.Handler) http.Handler {
	return localweb.Wrap(localweb.Surface{Name: "dashboard", Listener: at, Session: s, Handoff: handoff}, desktop.DashboardHandler(dcfg))
}

func readerHandler(rm http.Handler, at net.Addr, s *localweb.Session) http.Handler {
	return localweb.Wrap(localweb.Surface{Name: "reader", Listener: at, Session: s}, rm)
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
// browser (the macOS fallback argv; the opener itself is internal/browser's, which
// keeps a launch token out of argv there [CC5]). A pure function of GOOS so a test
// can assert the argv without a display.
func openBrowserArgv(goos, url string) (string, []string) { return browser.Argv(goos, url) }

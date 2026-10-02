package main

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-271, R19, R12, S41
// `mailarchive-desktop` refuses a non-loopback bind: the dashboard is a control
// surface (capture / sign-in / schedule) and must never be reachable from the
// network. The refusal happens before the server binds.
func TestDesktopRefusesNonLoopback(t *testing.T) {
	err := run([]string{"-addr", "0.0.0.0:8097", "-out", t.TempDir()})
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	assure.Reached(t, msg, "non-loopback refusal")
	if !strings.Contains(msg, "loopback") {
		t.Errorf("refusal should name 'loopback': %v", err)
	}
}

// covers: MA-281, S41
// The open-a-URL argv is a pure function of GOOS, so a click can be launched with
// no display: the OS default-handler command on each platform (review finding R1).
func TestOpenBrowserArgv(t *testing.T) {
	cases := []struct {
		goos string
		name string
		args []string
	}{
		{"darwin", "open", []string{"http://127.0.0.1:8097/"}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", "http://127.0.0.1:8097/"}},
		{"linux", "xdg-open", []string{"http://127.0.0.1:8097/"}},
	}
	for _, c := range cases {
		name, args := openBrowserArgv(c.goos, "http://127.0.0.1:8097/")
		if name != c.name {
			t.Errorf("%s: open command = %q, want %q", c.goos, name, c.name)
		}
		if strings.Join(args, " ") != strings.Join(c.args, " ") {
			t.Errorf("%s: open args = %v, want %v", c.goos, args, c.args)
		}
	}
}

// covers: MA-281, R12, S41
// When the dashboard port is already bound (an instance is already running), a
// normal start does NOT fail to bind: it opens the browser to the running
// instance and returns cleanly (the "reconnect" path for a second shortcut
// click, review finding R1).
func TestDesktopReconnectsWhenAlreadyRunning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0") // stand in for the running instance
	if err != nil {
		t.Fatalf("pre-bind the dashboard port: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	var opened string
	restore := openBrowser
	openBrowser = func(url string) { opened = url }
	defer func() { openBrowser = restore }()

	cfgPath := filepath.Join(t.TempDir(), "graph.json")
	err = run([]string{"-addr", addr, "-config", cfgPath, "-out", t.TempDir()})
	if err != nil {
		t.Fatalf("already-running start should reconnect, not error: %v", err)
	}
	if opened != "http://"+addr+"/" {
		t.Errorf("reconnect should open the browser at the running instance; opened %q", opened)
	}
}

// covers: MA-281, S41
// -no-browser suppresses the auto-open (the background/Startup launch): the
// reconnect path still returns cleanly but opens no browser.
func TestDesktopNoBrowserSuppressesOpen(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind the dashboard port: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	opened := false
	restore := openBrowser
	openBrowser = func(string) { opened = true }
	defer func() { openBrowser = restore }()

	cfgPath := filepath.Join(t.TempDir(), "graph.json")
	err = run([]string{"-addr", addr, "-no-browser", "-config", cfgPath, "-out", t.TempDir()})
	if err != nil {
		t.Fatalf("already-running start should reconnect, not error: %v", err)
	}
	if opened {
		t.Error("-no-browser must suppress the auto-open; a browser was launched")
	}
}

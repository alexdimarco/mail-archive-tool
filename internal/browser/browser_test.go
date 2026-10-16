package browser

import (
	"strings"
	"testing"
)

const link = "http://127.0.0.1:8099/_open?t=SECRETTOKEN-0123456789"

func argvText(c Cmd) string { return c.Name + " " + strings.Join(c.Args, " ") }

// covers: MA-373, MA-281, R23, S41
// The opener a launch link goes through: on macOS the URL is written to
// osascript's stdin as `open location "<url>"`, so the token is in no process's
// argv — which any account can read there [CC5]; Linux and Windows use the OS
// default-handler command, and Argv (the fallback if the lab drill finds a
// prompt, MA-377) keeps the URL in argv on macOS too.
func TestOpenerKeepsTheTokenOutOfArgvOnMacOS(t *testing.T) {
	mac := Command("darwin", link)
	if mac.Name != "osascript" || strings.Contains(argvText(mac), "SECRETTOKEN") {
		t.Errorf("macOS opener = %q: want osascript with the token in no argv", argvText(mac))
	}
	if want := "open location \"" + link + "\"\n"; mac.Stdin != want {
		t.Errorf("macOS opener stdin = %q, want %q", mac.Stdin, want)
	}
	// A hostile quote or backslash in a URL cannot break out of the AppleScript string.
	if got := Command("darwin", `http://x/"; do shell script "id`+"\n").Stdin; strings.Count(got, `"`) != strings.Count(got, `\"`)+2 || strings.Contains(got, "\n\"") {
		t.Errorf("a quote in the URL is not escaped: %q", got)
	}
	for _, c := range []struct {
		goos, name, args string
	}{
		{"windows", "rundll32", "url.dll,FileProtocolHandler " + link},
		{"linux", "xdg-open", link},
		{"freebsd", "xdg-open", link},
	} {
		got := Command(c.goos, link)
		if got.Name != c.name || strings.Join(got.Args, " ") != c.args || got.Stdin != "" {
			t.Errorf("%s: %q (stdin %q), want %s %s", c.goos, argvText(got), got.Stdin, c.name, c.args)
		}
	}
	if name, args := Argv("darwin", link); name != "open" || len(args) != 1 || args[0] != link {
		t.Errorf("the macOS fallback argv = %s %v, want open <url>", name, args)
	}
}

// covers: MA-373, R23
// No auto-open without a display [CC18]: on Linux with neither DISPLAY nor
// WAYLAND_DISPLAY (a headless SSH session, where xdg-open could start a text
// browser) nothing is opened and the link is printed; a display of either kind,
// and macOS and Windows, open normally.
func TestNoDisplayIsDetectedFromTheEnvironment(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	for _, c := range []struct {
		name string
		goos string
		env  func(string) string
		want bool
	}{
		{"linux, nothing set", "linux", env(), true},
		{"linux, over ssh, nothing set", "linux", env("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22", "SSH_TTY", "/dev/pts/3"), true},
		{"linux, X11", "linux", env("DISPLAY", ":0"), false},
		{"linux, Wayland", "linux", env("WAYLAND_DISPLAY", "wayland-0"), false},
		{"linux, ssh with X forwarding", "linux", env("SSH_CONNECTION", "x", "DISPLAY", "localhost:10.0"), false},
		{"macOS", "darwin", env(), false},
		{"windows", "windows", env(), false},
	} {
		if got := NoDisplay(c.goos, c.env); got != c.want {
			t.Errorf("%s: NoDisplay = %v, want %v", c.name, got, c.want)
		}
	}
}

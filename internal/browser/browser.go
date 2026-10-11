// Package browser opens a launch link in the OS default browser — for `serve`,
// `setup` and MailArchive Desktop alike (design-local-confidentiality §4.C3).
// A launch link carries a one-time token, so how it is handed over matters:
// on macOS the URL goes to osascript on stdin, so the token is in no process's
// argv, which any account can read there (setuid /bin/ps, no hidepid
// equivalent) [CC5].
package browser

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Argv is the detached command that opens url in the OS default browser: the OS
// default-handler command each platform has, the one the desktop has always
// used. A pure function of goos, so a test can assert it without a display. On
// macOS it is also the fallback should the osascript opener (Command) show a
// prompt in the lab drill (MA-377); its argv carries the URL.
func Argv(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

// Cmd is a command and the bytes written to its stdin.
type Cmd struct {
	Name  string
	Args  []string
	Stdin string
}

// Command is what Open runs for url: Argv's command everywhere except macOS,
// where it is osascript reading `open location "<url>"` from stdin, so the URL —
// and the token in it — is in no process's argv; LaunchServices delivers it by
// Apple Event [CC5]. Windows keeps the URL in argv (a standard account cannot
// read another account's command lines there); Linux's /proc/*/cmdline is the
// stated residual (§8.4).
func Command(goos, url string) Cmd {
	if goos == "darwin" {
		return Cmd{Name: "osascript", Args: []string{"-"}, Stdin: "open location " + appleScriptString(url) + "\n"}
	}
	name, args := Argv(goos, url)
	return Cmd{Name: name, Args: args}
}

// appleScriptString quotes s as an AppleScript string literal.
func appleScriptString(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", ``, "\r", ``).Replace(s)
	return `"` + s + `"`
}

// NoDisplay reports a session with no display to open a browser on: on Linux
// (and other X11/Wayland systems), neither DISPLAY nor WAYLAND_DISPLAY is set —
// a headless SSH session, where xdg-open may start a text browser. macOS and
// Windows always have one [CC18].
func NoDisplay(goos string, getenv func(string) string) bool {
	switch goos {
	case "darwin", "windows":
		return false
	}
	return getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == ""
}

// Open opens url in the OS default browser and reports a failure to start the
// opener (the desktop shows the link in a dialog when it fails; the windowsgui
// binary has no console to show an error). It is a var so a test can observe the
// launch without spawning a browser.
var Open = func(url string) error {
	c := Command(runtime.GOOS, url)
	if c.Stdin != "" {
		// osascript finishes at once; waiting for it makes the error real, and keeps
		// the stdin copy alive until the script has been read.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, c.Name, c.Args...)
		cmd.Stdin = strings.NewReader(c.Stdin)
		return cmd.Run()
	}
	cmd := exec.Command(c.Name, c.Args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

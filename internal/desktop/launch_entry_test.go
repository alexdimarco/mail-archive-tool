package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-271, R19, R12, S41
// `mailarchive-desktop` refuses a non-loopback bind: the dashboard is a control
// surface (capture / sign-in / schedule) and must never be reachable from the
// network. The refusal happens before the server binds.
func TestDesktopRefusesNonLoopback(t *testing.T) {
	err := Launch([]string{"-addr", "0.0.0.0:8097", "-out", t.TempDir()})
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
// On macOS it is the fallback argv: the opener itself keeps a launch token out of
// argv there (internal/browser, MA-373); the reconnect and -no-browser cases of
// this row are TestDesktopReconnectsWhenAlreadyRunning and
// TestDesktopNoBrowserSuppressesOpen (instance_test.go).
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

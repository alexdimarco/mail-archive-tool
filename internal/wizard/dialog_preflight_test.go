package wizard

import "testing"

// dialogPreflightRefusal is the exact §4.4 stderr text the wizard shows when it
// cannot open its native dialogs; it must match the string in wizard.go.
const dialogPreflightRefusal = "mailarchive wizard: the wizard cannot open its windows. Why: it is made of native dialogs, which need the zenity tool and a desktop session. Do this: install the zenity package and run mailarchive wizard again from your desktop, or use the commands in the README (mailarchive -h)."

// covers: MA-639, R12, S59
// The wizard's dialog preflight is a pure decision over the OS, whether a dialog
// tool is on PATH, and whether a display is advertised (§4.4). On Linux and the
// BSDs it proceeds only when a tool AND a display are present, otherwise it
// returns the exact refusal text; macOS (osascript) and Windows (a native
// MessageBox) always proceed. The built-program leg — the exact stderr text,
// exit 1, and that a dialog tool is never invoked — is cmd/mailarchive's
// TestMA639_WizardRefusesWithoutADialog, which is Linux-gated.
func TestMA639_DialogPreflight(t *testing.T) {
	cases := []struct {
		goos          string
		tool, display bool
		wantOK        bool
	}{
		{"linux", true, true, true},
		{"linux", false, true, false},
		{"linux", true, false, false},
		{"linux", false, false, false},
		{"freebsd", true, true, true},
		{"freebsd", false, true, false},
		{"openbsd", true, false, false},
		{"netbsd", false, false, false},
		{"darwin", false, false, true},  // osascript is always present
		{"windows", false, false, true}, // a native MessageBox needs nothing
	}
	for _, c := range cases {
		text, ok := dialogPreflight(c.goos, c.tool, c.display)
		if ok != c.wantOK {
			t.Errorf("dialogPreflight(%q, tool=%v, display=%v) ok=%v, want %v", c.goos, c.tool, c.display, ok, c.wantOK)
		}
		switch {
		case ok && text != "":
			t.Errorf("dialogPreflight(%q, tool=%v, display=%v) proceeds but returned text %q", c.goos, c.tool, c.display, text)
		case !ok && text != dialogPreflightRefusal:
			t.Errorf("dialogPreflight(%q, tool=%v, display=%v) refusal = %q, want the exact §4.4 text", c.goos, c.tool, c.display, text)
		}
	}
}

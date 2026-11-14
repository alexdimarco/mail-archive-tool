package instance

import (
	"runtime"
	"strconv"
	"testing"
)

// covers: MA-1427, R50
// The sign-in session a launcher sends and an instance compares (R50, the fold's third
// contradiction). On macOS it is the audit session, read through getaudit_addr with no
// cgo: a positive number, the same on every call. On Windows it is "" (the lock and the
// instance file are already per logon session). Elsewhere it is XDG_SESSION_ID when that
// is a bounded printable value, and "" (cannot tell) otherwise.
func TestMA1427_GUISessionIsTheSignInSession(t *testing.T) {
	switch runtime.GOOS {
	case "darwin":
		s := GUISession()
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 || GUISession() != s {
			t.Errorf("GUISession on macOS = %q, want a stable positive audit session id", s)
		}
	case "windows":
		if s := GUISession(); s != "" {
			t.Errorf("GUISession on Windows = %q, want \"\"", s)
		}
	default:
		// Where systemd runs, logind's answer outranks XDG_SESSION_ID (the S1 fix round 3,
		// C2; TestMA1427_LogindsAnswerOutranksTheEnvironment): the environment is read here
		// as on a system without it.
		noLogindForTest(t)
		t.Setenv("XDG_SESSION_ID", "c2")
		if s := GUISession(); s != "c2" {
			t.Errorf("GUISession = %q, want c2", s)
		}
		// A value with the separator is never sent: the rule (guiSessionFrom) then falls
		// back to the audit session, proven in TestMA1427_ASignInThatCannotBeReadIsUnknown.
		t.Setenv("XDG_SESSION_ID", "a|b")
		if s := GUISession(); s == "a|b" {
			t.Errorf("GUISession with the separator = %q, want it refused", s)
		}
	}
}

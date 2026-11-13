//go:build !darwin

package instance

import (
	"os"
	"runtime"
)

const goosWindows = runtime.GOOS == "windows"

// GUISession is this process's sign-in session where the lock is per account and
// the instance must tell a launcher of another sign-in apart (R50, MB-9): on Linux
// systemd-logind's XDG_SESSION_ID (a local session and an xrdp one differ). On
// Windows the lock and the instance file are already per logon session
// (LogonSession), so a launcher only ever reaches its own session's instance: "".
// "" means "cannot tell", and the instance then shows the window as before.
func GUISession() string {
	if goosWindows {
		return ""
	}
	s := os.Getenv("XDG_SESSION_ID")
	if !validField(s, maxSessLen) {
		return ""
	}
	return s
}

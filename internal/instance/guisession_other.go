//go:build !darwin

package instance

import (
	"os"
	"runtime"
	"strings"
)

const goosWindows = runtime.GOOS == "windows"

// GUISession is this process's sign-in session where the lock is per account and
// the instance must tell a launcher of another sign-in apart (R50, MB-9): on Linux
// systemd-logind's XDG_SESSION_ID (a local session and an xrdp one differ), and, when
// the environment does not carry it, the kernel's audit session of this process
// (/proc/self/sessionid), which logind uses as its session id when auditing is on. On
// Windows the lock and the instance file are already per logon session
// (LogonSession), so a launcher only ever reaches its own session's instance: "".
// "" means "cannot tell": on Linux and the BSDs the instance then shows nothing for
// that launch (fail closed, the S1 fix round, A3).
func GUISession() string {
	if goosWindows {
		return ""
	}
	return guiSessionFrom(os.Getenv("XDG_SESSION_ID"), readSessionID)
}

// readSessionID reads the kernel's audit session id of this process.
func readSessionID() string {
	b, err := os.ReadFile("/proc/self/sessionid")
	if err != nil || len(b) > 32 {
		return ""
	}
	return string(b)
}

// guiSessionFrom is GUISession's rule over its inputs: XDG_SESSION_ID when it is a
// valid field, else the audit session id when it is set (4294967295 is "unset").
func guiSessionFrom(env string, audit func() string) string {
	if env != "" && validField(env, maxSessLen) {
		return env
	}
	s := strings.TrimSpace(audit())
	if s == "" || s == "4294967295" || !validField(s, maxSessLen) {
		return ""
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return s
}

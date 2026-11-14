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
// the environment does not carry it, logind's own answer for this process: the session
// scope (session-<id>.scope) its control group sits in, which is what logind's
// sd_pid_get_session reads. A process the systemd user manager started (GNOME on Wayland
// starts apps that way) sits under user@<uid>.service, which every sign-in of the account
// shares, so it has no session there and gets "" — never an id shared by two sign-ins
// (the S1 fix round 2, B8: the kernel's audit session is no longer used, since such a
// process carries the user manager's, one per account). On Windows the lock and the
// instance file are already per logon session (LogonSession), so a launcher only ever
// reaches its own session's instance: "". "" means "cannot tell": on Linux and the BSDs
// the instance then shows nothing for that launch (fail closed, the S1 fix round, A3).
// Whether a GNOME-started app gets an id here is the Linux lab, MA-1439 (pending).
func GUISession() string {
	if goosWindows {
		return ""
	}
	return guiSessionFrom(os.Getenv("XDG_SESSION_ID"), logindSession)
}

// logindSession is logind's session of this process, read from its control group; a var
// so MA-1427's test can give either answer.
var logindSession = func() string {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || len(b) > 64<<10 {
		return ""
	}
	return sessionFromCgroup(string(b))
}

// sessionFromCgroup is logind's rule (systemd's cg_path_get_session) over the text of
// /proc/<pid>/cgroup: on the systemd hierarchy (the unified "0::" line, or the legacy
// "name=systemd" one) the first unit below the slices must be session-<id>.scope, and
// <id> is the session; anything else (a service, the user manager, no systemd) is "".
func sessionFromCgroup(text string) string {
	path := ""
	for _, line := range strings.Split(text, "\n") {
		f := strings.SplitN(line, ":", 3)
		if len(f) != 3 {
			continue
		}
		if f[1] == "name=systemd" {
			path = f[2]
			break
		}
		if f[0] == "0" && f[1] == "" {
			path = f[2]
		}
	}
	for _, unit := range strings.Split(path, "/") {
		if unit == "" || strings.HasSuffix(unit, ".slice") {
			continue
		}
		id, ok := strings.CutPrefix(unit, "session-")
		if !ok {
			return ""
		}
		id, ok = strings.CutSuffix(id, ".scope")
		if !ok || id == "" {
			return ""
		}
		for _, c := range id {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				return ""
			}
		}
		return id
	}
	return ""
}

// guiSessionFrom is GUISession's rule over its inputs: XDG_SESSION_ID when it is a
// valid field, else logind's session of the process when it is one.
func guiSessionFrom(env string, logind func() string) string {
	if env != "" && validField(env, maxSessLen) {
		return env
	}
	s := strings.TrimSpace(logind())
	if s == "" || !validField(s, maxSessLen) {
		return ""
	}
	return s
}

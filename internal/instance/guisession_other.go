//go:build !darwin

package instance

import (
	"os"
	"runtime"
	"strings"
)

const goosWindows = runtime.GOOS == "windows"

// GUISession is this process's sign-in session where the lock is per account and
// the instance must tell a launcher of another sign-in apart (R50, MB-9). On Linux it is
// logind's own answer for this process first: the session scope (session-<id>.scope) its
// control group sits in, which is what logind's sd_pid_get_session reads (a local session
// and an xrdp one differ). A process the systemd user manager started (GNOME on Wayland
// starts apps that way) sits under user@<uid>.service, which every sign-in of the account
// shares, so logind says it has no session, and it gets "" — whatever its environment
// says: the user manager's environment is shared by every sign-in, and a sign-in that
// imports its own into it (dbus-update-activation-environment --systemd --all) hands its
// XDG_SESSION_ID to apps the manager starts for any sign-in (the S1 fix round 3, C2; round
// 2's B8 had the environment first). XDG_SESSION_ID is used only where systemd does not
// run at all (elogind, a container, the BSDs). On Windows the lock and the instance file
// are already per logon session (LogonSession), so a launcher only ever reaches its own
// session's instance: "". "" means "cannot tell": on Linux and the BSDs the instance then
// shows nothing for that launch (fail closed, the S1 fix round, A3). What a GNOME- or
// xrdp-started app gets here is the Linux lab, MA-1439 (pending).
func GUISession() string {
	if goosWindows {
		return ""
	}
	return guiSessionFrom(os.Getenv("XDG_SESSION_ID"), logindSession)
}

// logindSession is logind's answer for this process: running reports whether systemd (and
// so logind) manages this system, and id is the session its control group sits in ("" for
// none). A var so MA-1427's tests can give any answer.
var logindSession = func() (id string, running bool) {
	_, err := os.Stat("/run/systemd/system") // systemd's own test that it is the init (sd_booted)
	booted := err == nil
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil || len(b) > 64<<10 {
		return "", booted // systemd runs and the group cannot be read: no session (fail closed)
	}
	return logindFrom(string(b), booted)
}

// logindFrom is logind's answer over the text of /proc/self/cgroup and whether systemd is
// the init: systemd runs when it is booted or when the process's group is one of its units
// (a .slice, .scope or .service on the systemd hierarchy), and then the session is
// sessionFromCgroup's; otherwise there is no logind to ask.
func logindFrom(cgroup string, booted bool) (id string, running bool) {
	if !booted && !onSystemdHierarchy(cgroup) {
		return "", false
	}
	return sessionFromCgroup(cgroup), true
}

// onSystemdHierarchy reports whether the group path sessionFromCgroup reads names a
// systemd unit.
func onSystemdHierarchy(text string) bool {
	for _, unit := range strings.Split(systemdPath(text), "/") {
		if strings.HasSuffix(unit, ".slice") || strings.HasSuffix(unit, ".scope") || strings.HasSuffix(unit, ".service") {
			return true
		}
	}
	return false
}

// systemdPath is the process's group path on the systemd hierarchy: the legacy
// "name=systemd" line, else the unified "0::" line.
func systemdPath(text string) string {
	path := ""
	for _, line := range strings.Split(text, "\n") {
		f := strings.SplitN(line, ":", 3)
		if len(f) != 3 {
			continue
		}
		if f[1] == "name=systemd" {
			return f[2]
		}
		if f[0] == "0" && f[1] == "" {
			path = f[2]
		}
	}
	return path
}

// sessionFromCgroup is logind's rule (systemd's cg_path_get_session) over the text of
// /proc/<pid>/cgroup: on the systemd hierarchy (the unified "0::" line, or the legacy
// "name=systemd" one) the first unit below the slices must be session-<id>.scope, and
// <id> is the session; anything else (a service, the user manager, no systemd) is "".
func sessionFromCgroup(text string) string {
	for _, unit := range strings.Split(systemdPath(text), "/") {
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

// guiSessionFrom is GUISession's rule over its inputs: where logind runs, its session of
// the process when it is a valid field and "" otherwise, whatever XDG_SESSION_ID says (an
// id that agrees is the same id); where it does not, XDG_SESSION_ID when it is a valid
// field (the S1 fix round 3, C2).
func guiSessionFrom(env string, logind func() (string, bool)) string {
	if s, running := logind(); running {
		s = strings.TrimSpace(s)
		if s == "" || !validField(s, maxSessLen) {
			return ""
		}
		return s
	}
	if env != "" && validField(env, maxSessLen) {
		return env
	}
	return ""
}

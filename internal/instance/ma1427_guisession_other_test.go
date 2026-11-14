//go:build !darwin

package instance

import (
	"strings"
	"testing"
)

// covers: MA-1427, R50
// The Linux sign-in rule over its inputs (the S1 fix round, A3). Positive twin first:
// XDG_SESSION_ID when it is a bounded printable value. When the environment does not
// carry it (an autostart, `su`, a systemd user unit), logind's session of the process.
// Neither readable ("", a value with the separator, one out of bounds) is "" — cannot
// tell, which the instance answers by showing nothing (MA-1427's fail-closed rule).
// Amended 2026-11-13 (the S1 fix round 2, B8; amended, not weakened): the fallback is
// logind's session of the process, no longer the kernel's audit session, which a process
// the user manager started shares with every sign-in of the account.
func TestMA1427_ASignInThatCannotBeReadIsUnknown(t *testing.T) {
	none := func() string { return "" }
	if s := guiSessionFrom("c2", func() string { return "7" }); s != "c2" {
		t.Errorf("XDG_SESSION_ID c2 gave %q", s)
	}
	if s := guiSessionFrom("", func() string { return "7\n" }); s != "7" {
		t.Errorf("no XDG_SESSION_ID, logind's session 7: %q, want 7", s)
	}
	for name, c := range map[string][2]string{
		"nothing":               {"", ""},
		"separator, no logind":  {"a|b", ""},
		"logind with separator": {"", "1|2"},
		"logind out of bounds":  {"", strings.Repeat("9", 65)},
		"logind with a control": {"", "1\x01"},
	} {
		logind := none
		if c[1] != "" {
			v := c[1]
			logind = func() string { return v }
		}
		if s := guiSessionFrom(c[0], logind); s != "" {
			t.Errorf("%s: %q, want \"\" (cannot tell)", name, s)
		}
	}
}

// covers: MA-1427, R50
// The fallback sign-in is logind's, never one shared by every sign-in of the account (the
// S1 fix round 2, B8). Positive twin first: a process in a sign-in's own scope
// (session-2.scope, on the unified hierarchy or the legacy name=systemd one) has that
// session. A process the systemd user manager started (GNOME on Wayland starts apps under
// user@<uid>.service, which every sign-in of the account shares) has none — "", so the
// instance shows nothing for it rather than treat two sign-ins as one; so do a container
// with no systemd, a system service, a session scope nested under the user manager, and an
// id logind would not make. GUISession uses this rule when XDG_SESSION_ID is absent. Which
// answer a real GNOME or xrdp sign-in gets is the Linux lab, MA-1439 (pending).
func TestMA1427_TheLinuxSignInIsLogindsNeverTheAccounts(t *testing.T) {
	cases := map[string]struct{ cgroup, want string }{
		"a sign-in's scope (unified)": {"0::/user.slice/user-1000.slice/session-2.scope\n", "2"},
		"a sign-in's scope (legacy)":  {"12:pids:/user.slice/user-1000.slice/session-c3.scope\n1:name=systemd:/user.slice/user-1000.slice/session-c3.scope\n0::/\n", "c3"},
		"the user manager's app":      {"0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-gnome-org.gnome.Terminal-4242.scope\n", ""},
		"the user manager itself":     {"0::/user.slice/user-1000.slice/user@1000.service/init.scope\n", ""},
		"a scope nested in it":        {"0::/user.slice/user-1000.slice/user@1000.service/session-9.scope\n", ""},
		"a container":                 {"0::/\n", ""},
		"a system service":            {"0::/system.slice/cron.service\n", ""},
		"an id logind would not make": {"0::/user.slice/user-1000.slice/session-a_b.scope\n", ""},
		"nothing":                     {"", ""},
	}
	if got := sessionFromCgroup(cases["a sign-in's scope (unified)"].cgroup); got != "2" {
		t.Fatalf("twin: a sign-in's own scope gave %q, want 2", got)
	}
	for name, c := range cases {
		if got := sessionFromCgroup(c.cgroup); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
	if goosWindows {
		return // Windows' lock is per logon session: GUISession is always ""
	}
	t.Setenv("XDG_SESSION_ID", "")
	orig := logindSession
	t.Cleanup(func() { logindSession = orig })
	logindSession = func() string { return "c7" }
	if s := GUISession(); s != "c7" {
		t.Errorf("with no XDG_SESSION_ID, GUISession gave %q, want logind's c7", s)
	}
	logindSession = func() string { return "" }
	if s := GUISession(); s != "" {
		t.Errorf("with no XDG_SESSION_ID and no logind session, GUISession gave %q, want \"\" (cannot tell)", s)
	}
}

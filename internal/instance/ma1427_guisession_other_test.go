//go:build !darwin

package instance

import (
	"strings"
	"testing"
)

// noLogind is logind's answer where systemd does not run; logindSays one where it does.
func noLogind() (string, bool) { return "", false }

func logindSays(id string) func() (string, bool) {
	return func() (string, bool) { return id, true }
}

// noLogindForTest makes GUISession see a system without systemd for the test, so the
// environment's XDG_SESSION_ID is what it reads (MA-1427's rule where logind is absent).
func noLogindForTest(t *testing.T) {
	orig := logindSession
	t.Cleanup(func() { logindSession = orig })
	logindSession = noLogind
}

// covers: MA-1427, R50
// The Linux sign-in rule over its inputs (the S1 fix round, A3). Positive twin first:
// where systemd does not run, XDG_SESSION_ID when it is a bounded printable value; where
// it runs, logind's session of the process. Neither readable ("", a value with the
// separator, one out of bounds) is "" — cannot tell, which the instance answers by showing
// nothing (MA-1427's fail-closed rule).
// Amended 2026-11-13 (the S1 fix round 2, B8; amended, not weakened): the fallback is
// logind's session of the process, no longer the kernel's audit session, which a process
// the user manager started shares with every sign-in of the account.
// Amended 2026-11-14 (the S1 fix round 3, C2; amended, not weakened): logind's answer comes
// first where systemd runs; XDG_SESSION_ID is read only where it does not
// (TestMA1427_LogindsAnswerOutranksTheEnvironment).
func TestMA1427_ASignInThatCannotBeReadIsUnknown(t *testing.T) {
	if s := guiSessionFrom("c2", noLogind); s != "c2" {
		t.Errorf("no systemd, XDG_SESSION_ID c2 gave %q", s)
	}
	if s := guiSessionFrom("", logindSays("7\n")); s != "7" {
		t.Errorf("no XDG_SESSION_ID, logind's session 7: %q, want 7", s)
	}
	for name, c := range map[string]struct {
		env    string
		logind func() (string, bool)
	}{
		"nothing":               {"", noLogind},
		"separator, no logind":  {"a|b", noLogind},
		"logind with separator": {"", logindSays("1|2")},
		"logind out of bounds":  {"", logindSays(strings.Repeat("9", 65))},
		"logind with a control": {"", logindSays("1\x01")},
	} {
		if s := guiSessionFrom(c.env, c.logind); s != "" {
			t.Errorf("%s: %q, want \"\" (cannot tell)", name, s)
		}
	}
}

// covers: MA-1427, R50
// logind's own answer outranks the environment (the S1 fix round 3, C2). A process the
// systemd user manager started belongs to no sign-in — logind says so — yet it can carry
// an XDG_SESSION_ID: the manager's environment is shared by every sign-in of the account,
// and a sign-in that imports its own into it (dbus-update-activation-environment --systemd
// --all) hands its id to apps the manager starts for any sign-in. Positive twins first:
// where systemd does not run at all, XDG_SESSION_ID is the sign-in; where it runs, a process
// in a sign-in's own scope has that session, and an environment that agrees changes
// nothing. Then: a process in session-4.scope whose environment says 7 is 4; a process
// under the user manager whose environment says 7 is "" (cannot tell, fail closed), as is
// one whose group cannot be read while systemd runs. logind's answer: a systemd unit in the
// group path, or a booted systemd, means logind runs; a container's "/" with no systemd
// means it does not.
func TestMA1427_LogindsAnswerOutranksTheEnvironment(t *testing.T) {
	if s := guiSessionFrom("7", noLogind); s != "7" {
		t.Fatalf("twin: no systemd, XDG_SESSION_ID 7 gave %q", s)
	}
	if s := guiSessionFrom("4", logindSays("4")); s != "4" {
		t.Fatalf("twin: logind 4 and an environment that agrees gave %q", s)
	}
	if s := guiSessionFrom("7", logindSays("4")); s != "4" {
		t.Errorf("a process in session-4.scope whose environment says 7: %q, want logind's 4", s)
	}
	if s := guiSessionFrom("7", logindSays("")); s != "" {
		t.Errorf("a process logind gives no session (the user manager's) whose environment says 7: %q, want \"\"", s)
	}

	userManager := "0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-org.mailarchive-42.scope\n"
	for name, c := range map[string]struct {
		cgroup  string
		booted  bool
		id      string
		running bool
	}{
		"a sign-in's scope":                       {"0::/user.slice/user-1000.slice/session-4.scope\n", true, "4", true},
		"the user manager's app":                  {userManager, true, "", true},
		"the user manager's app, /run not seen":   {userManager, false, "", true},
		"a system service":                        {"0::/system.slice/cron.service\n", false, "", true},
		"a container with no systemd":             {"0::/\n", false, "", false},
		"a container on a booted systemd":         {"0::/\n", true, "", true},
		"the legacy hierarchy, a sign-in's scope": {"1:name=systemd:/user.slice/user-1000.slice/session-c3.scope\n0::/\n", false, "c3", true},
		"no group at all, no systemd":             {"", false, "", false},
	} {
		id, running := logindFrom(c.cgroup, c.booted)
		if id != c.id || running != c.running {
			t.Errorf("%s: logind (%q, running %v), want (%q, %v)", name, id, running, c.id, c.running)
		}
	}

	if goosWindows {
		return // Windows' lock is per logon session: GUISession is always ""
	}
	orig := logindSession
	t.Cleanup(func() { logindSession = orig })
	t.Setenv("XDG_SESSION_ID", "7")
	logindSession = func() (string, bool) { return logindFrom(userManager, true) }
	if s := GUISession(); s != "" {
		t.Errorf("GUISession under the user manager with XDG_SESSION_ID=7: %q, want \"\" (cannot tell)", s)
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
// id logind would not make. Which answer a real GNOME or xrdp sign-in gets is the Linux
// lab, MA-1439 (pending).
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
	logindSession = logindSays("c7")
	if s := GUISession(); s != "c7" {
		t.Errorf("with no XDG_SESSION_ID, GUISession gave %q, want logind's c7", s)
	}
	logindSession = logindSays("")
	if s := GUISession(); s != "" {
		t.Errorf("with no XDG_SESSION_ID and no logind session, GUISession gave %q, want \"\" (cannot tell)", s)
	}
}

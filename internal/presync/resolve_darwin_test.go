//go:build darwin

package presync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// covers: MA-572
// Running elevated is refused at S1 with presync_refused_elevated, BEFORE any
// helper runs (zero helpers) — Thunderbird would create profile files the owner
// could not then open.
func TestMacResolveRefusesElevation(t *testing.T) {
	e := newMacEnv("")
	e.geteuid = func() int { return 0 }
	ran := false
	e.run = func(*exec.Cmd) (string, string, error) { ran = true; return "", "", nil }

	_, outcome, ok := e.resolve("/some/profile", false)
	if ok || outcome != OutcomeRefusedElevated {
		t.Errorf("resolve under euid 0 = (%v, %q), want (false, refused-elevated)", ok, outcome.Name)
	}
	if ran {
		t.Error("a helper ran despite the elevation refusal (want zero helpers)")
	}
}

// covers: MA-562
// App resolution accepts only a .app directory that is not world-writable and
// whose plutil bundle id is Thunderbird's; a world-writable bundle, a wrong
// bundle id and a non-.app path are each refused, and resolution falls through
// the bad candidates to the good one.
func TestMacResolveApp(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "Thunderbird.app")
	mustMkdir(t, filepath.Join(good, "Contents"))
	ww := filepath.Join(tmp, "WorldWritable.app")
	mustMkdir(t, ww)
	if err := os.Chmod(ww, 0o777); err != nil {
		t.Fatal(err)
	}
	wrong := filepath.Join(tmp, "Wrong.app")
	mustMkdir(t, wrong)

	e := newMacEnv("")
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		last := cmd.Args[len(cmd.Args)-1] // <app>/Contents/Info.plist
		// `good` and `ww` both carry Thunderbird's id, so ONLY the world-writable
		// mode distinguishes `ww`; `wrong` carries a different id.
		if strings.HasPrefix(last, good) || strings.HasPrefix(last, ww) {
			return "org.mozilla.thunderbird\n", "", nil
		}
		return "org.mozilla.firefox\n", "", nil
	}

	b, exe, ok := e.resolveApp([]string{good})
	if !ok || b != good || exe != filepath.Join(good, "Contents", "MacOS", "thunderbird") {
		t.Errorf("good app = (%q, %q, %v)", b, exe, ok)
	}
	if _, _, ok := e.resolveApp([]string{ww}); ok {
		t.Error("a world-writable .app with a valid bundle id was accepted")
	}
	if _, _, ok := e.resolveApp([]string{wrong}); ok {
		t.Error("an app with the wrong bundle id was accepted")
	}
	if _, _, ok := e.resolveApp([]string{tmp}); ok {
		t.Error("a non-.app path was accepted")
	}
	if b, _, ok := e.resolveApp([]string{ww, wrong, good}); !ok || b != good {
		t.Errorf("resolveApp did not fall through bad candidates to the good one: (%q, %v)", b, ok)
	}
}

// covers: MA-562, MA-578
// Profile validation: a listed, user-owned, non-writable profile with prefs.js
// is accepted; a backup copy absent from profiles.ini is refused as a derived
// profile but accepted when the owner names it; group/world-writable, a missing
// prefs.js and another owner are each refused.
func TestMacValidateProfileMembership(t *testing.T) {
	home := t.TempDir()
	e := newMacEnv("")
	e.homeDir = func() (string, error) { return home, nil }

	appSupport := filepath.Join(home, "Library", "Thunderbird")
	mustMkdir(t, appSupport)
	listed := filepath.Join(appSupport, "Profiles", "abc.default")
	mustMkdir(t, listed)
	writeFile(t, filepath.Join(listed, "prefs.js"), "// prefs")
	writeFile(t, filepath.Join(appSupport, "profiles.ini"),
		"[Profile0]\nName=default\nIsRelative=1\nPath=Profiles/abc.default\nDefault=1\n")

	cp, ok := e.validateProfile(listed, false)
	if !ok {
		t.Fatal("a listed, valid profile was refused")
	}
	if want, _ := filepath.EvalSymlinks(listed); cp != want {
		t.Errorf("canonical profile = %q, want %q", cp, want)
	}

	backup := filepath.Join(home, "backup", "abc.default")
	mustMkdir(t, backup)
	writeFile(t, filepath.Join(backup, "prefs.js"), "// prefs")
	if _, ok := e.validateProfile(backup, false); ok {
		t.Error("an unlisted backup profile was accepted as a derived profile (PC3)")
	}
	if _, ok := e.validateProfile(backup, true); !ok {
		t.Error("a named profile was refused (naming it bypasses membership)")
	}

	ww := filepath.Join(home, "ww.default")
	mustMkdir(t, ww)
	writeFile(t, filepath.Join(ww, "prefs.js"), "x")
	if err := os.Chmod(ww, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.validateProfile(ww, true); ok {
		t.Error("a group/world-writable profile was accepted")
	}

	noprefs := filepath.Join(home, "noprefs")
	mustMkdir(t, noprefs)
	if _, ok := e.validateProfile(noprefs, true); ok {
		t.Error("a profile without prefs.js was accepted")
	}

	e.statUID = func(os.FileInfo) (uint32, bool) { return uint32(e.uid + 1), true }
	if _, ok := e.validateProfile(listed, true); ok {
		t.Error("a profile owned by another user was accepted")
	}
}

// covers: MA-578
// The profiles.ini parser returns each section's Path (resolved by IsRelative)
// and each Install* Default (relative); it strips control characters, caps the
// read, and never panics on a huge file.
func TestParseProfilesINI(t *testing.T) {
	appSupport := t.TempDir()
	rel := filepath.Join(appSupport, "Profiles", "rel.default")
	mustMkdir(t, rel)
	inst := filepath.Join(appSupport, "Profiles", "inst.default")
	mustMkdir(t, inst)
	absDir := filepath.Join(t.TempDir(), "abs.default")
	mustMkdir(t, absDir)

	ini := "[Install123]\nDefault=Profiles/inst.default\nLocked=1\n\n" +
		"[Profile0]\nIsRelative=1\nPath=Profiles/rel.default\nDefault=1\n\n" +
		"[Profile1]\nIsRelative=0\nPath=" + absDir + "\n"
	got := parseProfilesINI([]byte(ini), appSupport)
	for _, want := range []string{rel, inst, absDir} {
		c, _ := filepath.EvalSymlinks(want)
		if !containsStr(got, c) {
			t.Errorf("parseProfilesINI missing %q; got %v", c, got)
		}
	}

	// A control character in a value is stripped (the path still resolves).
	ctl := "[Profile0]\nIsRelative=1\nPath=Profiles/rel\x07.default\n"
	if g := parseProfilesINI([]byte(ctl), appSupport); !containsStr(g, func() string { c, _ := filepath.EvalSymlinks(rel); return c }()) {
		t.Errorf("control char not stripped from a Path value; got %v", g)
	}

	// A huge file is truncated and parsed without panic.
	if g := parseProfilesINI(make([]byte, profilesINIMaxBytes+100), appSupport); len(g) != 0 {
		t.Errorf("a huge all-NUL file yielded %d profiles, want 0", len(g))
	}
}

// covers: MA-562
// resolve end to end on fixtures: not elevated, a valid home-folder bundle, and
// a listed profile -> the plan carries the bundle, its inner executable and the
// canonical profile.
func TestMacResolveHappy(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Applications", "Thunderbird.app")
	mustMkdir(t, filepath.Join(app, "Contents"))
	appSupport := filepath.Join(home, "Library", "Thunderbird")
	mustMkdir(t, appSupport)
	prof := filepath.Join(appSupport, "Profiles", "p.default")
	mustMkdir(t, prof)
	writeFile(t, filepath.Join(prof, "prefs.js"), "x")
	writeFile(t, filepath.Join(appSupport, "profiles.ini"), "[Profile0]\nIsRelative=1\nPath=Profiles/p.default\n")

	e := newMacEnv("")
	e.geteuid = func() int { return 501 }
	e.homeDir = func() (string, error) { return home, nil }
	e.run = func(cmd *exec.Cmd) (string, string, error) {
		last := cmd.Args[len(cmd.Args)-1]
		if strings.HasPrefix(last, app) {
			return "org.mozilla.thunderbird\n", "", nil
		}
		return "org.mozilla.firefox\n", "", nil // a real /Applications/Thunderbird.app, if present, is skipped
	}

	plan, outcome, ok := e.resolve(prof, false)
	if !ok || outcome != OutcomeOK {
		t.Fatalf("resolve = (%v, %q), want ok", ok, outcome.Name)
	}
	if plan.bundle != app {
		t.Errorf("bundle = %q, want %q", plan.bundle, app)
	}
	if plan.exe != filepath.Join(app, "Contents", "MacOS", "thunderbird") {
		t.Errorf("exe = %q", plan.exe)
	}
	if want, _ := filepath.EvalSymlinks(prof); plan.profile != want {
		t.Errorf("profile = %q, want %q", plan.profile, want)
	}
}

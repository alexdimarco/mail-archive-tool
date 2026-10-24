//go:build windows

package presync

import (
	"os"
	"path/filepath"
	"testing"
)

// mkFile writes an empty regular file, creating parents.
func mkFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-562
// resolveWinApp accepts the first candidate that is a regular file named
// thunderbird.exe (case-insensitively), and rejects a missing path, a directory
// of that name, and a differently-named file — the location is the authenticity
// basis (PC-W8), so only "regular file, right name, in a listed location" passes.
func TestResolveWinApp(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "pf", "Mozilla Thunderbird", "thunderbird.exe")
	mkFile(t, good)
	if exe, ok := resolveWinApp([]string{good}); !ok || exe != good {
		t.Errorf("a real thunderbird.exe was not accepted: (%q,%v)", exe, ok)
	}

	// Case-insensitive basename, first-match order.
	mixed := filepath.Join(dir, "pf2", "Thunderbird.EXE")
	mkFile(t, mixed)
	if _, ok := resolveWinApp([]string{mixed}); !ok {
		t.Error("a case-different thunderbird.exe basename was not accepted")
	}

	// A directory named thunderbird.exe is not a regular file.
	asDir := filepath.Join(dir, "d", "thunderbird.exe")
	if err := os.MkdirAll(asDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wrongName := filepath.Join(dir, "w", "firefox.exe")
	mkFile(t, wrongName)
	missing := filepath.Join(dir, "nope", "thunderbird.exe")
	if exe, ok := resolveWinApp([]string{asDir, wrongName, missing}); ok {
		t.Errorf("a non-regular / wrong-name / missing candidate set resolved to %q", exe)
	}

	// First valid candidate wins even when a later one is also valid.
	if exe, _ := resolveWinApp([]string{missing, good, mixed}); exe != good {
		t.Errorf("first-match order broken: got %q, want %q", exe, good)
	}
}

// covers: MA-572
// Elevation refusal (design §5, MA-572): an elevated token ends resolve as
// presync_refused_elevated BEFORE any app/profile resolution, so a Thunderbird is
// never started from an elevated token (it would write owner-unreadable files).
func TestWinResolveRefusesElevated(t *testing.T) {
	appCalled := false
	e := &winEnv{
		w:        fakeWin32{isElev: true},
		appCands: func() []string { appCalled = true; return nil },
	}
	plan, out, ok := e.resolve(`C:\whatever`, false, nil)
	if ok || out != OutcomeRefusedElevated {
		t.Errorf("elevated resolve = (%+v, %v, %v), want refused-elevated", plan, out, ok)
	}
	if appCalled {
		t.Error("app resolution ran despite an elevated token — refusal must come first")
	}
}

// covers: MA-562
// validateProfile accepts a usable directory and rejects the hostile/unusable
// cases (design §5, §4.3): empty or NUL, a non-directory, and a directory missing
// prefs.js. A profile named by the owner (-thunderbird-profile) bypasses the
// profiles.ini membership test (PC3).
func TestWinValidateProfile(t *testing.T) {
	e := &winEnv{roaming: func() (string, bool) { return "", false }} // no membership source

	good := t.TempDir()
	mkFile(t, filepath.Join(good, "prefs.js"))
	if cp, ok := e.validateProfile(good, true); !ok || cp == "" {
		t.Errorf("a named, prefs-bearing profile was not accepted: (%q,%v)", cp, ok)
	}

	if _, ok := e.validateProfile("", true); ok {
		t.Error("an empty profile path was accepted")
	}
	if _, ok := e.validateProfile("a\x00b", true); ok {
		t.Error("a NUL-bearing profile path was accepted")
	}

	noPrefs := t.TempDir()
	if _, ok := e.validateProfile(noPrefs, true); ok {
		t.Error("a profile without prefs.js was accepted")
	}

	notDir := filepath.Join(t.TempDir(), "file")
	mkFile(t, notDir)
	if _, ok := e.validateProfile(notDir, true); ok {
		t.Error("a non-directory profile path was accepted")
	}
}

// covers: MA-578
// profiles.ini membership on Windows (design §4.3, PC3): a derived profile is
// accepted only when it is listed in %APPDATA%\Thunderbird\profiles.ini; one that
// is not listed is refused. Membership reuses the shared parseProfilesINI.
func TestWinProfileMembership(t *testing.T) {
	roaming := t.TempDir()
	tbDir := filepath.Join(roaming, "Thunderbird")
	listed := filepath.Join(tbDir, "Profiles", "abc.default")
	mkFile(t, filepath.Join(listed, "prefs.js"))
	ini := "[Profile0]\nName=default\nIsRelative=1\nPath=Profiles/abc.default\n"
	if err := os.WriteFile(filepath.Join(tbDir, "profiles.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}

	e := &winEnv{roaming: func() (string, bool) { return roaming, true }}

	if cp, ok := e.validateProfile(listed, false); !ok || cp == "" {
		t.Errorf("a profiles.ini-listed profile was not accepted unnamed: (%q,%v)", cp, ok)
	}

	// A real, prefs-bearing profile that profiles.ini does not list is refused.
	unlisted := filepath.Join(roaming, "Thunderbird", "Profiles", "stray.default")
	mkFile(t, filepath.Join(unlisted, "prefs.js"))
	if _, ok := e.validateProfile(unlisted, false); ok {
		t.Error("a profile absent from profiles.ini was accepted unnamed (PC3)")
	}
}

// covers: MA-562
// The full resolve happy path with seams injected: not elevated, a resolvable
// thunderbird.exe, and a named profile → a winPlan carrying the exe and the
// canonical profile, OutcomeOK.
func TestWinResolveHappyPath(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "Mozilla Thunderbird", "thunderbird.exe")
	mkFile(t, exe)
	prof := t.TempDir()
	mkFile(t, filepath.Join(prof, "prefs.js"))

	e := &winEnv{
		w:        fakeWin32{isElev: false},
		appCands: func() []string { return []string{exe} },
		roaming:  func() (string, bool) { return "", false },
	}
	plan, out, ok := e.resolve(prof, true, nil)
	if !ok || out != OutcomeOK {
		t.Fatalf("resolve = (%+v, %v, %v), want OK", plan, out, ok)
	}
	if plan.exe != exe {
		t.Errorf("plan.exe = %q, want %q", plan.exe, exe)
	}
	if cp, _ := filepath.EvalSymlinks(prof); plan.profile != cp {
		t.Errorf("plan.profile = %q, want canonical %q", plan.profile, cp)
	}
}

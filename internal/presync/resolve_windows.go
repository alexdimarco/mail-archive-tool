//go:build windows

package presync

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// This file is the Windows S1 planning (design §5, §4.3, PC-W8): it refuses an
// elevated token, resolves thunderbird.exe from an admin-owned known-folder
// location, and validates the profile, before any session starts. On any failure
// it returns an Outcome to RECORD without starting a session and without running
// Thunderbird — the archive still runs. It takes no setting or path from the
// archive.
//
// Authenticity is a stated REDUCTION from macOS (PC-W8, DC4): there is no
// portable per-file writability/ACL or publisher check in v1, so the app is
// verified by LOCATION only — the machine-scope, admin-owned Program Files,
// resolved via SHGetKnownFolderPath (never the attacker-influenceable
// %ProgramFiles% environment variable), under the same-user-out-of-scope threat
// model. Q-W3 reopens whether an Authenticode publisher check is wanted.

const thunderbirdExeName = "thunderbird.exe"

// winPlan is what S1 resolution produced: the resolved thunderbird.exe (matched
// against a running process's image path) and the canonical profile.
type winPlan struct {
	exe     string
	profile string
}

// resolve plans the session (design §5). `named` is true when the owner gave the
// profile with -thunderbird-profile, which — and only which — bypasses the
// profiles.ini membership test (PC3). It returns the plan, or an Outcome to
// record WITHOUT running any helper.
func (e *winEnv) resolve(profile string, named bool, inputs []string) (winPlan, Outcome, bool) {
	// §5 / MA-572: never run from an elevated token — a Thunderbird started
	// elevated would create profile files the owner could not then open. Refuse
	// before anything else runs.
	if e.w.elevated() {
		return winPlan{}, OutcomeRefusedElevated, false
	}
	exe, ok := resolveWinApp(e.appCands())
	if !ok {
		return winPlan{}, OutcomeAppNotFound, false
	}
	if profile == "" && !named {
		// No -thunderbird-profile given: derive the one profiles.ini-listed profile
		// that contains the archive's inputs (§4.3). A derived profile is already
		// listed, so it is treated as named (membership need not be re-checked).
		if d, dok := deriveProfile(inputs, e.listedProfiles()); dok {
			profile, named = d, true
		}
	}
	cprofile, ok := e.validateProfile(profile, named)
	if !ok {
		return winPlan{}, OutcomeProfileUnusable, false
	}
	return winPlan{exe: exe, profile: cprofile}, OutcomeOK, true
}

// realAppCandidates are the fixed v1 locations, in order (design §5, PC-W8): the
// machine-scope Program Files and Program Files (x86), then the per-user install
// under the local AppData Programs folder (FOLDERID_LocalAppData; same user
// already out of scope) — each resolved via SHGetKnownFolderPath, never the
// %ProgramFiles% environment variable. This reads an INSTALL location only; the
// pre-sync writes no state here (its state dir is the parent's pinned non-roaming
// helper, wired in run_windows.go). No -thunderbird-app in v1 (PC22).
func realAppCandidates() []string {
	var cands []string
	add := func(id *windows.KNOWNFOLDERID, parts ...string) {
		base, err := windows.KnownFolderPath(id, 0)
		if err != nil || base == "" {
			return
		}
		cands = append(cands, filepath.Join(append([]string{base}, parts...)...))
	}
	add(windows.FOLDERID_ProgramFiles, "Mozilla Thunderbird", thunderbirdExeName)
	add(windows.FOLDERID_ProgramFilesX86, "Mozilla Thunderbird", thunderbirdExeName)
	add(windows.FOLDERID_LocalAppData, "Programs", "Mozilla Thunderbird", thunderbirdExeName)
	return cands
}

// realRoamingAppData returns %APPDATA% (FOLDERID_RoamingAppData), the base for
// Thunderbird's profiles.ini.
func realRoamingAppData() (string, bool) {
	p, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil || p == "" {
		return "", false
	}
	return p, true
}

// resolveWinApp accepts the first candidate that is a regular file named
// thunderbird.exe. There is no portable per-file ACL/writability or publisher
// check in v1 (PC-W8): the location — an admin-owned known folder — is the
// authenticity basis under the same-user-out-of-scope threat model.
func resolveWinApp(candidates []string) (string, bool) {
	for _, exe := range candidates {
		if !eqFold(filepath.Base(exe), thunderbirdExeName) {
			continue
		}
		fi, err := os.Stat(exe)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		return exe, true
	}
	return "", false
}

// validateProfile canonicalises the profile and checks it is usable (design §5,
// §4.3): a directory containing prefs.js as a regular file; and — unless the
// owner named it — listed in Thunderbird's profiles.ini (PC3), so a backup, an
// old copy or an inherited profile is never started. Ownership and mode are NOT
// checked on Windows (no portable per-file API; stated reduction, PC-W8); the
// profile's parent, %APPDATA%, is itself the user's ACL-scoped directory.
func (e *winEnv) validateProfile(profile string, named bool) (string, bool) {
	if profile == "" || strings.IndexByte(profile, 0) >= 0 {
		return "", false
	}
	cpath, err := filepath.EvalSymlinks(profile)
	if err != nil {
		return "", false
	}
	fi, err := os.Stat(cpath)
	if err != nil || !fi.IsDir() {
		return "", false
	}
	if pj, err := os.Stat(filepath.Join(cpath, "prefs.js")); err != nil || !pj.Mode().IsRegular() {
		return "", false
	}
	if named {
		return cpath, true // the owner consented by typing the path; bypasses membership
	}
	for _, listed := range e.listedProfiles() {
		if listed == cpath {
			return cpath, true
		}
	}
	return "", false // a derived profile Thunderbird does not list (PC3)
}

// listedProfiles reads and parses %APPDATA%\Thunderbird\profiles.ini (the Windows
// roaming application-data location), bounded and untrusted — shared with macOS
// via parseProfilesINI (resolve.go).
func (e *winEnv) listedProfiles() []string {
	roaming, ok := e.roaming()
	if !ok {
		return nil
	}
	appData := filepath.Join(roaming, "Thunderbird")
	data, err := os.ReadFile(filepath.Join(appData, "profiles.ini"))
	if err != nil {
		return nil
	}
	return parseProfilesINI(data, appData)
}

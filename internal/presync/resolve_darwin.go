//go:build darwin

package presync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// This file is the macOS S1 planning (design §4.5 S1, §4.7.1, §4.3, §5.2): it
// refuses elevation, resolves and validates the Thunderbird .app, and validates
// the profile, before any session starts. On any failure it returns an Outcome
// to RECORD without starting a session and without running Thunderbird — the
// archive, as ever, still runs. It takes no setting or path from the archive.

// thunderbirdBundleID is the CFBundleIdentifier the macOS app resolution
// requires (parseProfilesINI and its cap live in the shared resolve.go).
const thunderbirdBundleID = "org.mozilla.thunderbird"

// macPlan is what S1 resolution produced: the validated .app bundle (for the
// launcher), its inner executable (matched against `ps -o comm=`), and the
// canonical profile.
type macPlan struct {
	bundle  string
	exe     string
	profile string
}

// resolve plans the session (design §4.5 S1). `named` is true when the owner
// gave the profile explicitly with -thunderbird-profile, which — and only which
// — bypasses the profiles.ini membership test (PC3). It returns the plan, or an
// Outcome to record WITHOUT running any helper.
func (e *macEnv) resolve(profile string, named bool, inputs []string) (macPlan, Outcome, bool) {
	// §5.4 rule 4 / MA-572: never run elevated — Thunderbird would create
	// profile files the owner could not then open. Refuse before any helper.
	if e.geteuid() == 0 {
		return macPlan{}, OutcomeRefusedElevated, false
	}
	bundle, exe, ok := e.resolveApp(e.appCandidates())
	if !ok {
		return macPlan{}, OutcomeAppNotFound, false
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
		return macPlan{}, OutcomeProfileUnusable, false
	}
	return macPlan{bundle: bundle, exe: exe, profile: cprofile}, OutcomeOK, true
}

// appCandidates are the fixed v1 locations, in order (design §4.7.1): the system
// Applications folder and the user's own (home from os.UserHomeDir, never $HOME
// text from elsewhere). No -thunderbird-app in v1 (PC22).
func (e *macEnv) appCandidates() []string {
	cands := []string{"/Applications/Thunderbird.app"}
	if home, err := e.homeDir(); err == nil && home != "" {
		cands = append(cands, filepath.Join(home, "Applications", "Thunderbird.app"))
	}
	return cands
}

// resolveApp accepts the first candidate that is a .app directory, is not
// world-writable, and whose CFBundleIdentifier (read via plutil) is
// Thunderbird's; it returns the bundle and its inner executable.
func (e *macEnv) resolveApp(candidates []string) (bundle, exe string, ok bool) {
	for _, app := range candidates {
		if !strings.HasSuffix(app, ".app") {
			continue
		}
		fi, err := os.Stat(app)
		if err != nil || !fi.IsDir() {
			continue
		}
		if fi.Mode().Perm()&0o002 != 0 { // world-writable
			continue
		}
		if !e.bundleIDIs(app, thunderbirdBundleID) {
			continue
		}
		return app, filepath.Join(app, "Contents", "MacOS", "thunderbird"), true
	}
	return "", "", false
}

// bundleIDIs reads the app's CFBundleIdentifier with plutil and reports whether
// it equals want. plutil reads a binary or text plist correctly (PC17); a byte
// search of the file would wrongly refuse a binary plist.
func (e *macEnv) bundleIDIs(app, want string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, _, err := e.run(plutilBundleIDCmd(ctx, app))
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == want
}

// validateProfile canonicalises the profile and checks it is usable (design §4.3,
// §5.2): a directory the current user owns, neither group- nor world-writable,
// containing prefs.js as a regular file; and — unless the owner named it — listed
// in Thunderbird's profiles.ini (PC3), so a backup, an old copy, a mounted disk
// image or an inherited profile is never started.
func (e *macEnv) validateProfile(profile string, named bool) (string, bool) {
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
	if fi.Mode().Perm()&0o022 != 0 { // group- or world-writable
		return "", false
	}
	if !e.ownedByUser(fi) {
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

func (e *macEnv) ownedByUser(fi os.FileInfo) bool {
	uid, ok := e.statUID(fi)
	return ok && int(uid) == e.uid
}

// listedProfiles reads and parses ~/Library/Thunderbird/profiles.ini (the macOS
// application-support location), bounded and untrusted.
func (e *macEnv) listedProfiles() []string {
	home, err := e.homeDir()
	if err != nil || home == "" {
		return nil
	}
	appSupport := filepath.Join(home, "Library", "Thunderbird")
	data, err := os.ReadFile(filepath.Join(appSupport, "profiles.ini"))
	if err != nil {
		return nil
	}
	return parseProfilesINI(data, appSupport)
}

// statUIDDarwin reads a file's owning uid from the Unix stat.
func statUIDDarwin(fi os.FileInfo) (uint32, bool) {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Uid, true
	}
	return 0, false
}

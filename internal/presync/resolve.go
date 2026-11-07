package presync

import (
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/thunderbird"
)

// This file holds the build-tag-free parts of S1 resolution shared by every
// adapter: the profiles.ini parser and its control-stripping, so the PC3
// membership test is written and reviewed once and the Windows resolver
// (resolve_windows.go) reuses it identically to macOS (resolve_darwin.go).

// profilesINIMaxBytes bounds the untrusted profiles.ini read (design §4.3).
const profilesINIMaxBytes = 256 * 1024

// deriveProfile picks the Thunderbird profile to pre-sync when the caller gave no
// explicit -thunderbird-profile (api.go, §4.3): the single profiles.ini-listed
// profile that CONTAINS one of the archive's inputs. Exactly one must match — zero
// (no input lies inside a listed profile) or more than one (ambiguous) yields no
// derivation, so the pre-sync fails safe (presync_profile_unusable) rather than
// sync a guessed profile. `listed` are canonical paths (from listedProfiles); each
// input is canonicalised here before the containment test.
func deriveProfile(inputs, listed []string) (string, bool) {
	match := map[string]bool{}
	for _, in := range inputs {
		cin, err := filepath.EvalSymlinks(in)
		if err != nil {
			continue
		}
		for _, prof := range listed {
			if underDir(prof, cin) {
				match[prof] = true
			}
		}
	}
	if len(match) == 1 {
		for p := range match {
			return p, true
		}
	}
	return "", false
}

// parseProfilesINI returns the canonical profile paths Thunderbird lists: each
// section's Path= (resolved against appSupport when IsRelative=1) and each
// Install* section's Default= (always relative). It is read capped and as
// untrusted text — values are control-stripped and used only to compare with a
// derived profile, never as a path to open or execute (PC3).
//
// The parsing is the shared thunderbird.ParseProfilesINI (design-restore-thunderbird
// §3.1, slice R2); presync keeps its own cap (a longer file is cut, not refused) and
// its own meaning of each value, and does not use the parser's other outputs (names,
// the Default=1 flag). Within a section, each naming Default= comes first, in file
// order, then the section's Path=, as presync has always listed them.
func parseProfilesINI(data []byte, appSupport string) []string {
	if len(data) > profilesINIMaxBytes {
		data = data[:profilesINIMaxBytes]
	}
	var profiles []string
	add := func(p string, rel bool) {
		if p == "" {
			return
		}
		if rel {
			p = filepath.Join(appSupport, filepath.FromSlash(p))
		}
		if cp, err := filepath.EvalSymlinks(p); err == nil {
			profiles = append(profiles, cp)
		} else {
			profiles = append(profiles, filepath.Clean(p))
		}
	}
	for _, s := range thunderbird.ParseProfilesINI(data) {
		// An Install* section's Default is a relative path to a profile; a Profile
		// section's Default=0/1 is a flag, not a path (the parser keeps only the former).
		for _, d := range s.Defaults {
			add(d, true)
		}
		add(s.Path, s.IsRelative)
	}
	return profiles
}

// stripControl removes ASCII and C1 control characters, so untrusted
// profiles.ini bytes cannot forge a path or a log line. Presync's own parser used
// it; it is kept, unchanged, as the oracle the shared parser is held to
// (ma1084_shared_parser_test.go).
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

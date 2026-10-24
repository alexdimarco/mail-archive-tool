package presync

import (
	"path/filepath"
	"strings"
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

	var curPath string
	var curRel, inSection bool
	flush := func() {
		if inSection {
			add(curPath, curRel)
		}
		curPath, curRel = "", false
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(stripControl(raw))
		if strings.HasPrefix(line, "[") {
			flush()
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Path":
			curPath = v
		case "IsRelative":
			curRel = v == "1"
		case "Default":
			// An Install* section's Default is a relative path to a profile; a
			// Profile section's Default=0/1 is a flag, not a path.
			if v != "" && v != "0" && v != "1" {
				add(v, true)
			}
		}
	}
	flush()
	return profiles
}

// stripControl removes ASCII and C1 control characters, so untrusted
// profiles.ini bytes cannot forge a path or a log line.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

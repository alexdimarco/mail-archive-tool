package thunderbird

import "fmt"

// panoramaPref is the setting that switches a profile to Thunderbird's newer storage,
// "Panorama" (mail/app/StaticPrefList.yaml, ESR 153 lines 43-46: a boolean, default false, read
// once at start-up). With it on, Thunderbird neither creates nor writes folderCache.json
// (nsMsgFolderCache.cpp, ESR 153 lines 182-187), so whatever folderCache.json is left says nothing
// about now (completeness design §2.3's TC-3 fold).
const panoramaPref = "mail.panorama.enabled"

// PanoramaEnabled reports whether the profile has mail.panorama.enabled true, as Thunderbird
// applies it: prefs.js, then user.js over it, read through the same reader as the account match
// (util.ReadRegularFile with the 16 MB cap; the allow-list of the lines Thunderbird writes). The
// error is non-nil when MailArchive cannot tell (completeness slice C1a reads it as "can't tell"):
//
//   - prefs.js or user.js cannot be read (missing prefs.js, a link, a pipe, over the cap);
//   - either file has a line MailArchive cannot read with certainty, anywhere in it;
//   - any statement of the setting, in either file, is not true or false. Thunderbird drops a user
//     value whose type differs from the setting's (Preferences.cpp's Pref::SetUserValue, as
//     wrongType's comment says), so the last statement would not be the one it applies. The check
//     is made here, for this one setting, and not added to wrongType: the account match's reading
//     of a profile is not changed by it.
//
// With no statement of the setting, Thunderbird's default (false) applies.
func PanoramaEnabled(profileDir string) (bool, error) {
	s, _, err := readPrefs(profileDir)
	if err != nil {
		return false, fmt.Errorf("MailArchive cannot read Thunderbird's settings, so it cannot tell whether %s is on: %w", panoramaPref, err)
	}
	if file, line, what := s.uncertain(); file != "" {
		return false, fmt.Errorf("%s line %d %s, so MailArchive cannot tell whether %s is on", file, line, what, panoramaPref)
	}
	for _, f := range []struct {
		name string
		t    *prefsText
	}{{"prefs.js", s.prefs}, {"user.js", s.user}} {
		for _, st := range f.t.stmts {
			if st.key == panoramaPref && st.value != "true" && st.value != "false" {
				return false, fmt.Errorf("%s line %d %s, so MailArchive cannot tell whether %s is on", f.name, st.line+1, whatNotBool, panoramaPref)
			}
		}
	}
	return s.vals[panoramaPref] == "true", nil
}

// LastVersionMajor is the major number of the Thunderbird that last opened the profile, from its
// compatibility.ini LastVersion (read through util.ReadRegularFile, 64 KB, as FindProfileDir
// reads it); ok is false when it cannot be read. It is the version the fix-it words follow
// (completeness design §6's L5 fold; offline_words.go's Place).
func LastVersionMajor(profileDir string) (int, bool) { return lastVersionMajor(profileDir) }

package thunderbird

import "strings"

// Thunderbird's profiles.ini, read once for every caller (docs/design-restore-thunderbird.md
// §3.1, slice R2): presync's profile check and the restore's profile list share this
// parser. It reads the file as untrusted text and resolves nothing: what a value means
// (a path relative to which folder, whether an absolute [Install…] default is taken as
// absolute, how a name is shown) is each caller's own rule.

// ProfilesINIMaxBytes is the most of profiles.ini a caller reads. Presync cuts the file
// there; the restore refuses a larger one (W-PROFINI, slice R3).
const ProfilesINIMaxBytes = 256 * 1024

// ProfilesSection is one [section] of profiles.ini, as written. Every value has its
// control characters removed and its spaces trimmed; none is resolved or opened.
type ProfilesSection struct {
	Header     string // the text between the brackets: "Profile0", "Install308046B0AF4A39CB", "General"
	Name       string // the last Name=
	Path       string // the last Path=
	IsRelative bool   // the last IsRelative= is "1"
	Default    string // the last Default= (in a [Profile…] section, the flag "1" or "0")
	// Defaults is every Default= that names a profile (not empty, "0" or "1"), in file
	// order: an [Install…] section's, which names the profile Thunderbird opens.
	Defaults []string
}

// IsInstall says the section is an [Install…] section.
func (s ProfilesSection) IsInstall() bool { return strings.HasPrefix(s.Header, "Install") }

// DefaultFlag says a [Profile…] section is marked Default=1.
func (s ProfilesSection) DefaultFlag() bool { return s.Default == "1" }

// ParseProfilesINI returns profiles.ini's sections in file order. Control characters are
// removed from every line before anything is read from it, so a value cannot forge a
// path or a log line; a line starting with "[" begins a section; lines before the first
// section, and lines with no "=", are ignored. It reads all of data: the caller bounds it.
func ParseProfilesINI(data []byte) []ProfilesSection {
	var out []ProfilesSection
	var cur *ProfilesSection
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(StripControl(raw))
		if strings.HasPrefix(line, "[") {
			header := strings.TrimPrefix(line, "[")
			if i := strings.IndexByte(header, ']'); i >= 0 {
				header = header[:i]
			}
			out = append(out, ProfilesSection{Header: strings.TrimSpace(header)})
			cur = &out[len(out)-1]
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Name":
			cur.Name = v
		case "Path":
			cur.Path = v
		case "IsRelative":
			cur.IsRelative = v == "1"
		case "Default":
			cur.Default = v
			if v != "" && v != "0" && v != "1" {
				cur.Defaults = append(cur.Defaults, v)
			}
		}
	}
	return out
}

// StripControl removes ASCII and C1 control characters (C0, DEL, U+0080..U+009F), so
// untrusted text cannot forge a path or a log line.
func StripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}

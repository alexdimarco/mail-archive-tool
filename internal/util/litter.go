package util

import (
	"regexp"
	"strings"
)

// litter are the names operating systems drop into folders and drive roots, per OS
// (design-encrypted-archives-phase3 §4.5, extended by the P3-0 adversarial pass,
// CRIT-04); every case ignores them. Matched case-insensitively.
var litter = map[string]bool{
	// macOS
	".ds_store": true, ".spotlight-v100": true, ".fseventsd": true, ".trashes": true, ".temporaryitems": true,
	".documentrevisions-v100": true, ".volumeicon.icns": true, ".apdisk": true, ".localized": true, "icon\r": true,
	".com.apple.timemachine.donotpresent": true,
	// Windows
	"thumbs.db": true, "desktop.ini": true, "$recycle.bin": true, "system volume information": true,
	// Linux
	"lost+found": true,
}

// litterPrefixes are litter names that carry a suffix: AppleDouble "._*", the Linux
// desktop trash ".Trash-<uid>", macOS installer sandboxes; and Windows chkdsk's
// "found.NNN".
var (
	litterPrefixes = []string{"._", ".trash-", ".pkinstallsandboxmanager"}
	foundRe        = regexp.MustCompile(`^found\.[0-9]{3}$`)
)

// IsLitter reports an operating system's own file: a folder holding only these is
// empty to a person.
func IsLitter(name string) bool {
	low := strings.ToLower(name)
	if litter[low] || foundRe.MatchString(low) {
		return true
	}
	for _, p := range litterPrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

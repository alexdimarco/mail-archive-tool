//go:build windows

package tbengine

import (
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// otherSIDs are the accounts beyond this one a Windows folder commonly lets read (R7-01
// independent F5): Everyone, Authenticated Users (a second drive's real default), Users,
// Guests. Compared by SID, never by name: names are translated on a non-English Windows.
var otherSIDs = map[string]string{
	"S-1-1-0":      "Everyone",
	"S-1-5-11":     "Authenticated Users",
	"S-1-5-32-545": "Users",
	"S-1-5-32-546": "Guests",
}

// othersOnWindows reads path's access list and names each entry allowing one of
// otherSIDs; a missing access list (everyone allowed everything) is named too.
func othersOnWindows(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read the access list of %s: %v", path, err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return fmt.Sprintf("%s has no access list (everyone allowed): %v", path, err)
	}
	out := ""
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatalf("read entry %d of %s: %v", i, path, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if name, ok := otherSIDs[sid]; ok {
			out += fmt.Sprintf("%s (%s) allowed %#x; ", name, sid, ace.Mask)
		}
	}
	if out != "" {
		return path + ": " + out + sd.String()
	}
	return ""
}

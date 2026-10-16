//go:build windows

package app

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows: the destination's DACL is run through the inheritance algorithm for a
// new directory (the export root) and a new file below it (inheritedACEs, the same
// result CreatePrivateObjectSecurityEx gives), and the line names every principal
// other than the user, SYSTEM and Administrators that gets read access. A NULL DACL
// grants everyone full access and is named so; a descriptor that cannot be read is
// the unknown posture, never "only you".

const (
	aceObjectInherit    = 0x1
	aceContainerInherit = 0x2
	aceNoPropagate      = 0x4
	aceInheritOnly      = 0x8

	maskFileReadData  = 0x1
	maskGenericRead   = 0x80000000
	maskGenericAll    = 0x10000000
	maskMaximumAllowd = 0x02000000
)

// daclEntries reads a descriptor's DACL as the posture's model. everyoneIfNull is
// the entry a NULL DACL stands for.
func daclEntries(sd *windows.SECURITY_DESCRIPTOR) ([]ceACE, bool) {
	dacl, _, err := sd.DACL()
	if err != nil {
		return nil, false
	}
	if dacl == nil { // a NULL DACL: everyone, full access
		return []ceACE{{Allow: true, SID: "S-1-1-0", Who: "Everyone", Read: true, OI: true, CI: true}}, true
	}
	var out []ceACE
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return nil, false
		}
		kind := ace.Header.AceType
		if kind != windows.ACCESS_ALLOWED_ACE_TYPE && kind != windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		who := sid.String()
		if account, domain, _, err := sid.LookupAccount(""); err == nil {
			who = domain + `\` + account
			if domain == "" {
				who = account
			}
		}
		f := ace.Header.AceFlags
		out = append(out, ceACE{
			Allow: kind == windows.ACCESS_ALLOWED_ACE_TYPE, SID: sid.String(), Who: who,
			Read: ace.Mask&(maskFileReadData|maskGenericRead|maskGenericAll|maskMaximumAllowd) != 0,
			OI:   f&aceObjectInherit != 0, CI: f&aceContainerInherit != 0, IO: f&aceInheritOnly != 0, NP: f&aceNoPropagate != 0,
		})
	}
	return out, true
}

func meSID() string {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return ""
	}
	return tu.User.Sid.String()
}

func osInheritedPosture(dir string) CopyPosture {
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return newPosture(postureKindUnknown, nil)
	}
	aces, ok := daclEntries(sd)
	return windowsPosture(aces, meSID(), ok)
}

func osRealizedPosture(path string, isDir bool) CopyPosture {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return newPosture(postureKindUnknown, nil)
	}
	aces, ok := daclEntries(sd)
	return windowsRealized(aces, meSID(), ok)
}

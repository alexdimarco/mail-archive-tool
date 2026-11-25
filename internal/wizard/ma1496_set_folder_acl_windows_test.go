//go:build windows

package wizard

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// setFolderACLWhy says why the folder at p is not as a set leaves a new folder on Windows
// (restrictSetFolder, winsec.RestrictFolderToOwnerAndAdmins; round D): this user its owner, its access
// list protected (nothing inherited from the parent) and holding exactly three allow entries, full
// control inherited by files and folders, for this user, SYSTEM and Administrators, in that order. ""
// when it is. Read by the SIDs themselves, never the access list's text (which writes the built-in
// Administrator's SID as "LA").
func setFolderACLWhy(t *testing.T, p string) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Sprintf("its access list cannot be read: %v", err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.Equals(tu.User.Sid) {
		return fmt.Sprintf("its owner is %v, not this user %s (%v)", owner, tu.User.Sid, err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Sprintf("its access list is not protected from its parent's (%v)", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return fmt.Sprintf("it has no access list (%v)", err)
	}
	type entry struct {
		kind, flags uint8
		mask        uint32
		sid         string
	}
	var got []entry
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Sprintf("entry %d cannot be read: %v", i, err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		got = append(got, entry{ace.Header.AceType, ace.Header.AceFlags, uint32(ace.Mask), sid.String()})
	}
	const fullControl, oici = 0x1F01FF, windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	want := []entry{
		{windows.ACCESS_ALLOWED_ACE_TYPE, oici, fullControl, tu.User.Sid.String()},
		{windows.ACCESS_ALLOWED_ACE_TYPE, oici, fullControl, system.String()},
		{windows.ACCESS_ALLOWED_ACE_TYPE, oici, fullControl, admins.String()},
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Sprintf("its entries are %+v, want %+v", got, want)
	}
	return ""
}

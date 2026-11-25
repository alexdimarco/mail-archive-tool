//go:build windows

package winsec

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// aclEntry is one entry of a folder's access list, as Windows keeps it.
type aclEntry struct {
	kind  uint8  // ACCESS_ALLOWED_ACE_TYPE, ACCESS_DENIED_ACE_TYPE, …
	flags uint8  // OBJECT_INHERIT_ACE, CONTAINER_INHERIT_ACE, INHERITED_ACE, …
	mask  uint32 // the rights
	sid   string
}

// folderACL reads path's owner, whether its access list is protected (nothing inherited from its
// parent), and its entries in order, by the SIDs themselves (never the SDDL text, which writes the
// built-in Administrator's SID as "LA").
func folderACL(t *testing.T, path string) (owner string, protected bool, entries []aclEntry) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	o, _, err := sd.Owner()
	if err != nil || o == nil {
		t.Fatalf("the owner of %s: %v", path, err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		t.Fatalf("the access list of %s: %v", path, err)
	}
	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		entries = append(entries, aclEntry{ace.Header.AceType, ace.Header.AceFlags, uint32(ace.Mask), sid.String()})
	}
	return o.String(), control&windows.SE_DACL_PROTECTED != 0, entries
}

// covers: MA-1496
// RestrictFolderToOwnerAndAdmins on a real folder (round D, the lead's call revising the review's
// RV-05 fold; the Windows lab of MA-1503 read a set's folder with `icacls` and found the user alone,
// no SYSTEM and no Administrators). Positive twin first: RestrictFolderToOwner, unchanged for its
// other callers, gives this user alone. Then the new folder: this user its owner, its access list
// protected (nothing inherited from its parent), and exactly three allow entries, full control
// inherited by files and folders, for this user, SYSTEM and Administrators, in that order; a folder
// made in it inherits the same three, and nothing else.
func TestAFolderForTheOwnerKeepsSystemAndAdministrators(t *testing.T) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	user := tu.User.Sid.String()
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	const fullControl = 0x1F01FF // FA
	const oici = windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE
	allow := func(flags uint8, sid string) aclEntry {
		return aclEntry{windows.ACCESS_ALLOWED_ACE_TYPE, flags, fullControl, sid}
	}

	// Positive twin: the owner-only folder, as before.
	only := filepath.Join(t.TempDir(), "restored")
	if err := os.Mkdir(only, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RestrictFolderToOwner(only); err != nil {
		t.Fatal(err)
	}
	owner, protected, entries := folderACL(t, only)
	if owner != user || !protected || !reflect.DeepEqual(entries, []aclEntry{allow(oici, user)}) {
		t.Fatalf("positive twin: RestrictFolderToOwner gives this user alone: owner %s, protected %v, %+v", owner, protected, entries)
	}

	dir := filepath.Join(t.TempDir(), "qzsam@x.example")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := RestrictFolderToOwnerAndAdmins(dir); err != nil {
		t.Fatal(err)
	}
	owner, protected, entries = folderACL(t, dir)
	want := []aclEntry{allow(oici, user), allow(oici, system.String()), allow(oici, admins.String())}
	if owner != user || !protected || !reflect.DeepEqual(assure.Reached(t, entries, "the folder's entries"), want) {
		t.Errorf("the set folder: owner %s (want %s), protected %v, entries %+v, want %+v", owner, user, protected, entries, want)
	}

	kid := filepath.Join(dir, "attachments")
	if err := os.Mkdir(kid, 0o700); err != nil {
		t.Fatal(err)
	}
	_, _, inherited := folderACL(t, kid)
	inh := uint8(oici | windows.INHERITED_ACE)
	if want := []aclEntry{allow(inh, user), allow(inh, system.String()), allow(inh, admins.String())}; !reflect.DeepEqual(inherited, want) {
		t.Errorf("a folder made in it: %+v, want %+v", inherited, want)
	}
}

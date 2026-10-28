package winsec

import (
	"testing"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-854, R37
// A secret file owned by and readable only by the built-in Administrator (RID 500) is
// that user's own when the user is that Administrator (the P3-8 fixes' pass,
// WINACL-1): Windows writes the account as "LA" in the access list, and the check
// resolved only the user's full SID, so it refused every password or recovery-key
// file that user had. The alias is resolved by this computer's own lookup; the access
// lists are made from text, so this runs under any account. Positive twin: the same
// file for another user is still refused (it is not theirs), and a group's alias is
// never the user.
func TestTheBuiltInAdministratorsOwnSecretFileIsTheirs(t *testing.T) {
	la, err := windows.StringToSid("LA")
	if err != nil {
		t.Fatal(err)
	}
	admin := la.String()
	sd, err := windows.SecurityDescriptorFromString("O:LAD:PAI(A;;FA;;;LA)(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the access list reads %s", sd.String())
	const other = "S-1-5-21-1-2-3-1001"
	if checkSecretSD(sd, other) == nil {
		t.Fatal("positive twin: the Administrator's file is not another user's")
	}
	isAdmin := UserEntry(admin)
	if isAdmin("BA") || isAdmin("BU") || isAdmin("WD") || UserEntry(other)("LA") || !isAdmin(admin) {
		t.Fatal("positive twin: a group's alias or another SID taken for the user")
	}
	assure.Reached(t, isAdmin("LA") && checkSecretSD(sd, admin) == nil,
		"the built-in Administrator's own file is theirs alone")
}

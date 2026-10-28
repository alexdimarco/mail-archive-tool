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
	if checkSecretSD(sd, other, "") == nil {
		t.Fatal("positive twin: the Administrator's file is not another user's")
	}
	isAdmin := UserEntry(admin)
	if isAdmin("BA") || isAdmin("BU") || isAdmin("WD") || UserEntry(other)("LA") || !isAdmin(admin) {
		t.Fatal("positive twin: a group's alias or another SID taken for the user")
	}
	assure.Reached(t, isAdmin("LA") && checkSecretSD(sd, admin, "") == nil,
		"the built-in Administrator's own file is theirs alone")
}

// covers: MA-855, R37
// An administrator running elevated makes files Administrators own, so their own
// password and recovery-key files are owned by the token's default owner, not by them
// (P3-8's CI run on windows-latest): such a file is theirs when that is this token's
// default owner, and still refused if another account can read it. A file another
// account owns is still refused, and Administrators is never an owner for a token
// whose default owner is the user.
func TestAnElevatedAdministratorsOwnSecretFileIsTheirs(t *testing.T) {
	const user = "S-1-5-21-1-2-3-1001"
	ba, err := windows.StringToSid("BA")
	if err != nil {
		t.Fatal(err)
	}
	admins := ba.String()
	mine, err := windows.SecurityDescriptorFromString("O:BAD:PAI(A;;FA;;;BA)(A;;FA;;;SY)(A;;FA;;;" + user + ")")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := windows.SecurityDescriptorFromString("O:BAD:PAI(A;;FA;;;BA)(A;;FR;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := windows.SecurityDescriptorFromString("O:S-1-5-21-1-2-3-2002D:PAI(A;;FA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	if checkSecretSD(mine, user, user) == nil {
		t.Fatal("positive twin: a non-elevated token's file owned by Administrators is not the user's")
	}
	assure.Reached(t, checkSecretSD(mine, user, admins) == nil && checkSecretSD(shared, user, admins) != nil &&
		checkSecretSD(foreign, user, admins) != nil,
		"an elevated administrator's own file is theirs; one others can read, or another's, is not")
}

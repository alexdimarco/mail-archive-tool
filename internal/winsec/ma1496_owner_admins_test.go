package winsec

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1496
// The access list a per-mailbox archive's folder is given on Windows (round D, the lead's call
// revising the review's RV-05 fold: the Windows lab of MA-1503 found a set's folder listing the user
// alone, so backup software that runs as SYSTEM could not copy it). It is protected (nothing inherited
// from the parent) and gives full control, inherited by what is made in the folder, to the user,
// SYSTEM and Administrators, and to no one else. Positive twin of the checks first: the same list
// with Users (BU) granted too is one others can change and read. Then the folder's own list: no other
// account can change it (DACLWritableByOthers false, the check setChildMine makes, so INS-01 stays
// fixed) nor read it (DACLReadableByOthers false). The read-back from a real folder is the Windows
// test's (TestAFolderForTheOwnerKeepsSystemAndAdministrators, the lab).
func TestASetFolderIsTheOwnersAndKeepsSystemAndAdministrators(t *testing.T) {
	const user = "S-1-5-21-1-2-3-1001"
	got := ownerAndAdminsDACL(user)
	wider := got + "(A;OICI;FA;;;BU)"
	if !DACLWritableByOthers(wider, user) || !DACLReadableByOthers(wider, user) {
		t.Fatalf("positive twin: a list that also grants Users is not seen as others': %s", wider)
	}
	want := "D:P(A;OICI;FA;;;" + user + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	if got != want {
		t.Fatalf("the set folder's access list:\n got %s\nwant %s", got, want)
	}
	aces, torn := splitACEs(strings.TrimPrefix(got, "D:P"))
	if torn || len(assure.Reached(t, aces, "the list's entries")) != 3 {
		t.Fatalf("the list's entries: %q (torn %v)", aces, torn)
	}
	if DACLWritableByOthers(got, user) || DACLReadableByOthers(got, user) {
		t.Errorf("the set folder's list lets another account change or read it: %s", got)
	}
}

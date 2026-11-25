package winsec

// ownerAndAdminsDACL is the access list RestrictFolderToOwnerAndAdmins gives a folder, in SDDL, for
// the user whose SID is user: protected (P: nothing inherited from the folder's parent), with full
// control (FA) for the user, SYSTEM (SY) and the Administrators group (BA), each inherited by the
// files and folders made in it (OICI), and no other entry. Off any build tag, so its words are tested
// on any OS.
func ownerAndAdminsDACL(user string) string {
	return "D:P(A;OICI;FA;;;" + user + ")(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
}

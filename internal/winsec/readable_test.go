package winsec

import (
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-782
// A password file's access list (synthetic SDDL): readable by the user, SYSTEM and
// Administrators only is safe; any allow entry letting another account read — or
// rewrite the access list to read — is not, whatever deny entry sits beside it; an
// inherit-only entry is not the file's own access; and what cannot be shown safe is
// not (no DACL, a NULL DACL, an unknown right, a torn entry).
func TestAPasswordFileMayBeReadableOnlyByItsUser(t *testing.T) {
	const user = "S-1-5-21-1-2-3-1001"
	safe := map[string]string{
		"user, SYSTEM, Administrators":      "O:" + user + "D:PAI(A;;FA;;;" + user + ")(A;;FA;;;SY)(A;;FA;;;BA)",
		"inherited the same":                "O:" + user + "D:AI(A;ID;FA;;;" + user + ")(A;ID;FA;;;S-1-5-18)(A;ID;FA;;;S-1-5-32-544)",
		"Users may only read attributes":    "D:(A;;FA;;;" + user + ")(A;;0x80;;;BU)",
		"an inherit-only grant to Users":    "D:(A;;FA;;;" + user + ")(A;OICIIO;GR;;;BU)",
		"a deny entry for Everyone":         "D:(D;;FA;;;WD)(A;;FA;;;" + user + ")",
		"Everyone may read the access list": "D:(A;;FA;;;" + user + ")(A;;RC;;;WD)",
		"a callback deny for Users":         "D:(XD;;FR;;;BU;(Member_of {SID(BU)}))(A;;FA;;;" + user + ")",
		"an audit entry":                    "D:(A;;FA;;;" + user + ")(AU;SA;FR;;;WD)",
	}
	for name, sddl := range safe {
		if DACLReadableByOthers(sddl, user) {
			t.Errorf("%s: %s judged readable by others", name, sddl)
		}
	}
	assure.Reached(t, len(safe), "safe access lists")
	unsafe := map[string]string{
		"Users read":                          "D:(A;;FA;;;" + user + ")(A;;FR;;;BU)",
		"Everyone generic read":               "D:(A;;FA;;;" + user + ")(A;;GR;;;WD)",
		"Authenticated Users read data, hex":  "D:(A;;FA;;;" + user + ")(A;;0x1;;;AU)",
		"another user full control":           "D:(A;;FA;;;" + user + ")(A;;FA;;;S-1-5-21-1-2-3-1002)",
		"another user may rewrite the DACL":   "D:(A;;FA;;;" + user + ")(A;;WD;;;S-1-5-21-1-2-3-1002)",
		"a deny does not cancel a grant":      "D:(D;;FR;;;BU)(A;;FR;;;BU)(A;;FA;;;" + user + ")",
		"Creator Owner is not the user":       "D:(A;;FA;;;CO)",
		"no DACL":                             "O:" + user,
		"NULL DACL":                           "D:NO_ACCESS_CONTROL",
		"an unknown right":                    "D:(A;;ZZ;;;BU)",
		"a torn entry":                        "D:(A;;FA;;;" + user,
		"a callback allow for Everyone":       "D:(A;;FA;;;" + user + ")(XA;;FR;;;WD;(Member_of {SID(WD)}))",
		"a callback allow, condition with ()": "D:(XA;;FR;;;BU;(WIN://SYSAPPID != \"x\"))(A;;FA;;;" + user + ")",
		"a resource-attribute-style allow":    "D:(ZA;;FR;;;AU)",
		"an entry type unknown here":          "D:(QQ;;FR;;;BU)",
		"no user given: the user's own grant": "D:(A;;FA;;;" + user + ")",
	}
	for name, sddl := range unsafe {
		u := user
		if name == "no user given: the user's own grant" {
			u = ""
		}
		if !DACLReadableByOthers(sddl, u) {
			t.Errorf("%s: %s judged safe", name, sddl)
		}
	}
}

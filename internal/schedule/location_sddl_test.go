package schedule

import "testing"

// covers: MA-649, R33, S59
// The pure core of Q20's Windows DACL check (§5.3): a security descriptor in
// SDDL form is "writable by others" when its DACL grants a write-class right to
// any principal other than the owner, SYSTEM, Administrators or CREATOR OWNER.
// This runs on any OS with synthetic SDDL; the live DACL read that feeds it is
// Windows-only and is verified on the Parallels box (lab item L-W15). Every case
// pins the owner as S-1-5-21-1-2-3-1001.
func TestMA649_SDDLDACLWritableByOthers(t *testing.T) {
	const owner = "S-1-5-21-1-2-3-1001"
	cases := []struct {
		name string
		sddl string
		want bool
	}{
		{"program-files shape (owner/SY/BA full, Users read+exec)",
			"O:" + owner + "G:SYD:(A;OICIID;FA;;;SY)(A;OICIID;FA;;;BA)(A;OICIID;0x1200a9;;;BU)(A;OICIID;FA;;;" + owner + ")", false},
		{"Authenticated Users: Modify is unsafe",
			"O:" + owner + "G:SYD:(A;;FA;;;SY)(A;;0x1301bf;;;AU)", true},
		{"Everyone: Full is unsafe",
			"O:" + owner + "D:(A;;FA;;;WD)", true},
		{"Users read+execute only is safe",
			"O:" + owner + "D:(A;;0x1200a9;;;BU)(A;;FA;;;BA)", false},
		{"owner-only full is safe",
			"O:" + owner + "D:(A;;FA;;;" + owner + ")", false},
		{"a DENY ace to Authenticated Users does not grant",
			"O:" + owner + "D:(D;;FA;;;AU)(A;;FA;;;SY)", false},
		{"GENERIC_WRITE to Authenticated Users is unsafe",
			"O:" + owner + "D:(A;;GW;;;AU)", true},
		{"a bare write bit to another named SID is unsafe",
			"O:" + owner + "D:(A;;0x2;;;S-1-5-21-9-9-9-5555)", true},
		{"a write grant to CREATOR OWNER is safe",
			"O:" + owner + "D:(A;;FA;;;CO)(A;;FA;;;SY)", false},
		{"no DACL section fails closed",
			"O:" + owner + "G:SY", true},
		{"a NULL DACL fails closed",
			"O:" + owner + "D:NO_ACCESS_CONTROL", true},
		{"a write grant to the Users group is unsafe",
			"O:" + owner + "D:(A;;0x1301bf;;;BU)", true},
	}
	for _, c := range cases {
		if got := sddlDACLWritableByOthers(c.sddl, owner); got != c.want {
			t.Errorf("%s:\n  sddlDACLWritableByOthers(%q) = %v, want %v", c.name, c.sddl, got, c.want)
		}
	}
}

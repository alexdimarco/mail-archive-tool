package schedule

import "mail-archive-tool/internal/winsec"

// sddlDACLWritableByOthers forwards to winsec.DACLWritableByOthers, the shared
// Q20/MA-649 predicate now also used by the headless job-file owner-only check
// (internal/job). Kept as a package-local name so location_sddl_test.go's
// synthetic-SDDL cases (MA-649) still exercise the decision here.
func sddlDACLWritableByOthers(sddl, ownerSid string) bool {
	return winsec.DACLWritableByOthers(sddl, ownerSid)
}

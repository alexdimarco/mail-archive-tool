//go:build windows

package job

import (
	"fmt"
	"os"

	"mail-archive-tool/internal/winsec"
)

// ownerOnly refuses a job file whose DACL grants a write-class right to any
// account other than the owner, SYSTEM, Administrators or CREATOR OWNER: a
// scheduled (unattended) job is owner-only (R12, MA-640). Windows carries no POSIX
// mode bits — Go reports mode 0666 for every writable file, so the unix check would
// reject every file, including ones the wizard itself wrote — so the owner-only
// guarantee is read from the security descriptor, the same winsec mechanism the
// schedule install-location guard uses (Q20/MA-649). Fail-closed via winsec.
func ownerOnly(_ os.FileInfo, path string) error {
	if winsec.PathWritableByOthers(path) {
		return fmt.Errorf("job file %s can be changed by another account on this computer: a scheduled job must be owner-only; move it under your own user profile and re-create the schedule from the wizard", path)
	}
	return nil
}

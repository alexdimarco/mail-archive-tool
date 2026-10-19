//go:build !windows

package job

import (
	"fmt"
	"os"
)

// ownerOnly refuses a job file that is group- or world-accessible: a scheduled
// (unattended) job is owner-only (R12, MA-640). On unix the POSIX mode bits say it
// directly; the Windows twin (ownercheck_windows.go) reads the DACL instead,
// because Go synthesizes mode 0666 for every writable file there.
func ownerOnly(fi os.FileInfo, path string) error {
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("job file %s is group- or world-accessible (mode %#o): a scheduled job must be owner-only; re-create the schedule from the wizard", path, fi.Mode().Perm())
	}
	return nil
}

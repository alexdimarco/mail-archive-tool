//go:build unix

package export

import "syscall"

// Host-appropriate run-fatal / not-run-fatal codes for TestIsRunFatalClassifiesTheVolumeErrors.
// On unix the real errnos; the windows twin uses the ERROR_* numbers IsRunFatal matches,
// because the unix errnos are non-matching invented values on Windows.
var (
	runFatalErrnos    = []syscall.Errno{syscall.ENOSPC, syscall.EDQUOT, syscall.EROFS, syscall.EIO, syscall.ENXIO, syscall.ENODEV, syscall.ENOTCONN, syscall.ESTALE, syscall.EHOSTDOWN}
	notRunFatalErrnos = []syscall.Errno{syscall.EACCES, syscall.EPERM, syscall.ENAMETOOLONG, syscall.ENOENT, syscall.EEXIST, syscall.EINVAL}
)

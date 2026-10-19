//go:build unix

package app

import "syscall"

// Host-appropriate volume-fault codes for the fault-injection tests. The product's
// destinationFatal / FatalKind match the real OS codes (unix errnos here, the Windows
// ERROR_* numbers in the windows twin); syscall.ENOSPC and friends are non-matching
// invented values on Windows, so injecting them there would simulate a fault the
// product correctly ignores. Injecting the host's own codes keeps each test's
// invariant proven on every OS. See TestCopyExportDestinationFatalErrnoTable and
// internal/export windowsFatalKind for the mapping.
var (
	errFull     syscall.Errno = syscall.ENOSPC // disk full  -> FatalFull
	errQuota    syscall.Errno = syscall.EDQUOT // over quota -> FatalFull
	errReadOnly syscall.Errno = syscall.EROFS  // read-only  -> FatalReadOnly
)

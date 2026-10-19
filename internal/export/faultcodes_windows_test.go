//go:build windows

package export

import "syscall"

// Windows twin of faultcodes_unix_test.go: the ERROR_* numbers windowsRunFatal
// classifies as run-fatal (and a set it does not), mirroring the unix errno sets.
var (
	runFatalErrnos    = []syscall.Errno{112, 39, 19, 21, 53, 55, 64, 1167, 1295}
	notRunFatalErrnos = []syscall.Errno{5, 2, 123, 206, 183}
)

//go:build windows

package state

import (
	"testing"

	"golang.org/x/sys/windows"
)

// covers: MA-399, R5
// The errnos replaceFile waits out on Windows are exactly x/sys/windows'
// ERROR_ACCESS_DENIED and ERROR_SHARING_VIOLATION (spelled out in replace.go
// so the retry is exercised on every OS).
func TestWindowsInUseErrnosMatchXSys(t *testing.T) {
	if errorAccessDenied != windows.ERROR_ACCESS_DENIED || errorSharingViolation != windows.ERROR_SHARING_VIOLATION {
		t.Fatalf("spelled-out errnos %d/%d, x/sys/windows has %d/%d", errorAccessDenied, errorSharingViolation, windows.ERROR_ACCESS_DENIED, windows.ERROR_SHARING_VIOLATION)
	}
}

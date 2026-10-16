//go:build windows

package util

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func freeBytes(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, fmt.Errorf("free space of %s: %w", dir, err)
	}
	var avail, total, free uint64
	// lpFreeBytesAvailableToCaller respects the caller's quota.
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, fmt.Errorf("free space of %s: %w", dir, err)
	}
	return avail, nil
}

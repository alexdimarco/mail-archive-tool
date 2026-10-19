//go:build linux

package app

import (
	"time"

	"golang.org/x/sys/unix"
)

// suspendClock reads CLOCK_BOOTTIME, which keeps counting while the computer is
// suspended. Go's own monotonic clock (CLOCK_MONOTONIC here) stops in suspend, so a
// ticker timed by it alone never sees the gap (RR2b-C9).
func suspendClock() time.Duration {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &ts); err != nil {
		return time.Since(clockBase)
	}
	return time.Duration(ts.Nano())
}

var clockBase = time.Now()

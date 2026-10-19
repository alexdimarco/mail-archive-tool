//go:build darwin

package app

import (
	"time"

	"golang.org/x/sys/unix"
)

// suspendClock reads macOS's CLOCK_MONOTONIC, which (unlike CLOCK_UPTIME_RAW and Go's
// own monotonic clock, mach_absolute_time) keeps counting while the computer
// sleeps: the clock the suspend detector needs (RR2b-C9).
func suspendClock() time.Duration {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return time.Since(clockBase)
	}
	return time.Duration(ts.Nano())
}

var clockBase = time.Now()

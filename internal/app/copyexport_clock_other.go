//go:build !linux && !darwin

package app

import "time"

// suspendClock is Go's own monotonic clock. On Windows it reads interrupt time,
// which keeps counting in suspend (design §4.13, RR2b-C9); elsewhere there is no
// better clock without cgo, and a suspend there is simply counted as awake time.
func suspendClock() time.Duration { return time.Since(clockBase) }

var clockBase = time.Now()

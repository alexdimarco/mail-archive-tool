package presync

import (
	"context"
	"time"
)

// sleepCtx waits for d, or until ctx or stop ends, whichever comes first, and
// reports whether the whole d elapsed (design-presync-attended A5, C-2/T4). Either
// context may be nil (never ends). Both adapters' sleeps are this one function, in
// an untagged file, so the Linux test gate runs the same wait the products use and
// no session wait can outlast a cancel, a Lock or the watchdog abandoning it (T17).
func sleepCtx(ctx, stop context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	var c1, c2 <-chan struct{}
	if ctx != nil {
		c1 = ctx.Done()
	}
	if stop != nil {
		c2 = stop.Done()
	}
	select {
	case <-t.C:
		return true
	case <-c1:
		return false
	case <-c2:
		return false
	}
}

package presync

import "testing"

// covers: MA-579
// idleMillis32 computes idle time in the 32-bit tick domain, so a 64-bit uptime past
// 2^32 ms (~49.7 days) — or a dwTime wrap — never overcounts and makes the owner-active
// gate read the owner as AWAY while they are active (adversarial finding B1).
func TestIdleMillis32(t *testing.T) {
	const twoPow32 = uint64(1) << 32
	// Uptime 5 s past 2^32 ms, last input 4 s ago in the tick's low bits: idle is 1 s,
	// not weeks (the 64-bit subtraction returned ~2^32 ms here).
	if got := idleMillis32(twoPow32+5000, 4000); got != 1000 {
		t.Errorf("past-2^32 uptime: idle = %d ms, want 1000", got)
	}
	// Normal case, no wrap.
	if got := idleMillis32(50000, 20000); got != 30000 {
		t.Errorf("normal: idle = %d ms, want 30000", got)
	}
	// dwTime wrapped relative to the tick low bits: 32-bit subtraction stays correct.
	if got := idleMillis32(1000, 0xFFFFFFF0); got != 1000+0x10 {
		t.Errorf("wrapped dwTime: idle = %d ms, want %d", got, 1000+0x10)
	}
}

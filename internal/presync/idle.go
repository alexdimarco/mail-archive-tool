package presync

// idleMillis32 computes elapsed idle milliseconds in the 32-bit tick domain that the
// Windows GetLastInputInfo dwTime lives in (adversarial finding B1). GetTickCount64 is
// a 64-bit uptime in ms, but dwTime is a 32-bit tick that wraps every ~49.7 days, so
// the difference must be truncated to 32 bits: unsigned 32-bit subtraction then
// resolves a single wrap correctly, whereas a 64-bit subtraction overcounts by a
// multiple of 2^32 once uptime exceeds 2^32 ms — which previously made the owner-active
// gate read a huge idle and judge the owner AWAY while they were at the keyboard.
func idleMillis32(tick uint64, liTime uint32) uint32 {
	return uint32(tick) - liTime
}

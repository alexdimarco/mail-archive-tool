package tbrestore

// SetProcessProbeForTest makes check B (the Thunderbird process list) answer through fn
// for a test in a package above this one — the engine's and the command line's (slice
// R6) — since the person's own Thunderbird may be running on the computer the suite
// runs on, and the OS process list is an external probe (CLAUDE.md: mocks are for
// external binaries). It returns the function that puts the real probe back. Only
// tests call it.
func SetProcessProbeForTest(fn func() (n int, known bool)) (restore func()) {
	was := processProbe
	processProbe = fn
	return func() { processProbe = was }
}

//go:build !windows

package keepawake

// Hold is the in-process equivalent of Windows' execution-state request where one
// is available without cgo or a shelled-out helper: on macOS (IOPMAssertion, in
// IOKit) and Linux (a logind inhibitor over D-Bus) there is none here, so no
// request is made and a run relies on the sleep being a pause, not a kill (design
// §4.15, RR2-C8).
func Hold() (release func()) { return func() {} }

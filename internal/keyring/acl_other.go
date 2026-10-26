//go:build !linux && !windows

package keyring

import "os"

// extendedACL reports false: this build cannot read the access list here (macOS
// keeps it out of reach without cgo), so the mode alone is judged — a residual of
// the P3-1a pass, SEC-06.
func extendedACL(f *os.File) bool { return false }

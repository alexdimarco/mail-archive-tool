//go:build !windows

package app

import "golang.org/x/sys/unix"

// destNotWritable says why the preview expects a folder cannot be created in dest, "" when it
// can (or cannot be told): access(2) for writing and searching, which writes nothing. The
// preview raises the same sentence Start would (X14: look before writing), instead of a clean
// preview followed by a refusal at Start (friction F17).
func destNotWritable(dest string) string {
	if err := unix.Access(dest, unix.W_OK|unix.X_OK); err != nil {
		return leaveName(err)
	}
	return ""
}

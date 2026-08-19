//go:build windows

package thunderbird

import "os"

// pidAlive reports whether the process is alive. Windows Thunderbird does not
// use the Unix `lock` symlink (Running() probes parent.lock there instead), so
// this path is rarely reached; os.FindProcess opens the process and fails if it
// no longer exists.
func pidAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}

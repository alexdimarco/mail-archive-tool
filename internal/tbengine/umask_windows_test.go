//go:build windows

package tbengine

// setUmask: Windows has no umask; nothing to set.
func setUmask(m int) int { return m }

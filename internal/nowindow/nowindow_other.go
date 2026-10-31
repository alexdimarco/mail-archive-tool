//go:build !windows

package nowindow

import "os/exec"

// hide does nothing: only Windows opens a window for a console program's child.
func hide(*exec.Cmd) {}

//go:build windows

package nowindow

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hide adds CREATE_NO_WINDOW to cmd's creation flags, keeping any flag already
// asked for.
func hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

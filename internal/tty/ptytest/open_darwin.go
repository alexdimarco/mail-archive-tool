//go:build darwin

package ptytest

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

// openMaster opens /dev/ptmx, grants and unlocks the slave and returns its path.
func openMaster() (int, string, error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", err
	}
	for _, req := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), 0); errno != 0 {
			unix.Close(fd)
			return -1, "", errno
		}
	}
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		unix.Close(fd)
		return -1, "", errno
	}
	n := 0
	for n < len(name) && name[n] != 0 {
		n++
	}
	return fd, string(name[:n]), nil
}

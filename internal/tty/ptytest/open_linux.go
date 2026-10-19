//go:build linux

package ptytest

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// openMaster opens /dev/ptmx, unlocks the slave and returns its path.
func openMaster() (int, string, error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", err
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		unix.Close(fd)
		return -1, "", err
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		unix.Close(fd)
		return -1, "", err
	}
	return fd, "/dev/pts/" + strconv.Itoa(n), nil
}

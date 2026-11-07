//go:build darwin

package tbengine

import "golang.org/x/sys/unix"

const rdonlyFlag = unix.MNT_RDONLY

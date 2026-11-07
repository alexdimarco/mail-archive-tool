//go:build linux

package tbengine

import "golang.org/x/sys/unix"

const rdonlyFlag = unix.ST_RDONLY

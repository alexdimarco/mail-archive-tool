//go:build windows

package util

// secureOpenFlags: Windows has neither O_NOFOLLOW nor O_NONBLOCK; the Lstat and
// fstat regular-file checks in ReadSecureFile cover the symlink/FIFO/device cases.
const secureOpenFlags = 0

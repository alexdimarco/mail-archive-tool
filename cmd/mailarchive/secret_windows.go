//go:build windows

package main

// secretOpenFlags: Windows has neither O_NOFOLLOW nor O_NONBLOCK; the Lstat and
// fstat regular-file checks in readSecret cover the cases.
const secretOpenFlags = 0

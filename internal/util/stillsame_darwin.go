package util

import "syscall"

// statCtime is a file's change time in nanoseconds.
func statCtime(s *syscall.Stat_t) int64 { return s.Ctimespec.Nano() }

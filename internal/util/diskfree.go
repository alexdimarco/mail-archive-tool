package util

// FreeBytes returns the bytes a program with this process's quota can still
// write on the volume that holds dir (statfs's available blocks, or
// GetDiskFreeSpaceEx's lpFreeBytesAvailableToCaller). It reads only: the copy
// export asks it before every message (extraction design §4.7, §4.13), so a
// volume that is nearly full stops the run with a legible refusal instead of a
// half-written message. dir must exist.
func FreeBytes(dir string) (uint64, error) { return freeBytes(dir) }

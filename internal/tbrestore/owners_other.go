//go:build !darwin

package tbrestore

// OwnersIgnored: only macOS mounts a volume that ignores ownership (R7-01); elsewhere a
// drive that cannot keep others out is told by its file system (FAT, exFAT, a share).
func OwnersIgnored(string) bool { return false }

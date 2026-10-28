package util

import "os"

// StillSame says now is still the file start described: the same file (os.SameFile)
// with the same size, modification time and, on Unix, change time. os.SameFile alone
// is fooled where a file system gives a removed file's number to the next file made —
// Linux does, at once (P3-8's CI run on ubuntu) — so a program replaced by delete and
// re-create would pass as itself. A replacement made after start differs in its change
// time, which the person cannot set; only one made within the same instant as start, of
// the same size, is not told apart. For a file that is not meant to change while it is
// compared — this program's own file (R14) — never a folder, whose times change as its
// entries do.
func StillSame(start, now os.FileInfo) bool {
	if start == nil || now == nil || !os.SameFile(start, now) {
		return false
	}
	return start.Size() == now.Size() && start.ModTime().Equal(now.ModTime()) && changeTimeSame(start, now)
}

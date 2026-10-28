package util

import (
	"os"
	"sync/atomic"
)

// PinIdentity fixes fi's file identity now and returns fi, and ok says the pin took.
// On Windows a FileInfo from os.Stat or os.Lstat loads the file's volume and index
// only when os.SameFile first asks for them — by opening the path again then — so an
// identity taken early and compared later would be the later file's: a file swapped
// meanwhile would match itself (P3-8 Windows lab, B1). Every FileInfo kept to be
// compared later with os.SameFile is pinned where it is taken. Elsewhere the identity
// is fixed already and the pin always takes.
//
// A pin that does not take (the path could not be opened again: removed, renamed
// away, or about to be deleted) leaves the identity to be read at the comparison,
// from whatever is at the path then. So a caller treats !ok as a mismatch and
// refuses — never as a pass (the P3-8 fixes' pass, WINPATH-1). A nil fi never takes.
func PinIdentity(fi os.FileInfo) (os.FileInfo, bool) {
	if fi == nil || pinsFail.Load() {
		return fi, false
	}
	return fi, os.SameFile(fi, fi) // false only when the identity could not be read
}

// pinsFail makes every pin report that it did not take: fault injection only, for a
// test in another package to show its caller refuses (FailPinsForTest).
var pinsFail atomic.Bool

// FailPinsForTest makes every PinIdentity report that its pin did not take until the
// returned function is called: fault injection for a caller's test (a pin fails for
// real only in a race on Windows). Never called outside tests.
func FailPinsForTest() (restore func()) {
	pinsFail.Store(true)
	return func() { pinsFail.Store(false) }
}

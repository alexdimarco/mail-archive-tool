//go:build !windows

package archdelete

import (
	"io/fs"
	"strings"
	"syscall"
	"testing"
)

// covers: MA-967, R36
func TestADeniedFileOnMacOSAndLinuxIsNotBlamedOnWindows(t *testing.T) {
	// del-s1b-A3-08: a file locked in the Finder (EPERM), a folder that may not be
	// changed (EACCES) or a read-only drive (EROFS) gets this platform's remedy, never
	// "Windows security or antivirus". Windows' own words: the table test
	// TestEachLeftoverGetsTheRemedyOfItsErrorType, run on the lab VM.
	for _, errno := range []syscall.Errno{syscall.EPERM, syscall.EACCES, syscall.EROFS} {
		t.Run(errno.Error(), func(t *testing.T) {
			dir := plainBesidePersonsFiles(t)
			r := newRig(t, dir)
			faultOn(t, nil, func(rel string) error {
				if rel == "search.db" {
					return &fs.PathError{Op: "remove", Path: rel, Err: errno}
				}
				return nil
			})
			res := r.run(t, false)
			if res.Status != StatusPartial || strings.Contains(res.Lines[0], "Windows") || strings.Contains(res.Lines[0], "antivirus") ||
				!strings.Contains(res.Lines[0], "search.db") || !strings.HasSuffix(res.Lines[0], "a read-only drive): unlock it and click Delete again, or delete it by hand.") { // one file (MA-1028, B7)
				t.Fatalf("%s", res.Message())
			}
		})
	}
}

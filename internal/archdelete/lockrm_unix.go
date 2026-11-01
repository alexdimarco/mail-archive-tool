//go:build !windows

package archdelete

import (
	"errors"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/lockfile"
)

// removeLockFile removes .mailarchive.lock inside root (folder's) only while holding
// its lock (del-s1b-A2-06, A3-09): on macOS and Linux a file another process has
// locked can still be removed, so a run that took the lock after step 7's release
// (or after step 8's Forget let go of it) would lose its lock file, and another run
// could then take a new one beside it. A lock a run holds is lockfile.ErrHeld, and
// the file stays.
func removeLockFile(root *os.Root, folder string) error {
	f, err := lockfile.HoldFile(filepath.Join(folder, lockName))
	if err != nil {
		return err
	}
	defer f.Close()
	held, err := f.Stat()
	if err != nil {
		return err
	}
	cur, err := root.Lstat(lockName)
	if err != nil {
		return err
	}
	if !os.SameFile(held, cur) {
		return errors.New("the lock file changed while it was being removed")
	}
	return removeFileFn(root, lockName)
}

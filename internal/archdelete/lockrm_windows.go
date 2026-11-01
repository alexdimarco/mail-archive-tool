//go:build windows

package archdelete

import "os"

// removeLockFile removes .mailarchive.lock inside root. Windows cannot delete a file
// another process holds open (a lock's handle does not share delete), so a lock a run
// took after step 7's release stays, and the removal is retried, then reported.
func removeLockFile(root *os.Root, _ string) error { return removeFileFn(root, lockName) }

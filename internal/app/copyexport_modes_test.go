//go:build !windows

package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// covers: MA-439, R29, R4
// Export directories are 0700 and files 0600 under umask 022 and umask 0, set at
// creation (O_CREATE 0600, Mkdir 0700) and never by a later chmod: the root and every
// directory under it, the marker while it stands, README, the manifest, the sums and
// every message; the run record, the journal and the ledger in the state directory are
// 0600 in a 0700 directory too.
func TestCopyExportModes(t *testing.T) {
	for _, umask := range []int{0o022, 0} {
		old := syscall.Umask(umask)
		func() {
			defer syscall.Umask(old)
			e, src := newCEEnv(t), newFakeSrc()
			markerMode := fs.FileMode(0)
			var root string
			p := cePlan(t, e, src, ceMsgs(3, "Inbox", "Clients"))
			root = filepath.Join(p.Dest, p.ExportName)
			e.faults = &copyFaults{hook: func(step string) {
				if step == "W4" {
					if fi, err := os.Stat(filepath.Join(root, copyMarkerName)); err == nil {
						markerMode = fi.Mode().Perm()
					}
				}
			}}
			res := mustRun(t, e, p, src)
			if markerMode != 0o600 {
				t.Errorf("umask %04o: the marker is %04o, want 0600", umask, markerMode)
			}
			check := func(path string, want fs.FileMode) {
				fi, err := os.Lstat(path)
				if err != nil {
					t.Errorf("umask %04o: %v", umask, err)
					return
				}
				if fi.Mode().Perm() != want {
					t.Errorf("umask %04o: %s is %04o, want %04o", umask, path, fi.Mode().Perm(), want)
				}
			}
			filepath.WalkDir(res.Root, func(path string, d fs.DirEntry, err error) error {
				if d.IsDir() {
					check(path, 0o700)
				} else {
					check(path, 0o600)
				}
				return nil
			})
			check(e.StateDir, 0o700)
			check(e.recordsDir(), 0o700)
			for _, f := range []string{e.recordPath(res.RunID), e.journalPath(), e.ledgerPath()} {
				check(f, 0o600)
			}
		}()
	}
}

//go:build windows

package tbrestore

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/winsec"
)

// makePrivate (Windows): new files take Local Folders' inherited access list (a folder
// on a second drive lets every signed-in account read), so the entry gets the explicit,
// protected owner-only list the recovery-key file gets (winsec.RestrictToOwner; a folder's
// entry inherited by what is made in it), then is read back through the root by its
// handle with the recovery-key file's own check (winsec.CheckSecretFile: owned by this
// user, no one but the user, SYSTEM or Administrators may read it).
func makePrivate(root *os.Root, name string, dir bool) error {
	path := filepath.Join(root.Name(), filepath.FromSlash(name))
	restrict := winsec.RestrictToOwner
	if dir {
		restrict = winsec.RestrictFolderToOwner
	}
	if err := restrict(path); err != nil {
		return err
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return winsec.CheckSecretFile(windows.Handle(f.Fd()))
}

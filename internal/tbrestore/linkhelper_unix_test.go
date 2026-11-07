//go:build !windows

package tbrestore

import "os"

// linkKinds are the links a test can plant on this platform.
func linkKinds() []string { return []string{"symlink"} }

// makeLink makes at link a symbolic link whose target is rel (the folder or file it
// stands for is abs; a junction would use it).
func makeLink(kind, link, abs, rel string) error { return os.Symlink(rel, link) }

func isNoSymlinkPrivilege(error) bool { return false }

// keptHandles is how many second descriptors of the held lock file the lock keeps.
func keptHandles(l *ProfileLock) int {
	held.Lock()
	defer held.Unlock()
	return len(l.kept)
}

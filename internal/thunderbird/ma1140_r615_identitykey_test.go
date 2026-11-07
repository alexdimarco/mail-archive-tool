package thunderbird

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1140, R40
// R6-15 (settled 2026-11-07): one profile reached by a spelling no link resolves has one
// key, so one restore lock, one settle scope and one presync view. macOS: the folder and
// its /System/Volumes/Data firmlink spelling; Windows: the folder and a junction to it.
// Twins: two folders still have two keys, and a folder that is not there keeps the path's
// key (so a gone profile is still named the same way as before).
func TestProfileKeyFollowsTheFolderNotItsSpelling(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	prof := filepath.Join(root, "Profiles", "abc.default")
	other := filepath.Join(root, "Profiles", "xyz.work")
	for _, d := range []string{prof, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	k := ProfileKey(prof)
	assure.Reached(t, k != ProfileKey(other), "two profile folders have two keys")
	gone := filepath.Join(root, "Profiles", "gone.default")
	assure.Reached(t, ProfileKey(gone) == profileKey(gone, runtime.GOOS), "a folder that is not there keeps its path's key")

	var alias string
	switch runtime.GOOS {
	case "darwin":
		alias = "/System/Volumes/Data" + prof
		if _, err := os.Stat(alias); err != nil {
			t.Skipf("no firmlink spelling of %s here: %v", prof, err)
		}
	case "windows":
		alias = filepath.Join(root, "j")
		if b, err := exec.Command("cmd", "/c", "mklink", "/J", alias, filepath.Join(root, "Profiles")).CombinedOutput(); err != nil {
			t.Skipf("mklink /J: %v %s", err, b)
		}
		alias = filepath.Join(alias, "abc.default")
	default:
		// Linux has no spelling a link does not resolve that a test can make without
		// privilege (a bind mount); the symlink case is MA-1140's own test.
		alias = filepath.Join(root, "link")
		if err := os.Symlink(prof, alias); err != nil {
			t.Fatal(err)
		}
	}
	assure.Reached(t, ProfileKey(alias) == k, "the profile through "+alias+" has the key of "+prof)
	lp, err := RestoreLockPath(alias)
	assure.Reached(t, err == nil && filepath.Base(lp) == RestoreLockName(k), "its restore lock is the one restore lock")
}

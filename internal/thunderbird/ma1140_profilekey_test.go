package thunderbird

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-1140, R40
// thunderbird.ProfileKey, the one key presync, the backup skip and the restore share
// (docs/design-restore-thunderbird.md §3.4 foldcheck note N3): one profile spelled
// two ways — through a link, with "..", and (on Windows and macOS) in another case —
// has one key; two profiles have two; the restore lock's name is "restore-" + the key
// + ".lock", which IsRestoreLockName knows and nothing near it matches.
func TestProfileKeyOneProfileTwoSpellings(t *testing.T) {
	root := t.TempDir()
	prof := filepath.Join(root, "Profiles", "abc.default")
	if err := os.MkdirAll(prof, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "Profiles", "xyz.work")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	k := ProfileKey(prof)
	if len(k) != 16 || strings.Trim(k, "0123456789abcdef") != "" {
		t.Fatalf("a key is 16 lowercase hex characters: %q", k)
	}
	if got := ProfileKey(filepath.Join(root, "Profiles", "xyz.work", "..", "abc.default")); got != k {
		t.Errorf("a spelling with .. has the same key: %s vs %s", got, k)
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(root, "link")
		if err := os.Symlink(prof, link); err != nil {
			t.Fatal(err)
		}
		if got := ProfileKey(link); got != k {
			t.Errorf("a link to the profile has the same key: %s vs %s", got, k)
		}
	}
	if got := profileKey(strings.ToUpper(prof), "darwin"); got != profileKey(prof, "darwin") {
		t.Errorf("on macOS another case has the same key")
	}
	if got := profileKey(strings.ToUpper(prof), "windows"); got != profileKey(prof, "windows") {
		t.Errorf("on Windows another case has the same key")
	}
	if ProfileKey(other) == k {
		t.Error("two profiles have two keys")
	}

	name := RestoreLockName(k)
	if name != "restore-"+k+".lock" || !IsRestoreLockName(name) {
		t.Fatalf("the restore lock's name: %q", name)
	}
	for _, bad := range []string{"restore-x.lock", "restore-" + k + ".lock.bak", "restore-" + strings.ToUpper(k) + ".lock", "restore-" + k[:15] + ".lock", "Restore-" + k + ".lock", "restore-" + k + "0.lock"} {
		if IsRestoreLockName(bad) {
			t.Errorf("%q is not a restore lock's name", bad)
		}
	}
}

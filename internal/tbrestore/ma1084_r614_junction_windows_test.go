//go:build windows

package tbrestore

import (
	"os/exec"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// junction makes a junction at link leading to target (mklink /J), or skips.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if b, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, b)
	}
}

// covers: MA-1084, MA-1143, R40
// R6-14 (settled 2026-11-07, the lead's call; Windows): a junction on the way to a
// profile (a profile moved with `mklink /J`) is followed with the repository's own
// resolver, never refused as "not a Thunderbird profile". One that leads inside the
// user's own folder is allowed, and the profile is named by where it really is; one that
// leads outside it is refused W-JUNCTION naming where it leads, nothing changed; that
// folder named directly is allowed (the positive twin), as are the listed profiles'.
func TestAJunctionOnTheWayToAProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	inside := makeProfile(t, filepath.Join(home, "TB", "p.default"), localPrefs()...)
	j := filepath.Join(t.TempDir(), "j")
	junction(t, j, filepath.Join(home, "TB"))
	p, err := CheckProfile(filepath.Join(j, "p.default"))
	assure.Reached(t, err == nil && p.Dir == canonical(inside), "a junction into the user's own folder is followed: "+p.Dir)

	outside := makeProfile(t, filepath.Join(t.TempDir(), "TB", "p.default"), localPrefs()...)
	j2 := filepath.Join(t.TempDir(), "j2")
	junction(t, j2, filepath.Dir(outside))
	before := snapshot(t, filepath.Dir(outside))
	_, err = CheckProfile(filepath.Join(j2, "p.default"))
	refusedAs(t, err, "W-JUNCTION", func() bool { return snapshot(t, filepath.Dir(outside)) == before }, "folder link (a junction)", "outside your own user folder", "choose it by that path")
	direct, err := CheckProfile(outside)
	assure.Reached(t, err == nil && direct.Usable, "positive twin: the folder it leads to, named directly, is a profile")

	base := t.TempDir()
	writeFile(t, filepath.Join(base, ProfilesININame), "[Profile0]\nName=Moved\nIsRelative=0\nPath="+filepath.Join(j2, "p.default")+"\n")
	list, err := ReadProfiles([]string{base})
	assure.Reached(t, err == nil && len(list.Profiles) == 1 && !list.Profiles[0].Usable, "a listed profile through such a junction is listed, not usable")
}

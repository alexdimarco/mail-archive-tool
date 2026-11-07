//go:build darwin

package tbengine

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1128, R37, S72
// R7-01 (settled 2026-11-07): where private-at-creation cannot be guaranteed, an encrypted
// archive's plan says who else could read the restored mail and asks a second time. A
// macOS drive set to "Ignore ownership on this volume" (the default for an external drive)
// lets every account act as the owner, so mode 0700 and an empty access list keep no one
// out, though they read back as owner-only. A real one: a disk image attached with
// `-owners off` (no admin rights needed). Positive twin first: the same archive into a
// profile on this Mac's own drive is not asked twice.
func TestADriveThatIgnoresOwnershipIsAskedTwice(t *testing.T) {
	out, u := encryptedArchive(t)
	own := plan(t, Request{Out: out, Keys: u, Profile: newProfile(t, "").choice()})
	assure.Reached(t, !own.AskTwice(), fmt.Sprintf("this Mac's own drive is not asked twice: %v", own.Codes()))

	tmp := t.TempDir()
	// Sparse: the restore keeps 512 MiB free, and the image takes only what is written.
	img, mnt := filepath.Join(tmp, "owners.sparseimage"), filepath.Join(tmp, "mnt")
	if b, err := exec.Command("hdiutil", "create", "-size", "1g", "-type", "SPARSE", "-fs", "APFS", "-volname", "ma1128", "-quiet", img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil create: %v %s", err, b)
	}
	if b, err := exec.Command("hdiutil", "attach", "-owners", "off", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil attach: %v %s", err, b)
	}
	t.Cleanup(func() { exec.Command("hdiutil", "detach", "-force", mnt).Run() })
	pr := newProfile(t, mnt)
	p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
	assure.Reached(t, p.AskTwice() && strings.Contains(codesOf(p), "W-READERS") && strings.Contains(p.Text(), "ignore ownership") && strings.Contains(p.Text(), "every account on this computer"),
		fmt.Sprintf("a drive that ignores ownership names the readers and asks twice: %s\n%s", codesOf(p), p.Text()))
	pp := plan(t, Request{Out: plainArchive(t, true), Profile: pr.choice()})
	assure.Reached(t, !pp.AskTwice(), "a plain archive there is asked nothing more")
}

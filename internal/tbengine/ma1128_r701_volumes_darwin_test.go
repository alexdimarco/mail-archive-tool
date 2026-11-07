//go:build darwin

package tbengine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// attachImage makes a real disk image of the format fsName ("MS-DOS FAT32", "ExFAT",
// "APFS", "HFS+"), attaches it at a folder of its own with ownership on or off, and
// detaches it when the test ends (the image is in the test's temp folder, deleted after).
// Sparse: the restore keeps 512 MiB free, and the image takes only what is written. No
// admin rights are needed.
func attachImage(t *testing.T, fsName, owners string) string {
	t.Helper()
	tmp := t.TempDir()
	img, mnt := filepath.Join(tmp, "v.sparseimage"), filepath.Join(tmp, "mnt")
	if b, err := exec.Command("hdiutil", "create", "-size", "1g", "-type", "SPARSE", "-fs", fsName, "-volname", "R701", "-quiet", img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil create %s: %v %s", fsName, err, b)
	}
	if b, err := exec.Command("hdiutil", "attach", "-owners", owners, "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil attach %s: %v %s", fsName, err, b)
	}
	t.Cleanup(func() { exec.Command("hdiutil", "detach", "-force", mnt).Run() })
	return mnt
}

// covers: MA-1128, R37, R40, S72
// R7-01 independent review F1 (settled 2026-11-07, the lead's call): a profile whose
// Local Folders is on a drive formatted FAT32 or exFAT (a drive shared with Windows,
// attached as macOS mounts a USB drive: ownership ignored) is restored into again. Such a
// volume holds no access list, so the restore makes its entries with the plain exclusive
// create (0700 / 0600) instead of the access-list call the volume refuses; the plan
// already treats it as not private. Real disk images made and attached here: a plain
// archive's restore finishes; an encrypted one is asked twice (W-READERS), refused without
// the second yes with nothing written (W-NOTAGAIN), and finishes with it. No words carry
// "invalid argument". Before the fix every one of these failed at the container
// ("mkdir … invalid argument").
func TestARestoreOntoAFATOrExFATDriveFinishes(t *testing.T) {
	out, u := encryptedArchive(t)
	plain := plainArchive(t, true)
	for _, fsName := range []string{"MS-DOS FAT32", "ExFAT"} {
		t.Run(fsName, func(t *testing.T) {
			mnt := attachImage(t, fsName, "off")
			pr := newProfile(t, filepath.Join(mnt, "plain"))
			r := mustRun(t, plan(t, Request{Out: plain, Profile: pr.choice()}), Start{})
			assure.Reached(t, r.Done() && r.Write.Restored == 4 && !strings.Contains(r.Words, "invalid argument"),
				fmt.Sprintf("%s: the plain restore finished: %s", fsName, r.Words))
			assure.Reached(t, len(restoredEntries(t, pr.local, r.Write.Top)) >= 5, fmt.Sprintf("%s: the restored folders are on the drive", fsName))

			pe := newProfile(t, filepath.Join(mnt, "enc"))
			p := plan(t, Request{Out: out, Keys: u, Profile: pe.choice()})
			assure.Reached(t, p.AskTwice() && strings.Contains(codesOf(p), "W-READERS"), fmt.Sprintf("%s: the encrypted plan asks twice: %s\n%s", fsName, codesOf(p), p.Text()))
			before := snapshot(t, pe.dir)
			_, err := Run(context.Background(), p, Start{NotEncryptedOK: true})
			refusedAs(t, err, "W-NOTAGAIN", func() bool { return snapshot(t, pe.dir) == before }, "not confirmed a second time")
			re := mustRun(t, p, Start{NotEncryptedOK: true, ConfirmAgain: true})
			assure.Reached(t, re.Done() && re.Write.Restored == 4 && !strings.Contains(re.Words, "invalid argument"),
				fmt.Sprintf("%s: confirmed twice, the encrypted restore finished: %s", fsName, re.Words))
		})
	}
}

// covers: MA-1128, R37, R40, S72
// R7-01 independent review F1, the twin: a volume that holds access lists (APFS, HFS+, on
// real disk images attached with ownership on) still makes every entry with the
// access-list call, so nothing is born with Local Folders' inheritable "everyone may read"
// entry. Each entry is read with `ls -led` the instant its create returns and again after
// the restore, the umask 0: owner-only, no access-list line at all.
func TestARestoreOntoAnAPFSOrHFSDriveIsStillPrivateFromCreation(t *testing.T) {
	was := setUmask(0)
	defer setUmask(was)
	out, u := encryptedArchive(t)
	for _, fsName := range []string{"APFS", "HFS+"} {
		t.Run(fsName, func(t *testing.T) {
			mnt := attachImage(t, fsName, "on")
			if tbrestore.OwnersIgnored(mnt) {
				t.Fatalf("%s attached with ownership on reads as ignoring it", fsName)
			}
			pr := newProfile(t, mnt)
			grantOthersRead(t, pr.local)
			var looked, open []string
			fsWas := fsFor
			fsFor = func(r *os.Root) tbrestore.FS {
				return &bornFS{FS: tbrestore.NewRootFS(r), local: pr.local, look: func(p string) {
					looked = append(looked, p)
					if why := othersMayRead(t, p); why != "" {
						open = append(open, p+": "+why)
					}
				}}
			}
			defer func() { fsFor = fsWas }()
			// A disk image is an external drive, so the plan asks twice for that (W-LEAVES);
			// not for who else may read it: the drive can hold owner-only.
			p := plan(t, Request{Out: out, Keys: u, Profile: pr.choice()})
			assure.Reached(t, len(p.Codes()) > 0 && !strings.Contains(codesOf(p), "W-READERS"), fmt.Sprintf("%s: not asked about other readers: %v", fsName, p.Codes()))
			r := mustRun(t, p, Start{NotEncryptedOK: true, ConfirmAgain: true})
			assure.Reached(t, r.Done(), fmt.Sprintf("%s: the restore finished: %s", fsName, r.Words))
			assure.Reached(t, len(looked) >= 6, fmt.Sprintf("%s: every create was looked at: %v", fsName, looked))
			if len(open) > 0 {
				t.Fatalf("%s: born readable by other accounts:\n%s", fsName, strings.Join(open, "\n"))
			}
			for _, e := range append(restoredEntries(t, pr.local, r.Write.Top), filepath.Join(pr.local, r.Write.Top)) {
				if why := othersMayRead(t, e); why != "" {
					t.Fatalf("%s: %s can be read by other accounts after the restore:\n%s", fsName, e, why)
				}
			}
		})
	}
}

//go:build darwin

package tbrestore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// attachImage makes a real disk image of the format fsName, attaches it at a folder of its
// own with ownership off (as macOS mounts a USB drive) and detaches it when the test ends;
// the image is in the test's temp folder, deleted after. It returns a Local Folders made
// on it.
func attachImage(t *testing.T, fsName string) string {
	t.Helper()
	tmp := t.TempDir()
	img, mnt := filepath.Join(tmp, "v.sparseimage"), filepath.Join(tmp, "mnt")
	if b, err := exec.Command("hdiutil", "create", "-size", "64m", "-type", "SPARSE", "-fs", fsName, "-volname", "R701", "-quiet", img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil create %s: %v %s", fsName, err, b)
	}
	if b, err := exec.Command("hdiutil", "attach", "-owners", "off", "-nobrowse", "-mountpoint", mnt, img).CombinedOutput(); err != nil {
		t.Skipf("hdiutil attach %s: %v %s", fsName, err, b)
	}
	t.Cleanup(func() { exec.Command("hdiutil", "detach", "-force", mnt).Run() })
	local := filepath.Join(mnt, "Local Folders")
	if err := os.Mkdir(local, 0o700); err != nil {
		t.Fatal(err)
	}
	return canonical(local)
}

// topCreates makes the two entries a restore makes straight in Local Folders (the
// container, the parent file) through the writer's FS, and says what each returned.
func topCreates(t *testing.T, local string) (mkErr, crErr error) {
	t.Helper()
	root, err := os.OpenRoot(local)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f := newRootFS(root)
	mkErr = f.Mkdir("C.sbd")
	w, crErr := f.Create("C")
	if crErr == nil {
		w.Close()
	}
	return mkErr, crErr
}

// ownerOnlyNoACL says whether path is this user's, 0700/0600, with no access-list line.
func ownerOnlyNoACL(t *testing.T, path string) bool {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	b, _ := exec.Command("ls", "-led", path).CombinedOutput()
	return fi.Mode().Perm()&0o077 == 0 && len(strings.Split(strings.TrimSpace(string(b)), "\n")) == 1
}

// covers: MA-1128, R37, R40, S72
// R7-01 independent review F1 (settled 2026-11-07, the lead's call): the call that makes an
// entry straight in Local Folders is chosen by asking the volume whether it holds access
// lists (pathconf _PC_EXTENDED_SECURITY_NP on the root's own open folder), not by trying.
//   - FAT32 and exFAT (real disk images, ownership off): the volume says no, so the plain
//     exclusive create is used (0700 / 0600; there is no access list to inherit) and both
//     entries are made — before the fix "invalid argument". APFS (this Mac's temp folder):
//     the volume says yes; both made owner-only with no access list.
//   - The query failing: on a volume detected as not private (FAT32) the plain create is
//     used; on a private one (APFS) nothing is made and the words say so plainly — the
//     drive's format, and what to do — never silently the plain create.
//   - The volume saying yes but refusing the call (a FAT32 volume made to answer yes):
//     nothing is made, the same plain words naming its format; never "invalid argument".
func TestTheCreateIsChosenByWhatTheVolumeHolds(t *testing.T) {
	_, apfs := localProfile(t)
	if mk, cr := topCreates(t, apfs); mk != nil || cr != nil {
		t.Fatalf("APFS: the container and the parent file are made: %v, %v", mk, cr)
	}
	for _, n := range []string{"C.sbd", "C"} {
		assure.Reached(t, ownerOnlyNoACL(t, filepath.Join(apfs, n)), "APFS: "+n+" is owner-only with no access list")
	}
	for _, fsName := range []string{"MS-DOS FAT32", "ExFAT"} {
		local := attachImage(t, fsName)
		mk, cr := topCreates(t, local)
		assure.Reached(t, mk == nil && cr == nil, fsName+": the container and the parent file are made with the plain create: "+errText(mk)+" "+errText(cr))
	}

	was := extendedSecurityOf
	defer func() { extendedSecurityOf = was }()
	queryFails := errors.New("the volume did not answer")
	extendedSecurityOf = func(*os.Root) (bool, error) { return false, queryFails }
	fat := attachImage(t, "MS-DOS FAT32")
	mk, cr := topCreates(t, fat)
	assure.Reached(t, mk == nil && cr == nil, "the query failing on FAT32 (not private): the plain create: "+errText(mk)+" "+errText(cr))

	_, priv := localProfile(t)
	before := snapshot(t, priv)
	mk, cr = topCreates(t, priv)
	for _, err := range []error{mk, cr} {
		refusedAs(t, err, "W-NOTPRIVATE", func() bool { return snapshot(t, priv) == before },
			priv, "APFS", "readable only by your account", "nothing was added", "this computer's own drive")
		assure.Reached(t, err != nil && !strings.Contains(err.Error(), "invalid argument") && !strings.Contains(err.Error(), queryFails.Error()), "no raw error reaches the words: "+errText(err))
	}

	extendedSecurityOf = func(*os.Root) (bool, error) { return true, nil }
	fat2 := attachImage(t, "MS-DOS FAT32")
	before = snapshot(t, fat2)
	mk, cr = topCreates(t, fat2)
	for _, err := range []error{mk, cr} {
		refusedAs(t, err, "W-NOTPRIVATE", func() bool { return snapshot(t, fat2) == before },
			fat2, "FAT", "readable only by your account", "this computer's own drive")
		assure.Reached(t, err != nil && !strings.Contains(err.Error(), "invalid argument"), "no raw error reaches the words: "+errText(err))
	}
}

func errText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// covers: MA-1128, R37, R40
// R7-01 independent review F3 and F6 (settled 2026-11-07, the lead's call): an entry made
// by path (mkdir_extended / open_extended) is bound to the root only when the root's look
// at its name finds this user's own folder (or file), of the kind made, not a link. Local
// Folders is reached through a link (L); the link is re-pointed to another folder (B) for
// the create, and, for the planted case, back in the instant after it (afterPathCreateSeam)
// to a folder holding an entry of the name that is another account's (/private/var,
// root's): the identity check alone passed it (F3). Now each is refused in plain words
// (W-CHANGED: a folder on the way changed; what MailArchive made may be in another place
// and was left there, since it cannot be sure it is its own) — never a raw "statat … no
// such file" (F6) — and nothing is removed: the planted entry is not this restore's, and
// the one made in B cannot be shown to be. Positive twin first: no swap, both made and
// bound.
func TestAnEntryMadeByPathIsBoundOnlyToThisUsersOwn(t *testing.T) {
	tmp := canonical(t.TempDir())
	a, b := filepath.Join(tmp, "A"), filepath.Join(tmp, "B")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(tmp, "L")
	point := func(to string) {
		t.Helper()
		os.Remove(link)
		if err := os.Symlink(to, link); err != nil {
			t.Fatal(err)
		}
	}
	open := func(to string) *os.Root {
		t.Helper()
		point(to)
		r, err := os.OpenRoot(link)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		return r
	}

	point(a)
	if mk, cr := topCreates(t, link); mk != nil || cr != nil {
		t.Fatalf("no swap: both made and bound: %v %v", mk, cr)
	}

	was := afterPathCreateSeam
	defer func() { afterPathCreateSeam = was }()
	for _, tc := range []struct {
		what  string
		root  string // where the root is opened
		back  string // where the link points again right after the create ("" none)
		name  string
		dir   bool
		inB   string // what the create made in B, left there
		stays string // the entry the root's look found, untouched
	}{
		{"F3 planted folder", "/private", "/private", "var", true, "var", "/private/var"},
		{"F3 planted, a file made", "/private", "/private", "var", false, "var", "/private/var"},
		{"F6 the name gone", a, "", "D.sbd", true, "D.sbd", ""},
		{"F6 the name gone, a file", a, "", "D", false, "D", ""},
	} {
		t.Run(tc.what, func(t *testing.T) {
			r := open(tc.root)
			point(b)
			afterPathCreateSeam = func(string) {
				if tc.back != "" {
					point(tc.back)
				}
			}
			defer func() { afterPathCreateSeam = func(string) {} }()
			defer os.RemoveAll(filepath.Join(b, tc.inB))
			var stayed os.FileInfo
			if tc.stays != "" {
				stayed, _ = os.Lstat(tc.stays)
			}
			f := newRootFS(r)
			var err error
			if tc.dir {
				err = f.Mkdir(tc.name)
			} else {
				var w wfile
				if w, err = f.Create(tc.name); err == nil {
					w.Close()
				}
			}
			refusedAs(t, err, "W-CHANGED", func() bool {
				_, inB := os.Lstat(filepath.Join(b, tc.inB))
				ok := inB == nil
				if stayed != nil {
					now, serr := os.Lstat(tc.stays)
					ok = ok && serr == nil && os.SameFile(stayed, now)
				}
				if tc.back == "" {
					_, inA := os.Lstat(filepath.Join(a, tc.name))
					ok = ok && errors.Is(inA, os.ErrNotExist)
				}
				return ok
			}, link, "changed", "left it there", "Click Check again")
			assure.Reached(t, err != nil && !strings.Contains(err.Error(), "statat") && !strings.Contains(err.Error(), "no such file"), "no raw error reaches the words: "+errText(err))
		})
	}
}

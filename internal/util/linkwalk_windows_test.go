//go:build windows

package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-854, R37
// A junction deeper than 260 characters still leads where it goes (the P3-8 fixes'
// pass, WINPATH-3): a folder reached through it into a synced folder is judged synced,
// so an unencrypted copy written through it asks twice (K5). The walk goes through the
// os package, which reaches long paths on a computer whose long-path setting is off;
// Windows' own final-path call, opened without the long-path prefix, failed there and
// the junction was silently kept. Positive twin: the synced folder named directly.
func TestAJunctionPastTheLongPathLimitStillLeadsWhereItGoes(t *testing.T) {
	base := t.TempDir()
	synced := filepath.Join(base, "Dropbox", "Mail")
	os.MkdirAll(synced, 0o755)
	deep := base
	for len(deep) < 300 {
		deep = filepath.Join(deep, strings.Repeat("d", 40))
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// mklink cannot make a link at a long path; the junction is made short and moved.
	short := filepath.Join(base, "Plain")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", short, synced).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	link := filepath.Join(deep, "Plain")
	if err := os.Rename(short, link); err != nil {
		t.Fatal(err)
	}
	if _, direct := UnderCloudSync(ResolveExisting(synced)); !direct {
		t.Fatal("positive twin: the synced folder named directly")
	}
	through := ResolveExisting(filepath.Join(link, "copy"))
	_, viaJunction := UnderCloudSync(through)
	assure.Reached(t, len(link) > 260 && viaJunction && strings.EqualFold(filepath.Dir(through), ResolveExisting(synced)),
		"the junction past 260 characters leads into the synced folder: "+through)
}

// covers: MA-854, R37
// A disk mounted in a folder that also has a drive letter keeps the folder's spelling
// (the P3-8 fixes' pass, WINPATH-2): an archive made at C:\…\mnt\Mail is not resolved
// to E:\Mail, whose letter can change at the next plug-in, so the record still finds
// it. Windows' own final-path call gave the drive letter. Lab only
// (MAILARCHIVE_MOUNT_LAB=1, an administrator or SYSTEM): it makes, mounts and removes
// a 32 MB virtual disk with diskpart. Positive twin: the disk named by its letter
// stays on its letter.
func TestAFolderMountedDiskKeepsItsFolderSpellingOnWindows(t *testing.T) {
	if os.Getenv("MAILARCHIVE_MOUNT_LAB") != "1" {
		t.Skip("mount lab: set MAILARCHIVE_MOUNT_LAB=1 as an administrator or SYSTEM (makes and mounts a virtual disk)")
	}
	base := t.TempDir()
	vhd := filepath.Join(base, "lab.vhdx")
	mnt := filepath.Join(base, "mnt")
	os.Mkdir(mnt, 0o755)
	diskpart := func(lines ...string) {
		t.Helper()
		script := filepath.Join(base, "diskpart.txt")
		os.WriteFile(script, []byte(strings.Join(lines, "\r\n")+"\r\n"), 0o644)
		if out, err := exec.Command("diskpart", "/s", script).CombinedOutput(); err != nil {
			t.Fatalf("diskpart: %v: %s", err, out)
		}
	}
	diskpart(`create vdisk file="`+vhd+`" maximum=32 type=expandable`, "attach vdisk", "create partition primary",
		"format fs=ntfs quick label=MALAB", `assign mount="`+mnt+`"`, "assign")
	t.Cleanup(func() {
		exec.Command("mountvol", mnt, "/D").Run()
		diskpart(`select vdisk file="`+vhd+`"`, "detach vdisk")
	})
	name := make([]uint16, 64)
	if err := windows.GetVolumeNameForVolumeMountPoint(windows.StringToUTF16Ptr(mnt+`\`), &name[0], uint32(len(name))); err != nil {
		t.Fatal(err)
	}
	paths := make([]uint16, 1024)
	var n uint32
	if err := windows.GetVolumePathNamesForVolumeName(&name[0], &paths[0], uint32(len(paths)), &n); err != nil {
		t.Fatal(err)
	}
	letter := ""
	for _, p := range strings.Split(string(utf16.Decode(paths[:n])), "\x00") {
		t.Logf("mounted at %q", p)
		if len(p) == 3 && p[1] == ':' {
			letter = p
		}
	}
	if letter == "" {
		t.Fatal("the lab disk has no drive letter, so this proves nothing")
	}
	if err := os.Mkdir(filepath.Join(mnt, "Mail"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveExisting(filepath.Join(letter, "Mail")); !strings.EqualFold(got, filepath.Join(letter, "Mail")) {
		t.Fatalf("positive twin: the disk by its letter: %s", got)
	}
	got := ResolveExisting(filepath.Join(mnt, "Mail", "copy"))
	assure.Reached(t, strings.EqualFold(got, filepath.Join(mnt, "Mail", "copy")),
		"the folder-mounted disk keeps its folder spelling, not "+letter+": "+got)
}

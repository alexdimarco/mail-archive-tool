package app

import (
	"strings"
	"testing"
)

// covers: MA-813, R37
// The removable-or-external check an unencrypted copy asks twice for (phase 3 K5):
// on macOS a volume mounted under /Volumes, or one the kernel marks removable, is;
// the boot volume and its system volumes are not. On Linux the mount holding the
// path — the longest mount point it is under — is when it sits under /media,
// /run/media or /mnt; the root, the home mount and the bare /mnt folder are not.
// The real check calls this host's temporary folder local, and the seam decides when
// set.
func TestARemovableOrExternalDriveIsRecognised(t *testing.T) {
	for _, c := range []struct {
		on   string
		flag bool
		want bool
	}{
		{"/Volumes/USB STICK", false, true},
		{"/Volumes/Backup", false, true},
		{"/", true, true},
		{"/", false, false},
		{"/System/Volumes/Data", false, false},
		{"/Volumes", false, false},
	} {
		if got := removableMountDarwin(c.on, c.flag); got != c.want {
			t.Errorf("macOS mount %q (removable flag %v): got %v, want %v", c.on, c.flag, got, c.want)
		}
	}

	mountinfo := strings.Split(strings.Join([]string{
		`22 1 8:2 / / rw,relatime shared:1 - ext4 /dev/sda2 rw`,
		`30 22 8:3 / /home rw,relatime shared:2 - ext4 /dev/sda3 rw`,
		`41 22 8:17 / /media/u/USB\040STICK rw,nosuid shared:3 - vfat /dev/sdb1 rw`,
		`42 22 8:33 / /run/media/u/Backup rw,nosuid shared:4 - exfat /dev/sdc1 rw`,
		`43 22 8:49 / /mnt/ext rw shared:5 - ext4 /dev/sdd1 rw`,
	}, "\n"), "\n")
	for abs, want := range map[string]bool{
		"/media/u/USB STICK/copies":   true,
		"/run/media/u/Backup":         true,
		"/mnt/ext/x":                  true,
		"/home/u/Mail":                false,
		"/srv/mail":                   false,
		"/media/u/not-mounted/folder": false,
		"/mnt":                        false,
	} {
		if got := removableMountinfo(mountinfo, abs); got != want {
			t.Errorf("Linux path %q: got %v, want %v", abs, got, want)
		}
	}

	e := &CopyExportEnv{}
	if e.removable(t.TempDir()) {
		t.Error("this host's temporary folder was called removable")
	}
	e.removableSeam = func(string) bool { return true }
	if !e.removable(t.TempDir()) {
		t.Error("the seam was not consulted")
	}
}

package app

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-853, R37
// On Windows a drive that can leave this computer is judged by the volume mounted
// where the path is, and by its bus when Windows calls it fixed (the P3-8 Windows
// lab, B31). Real: this computer's temporary folder is on a fixed internal disk whose
// bus is read without administrator rights, and is not called removable, even for a
// folder not made yet. Stood in (no external disk or folder-mounted volume can be
// attached in the lab): an external USB disk Windows reports as fixed leaves, as do
// FireWire and SD cards and a medium marked removable; a SATA or NVMe disk does not,
// nor one whose bus cannot be read; a removable volume mounted inside a folder on C:
// leaves while C: itself does not.
func TestAnExternalOrFolderMountedDriveIsJudgedByItsOwnVolumeOnWindows(t *testing.T) {
	// Positive twin, real: the temporary folder is on this computer's own disk.
	tmp := t.TempDir()
	root, err := volumeMountOf(filepath.Join(tmp, "not made yet", "copy"))
	if err != nil || !strings.EqualFold(root, filepath.VolumeName(tmp)+`\`) {
		t.Fatalf("the volume holding %s: %q, %v", tmp, root, err)
	}
	if dt := driveTypeOf(root); dt != windows.DRIVE_FIXED {
		t.Fatalf("drive type of %s: %d, want fixed", root, dt)
	}
	bt, removableMedia, err := storageBus(root)
	if err != nil {
		t.Fatalf("the bus of %s could not be read: %v", root, err)
	}
	t.Logf("%s: bus type %#x, removable medium %v", root, bt, removableMedia)
	if busLeaves(bt) || removableMedia || removableDrive(tmp) {
		t.Fatalf("this computer's own disk (%s, bus %#x) was called removable", root, bt)
	}

	type disk struct {
		dt   uint32
		bus  uint32
		rm   bool
		fail bool
	}
	stand := func(mounts map[string]disk) volumeOS {
		return volumeOS{
			mountOf: func(abs string) (string, error) {
				best := ""
				for m := range mounts {
					if strings.HasPrefix(strings.ToLower(abs+`\`), strings.ToLower(m)) && len(m) > len(best) {
						best = m
					}
				}
				return best, nil
			},
			driveType: func(root string) uint32 { return mounts[root].dt },
			bus: func(root string) (uint32, bool, error) {
				if d := mounts[root]; !d.fail {
					return d.bus, d.rm, nil
				}
				return 0, false, errors.New("the bus cannot be read")
			},
		}
	}
	internal := disk{dt: windows.DRIVE_FIXED, bus: 0xB} // SATA
	v := stand(map[string]disk{
		`C:\`:         internal,
		`C:\mnt\usb\`: {dt: windows.DRIVE_REMOVABLE},
		// A card in a reader reports a removable medium (WINACL-2).
		`C:\mnt\card\`: {dt: windows.DRIVE_FIXED, bus: busTypeSd, rm: true},
		`D:\`:          {dt: windows.DRIVE_FIXED, bus: 0x11}, // NVMe
		`E:\`:          {dt: windows.DRIVE_FIXED, bus: busTypeUsb},
		`F:\`:          {dt: windows.DRIVE_FIXED, bus: busType1394},
		`G:\`:          {dt: windows.DRIVE_FIXED, bus: 0xB, rm: true},
		`H:\`:          {dt: windows.DRIVE_FIXED, fail: true},
		`I:\`:          {dt: windows.DRIVE_CDROM},
	})
	for abs, want := range map[string]bool{
		`C:\Users\me\Mail`:          false,
		`C:\mnt`:                    false,
		`C:\mnt\usb\copies`:         true,
		`C:\mnt\card\copies`:        true,
		`D:\copies`:                 false,
		`E:\copies`:                 true,
		`F:\copies`:                 true,
		`G:\copies`:                 true,
		`H:\copies`:                 false,
		`I:\copies`:                 true,
		`C:\mnt\usb-not-mounted\x`:  false,
		`C:\mnt\usb\deeper\folders`: true,
	} {
		if got := v.leaves(abs); got != want {
			t.Errorf("%s: leaves %v, want %v", abs, got, want)
		}
	}
	assure.Reached(t, v.leaves(`E:\copies`) && v.leaves(`C:\mnt\usb\copies`) && !v.leaves(`C:\Users\me\Mail`),
		"an external USB disk reported fixed and a folder-mounted removable volume leave; C: does not")
}

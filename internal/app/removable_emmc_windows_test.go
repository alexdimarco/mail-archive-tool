package app

import (
	"testing"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/assure"
)

// covers: MA-854, R37
// A computer's own soldered-in eMMC disk does not leave it (the P3-8 fixes' pass,
// WINACL-2): many small laptops and tablets start from eMMC, which Windows reports as
// a fixed drive on the SD or MMC bus with no removable medium, and every unencrypted
// copy anywhere on C: was warned as leaving the computer and asked twice. Stood in (no
// eMMC disk in the lab). Positive twin: an SD card in a reader (removable medium) and
// a USB disk Windows reports as fixed still leave.
func TestAComputersOwnEMMCDiskDoesNotLeaveIt(t *testing.T) {
	type disk struct {
		bus uint32
		rm  bool
	}
	mounts := map[string]disk{
		`C:\`: {bus: busTypeSd},
		`D:\`: {bus: busTypeMmc},
		`E:\`: {bus: busTypeSd, rm: true},
		`F:\`: {bus: busTypeUsb},
	}
	v := volumeOS{
		mountOf:   func(abs string) (string, error) { return abs[:3], nil },
		driveType: func(string) uint32 { return windows.DRIVE_FIXED },
		bus:       func(root string) (uint32, bool, error) { return mounts[root].bus, mounts[root].rm, nil },
	}
	if !v.leaves(`E:\copies`) || !v.leaves(`F:\copies`) {
		t.Fatal("positive twin: an SD card in a reader and a USB disk reported fixed leave")
	}
	assure.Reached(t, !v.leaves(`C:\Users\me\copies`) && !v.leaves(`D:\copies`),
		"a fixed eMMC disk on the SD or MMC bus is the computer's own")
}

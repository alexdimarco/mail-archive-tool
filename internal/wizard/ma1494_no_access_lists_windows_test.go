//go:build windows

package wizard

import (
	"os"
	"testing"
)

// covers: MA-1494
// RV-02's volume check itself, on Windows (the re-verify of rounds A and B): read through an open
// folder's handle, this computer's own drive (the test's temp folder, NTFS or ReFS) keeps access lists,
// so setVolumeKeepsNoAccessLists says false and folders there are judged by their owner, as before.
// The FAT and exFAT side is the Windows lab's: a set made on an exFAT USB stick and run again brings
// each archive up to date, with no "(2)" made.
func TestTheVolumeCheckReadsThisComputersDriveAsKeepingAccessLists(t *testing.T) {
	d, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if setVolumeKeepsNoAccessLists(d) {
		t.Error("the temp folder's drive reads as keeping no access lists")
	}
}

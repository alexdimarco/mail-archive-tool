//go:build windows

package tbrestore

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// covers: MA-1143, R40
// The Windows legs with Windows' own calls (run on the VM; CI's test gate is
// Linux-only): this computer's own %LOCALAPPDATA%\Packages, found through
// FOLDERID_LocalAppData, answers without error; a Local Folders on the system drive is
// not a network drive (GetDriveTypeW, positive twin); one on a drive Windows reports
// remote, or at a UNC path, gets W-NETWORK naming it; the real identity of a local
// folder reads (volume serial and file index, and its file system name).
func TestRestoreWindowsLegsOnWindows(t *testing.T) {
	_ = StoreWarning() // the VM has no Store Thunderbird (lab 2026-11-06); either answer is fine here
	_, local := localProfile(t)
	lf := LocalFolders{Dir: local}
	if w := NetworkWarning(lf); w != "" {
		t.Fatalf("a folder on this computer's own drive is not a network drive: %q", w)
	}
	id, err := DirIdentity(local)
	if err != nil || id.Volume == 0 || id.FSType == "" {
		t.Fatalf("a local folder's identity reads: %+v %v", id, err)
	}

	was := driveTypeSeam
	driveTypeSeam = func(string) uint32 { return windows.DRIVE_REMOTE }
	defer func() { driveTypeSeam = was }()
	w := NetworkWarning(lf)
	if !strings.Contains(w, "network drive") || !strings.Contains(w, local) {
		t.Fatalf("a remote drive gives W-NETWORK naming the folder: %q", w)
	}
	driveTypeSeam = was
	unc := LocalFolders{Dir: `\\nas\share\Thunderbird\Local Folders`}
	if w := NetworkWarning(unc); !strings.Contains(w, `\\nas\share`) {
		t.Fatalf("a UNC Local Folders gives W-NETWORK: %q", w)
	}
}

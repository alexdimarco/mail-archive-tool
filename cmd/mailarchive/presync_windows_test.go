//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/presyncjob"
)

// covers: MA-559
// The Windows pre-sync wiring's state directory is per-user and NON-ROAMING
// (design §4.6, PC-W5): the command line's -thunderbird-sync now runs on the shared
// hook (design-presync-attended T20), whose state folder presyncjob.StateDir
// resolves under %LOCALAPPDATA% (via the pinned instance.Dir helper), never
// %APPDATA%\Roaming — so a live pid in the write-ahead record never travels to
// another machine.
func TestPresyncStateDirIsLocalNotRoaming(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\lab\AppData\Local`)
	dir, err := presyncjob.StateDir()
	if err != nil {
		t.Fatalf("presyncjob.StateDir: %v", err)
	}
	if !strings.HasPrefix(dir, `C:\Users\lab\AppData\Local`) {
		t.Errorf("presync state dir %q is not under %%LOCALAPPDATA%% (PC-W5)", dir)
	}
	if strings.Contains(strings.ToLower(dir), "roaming") {
		t.Errorf("presync state dir %q is under Roaming — PC-W5 forbids it", dir)
	}
	if filepath.Base(dir) != "presync" {
		t.Errorf("presync state dir %q does not end in 'presync'", dir)
	}
}

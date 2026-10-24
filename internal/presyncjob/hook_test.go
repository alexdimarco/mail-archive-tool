//go:build darwin || windows

package presyncjob

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
)

// covers: MA-580
// On a platform with a pre-sync adapter, SetHook installs app.Run's BeforeRead
// hook, so a scheduled or Run-now GUI job pre-syncs exactly as the -thunderbird-
// sync CLI flag does. It only SETS the hook here (it does not invoke it), so no
// Thunderbird is launched by this test.
func TestSetHookInstallsBeforeRead(t *testing.T) {
	var opts app.Options
	if opts.BeforeRead != nil {
		t.Fatal("precondition: BeforeRead should start nil")
	}
	if err := SetHook(&opts, "", io.Discard); err != nil {
		t.Fatalf("SetHook: %v", err)
	}
	if opts.BeforeRead == nil {
		t.Error("SetHook left opts.BeforeRead nil; a GUI/scheduled job would not pre-sync")
	}
}

// covers: MA-580
// StateDir is the shared per-user "presync" directory (never the archive).
func TestStateDirIsPresyncSubdir(t *testing.T) {
	dir, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(dir), "/presync") {
		t.Errorf("StateDir = %q, want it to end in /presync", dir)
	}
}

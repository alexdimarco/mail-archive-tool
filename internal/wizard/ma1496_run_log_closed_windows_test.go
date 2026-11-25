//go:build windows

package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/export"
)

// covers: MA-93, MA-1496
// On Windows a folder that holds an open file cannot be deleted or moved, so a just-made archive's
// folder must be removable right after its export returns, while this program (the wizard, or
// MailArchive Desktop that runs it) stays open (round D; the Windows lab of MA-1503). The garbage
// collector is off for the test, as in the lab's GOGC=off run, so a log left open is never closed by
// it. Positive twin first: while the export runs, its log cannot be removed (the check sees an open
// file here). Then the one-archive export's folder, and each folder of a set and the set's folder,
// are removed whole right after the export returns.
func TestAnArchivesFolderCanBeRemovedRightAfterItsExport(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	ownSettings(t)
	rows, _ := setMail(t)

	out := filepath.Join(t.TempDir(), "Archive")
	var whileOpen error
	s := &flowScript{summary: zenity.ErrCanceled}
	s.onOpen = func(int) { whileOpen = os.Remove(filepath.Join(out, "mailarchive.log")) }
	s.install(t)
	if err := runExport(wizardChoice{inputs: []string{rows[2].path}, out: out, mode: export.Incremental}); err != nil {
		t.Fatalf("the export: %v", err)
	}
	if whileOpen == nil {
		t.Fatal("positive twin: the log was removed while its export ran, so this check proves nothing here")
	}
	if err := os.RemoveAll(out); err != nil {
		t.Fatalf("the archive's folder cannot be removed right after its export: %v", err)
	}
	if _, err := os.Lstat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the archive's folder is still there: %v", err)
	}

	parent := setFresh(t)
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err := makeSet(rows, "darwin")
	if err != nil || len(made) != 5 {
		t.Fatalf("the set: %+v %v", made, err)
	}
	for _, m := range made {
		if m.Outcome != MadeFinished {
			t.Errorf("an archive of the set did not finish: %+v", m)
		}
		if err := os.RemoveAll(m.Folder); err != nil {
			t.Errorf("%s cannot be removed right after the set: %v", m.Folder, err)
		}
	}
	if err := os.Rename(parent, parent+" moved"); err != nil {
		t.Errorf("the set's folder cannot be moved right after the set: %v", err)
	}
}

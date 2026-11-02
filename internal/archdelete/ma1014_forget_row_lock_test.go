package archdelete

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1014, R36
// Step 9's and Remove from list's removal of a row (ForgetRowAt) changes Your archives under the
// list's lock (the delete residuals' security pass, CON-07): while another MailArchive program
// holds it past the bound, the removal is refused, archindex.BusyError, the list byte-identical;
// and Remove from list says "Could not remove the row from Your archives. Your archives (FILE) is
// being changed by another MailArchive window; try again.", the row kept. Positive twin first:
// with the lock free, the row is removed.
func TestARowIsRemovedOnlyUnderTheListsLock(t *testing.T) {
	settings := t.TempDir()
	list := filepath.Join(settings, archindex.Name)
	first, keep := filepath.Join(t.TempDir(), "First"), filepath.Join(t.TempDir(), "Keep")
	if err := archindex.WriteFile(list, archindex.Index{Entries: []archindex.Entry{{Path: first}, {Path: keep}}}); err != nil {
		t.Fatal(err)
	}
	if err := ForgetRowAt(list, first, "linux"); err != nil {
		t.Fatalf("positive twin: the row removed with the lock free: %v", err)
	}
	if idx, _ := archindex.ReadFile(list); len(idx.Entries) != 1 || idx.Entries[0].Path != keep {
		t.Fatalf("positive twin: entries %+v", idx.Entries)
	}

	dir := smallArchive(t, t.TempDir())
	if err := archindex.WriteFile(list, archindex.Index{Entries: []archindex.Entry{{Path: dir}, {Path: keep}}}); err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.Acquire(archindex.LockPath(list))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	before, _ := os.ReadFile(list)
	unchanged := func() bool { after, _ := os.ReadFile(list); return bytes.Equal(before, after) }

	err = ForgetRowAt(list, dir, "linux")
	var busy *archindex.BusyError
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 1, err.Error()
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("Your archives ("+list+") is being changed by another MailArchive window; try again"),
		assure.NoSideEffect(func() bool { return errors.As(err, &busy) && unchanged() }))

	_, x := unlistRig(t, dir)
	x.ForgetRow, x.SettingsDir = nil, settings
	res := yes(t, dir, x)
	rc = 0
	if !res.Removed {
		rc = 1
	}
	assure.Refused(t, rc, strings.Join(res.Lines, " "), assure.Code(1),
		assure.Names(msgRowFailed, "is being changed by another MailArchive window; try again."),
		assure.NoSideEffect(func() bool { return !res.Removed && unchanged() }))
}

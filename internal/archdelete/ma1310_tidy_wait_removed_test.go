package archdelete

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1310, R45
// Delete's step 3 and Remove from list's remove this computer's record of a wait for another
// computer's tidy-up of the folder's archive (<key>.tidy-wait; the B3 UX walk's U2), as they
// remove the alert files. Positive twin first: another folder's wait is kept.
func TestDeleteAndRemoveFromListRemoveTheTidyUpWait(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	dir := smallArchive(t, t.TempDir())
	other := filepath.Join(home, "other")
	for _, f := range []string{dir, other} {
		if _, err := gmailanswer.NoteTidyWait(f, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if failed, err := removeNameFiles([]string{schedule.DefaultNameFor(dir)}, []string{dir}, ""); err != nil {
		t.Fatalf("%s: %v", failed, err)
	}
	if _, ok := gmailanswer.ReadTidyWait(other); !ok {
		t.Fatal("positive twin: another folder's wait was removed")
	}
	p, _ := gmailanswer.TidyWaitPath(dir)
	if _, err := os.Lstat(p); err == nil {
		t.Fatalf("step 3 left the tidy-up wait %s", p)
	}
}

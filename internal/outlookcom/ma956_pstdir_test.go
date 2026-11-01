package outlookcom

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-956, R36
// A scheduled run's PST export never recreates its archive folder (design-archive-delete §7,
// MA-689's decoy; del-s2-A2-02): with ParentMustExist it makes only _outlook-pst itself, and
// an archive folder that is gone is an error, left gone. Positive twins first: the folder
// is made under an existing archive folder, an existing one is reused, and without
// ParentMustExist (a run at a terminal) the whole path is made as before.
func TestMA956_PSTDirNeverRecreatesTheArchiveFolder(t *testing.T) {
	out := t.TempDir()
	dir := filepath.Join(out, "_outlook-pst")
	if err := makePSTDir(dir, true); err != nil {
		t.Fatalf("under an existing archive folder: %v", err)
	}
	assure.Reached(t, isDir(dir), "the PST folder made")
	if err := makePSTDir(dir, true); err != nil {
		t.Fatalf("an existing PST folder is reused: %v", err)
	}
	attended := filepath.Join(t.TempDir(), "new archive", "_outlook-pst")
	if err := makePSTDir(attended, false); err != nil || !isDir(attended) {
		t.Fatalf("a run at a terminal makes the whole path: %v", err)
	}

	gone := filepath.Join(t.TempDir(), "archive")
	err := makePSTDir(filepath.Join(gone, "_outlook-pst"), true)
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, errText(err), assure.Code(1), assure.Names(gone),
		assure.NoSideEffect(func() bool { _, err := os.Lstat(gone); return os.IsNotExist(err) }))

	file := filepath.Join(out, "not-a-folder")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := makePSTDir(file, true); err == nil {
		t.Fatal("a file where the PST folder goes is not a PST folder")
	}
}

// covers: MA-956, R36
// RemoveExported removes the PSTs a refused run's export wrote — a full copy of the mailbox
// in a folder of the person's files its archive was deleted from (del-s2-A1-02) — and the
// PST folder when that leaves it empty; anything else in it is kept. Positive twin first:
// a folder still holding another file is kept with it.
func TestMA956_RemoveExportedPSTs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "_outlook-pst")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	a, b, other := filepath.Join(dir, "a.pst"), filepath.Join(dir, "b.pst"), filepath.Join(dir, "other.pst")
	for _, p := range []string{a, b, other} {
		if err := os.WriteFile(p, []byte("mail"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	RemoveExported(dir, []string{a, b})
	left, _ := os.ReadDir(dir)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	assure.Reached(t, slices.Equal(names, []string{"other.pst"}), "only the run's PSTs removed")

	RemoveExported(dir, []string{other})
	if isDir(dir) {
		t.Fatal("an emptied PST folder is removed")
	}
	RemoveExported(dir, []string{a}) // nothing there: no error, nothing made
	if isDir(dir) {
		t.Fatal("nothing is made back")
	}
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

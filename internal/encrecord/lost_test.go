package encrecord

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-777, R37
// The lost list (§4.3): a closed list of archive ids beside the record, owner-only,
// sorted and without repeats; clearing the last id removes the file, and clearing
// one never listed writes nothing; anything that is not an archive id is refused,
// whether asked to be listed or found in the file.
func TestTheLostListHoldsOnlyArchiveIDs(t *testing.T) {
	dir := sandbox(t)
	path := filepath.Join(dir, LostName)
	if err := ClearLost(idA); err != nil || exists(path) {
		t.Fatalf("clearing an id never listed wrote %s: %v", path, err)
	}
	for _, id := range []string{idB, idA, idB} {
		if err := MarkLost(id); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Lost()
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, slices.Equal(got, []string{idA, idB}), "the sorted lost list without repeats")
	if fi, err := os.Stat(path); err != nil || runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("the lost list must be owner-only: %v %v", fi.Mode(), err)
	}

	err = MarkLost("../../etc/passwd")
	code1, msg := code(err), ""
	if err != nil {
		msg = err.Error()
	}
	assure.Refused(t, code1, msg, assure.Code(1), assure.Names("not an archive id"),
		assure.NoSideEffect(func() bool { l, _ := Lost(); return len(l) == 2 }))

	for _, id := range []string{idA, idB} {
		if err := ClearLost(id); err != nil {
			t.Fatal(err)
		}
	}
	if exists(path) {
		t.Fatal("the emptied lost list was kept")
	}

	if err := os.WriteFile(path, []byte(idA+"\nnot an id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Lost(); err == nil {
		t.Fatal("a lost list holding something other than ids was read")
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

package delmark

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-1053, R36
// The bounded read reads only the very file it judged a regular marker (§15.3; the
// slice-1 pass's LOW-3). Its open goes through an os.Root, which follows a link that
// stays inside the folder, so a name swapped for such a link between the judgment and
// the open would otherwise be read through (the pass saw it 204 times in 2 million
// reads under a tight swap loop). Positive twin first: the same read without the swap
// gives the details. The swap is made at the open's seam, so the test is exact.
func TestTheReadReadsOnlyTheFileItJudged(t *testing.T) {
	was := openIn
	t.Cleanup(func() { openIn = was })
	d := t.TempDir()
	plant(t, d, marker)
	opened := 0
	openIn = func(root *os.Root, name string) (*os.File, error) {
		opened++
		return was(root, name)
	}
	if got := Read(d); !got.Known || opened != 1 {
		t.Fatalf("the positive twin: %+v, opened %d times", got, opened)
	}

	d = t.TempDir()
	plant(t, d, "not a marker")
	if err := os.WriteFile(filepath.Join(d, "inside.json"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	swapped := false
	openIn = func(root *os.Root, name string) (*os.File, error) {
		if err := os.Remove(filepath.Join(d, Name)); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, "inside.json", filepath.Join(d, Name))
		swapped = true
		return was(root, name)
	}
	if got := Read(d); got.Known || got.Run != "" || got.Began != "" {
		t.Fatalf("a link swapped in after the judgment was read through: %+v", got)
	}
	if !swapped {
		t.Fatal("the swap never ran")
	}
}

// covers: MA-1053, R36
// DM-22's routing, from the pass's probes: a folder that is not a reachable folder —
// a file where the folder should be, a folder whose parent this account cannot
// search — has no marker (Absent, no error), so the archive-absent refusal
// (app.RequireExistingOut, the same folder check) speaks for it; a folder this account
// cannot search itself is "could not check" (DM-23). Positive twin first: a reachable
// folder holding the marker.
func TestAFolderThatCannotBeReachedHasNoMarker(t *testing.T) {
	d := t.TempDir()
	plant(t, d, marker)
	if st, err := Present(d); st != Regular || err != nil {
		t.Fatalf("the positive twin: %v %v", st, err)
	}
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if st, err := Present(file); st != Absent || err != nil {
		t.Fatalf("a file where the folder should be: %v %v", st, err)
	}
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	parent := t.TempDir()
	arch := filepath.Join(parent, "archive")
	if err := os.Mkdir(arch, 0o700); err != nil {
		t.Fatal(err)
	}
	plant(t, arch, marker)
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0o700) })
	if st, err := Present(arch); st != Absent || err != nil {
		t.Fatalf("a folder whose parent cannot be searched: %v %v", st, err)
	}
}

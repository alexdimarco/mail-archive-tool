package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// observeAtomicWrite swaps the temp-create and directory-sync seams for the
// test's duration, recording the temp pattern used, the temp file created, and
// every directory synced together with whether dst already held its new bytes at
// that moment (the rename must precede the sync for the sync to make it durable).
func observeAtomicWrite(t *testing.T, dst string, want []byte) (patterns, temps, synced *[]string, renamedFirst *bool) {
	t.Helper()
	patterns, temps, synced = &[]string{}, &[]string{}, &[]string{}
	renamedFirst = new(bool)
	origCreate, origSync := createTemp, syncDirFn
	createTemp = func(dir, pattern string) (*os.File, error) {
		f, err := origCreate(dir, pattern)
		*patterns = append(*patterns, pattern)
		if f != nil {
			*temps = append(*temps, f.Name())
		}
		return f, err
	}
	syncDirFn = func(dir string) {
		got, err := os.ReadFile(dst)
		*renamedFirst = err == nil && string(got) == string(want)
		*synced = append(*synced, dir)
		origSync(dir)
	}
	t.Cleanup(func() { createTemp, syncDirFn = origCreate, origSync })
	return
}

// covers: MA-713
func TestWriteFileAtomic0600SyncsTheDirectoryAfterTheRename(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "sub", "settings.json")
	want := []byte(`{"a":1}`)
	_, temps, synced, renamedFirst := observeAtomicWrite(t, dst, want)

	if err := WriteFileAtomic0600(dst, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	assure.Reached(t, *synced, "directory syncs")
	if (*synced)[0] != filepath.Dir(dst) {
		t.Fatalf("synced %q, want the parent directory %q", (*synced)[0], filepath.Dir(dst))
	}
	if !*renamedFirst {
		t.Fatal("the directory was synced before the new bytes were renamed into place — the sync cannot make that rename durable")
	}
	for _, tmp := range assure.Reached(t, *temps, "temp files") {
		if _, err := os.Stat(tmp); !os.IsNotExist(err) {
			t.Fatalf("temp %s survived a successful write (stat err %v)", tmp, err)
		}
	}
}

// covers: MA-713
func TestWriteFileAtomic0600TempUsesTheCallersTempName(t *testing.T) {
	dst := filepath.Join(t.TempDir(), ".mailarchive-vault.json")
	want := []byte("vault")
	patterns, temps, synced, _ := observeAtomicWrite(t, dst, want)

	if err := WriteFileAtomic0600Temp(dst, want, ".mailarchive-*.tmp"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := assure.Reached(t, *patterns, "temp patterns"); got[0] != ".mailarchive-*.tmp" {
		t.Fatalf("temp pattern %q, want the caller's .mailarchive-*.tmp (the name SweepOrphans reaps)", got[0])
	}
	name := filepath.Base(assure.Reached(t, *temps, "temp files")[0])
	if !strings.HasPrefix(name, ".mailarchive-") || !strings.HasSuffix(name, ".tmp") {
		t.Fatalf("temp %q does not carry the sweepable .mailarchive-*.tmp name", name)
	}
	assure.Reached(t, *synced, "directory syncs")
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != string(want) {
		t.Fatalf("dst holds %q, want %q", got, want)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %04o, want 0600", fi.Mode().Perm())
	}
}

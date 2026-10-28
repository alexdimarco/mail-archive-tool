package job

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-863, R37
// Write flushes the job file's temporary copy to disk before it renames it into place
// (design-desktop-settings-card section 9 step 3, C-18): at the flush the new bytes are all in
// the temporary file and the old job file is still whole. A flush that fails is a failed write
// that leaves the old file as it was and no temporary file behind. Positive twin first: a
// working flush writes the file.
func TestWriteFlushesTheJobFileBeforeTheRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailarchive", "mailarchive-1234abcd.json")
	old := Job{Name: "mailarchive-1234abcd", Out: "/archive", Mode: "incremental"}
	if err := Write(path, old); err != nil {
		t.Fatal(err)
	}
	oldBytes, _ := os.ReadFile(path)

	orig := syncFile
	t.Cleanup(func() { syncFile = orig })
	flushed := 0
	syncFile = func(f *os.File) error {
		flushed++
		if f.Name() == path {
			t.Errorf("the job file itself was flushed, not its temporary copy")
		}
		if cur, _ := os.ReadFile(path); string(cur) != string(oldBytes) {
			t.Errorf("at the flush the job file was already replaced")
		}
		if tmp, _ := os.ReadFile(f.Name()); !strings.Contains(string(tmp), `"mode": "full"`) {
			t.Errorf("at the flush the temporary copy did not hold the new job: %s", tmp)
		}
		return orig(f)
	}
	if err := Write(path, Job{Name: "mailarchive-1234abcd", Out: "/archive", Mode: "full"}); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	if flushed != 1 {
		t.Fatalf("Write flushed %d times, want once before the rename", flushed)
	}
	if j, err := Read(path); err != nil || j.Mode != "full" {
		t.Fatalf("the new job was not written: %+v %v", j, err)
	}
	newBytes, _ := os.ReadFile(path)

	syncFile = func(*os.File) error { return errors.New("the disk refused the flush") }
	if err := Write(path, Job{Name: "mailarchive-1234abcd", Out: "/archive", Mode: "incremental"}); err == nil || !strings.Contains(err.Error(), "refused the flush") {
		t.Fatalf("a failed flush was not a failed write: %v", err)
	}
	if cur, _ := os.ReadFile(path); string(cur) != string(newBytes) {
		t.Error("a failed flush replaced the job file")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".job-*.tmp")); len(left) != 0 {
		t.Errorf("a failed flush left a temporary file: %v", left)
	}
}

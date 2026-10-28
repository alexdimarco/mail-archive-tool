package util

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-855, R37
// StillSame tells a file re-created at the same path from the file as it was, even
// when the file system gives the new file the removed one's number, as Linux does at
// once (P3-8's CI run on ubuntu): the size or the times differ. Positive twin: the
// file untouched is still the same; one rewritten in place with other bytes is not.
func TestAReCreatedFileIsNotStillTheSame(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mailarchive")
	os.WriteFile(p, []byte("the program that started"), 0o755)
	start, _ := os.Stat(p)
	again, _ := os.Stat(p)
	if !StillSame(start, again) {
		t.Fatal("positive twin: the file untouched is still the same")
	}
	time.Sleep(20 * time.Millisecond) // past a coarse file-system clock tick
	os.Remove(p)
	os.WriteFile(p, []byte("the program that started"), 0o755) // same size, same bytes
	recreated, _ := os.Stat(p)
	os.WriteFile(p, []byte("another program, longer"), 0o755)
	rewritten, _ := os.Stat(p)
	assure.Reached(t, !StillSame(start, recreated) && !StillSame(start, rewritten) && !StillSame(nil, again),
		"a re-created or rewritten file is not the file as it was")
}

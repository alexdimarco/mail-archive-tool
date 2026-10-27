package index

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-812, R37
// A sealed index image written into an unencrypted copy is kept as its search.db
// only when it is a rollback-journal SQLite database (Phase 3 P3-4): the image a
// sealed index holds passes; an empty file (it would open as a new index with no
// rows), bytes that are not SQLite and a write-ahead-log header are refused with
// ErrImage, and the copy rebuilds instead.
func TestAnImageFileIsCheckedBeforeACopyKeepsIt(t *testing.T) {
	ix, _ := OpenMemory(nil)
	img, err := ix.Image()
	ix.Close()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "good.db")
	os.WriteFile(good, img, 0o600)
	if err := CheckImageFile(good); err != nil {
		t.Fatalf("positive twin: a sealed index image refused: %v", err)
	}
	wal := append([]byte(nil), img...)
	wal[18], wal[19] = 2, 2
	for label, b := range map[string][]byte{
		"empty":          {},
		"not a database": []byte("hello, this is not sqlite at all, not even close"),
		"a WAL header":   wal,
	} {
		p := filepath.Join(dir, label)
		os.WriteFile(p, b, 0o600)
		if err := CheckImageFile(p); !errors.Is(err, ErrImage) {
			t.Errorf("%s: want ErrImage, got %v", label, err)
		}
	}
	if err := CheckImageFile(filepath.Join(dir, "missing")); err == nil || errors.Is(err, ErrImage) {
		t.Fatalf("a missing file: want its own error, got %v", err)
	}
}

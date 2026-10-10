package util

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-427, R29, R12
// FreeBytes reads the free space of the volume that holds a directory without
// writing anything, and fails legibly, naming the path, for a directory that is
// not there (the copy export's per-message budget reads it, extraction §4.13).
func TestFreeBytes(t *testing.T) {
	dir := t.TempDir()
	free, err := FreeBytes(dir)
	if err != nil {
		t.Fatalf("FreeBytes(%s): %v", dir, err)
	}
	assure.Reached(t, free, "free bytes of the temp volume")
	missing := filepath.Join(dir, "not-here")
	if _, err := FreeBytes(missing); err == nil {
		t.Fatalf("FreeBytes of a missing directory succeeded")
	} else if !strings.Contains(err.Error(), "not-here") {
		t.Errorf("the error does not name the path: %v", err)
	}
}

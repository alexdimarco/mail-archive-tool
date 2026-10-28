package util

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-854, R37
// A pin that does not take says so (the P3-8 fixes' pass, WINPATH-1): on Windows a
// file removed between its Stat and its pin cannot be opened again, so its identity
// would be read only at the comparison — from a decoy put at the path by then, which
// matched. PinIdentity reports it, so a caller refuses. Off Windows the identity is
// fixed by the Stat and the pin takes; either way the identity is never the decoy's.
// Positive twin: a file still there is pinned and matches itself. Nothing is never
// pinned.
func TestAPinThatDoesNotTakeSaysSo(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "archive.bin")
	os.WriteFile(p, []byte("the file as it was"), 0o600)

	// Positive twin: the file is there; the pin takes and it matches itself.
	here, _ := os.Stat(p)
	here, ok := PinIdentity(here)
	again, _ := os.Stat(p)
	if !ok || !os.SameFile(here, again) {
		t.Fatalf("positive twin: a file still there is pinned (ok %v)", ok)
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	pinned, ok := PinIdentity(fi)
	os.WriteFile(p, []byte("a decoy put where it was"), 0o600)
	decoy, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && ok {
		t.Fatal("on Windows a file removed before its pin cannot be pinned, and the pin said it took")
	}
	// The file number alone: on Windows the pin's own question. Off Windows Linux may
	// give the decoy the removed file's number at once, so the identity is the file as
	// it was by StillSame (number, size and times) there.
	same := os.SameFile(pinned, decoy)
	if runtime.GOOS != "windows" {
		same = StillSame(pinned, decoy)
	}
	assure.Reached(t, !ok || !same,
		"a pin either says it did not take or holds the file as it was, never the decoy")
	if _, ok := PinIdentity(nil); ok {
		t.Fatal("nothing was pinned, and the pin said it took")
	}
}

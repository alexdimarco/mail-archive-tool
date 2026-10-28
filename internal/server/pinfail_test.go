package server

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// covers: MA-854, R37
// A reader entry whose identity pin did not take is not served (the P3-8 fixes' pass,
// WINPATH-1): on Windows its identity would be read only when it is opened, from
// whatever is at the path then, so an entry swapped in would match. A pin fails for
// real only in a race on Windows, so it is injected. Positive twin: the pin takes and
// the page is served.
func TestAnEntryWhosePinDidNotTakeIsNotServed(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "messages"), 0o755)
	os.WriteFile(filepath.Join(dir, "messages", "index.html"), []byte("<p>page</p>"), 0o644)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	segs := []string{"messages", "index.html"}
	f, err := openChecked(root, segs)
	if err != nil {
		t.Fatalf("positive twin: the page is served: %v", err)
	}
	f.Close()
	restore := util.FailPinsForTest()
	defer restore()
	_, _, ok := checkEntry(root, segs)
	f, err = openChecked(root, segs)
	if err == nil {
		f.Close()
	}
	assure.Reached(t, !ok && err != nil, "an entry whose pin did not take is refused")
}

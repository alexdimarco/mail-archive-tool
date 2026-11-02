package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-979, R36
// The preview and export starts ask the file system about the archive's folder before they take
// the lock other workspace requests wait on (ws.mu, ex.mu), and under it only check whether a
// delete of that folder is under way (del-s3a re-verification, R5): an offline network folder
// stalls only the request that asked, never every other workspace request. Both are still refused
// while a delete of the folder runs, nothing exported. Positive twin first: with no delete under
// way the preview is made, its folder asked outside the lock too.
func TestThePreviewAndExportStartsAskTheDiskOutsideTheirLocks(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Asked")
	arch := archSource("archive-other", dir)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true))

	var mu sync.Mutex
	asked := 0
	var underLock []string
	orig := statFolder
	t.Cleanup(func() { statFolder = orig })
	statFolder = func(p string) (os.FileInfo, error) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			var held []string
			if e.d.ws.mu.TryLock() {
				e.d.ws.mu.Unlock()
			} else {
				held = append(held, "ws.mu")
			}
			if e.d.ex.mu.TryLock() {
				e.d.ex.mu.Unlock()
			} else {
				held = append(held, "ex.mu")
			}
			mu.Lock()
			asked++
			underLock = append(underLock, held...)
			mu.Unlock()
		}
		return orig(p)
	}
	report := func() (int, string) {
		mu.Lock()
		defer mu.Unlock()
		return asked, strings.Join(underLock, ", ")
	}

	id, _ := e.preview(sel)
	if n, held := report(); n == 0 || held != "" {
		t.Fatalf("the positive twin: the preview's start asked the folder %d times, under %q", n, held)
	}

	end, ok := e.d.running.tryBegin(dir, dir) // a delete of it under way
	if !ok {
		t.Fatal("no delete could be recorded")
	}
	defer end()
	before, _ := report()
	code, out := e.post("/api/exports/start", map[string]any{"previewId": id})
	assure.Refused(t, code, toText(out), assure.Code(code), assure.Names(deletingExport),
		assure.NoSideEffect(func() bool { _, live := e.d.exportState(); return !live }))
	f := e.previewFailure(sel)
	assure.Refused(t, http.StatusOK, toText(f), assure.Code(http.StatusOK), assure.Names(deletingPreview),
		assure.NoSideEffect(func() bool { _, live := e.d.exportState(); return !live }))
	if n, held := report(); n < before+2 || held != "" {
		t.Errorf("the export and preview starts asked the folder %d times (want at least 2), under %q (want no lock held)", n-before, held)
	}
}

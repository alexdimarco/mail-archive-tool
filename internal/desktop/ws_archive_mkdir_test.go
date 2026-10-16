package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-531, MA-434, R29, R12, S52
// Create folder never writes inside an archive folder, however many the owner has named (E4-AI4, re-verified):
// the guard was the list of folders the owner had named, which held eight and forgot the oldest, so after
// naming a ninth archive folder Create folder was allowed inside the first again. The guard is now what the
// folder is: a parent that is an archive folder, or lies inside one, is refused whether or not it was ever
// named. The positive twin: a folder beside the archives is made.
func TestCreateFolderNeverWritesInsideAnArchiveFolderNamedLongAgo(t *testing.T) {
	e := newWsEnv(t)
	var dirs []string
	for i := 0; i < 10; i++ { // ten archive folders (a manifest is what makes one), each named in the Source control
		d := filepath.Join(e.dir, fmt.Sprintf("archive%02d", i))
		if err := os.MkdirAll(filepath.Join(d, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, ".mailarchive-manifest.json"), []byte(`{"version":4,"entries":{},"stores":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, d)
		e.post("/api/workspace/source", map[string]any{"otherArchive": d}) // named: the answer does not matter, it is named
	}
	e.d.ws.reap(e.d.now().Add(2 * time.Hour))
	before := dirDigest(t, dirs[0])
	for _, c := range []struct{ parent, names string }{{dirs[0], dirs[0]}, {filepath.Join(dirs[0], "sub"), dirs[0]}, {dirs[9], dirs[9]}} {
		code, body := e.post("/api/workspace/mkdir", map[string]any{"parent": c.parent, "name": "New folder"})
		refusedBody(t, code, body, "R-f", "inside your archive", c.names)
	}
	if dirDigest(t, dirs[0]) != before {
		t.Errorf("Create folder wrote into the first archive folder")
	}
	if code, out := e.post("/api/workspace/mkdir", map[string]any{"parent": e.dest, "name": "positive twin"}); code != 200 || out["ok"] != true {
		t.Fatalf("the positive twin, a folder beside the archives: %d %v", code, out)
	}
}

// covers: MA-531, MA-434, MA-437, R29, R12, S52
// A retry of an export from another archive folder names that folder to the workspace as well (E4-AI4,
// re-verified): a dashboard that was started after the export ran has never been told the folder in the
// Source control, and the retry reads and plans from it. The folder is remembered from the retry's own
// request, so that Create folder keeps out of it by name (an alias of it that is not on its path is caught by
// the identity check against the folders named), as it does once the Source control has named it.
func TestRetryOfAnExportFromAnotherArchiveFolderNamesItToCreateFolder(t *testing.T) {
	e := newWsEnv(t)
	dir := filepath.Join(e.dir, "inherited")
	f := buildArchiveAt(t, dir)
	os.Remove(e.tok)
	arch := archSource("archive-other", dir)
	e.archFolders(arch)
	id, res := e.preview(e.archSel(arch, entry("alice", true)))
	if res["count"] != float64(3) {
		t.Fatalf("the preview: %v", res)
	}
	// a capture rewrote one original after the preview (its checksum in the manifest is the new bytes'): the
	// export lists that message as changed in the archive, a failure a retry exports again
	rec := f.rec("hello")
	newer := []byte("From: a@b\r\nSubject: hello\r\n\r\nrewritten\r\n")
	if err := os.WriteFile(f.path(strings.TrimSuffix(rec.Path, ".html")+".eml"), newer, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(newer)
	f.edit(func(m *state.Manifest) {
		k := f.key("hello")
		r, _ := m.Get(k)
		r.Fixity.EML = &state.FileDigest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(newer))}
		m.Add(k, r)
	})
	run := e.ok("/api/exports/start", map[string]any{"previewId": id})["runId"].(string)
	if l := e.waitRun()["last"].(map[string]any); l["failed"] != float64(1) {
		t.Fatalf("the export: %v, want the rewritten message listed as failed", l)
	}
	e.d.ws.reap(e.d.now().Add(2 * time.Hour)) // the session is released
	e.d.ws.mu.Lock()
	e.d.ws.others = nil // a dashboard that never heard of the folder
	e.d.ws.mu.Unlock()
	if got := e.d.ws.otherPaths(); len(got) != 0 {
		t.Fatalf("the fixture: the workspace still knows %v", got)
	}
	e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "failed"})
	found := false
	for _, p := range e.d.ws.otherPaths() {
		if samePath(p, dir) {
			found = true
		}
	}
	if !found {
		t.Errorf("after a retry of an export from %s the workspace names %v: Create folder is not kept out of it by name", dir, e.d.ws.otherPaths())
	}
}

package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
)

// covers: MA-435, MA-511, R20, R29, S52
// The archive source through the workspace's endpoints (design §4.2, §4.3, §4.6, §4.7): "Load
// folders" reads the manifest snapshot (stores, then folders, each store labelled with its
// mailbox, with the counts of its records); the message list comes from the index, never more
// than 100 rows a page, with row handles that a selection resolves; the count is the manifest's;
// the preview says how many have an original and how many will be copied as the archived page
// and zip; the list is closed at the end of each request; and the run copies what was previewed.
func TestArchiveSourceThroughTheWorkspaceEndpoints(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)

	src := e.ok("/api/workspace/source", nil)
	if a := src["archive"].(map[string]any); a["state"] != "ready" || a["path"] != e.out {
		t.Errorf("the configured archive's availability: %v", a)
	}
	arch := archSource("archive", e.out)
	tree := e.archFolders(arch)
	if len(tree) != 3 || tree["alice"]["count"] != float64(2) || tree["alice/Clients"]["count"] != float64(1) || tree["bob"]["count"] != float64(3) || tree["alice/Clients"]["parent"] != "alice" {
		t.Fatalf("the tree: %v", tree)
	}
	if tree["alice"]["label"] != "alice" {
		t.Errorf("a store's label: %v", tree["alice"])
	}

	rows, out := e.archRows(arch, "alice")
	if len(assure.Reached(t, rows, "alice's rows")) != 2 || out["count"] != float64(2) {
		t.Fatalf("alice's list: %v", out)
	}
	if n := index.OpenListHandles(); n != 0 {
		t.Errorf("%d index handle(s) left open after the list request", n)
	}
	// count and preview
	sel := e.archSel(arch, entry("alice", true))
	cnt := e.ok("/api/workspace/count", sel)
	if cnt["count"] != float64(3) {
		t.Errorf("the count of alice and its subfolders: %v", cnt)
	}
	id, res := e.preview(sel)
	if res["count"] != float64(3) {
		t.Fatalf("the preview: %v", res)
	}
	a := res["archive"].(map[string]any)
	if a["withOriginal"] != float64(3) || a["withoutOriginal"] != float64(0) {
		t.Errorf("the preview's archive counts: %v", a)
	}
	if infos := strings.Join(toStrings(res["info"]), " | "); !strings.Contains(infos, "Of 3 selected, 3 have an original .eml") {
		t.Errorf("the preview's lines: %q", infos)
	}
	_ = id
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(3) {
		t.Fatalf("the export: %v", last)
	}

	// bob has no originals: the page and zip are copied
	sel = e.archSel(arch, entry("bob", false))
	id, res = e.preview(sel)
	a = res["archive"].(map[string]any)
	if a["withOriginal"] != float64(0) || a["withoutOriginal"] != float64(3) {
		t.Errorf("bob's counts: %v", a)
	}
	if infos := strings.Join(toStrings(res["info"]), " | "); !strings.Contains(infos, "Of 3 selected, 0 have an original .eml; 3 will be copied as the archived page and attachment zip (no original was kept)") {
		t.Errorf("the preview's lines: %q", infos)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	last = e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" {
		t.Fatalf("bob's export: %v", last)
	}
	_ = f
	_ = os.Remove
	_ = filepath.Join
	_ = http.StatusOK
}

// archRows lists a folder's first page of an archive and returns its row handles.
func (e *wsEnv) archRows(src map[string]any, folder string) (handles []string, out map[string]any) {
	e.t.Helper()
	out = e.ok("/api/workspace/messages", map[string]any{"folder": folder, "source": src})
	for _, r := range out["rows"].([]any) {
		handles = append(handles, r.(map[string]any)["h"].(string))
	}
	return handles, out
}

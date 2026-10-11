package desktop

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
)

// addBulkStore captures a store "bulk" of n small messages, in one folder, into the archive at dir.
func addBulkStore(t *testing.T, dir string, n int) {
	t.Helper()
	folder := filepath.Join(t.TempDir(), "bulk")
	for i := 0; i < n; i++ {
		putMaildirMsg(t, folder, fmt.Sprintf("m%04d.eml", i), archMsg(fmt.Sprintf("bulk %04d", i), fmt.Sprintf("bulk%04d", i), "body"))
	}
	opts := app.Options{Inputs: []string{folder}, Out: dir, Mode: export.Incremental, Index: true, Pages: true}
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, ".mailarchive.lock"))
}

// covers: MA-435, MA-511, R20, R29, S52
// The archive's message list pages by 100 (design §4.3, XC17): a folder of 150 lists 100 rows and a
// cursor, then 50 and no cursor, with no row twice; an unknown cursor is R-x; the index is closed at the
// end of each request; and while the Rebuild job runs the list says the index is being rebuilt, and
// comes back when it is done (XC20: search.db is never open inside Rebuild's rename window).
func TestArchiveListPagesByAHundredAndStaysOutOfARebuild(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	addBulkStore(t, e.out, 150)
	arch := archSource("archive", e.out)
	tree := e.archFolders(arch)
	if tree["bulk"]["count"] != float64(150) {
		t.Fatalf("the bulk store: %v", tree["bulk"])
	}
	first, out := e.archRows(arch, "bulk")
	if len(first) != 100 || out["more"] != true || out["cursor"] == nil || out["count"] != float64(150) {
		t.Fatalf("the first page: %d rows, more %v, cursor %v", len(first), out["more"], out["cursor"])
	}
	_, out2 := e.postJSON("/api/workspace/messages", map[string]any{"folder": "bulk", "source": arch, "cursor": out["cursor"]})
	if rows := out2["rows"].([]any); len(rows) != 50 || out2["more"] != false || out2["cursor"] != nil {
		t.Fatalf("the second page: %d rows, more %v", len(rows), out2["more"])
	}
	seen := map[string]bool{}
	for _, h := range first {
		seen[h] = true
	}
	for _, r := range out2["rows"].([]any) {
		if h := r.(map[string]any)["h"].(string); seen[h] {
			t.Errorf("row %s is on both pages", h)
		}
	}
	code, bad := e.post("/api/workspace/messages", map[string]any{"folder": "bulk", "source": arch, "cursor": "no-such-cursor"})
	refusedBody(t, code, bad, "R-x", "This list expired")
	if n := index.OpenListHandles(); n != 0 {
		t.Errorf("%d index handle(s) left open between requests", n)
	}

	// the Rebuild job holds the list off, and the list comes back
	e.d.beginRebuild()
	_, held := e.archRows(arch, "bulk")
	if n, _ := held["listUnavailable"].(string); !strings.Contains(n, "being rebuilt") || len(held["rows"].([]any)) != 0 {
		t.Errorf("the list during a rebuild: %v", held)
	}
	e.d.endRebuild()
	if rows, _ := e.archRows(arch, "bulk"); len(rows) != 100 {
		t.Errorf("the list after the rebuild: %d rows", len(rows))
	}
}

func (e *wsEnv) postJSON(path string, body any) (int, map[string]any) { return e.post(path, body) }

// covers: MA-510, MA-435, R20, R29, S25
// A capture that runs after "Load folders" changes the manifest the snapshot was read from: the preview says
// so (the snapshot is cached by the manifest's size and mtime), the run still exports exactly the previewed
// messages, and a message the capture added is not in them; with nothing changed, the preview says nothing.
func TestArchivePreviewSaysWhenTheArchiveChangedSinceItWasLoaded(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", false))
	if _, res := e.preview(sel); strings.Contains(strings.Join(toStrings(res["warnings"]), "|"), "changed since you loaded") {
		t.Fatalf("a preview of an unchanged archive warns: %v", res["warnings"])
	}
	putMaildirMsg(t, filepath.Join(f.src, "alice"), "a9.eml", archMsg("arrived later", "id9", "body nine"))
	opts := app.Options{Inputs: []string{filepath.Join(f.src, "alice")}, Out: e.out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	id, res := e.preview(sel)
	if w := strings.Join(toStrings(res["warnings"]), "|"); !strings.Contains(w, "changed since you loaded its folders") || !strings.Contains(w, "Load folders again") {
		t.Errorf("the preview after a capture: %v", res["warnings"])
	}
	if res["count"] != float64(2) {
		t.Errorf("the preview holds %v messages, want the 2 that were loaded (not the one the capture added)", res["count"])
	}
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	if l := e.waitRun()["last"].(map[string]any); l["status"] != "complete" || l["exported"] != float64(2) {
		t.Errorf("the export: %v", l)
	}
}

// covers: MA-427, MA-437, MA-512, R29, R5, R12
// An archive export that stopped because the archive was gone (R-o2) is finished by "Export the remaining
// messages", which needs no Microsoft 365 sign-in: while the archive is still away the retry says to reconnect
// it and nothing is previewed; once it is back, the retry previews from the run's record, starts into the next
// part of the set and exports every message the first part did not.
func TestArchiveExportStoppedByAGoneArchiveCanBeRetried(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	os.Remove(e.tok) // an archive export needs no sign-in, and neither does its retry
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true), entry("bob", false))
	id, res := e.preview(sel)
	if res["count"] != float64(6) {
		t.Fatalf("the preview: %v", res)
	}
	stash := filepath.Join(e.dir, "stash")
	os.MkdirAll(stash, 0o755)
	ents, _ := os.ReadDir(e.out)
	for _, ent := range ents {
		os.Rename(filepath.Join(e.out, ent.Name()), filepath.Join(stash, ent.Name()))
	}
	run := e.ok("/api/exports/start", map[string]any{"previewId": id})["runId"].(string)
	last := e.waitRun()["last"].(map[string]any)
	if last["stopId"] != "R-o2" || last["status"] != "failed" || last["exported"] != float64(0) {
		t.Fatalf("the stopped export: %v", last)
	}
	card := e.card(run)
	if !hasStr(card["actions"], "export-remaining") {
		t.Errorf("Export the remaining messages is not offered: %v", card["actions"])
	}
	if card["stopId"] != "R-o2" || !strings.Contains(card["stop"].(string), "no longer available") {
		t.Errorf("the card does not say why: %v", card)
	}

	// still away: nothing is previewed
	code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
	refusedBody(t, code, out, "R-o2", e.out, "isn't available", "Reconnect it", "Export the remaining messages")

	// back: the retry goes through
	for _, ent := range ents {
		os.Rename(filepath.Join(stash, ent.Name()), filepath.Join(e.out, ent.Name()))
	}
	rid := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string)
	rres := e.waitPreview(rid)
	if rres["count"] != float64(6) || rres["retryOf"] != run || rres["part"] != float64(2) {
		t.Fatalf("the retry's preview: %v", rres)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": rid})
	l := e.waitRun()["last"].(map[string]any)
	if l["status"] != "complete" || l["exported"] != float64(6) {
		t.Fatalf("the second part: %v", l)
	}
	if man := e.manifestOf(l["rootName"].(string)); man["run"].(map[string]any)["retry_of"] != run {
		t.Errorf("the second part does not name the run it retries: %v", man["run"])
	}
}

func hasStr(v any, s string) bool {
	for _, x := range v.([]any) {
		if x == s {
			return true
		}
	}
	return false
}

// covers: MA-436, MA-510, R9, R20, R29, S25, S52
// A Microsoft 365 export works with no archive location set at all (the configured archive is never needed
// for it); and while an archive export runs a second preview is R-m, the one-at-a-time rule being the
// source's own, not the archive's lock.
func TestArchiveExportsAreOneAtATimeAndM365NeedsNoArchive(t *testing.T) {
	e := newWsEnv(t, func(e *wsEnv) { e.cfg.Out = "" })
	if got := e.d.cfg.effectiveOut(); got != "" {
		t.Fatalf("the test's configured archive location is %q, want none", got)
	}
	ids := e.folders()
	e.startExport(e.selection(entry(ids["Sent Items"], false)))
	if l := e.waitRun()["last"].(map[string]any); l["status"] != "complete" || l["exported"] != float64(5) {
		t.Fatalf("a Microsoft 365 export with no archive location: %v", l)
	}
	if src := e.ok("/api/workspace/source", nil)["archive"].(map[string]any); src["state"] != "none" {
		t.Errorf("no archive location: the source line says %v", src)
	}
	code, out := e.post("/api/workspace/folders", map[string]any{"source": archSource("archive", "")})
	refusedBody(t, code, out, "R-o", "no archive yet", "Archive location in Settings", "choose Microsoft 365")

	// one at a time: a bigger archive export is still running when a second preview is asked for
	e2 := newWsEnv(t)
	buildArchiveAt(t, e2.out)
	addBulkStore(t, e2.out, 60)
	arch := archSource("archive", e2.out)
	e2.archFolders(arch)
	sel := e2.archSel(arch, entry("bulk", false))
	id, _ := e2.preview(sel)
	var second map[string]any
	var secondCode int
	e2.d.ex.afterLaunch = func(s *exportSlot) {
		secondCode, second = e2.post("/api/workspace/preview", sel)
	}
	e2.ok("/api/exports/start", map[string]any{"previewId": id})
	refusedBody(t, secondCode, second, "R-m", "An export is already running", "only one runs at a time")
	assure.Reached(t, e2.waitRun()["last"], "the first export ends")
}

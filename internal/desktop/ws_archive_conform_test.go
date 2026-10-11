package desktop

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// covers: MA-435, R20, R29, S52
// The workspace never reads search.db inside Rebuild's rename window (design §4.3, XC20), through the
// Maintenance card's own Rebuild job and not a seam of the test's: while the job runs, the archive's
// message list says the index is being rebuilt and the preview says its names carry no index data,
// whole-folder selection still resolves from the manifest; when the job is done the list is back.
func TestTheRebuildJobHoldsTheArchiveListOffUntilItIsDone(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	if rows, _ := e.archRows(arch, "alice"); len(rows) != 2 { // the positive twin: the list works before the job
		t.Fatalf("alice's list before the rebuild: %d rows, want 2", len(rows))
	}

	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	let := func() { once.Do(func() { close(release) }) }
	t.Cleanup(let)
	e.d.rebuild = func(out string, l *log.Logger) (app.RebuildReport, error) {
		close(entered)
		<-release
		return app.RebuildReport{}, nil
	}
	e.ok("/api/rebuild", nil)
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the Rebuild job never started")
	}

	_, held := e.archRows(arch, "alice")
	if n, _ := held["listUnavailable"].(string); !strings.Contains(n, "being rebuilt") || len(held["rows"].([]any)) != 0 {
		t.Errorf("the list while the Rebuild job runs: %v, want it held off", held)
	}
	_, res := e.preview(e.archSel(arch, entry("alice", true)))
	if res["count"] != float64(3) || !strings.Contains(strings.Join(toStrings(res["warnings"]), "|"), "being rebuilt") {
		t.Errorf("whole-folder selection during the rebuild: %v, want 3 messages and a warning that the index is being rebuilt", res)
	}

	let()
	deadline := time.Now().Add(10 * time.Second)
	for e.d.rebuilding.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the Rebuild job never let the list back")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rows, out := e.archRows(arch, "alice"); len(rows) != 2 || out["listUnavailable"] != nil {
		t.Errorf("the list after the rebuild: %d rows, %v", len(rows), out["listUnavailable"])
	}
}

// archiveRowsOf is every row the archive's index lists, through the engine's own read-only open.
func archiveRowsOf(t *testing.T, dir string) string {
	t.Helper()
	a, err := app.OpenArchive(dir, app.ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	h, err := a.OpenList()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var out []string
	for _, n := range a.Tree() {
		tok, folder, _ := a.Locate(n.ID)
		if err := h.EachInFolder(tok, folder, func(r index.ListRow) error {
			out = append(out, r.Key+"|"+r.Subject+"|"+r.SenderEmail+"|"+r.Date.String())
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return strings.Join(out, "\n")
}

// covers: MA-436, R9, R20, R29, S52
// The archive is never written by a Microsoft 365 export either (design §4.14; the row says "for both
// sources"): with a real archive at the configured location and no capture running, every archive file
// except search.db-wal and -shm is byte-identical after the Microsoft 365 preview and after its run,
// search.db's rows are unchanged, and no .mailarchive.lock is created.
func TestAMicrosoft365ExportNeverWritesTheArchive(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	skip := []string{"search.db-wal", "search.db-shm"}
	if fileExists(filepath.Join(e.out, ".mailarchive.lock")) {
		t.Fatal("the fixture archive has a lock before the test starts")
	}
	before, rowsBefore := dirDigest(t, e.out, skip...), archiveRowsOf(t, e.out)
	if rowsBefore == "" {
		t.Fatal("the fixture archive lists no rows")
	}
	same := func(when string) {
		t.Helper()
		if dirDigest(t, e.out, skip...) != before {
			t.Errorf("the Microsoft 365 %s changed the archive: a file's bytes differ or a file was added", when)
		}
		if got := archiveRowsOf(t, e.out); got != rowsBefore {
			t.Errorf("the Microsoft 365 %s changed search.db's rows:\n%s\nafter\n%s", when, rowsBefore, got)
		}
		if fileExists(filepath.Join(e.out, ".mailarchive.lock")) {
			t.Errorf("the Microsoft 365 %s made .mailarchive.lock: it takes no archive lock", when)
		}
	}
	ids := e.folders()
	id, _ := e.preview(e.selection(entry(ids["Sent Items"], false)))
	same("preview")
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	if l := e.waitRun()["last"].(map[string]any); l["status"] != "complete" || l["exported"] != float64(5) {
		t.Fatalf("the Microsoft 365 export beside a real archive: %v", l)
	}
	same("run")
}

// covers: MA-435, R20, R29, S52
// Each store is labelled with the mailbox it holds (XC21), as the page shows it: a Graph store, whose
// source id is "mailbox:<address>", reads "alice — alice@contoso.org"; a store that holds no mailbox is
// its token alone; and the page puts the label where the store's name goes.
func TestArchiveStoresAreLabelledWithTheirMailbox(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	f.edit(func(m *state.Manifest) {
		for id, tok := range m.Stores {
			if tok == "alice" {
				delete(m.Stores, id)
				m.Stores["mailbox:alice@contoso.org"] = "alice"
			}
		}
	})
	arch := archSource("archive", e.out)
	out := e.ok("/api/workspace/folders", map[string]any{"source": arch})
	labels := map[string]any{}
	for _, f := range out["folders"].([]any) {
		m := f.(map[string]any)
		labels[m["id"].(string)] = m["label"]
	}
	if labels["alice"] != "alice — alice@contoso.org" || labels["bob"] != "bob" || labels["alice/Clients"] != nil {
		t.Errorf("the tree's labels: %v, want the Graph store with its mailbox, the maildir store by its token, and no label on a folder", labels)
	}
	stores, _ := out["stores"].([]any)
	if len(stores) != 2 || stores[0].(map[string]any)["mailbox"] != "alice@contoso.org" || stores[1].(map[string]any)["mailbox"] != "" {
		t.Errorf("the stores the page names: %v", stores)
	}
	if !strings.Contains(workspaceJS, "f.name=f.label") {
		t.Error("workspace.js does not show a store with the mailbox it holds")
	}
}

// covers: MA-510, R29, R5, R20, S25, S52
// A second start while an archive export runs is R-m (design §4.14), at Start as it is at the preview: a
// preview that was made before the first start and is started again while that export runs is refused
// and writes nothing; the first export completes.
func TestASecondArchiveStartIsRefusedWhileOneRuns(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	addBulkStore(t, e.out, 150)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	id, _ := e.preview(e.archSel(arch, entry("bulk", false)))
	e.d.ws.mu.Lock()
	saved := *e.d.ws.prev
	e.d.ws.mu.Unlock()
	var second map[string]any
	var secondCode int
	fired := false
	e.d.ex.afterLaunch = func(s *exportSlot) {
		if fired { // only the first start's launch presses it again
			return
		}
		fired = true
		e.d.ws.mu.Lock()
		again := saved // the page's Start button, pressed twice: the preview is still the one it holds
		e.d.ws.prev = &again
		e.d.ws.mu.Unlock()
		secondCode, second = e.post("/api/exports/start", map[string]any{"previewId": id})
	}
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	refusedBody(t, secondCode, second, "R-m", "An export is already running", "only one runs at a time")
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(150) {
		t.Errorf("the first export: %v", last)
	}
	entries, _ := os.ReadDir(e.dest)
	if len(entries) != 1 {
		t.Errorf("the destination holds %d entries, want the one export directory: a refused second start wrote nothing", len(entries))
	}
}

// covers: MA-512, R12, S52
// R-l2, the archive binding (design §7.6, RR2-C4), in both variants, carries its what, why and remedy
// substrings and no token, Graph URL query, "panic" or "goroutine" (assure's forbid list): the configured
// archive after Settings' location changed, and another archive folder that is no longer at its path.
func TestArchiveBindingRefusalsCarryWhatWhyAndRemedy(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	other := filepath.Join(e.dir, "inherited")
	buildArchiveAt(t, other)
	arch, oarch := archSource("archive", e.out), archSource("archive-other", other)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true))
	forbid := assure.Forbid("panic", "goroutine", "access_token", "refresh_token", "graph.microsoft.com")
	check := func(src map[string]any, names ...string) {
		t.Helper()
		code, out := e.post("/api/workspace/count", src)
		if out["id"] != "R-l2" {
			t.Fatalf("want R-l2, got %d %v", code, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused), assure.Names(names...), forbid)
	}
	elsewhere := filepath.Join(e.dir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = elsewhere }); err != nil {
		t.Fatal(err)
	}
	check(sel, "was made in the archive at "+e.out, "Settings now points at "+elsewhere, "Load folders again")

	e.archFolders(oarch)
	osel := e.archSel(oarch, entry("bob", false))
	e.ok("/api/workspace/count", osel) // the positive twin: still at its path
	if err := os.Rename(other, other+".away"); err != nil {
		t.Fatal(err)
	}
	check(osel, "was made from the archive at "+other, "no longer at that path", "choose the folder again", "Another archive folder")
}

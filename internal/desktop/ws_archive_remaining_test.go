package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// archiveStoppedBeforeAnyMessage previews all six messages of the configured archive, starts the export with the
// archive disconnected (R-o2 on the first message: six messages not attempted), reconnects it, and returns the
// run and the archive.
func archiveStoppedBeforeAnyMessage(t *testing.T) (*wsEnv, *archFixture, string) {
	t.Helper()
	e := newWsEnv(t)
	dir := e.out
	f := buildArchiveAt(t, dir)
	os.Remove(e.tok)
	arch := archSource("archive", dir)
	e.archFolders(arch)
	id, res := e.preview(e.archSel(arch, entry("alice", true), entry("bob", false)))
	if res["count"] != float64(6) {
		t.Fatalf("the preview: %v", res)
	}
	stash := filepath.Join(e.dir, "stash")
	os.MkdirAll(stash, 0o755)
	ents, _ := os.ReadDir(dir)
	for _, ent := range ents {
		os.Rename(filepath.Join(dir, ent.Name()), filepath.Join(stash, ent.Name()))
	}
	run := e.ok("/api/exports/start", map[string]any{"previewId": id})["runId"].(string)
	if last := e.waitRun()["last"].(map[string]any); last["stopId"] != "R-o2" {
		t.Fatalf("the stopped export: %v", last)
	}
	for _, ent := range ents {
		os.Rename(filepath.Join(stash, ent.Name()), filepath.Join(dir, ent.Name()))
	}
	return e, f, run
}

// removeMessageFiles removes a message's page, original and attachments zip, as a redaction does.
func removeMessageFiles(f *archFixture, slug string) {
	rec := f.rec(slug)
	stem := strings.TrimSuffix(rec.Path, ".html")
	for _, rel := range []string{rec.Path, stem + ".eml", stem + ".zip"} {
		os.Remove(f.path(rel))
	}
}

// covers: MA-437, MA-512, R29, R5, R12
// "Export the remaining messages" is not a dead end whatever has become of the remaining messages (E4-AC10,
// re-verified): the first fix kept a message whose files were removed in the retry's plan, but refused the
// retry (R-h, "None of those messages is in the archive any more") when every message that was left was in that
// state, and left out a record the manifest no longer lists, so that no part recorded it. In both the first
// part's card went on offering a button that answered R-h. A retry of messages that are all gone now lists them
// all as removed from the archive, and the card stops offering them.
func TestExportTheRemainingMessagesWhenEveryRemainingMessageIsGone(t *testing.T) {
	e, f, run := archiveStoppedBeforeAnyMessage(t)
	for _, slug := range []string{"hello", "second", "client-mail", "with-attachment", "plain-page", "third-page"} {
		removeMessageFiles(f, slug)
	}
	if !hasStr(e.card(run)["actions"], "export-remaining") {
		t.Fatalf("the fixture: the stopped run's card does not offer the remaining messages: %v", e.card(run)["actions"])
	}
	code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
	if code != 200 || out["previewId"] == nil {
		t.Fatalf("the retry of messages that are all gone: %d %v, want a preview that lists them as removed", code, out)
	}
	rres := e.waitPreview(out["previewId"].(string))
	if w := strings.Join(toStrings(rres["warnings"]), " | "); !strings.Contains(w, "no longer in the archive") {
		t.Errorf("the retry's preview does not say the messages are gone: %q", w)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": out["previewId"]})
	l := e.waitRun()["last"].(map[string]any)
	if l["exported"] != float64(0) || l["failed"] != float64(6) {
		t.Errorf("the second part: %v, want 0 exported and the 6 listed as removed", l)
	}
	if card := e.card(run); hasStr(card["actions"], "export-remaining") {
		code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
		t.Errorf("part 1 still offers Export the remaining messages (it answers %d %v)", code, out["id"])
	}
}

// covers: MA-437, MA-512, R29, R5, R12
// A record the manifest no longer lists is settled by the retry as a message whose files are gone is (E4-AC10,
// re-verified): the first fix left it out of the plan with a warning, so no part recorded it and the first part's
// card offered it for ever. It is carried into the plan and listed as removed from the archive, whether or not
// its files are still on disk (a record taken out of the manifest is a message the archive no longer
// has, and its files are not read). The positive twin is the retry of the same run with every record in place.
func TestExportTheRemainingMessagesWhenARecordIsNoLongerInTheManifest(t *testing.T) {
	e, f, run := archiveStoppedBeforeAnyMessage(t)
	key := f.key("hello")
	f.edit(func(m *state.Manifest) { m.Delete(key) }) // taken out of the manifest, its files left where they were
	rid := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string)
	rres := e.waitPreview(rid)
	if w := strings.Join(toStrings(rres["warnings"]), " | "); !strings.Contains(w, "no longer in the archive") {
		t.Errorf("the retry's preview does not say a message is gone: %q", w)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": rid})
	l := e.waitRun()["last"].(map[string]any)
	if l["exported"] != float64(5) || l["failed"] != float64(1) {
		t.Fatalf("the second part: %v, want 5 exported and the removed record listed", l)
	}
	man := e.manifestOf(l["rootName"].(string))
	if fl := man["failures"]; fl == nil || !strings.Contains(fl.([]any)[0].(map[string]any)["reason"].(string), "removed from the archive") {
		t.Errorf("the second part's failures: %v", man["failures"])
	}
	for _, m := range man["messages"].([]any) {
		if e := m.(map[string]any); e["source_id"] == key && (e["status"] != "failed" || len(e["files"].([]any)) != 0) {
			t.Errorf("the record the manifest no longer lists was exported: %v", e)
		}
	}
	if card := e.card(run); hasStr(card["actions"], "export-remaining") {
		code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
		t.Errorf("part 1 still offers Export the remaining messages (it answers %d %v)", code, out["id"])
	}
}

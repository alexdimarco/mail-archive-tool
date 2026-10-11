package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// The friction re-run's fixes for the archive source (EW-E4, docs/review-extraction-workspace-archive-friction.md):
// each test asserts what the owner now reads. The positive twin comes first in each.

// lacks fails the test when text carries any of the phrases.
func lacks(t *testing.T, what, text string, phrases ...string) {
	t.Helper()
	for _, p := range phrases {
		if strings.Contains(text, p) {
			t.Errorf("%s says %q, which is wrong for a count of one: %q", what, p, text)
		}
	}
}

// emlOf is the absolute path of a fixture message's original.
func (f *archFixture) emlOf(slug string) string {
	return f.path(strings.TrimSuffix(f.rec(slug).Path, ".html") + ".eml")
}

// covers: MA-435, R20, S52
// F19: a count of one reads as one in the preview's lines about the archive's records ("1 is no longer in
// the mailbox", "1 original has no checksum", "1 archived page doesn't begin ...", "1 record whose files were
// removed is skipped", "Of 1 selected, 1 has an original .eml") and in the list's note ("1 message ... is not in
// the search index"); the healthy archive's preview carries none of those lines.
func TestArchiveCountsOfOneReadAsOne(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	clients := entry("alice/Clients", false)

	// the positive twin: a healthy folder says how many have an original, and nothing about gaps
	e.archFolders(arch)
	_, res := e.preview(e.archSel(arch, clients))
	infos := strings.Join(toStrings(res["info"]), " | ")
	if a := res["archive"].(map[string]any); res["count"] != float64(1) || a["withOriginal"] != float64(1) || strings.Contains(infos, "no longer in the mailbox") || strings.Contains(infos, "no checksum") {
		t.Fatalf("the healthy folder's preview: %v %q", res, infos)
	}

	// one message that has left the mailbox and has no checksum recorded for its original
	key := f.key("client-mail")
	f.edit(func(m *state.Manifest) {
		r, _ := m.Get(key)
		r.Present, r.LastSeen, r.Fixity = false, time.Now().UTC(), nil
		m.Add(key, r)
	})
	e.archFolders(arch)
	_, res = e.preview(e.archSel(arch, clients))
	infos = strings.Join(toStrings(res["info"]), " | ")
	for _, want := range []string{"Of 1 selected, 1 has an original .eml.", "1 is no longer in the mailbox (kept in the archive).", "1 original has no checksum recorded in the archive, so it is copied as stored"} {
		if !strings.Contains(infos, want) {
			t.Errorf("the preview's lines lack %q: %q", want, infos)
		}
	}
	lacks(t, "the preview", infos, "1 are ", "1 originals", "1 have ", "Of 1 selected, 1 have")

	// one archived page that fails the archived-page gate
	scripted := []byte("<!DOCTYPE html><html><head></head><body><script>alert(1)</script></body></html>")
	pkey := f.key("plain-page")
	if err := os.WriteFile(f.path(f.rec("plain-page").Path), scripted, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(scripted)
	f.edit(func(m *state.Manifest) {
		r, _ := m.Get(pkey)
		r.Fixity.HTML = &state.FileDigest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(scripted))}
		m.Add(pkey, r)
	})
	e.archFolders(arch)
	_, res = e.preview(e.archSel(arch, entry("bob", false)))
	infos = strings.Join(toStrings(res["info"]), " | ")
	if !strings.Contains(infos, "1 archived page doesn't begin with the security policy a page of MailArchive carries, or holds a live refresh, so it will be copied as text (.archived-page.txt) and not as a page.") {
		t.Errorf("one quarantined page: %q", infos)
	}
	lacks(t, "the preview", infos, "1 archived pages", "so they will be copied")

	// one record whose files were removed since: skipped, and said once
	if err := os.Remove(f.emlOf("hello")); err != nil {
		t.Fatal(err)
	}
	e.archFolders(arch)
	_, res = e.preview(e.archSel(arch, entry("alice", false)))
	warns := strings.Join(toStrings(res["warnings"]), " | ")
	if !strings.Contains(warns, "1 record whose files were removed is skipped") {
		t.Errorf("one skipped record: %q", warns)
	}
	lacks(t, "the preview", warns, "1 records", "1 are skipped")
}

// covers: MA-435, R20, S52
// F19 and F18: the list says "1 message in this folder is not in the search index" for one, and a preview
// made while Rebuild runs says what to do: wait until Maintenance says it is done, then preview again.
func TestArchiveListAndPreviewDuringRebuildSayWhatToDo(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)

	// the positive twin: an index that has every row says nothing
	if _, out := e.archRows(arch, "alice"); out["listNote"] != nil {
		t.Fatalf("a complete index: the list says %v", out["listNote"])
	}
	db, err := sqlOpen(e.out + "/search.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=DELETE`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM docs WHERE key = ?`, f.key("second")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	_, out := e.archRows(arch, "alice")
	note, _ := out["listNote"].(string)
	if !strings.Contains(note, "1 message in this folder is not in the search index") {
		t.Errorf("one message missing from the index: %q", note)
	}
	lacks(t, "the list", note, "1 messages", "1 are")

	// a preview while the Rebuild job runs: the names carry nothing from the index, and the warning says what to do about it
	e.d.beginRebuild()
	defer e.d.endRebuild()
	_, res := e.preview(e.archSel(arch, entry("alice", false)))
	warns := strings.Join(toStrings(res["warnings"]), " | ")
	for _, want := range []string{"The search index is being rebuilt", "Wait until Maintenance says the rebuild is done, then click Preview copy export again"} {
		if !strings.Contains(warns, want) {
			t.Errorf("the preview during Rebuild lacks %q: %q", want, warns)
		}
	}
}

// covers: MA-512, MA-531, R12, S52
// F10: the words for a typed path of Another archive folder say what is true. A path pasted with its quotes (what
// Explorer's Copy as path gives) is the path; a path where there is no folder says so ("There is no folder at
// ..."); a file, or a manifest that is a folder, is said as it is; the example is a path of an archive's shape and
// not Downloads; and the box's placeholder agrees. The positive twin first: the quoted path of a real folder loads.
func TestAnotherArchiveFolderTypedPathWordsSayWhatIsTrue(t *testing.T) {
	e := newWsEnv(t)
	other := filepath.Join(e.dir, "inherited")
	buildArchiveAt(t, other)
	forbid := assure.Forbid("panic", "goroutine", "Downloads", "that is a file: it is a link or a folder")
	refused := func(what, path string, names ...string) {
		t.Helper()
		code, out := e.post("/api/workspace/source", map[string]any{"otherArchive": path})
		if out["id"] != "R-o3" {
			t.Fatalf("%s: want R-o3, got %d %v", what, code, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused), assure.Names(names...), forbid)
	}

	// the positive twin, and the same folder as Explorer's Copy as path gives it
	for _, typed := range []string{other, `"` + other + `"`, `'` + other + `'`, "  " + other + "\n"} {
		src := e.ok("/api/workspace/source", map[string]any{"otherArchive": typed})
		if a := src["archive"].(map[string]any); a["state"] != "ready" || a["path"] != other {
			t.Errorf("the path %q: %v, want the folder", typed, a)
		}
	}

	// a typo: there is no folder there
	refused("a path with a typo", other+"x", "There is no folder at "+other+"x", "check the spelling", "Browse", "index.html and README.txt")
	// a file where the folder should be
	refused("a file", filepath.Join(other, "README.txt"), filepath.Join(other, "README.txt"), "is a file, not a folder", "top folder", "index.html and README.txt")
	// a folder whose manifest is a folder
	odd := filepath.Join(e.dir, "odd")
	if err := os.MkdirAll(filepath.Join(odd, ".mailarchive-manifest.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	refused("a manifest that is a folder", odd, odd, "isn't a MailArchive archive", "is a link or a folder, not the archive's own file", "index.html and README.txt")
	// not a full path: the example is archive-shaped
	refused("a relative path", "relative/folder", "isn't a full path", "Browse", "MailArchive")

	page := e.get("/workspace").Body.String()
	if i := strings.Index(page, `id="otherPath"`); i < 0 || strings.Contains(page[i:i+260], "Downloads") || !strings.Contains(page[i:i+260], "MailArchive") {
		t.Errorf("the path box's placeholder: %.260s", page[max(i, 0):])
	}
}

// covers: MA-512, MA-531, R12, S52
// F11: a manifest cut short by an interrupted copy is said to be a copy that is incomplete (what to do: copy the
// archive again), never with Go's "unexpected end of JSON input" and never with advice to delete a file in a
// folder that is read-only; for the owner's own archive it keeps the engine's advice (restore from a backup) and
// names the file. A zero-byte manifest (a sync that has not finished) loads as an empty archive and says so. The
// positive twin: the healthy archive loads with no note.
func TestAnArchiveManifestThatIsCutShortOrEmptySaysSo(t *testing.T) {
	e := newWsEnv(t)
	other := filepath.Join(e.dir, "inherited")
	buildArchiveAt(t, other)
	oarch := archSource("archive-other", other)

	// the twin
	if out := e.ok("/api/workspace/folders", map[string]any{"source": oarch}); out["note"] != nil {
		t.Fatalf("a healthy archive carries a note: %v", out["note"])
	}
	manifest := filepath.Join(other, ".mailarchive-manifest.json")
	full, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}

	// cut short
	if err := os.WriteFile(manifest, full[:len(full)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	forbid := assure.Forbid("unexpected end of JSON input", "delete it", "panic", "goroutine")
	for _, route := range []string{"/api/workspace/source", "/api/workspace/folders"} {
		body := map[string]any{"otherArchive": other}
		if route == "/api/workspace/folders" {
			body = map[string]any{"source": oarch}
		}
		code, out := e.post(route, body)
		if out["id"] != "R-p" {
			t.Fatalf("%s: want R-p, got %d %v", route, code, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused),
			assure.Names(other, "incomplete", "cut short", "Copy the archive again", "nothing was written"), forbid)
	}
	// the owner's own archive keeps the advice that is right for it
	buildArchiveAt(t, e.out)
	cfgManifest := filepath.Join(e.out, ".mailarchive-manifest.json")
	cfgFull, _ := os.ReadFile(cfgManifest)
	os.WriteFile(cfgManifest, cfgFull[:len(cfgFull)/2], 0o644)
	code, out := e.post("/api/workspace/folders", map[string]any{"source": archSource("archive", e.out)})
	msg, _ := out["error"].(string)
	if out["id"] != "R-p" {
		t.Fatalf("the configured archive's cut-short manifest: %d %v", code, out)
	}
	assure.Refused(t, code, msg, assure.Code(statusRefused),
		assure.Names(".mailarchive-manifest.json", "cut short or damaged", "Restore it from a backup", "nothing was changed"), forbid)

	// empty: a sync that has not finished
	if err := os.WriteFile(manifest, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/workspace/source", "/api/workspace/folders"} {
		body := map[string]any{"otherArchive": other}
		if route == "/api/workspace/folders" {
			body = map[string]any{"source": oarch}
		}
		out := e.ok(route, body)
		note, _ := out["note"].(string)
		if route == "/api/workspace/source" {
			note, _ = out["archive"].(map[string]any)["note"].(string)
		}
		for _, want := range []string{"holds no messages yet", "manifest is empty", "still being copied or synced"} {
			if !strings.Contains(note, want) {
				t.Errorf("%s: the note %q lacks %q", route, note, want)
			}
		}
	}
	for _, want := range []string{"d.note", "a.note"} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js does not show the archive's note (missing %q) *(script)*", want)
		}
	}
}

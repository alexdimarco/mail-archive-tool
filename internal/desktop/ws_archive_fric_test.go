package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
			t.Errorf("%s says %q, which is wrong here: %q", what, p, text)
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

// covers: MA-512, MA-435, R12, S52
// F5: an archive the page held a moment ago and cannot find now is said to have gone away ("isn't there any
// more", with what to do: reconnect it), on every route: the message list, Start, Load folders and the Source
// line, for the configured archive; Start (R-l2), then Load folders, for another folder. It is never described as
// an archive that was never made ("no search index", "no archive yet - run Archive now first": on a Mac or Linux
// whose mount point stays, Archive now would start a new archive in it). The twin: when nothing held the archive
// (the page was open for more than 30 minutes, or was just opened), the old words stand.
func TestAnArchiveThatWentAwayIsSaidToHaveGone(t *testing.T) {
	forbid := assure.Forbid("no search index", "run Archive now first", "Rebuild", "yet —", "panic", "goroutine")
	refused := func(t *testing.T, what string, code int, out map[string]any, id string, names ...string) {
		t.Helper()
		if out["id"] != id {
			t.Fatalf("%s: want %s, got %d %v", what, id, code, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused), assure.Names(names...), forbid)
	}

	t.Run("the configured archive", func(t *testing.T) {
		e := newWsEnv(t)
		buildArchiveAt(t, e.out)
		arch := archSource("archive", e.out)
		e.archFolders(arch)
		sel := e.archSel(arch, entry("alice", true))
		id, _ := e.preview(sel)
		if rows, _ := e.archRows(arch, "alice"); len(rows) != 2 {
			t.Fatalf("the twin: the list of the archive that is there: %d rows", len(rows))
		}
		if err := os.RemoveAll(e.out); err != nil { // the drive is unplugged: its folder is gone
			t.Fatal(err)
		}

		out := e.ok("/api/workspace/messages", map[string]any{"folder": "alice", "source": arch})
		note, _ := out["listUnavailable"].(string)
		assure.Reached(t, note, "the list's note")
		assure.Refused(t, statusRefused, note, assure.Code(statusRefused), assure.Names(e.out, "isn't there any more", "Reconnect it", "Load folders"), forbid)

		code, body := e.post("/api/exports/start", map[string]any{"previewId": id})
		refused(t, "Start", code, body, "R-o2", e.out, "isn't there any more", "nothing was exported", "Reconnect it", "Preview copy export again")
		if entries, _ := os.ReadDir(e.dest); len(entries) != 0 {
			t.Errorf("Start wrote into the destination: %v", entries)
		}
		if _, err := os.Lstat(e.out); err == nil {
			t.Error("a click made the archive's folder again")
		}

		code, body = e.post("/api/workspace/folders", map[string]any{"source": arch})
		refused(t, "Load folders", code, body, "R-o2", e.out, "isn't there any more", "Reconnect it", "Load folders again")
		if a := e.ok("/api/workspace/source", nil)["archive"].(map[string]any); a["state"] == "ready" || !strings.Contains(a["note"].(string), "isn't there any more") {
			t.Errorf("the Source line for an archive that went away: %v", a)
		}

		for _, want := range []string{"d.id==='R-o2'", "a.state!=='gone'"} { // Load folders stays within reach once it is reconnected *(script)*
			if !strings.Contains(workspaceJS, want) {
				t.Errorf("workspace.js does not carry %q", want)
			}
		}

		// nothing held it any more (30 idle minutes): the old words, which are true for an archive never made
		e.d.ws.reap(time.Now().Add(sessionIdle + time.Minute))
		code, body = e.post("/api/workspace/folders", map[string]any{"source": arch})
		if body["id"] != "R-o" || !strings.Contains(body["error"].(string), "run Archive now first") {
			t.Errorf("an archive nothing held: %d %v, want R-o", code, body)
		}
	})

	t.Run("another archive folder", func(t *testing.T) {
		e := newWsEnv(t)
		other := filepath.Join(e.dir, "inherited")
		buildArchiveAt(t, other)
		oarch := archSource("archive-other", other)
		e.archFolders(oarch)
		id, _ := e.preview(e.archSel(oarch, entry("bob", false)))
		if err := os.RemoveAll(other); err != nil {
			t.Fatal(err)
		}
		code, body := e.post("/api/exports/start", map[string]any{"previewId": id})
		refused(t, "Start", code, body, "R-l2", other, "no longer at that path", "reconnect it", "choose the folder again", "Another archive folder")
		code, body = e.post("/api/workspace/folders", map[string]any{"source": oarch})
		refused(t, "Load folders", code, body, "R-o2", other, "isn't there any more", "Reconnect it", "choose it again", "Another archive folder")
		if strings.Contains(body["error"].(string), "isn't a MailArchive archive") {
			t.Errorf("a folder that went away is described as one that is not an archive: %v", body["error"])
		}
	})
}

// covers: MA-507, MA-437, MA-512, R12, R29
// F1 (the wording half): a retry is bound to the archive the export was made from, and when that archive has
// moved the refusal names the real options instead of one that exports everything ("Load folders again to start
// a new selection") or one that cannot work ("Reconnect it" for a folder that was moved): set the Archive
// location back and click the button again (it exports just what is left), or start a new export from the new
// place, which exports every message again, the ones already exported being in this export. The positive twin:
// with the archive back at the path, the same retry goes through.
func TestRetryOfAnArchiveExportThatMovedNamesTheRealOptions(t *testing.T) {
	stop := func(e *wsEnv, src map[string]any, dir string, entries ...map[string]any) (run string, stash string, ents []os.DirEntry) {
		e.archFolders(src)
		id, _ := e.preview(e.archSel(src, entries...))
		stash = filepath.Join(e.dir, "stash-"+filepath.Base(dir))
		os.MkdirAll(stash, 0o755)
		ents, _ = os.ReadDir(dir)
		var once sync.Once
		e.d.ex.env.SetStepHook(func(step string) { // the drive stops answering after the first message
			if step == "W6:1" {
				once.Do(func() {
					for _, ent := range ents {
						os.Rename(filepath.Join(dir, ent.Name()), filepath.Join(stash, ent.Name()))
					}
				})
			}
		})
		run = e.ok("/api/exports/start", map[string]any{"previewId": id})["runId"].(string)
		if last := e.waitRun()["last"].(map[string]any); last["stopId"] != "R-o2" {
			t.Fatalf("the export did not stop with R-o2: %v", last)
		}
		return run, stash, ents
	}

	t.Run("the configured archive was copied to a new place and Settings points there", func(t *testing.T) {
		e := newWsEnv(t)
		buildArchiveAt(t, e.out)
		os.Remove(e.tok)
		run, stash, ents := stop(e, archSource("archive", e.out), e.out, entry("alice", true), entry("bob", false))
		moved := filepath.Join(e.dir, "new-drive")
		os.MkdirAll(moved, 0o755)
		copyTree(t, stash, moved)
		if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = moved }); err != nil {
			t.Fatal(err)
		}
		code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
		if out["id"] != "R-l2" {
			t.Fatalf("the retry after the archive moved: %d %v", code, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused),
			assure.Names(e.out, moved, "set the Archive location in Settings back to "+e.out, "'Export the remaining messages'", "every message again", "already exported"),
			assure.Forbid("Load folders again to start a new selection"),
			assure.NoSideEffect(func() bool { entries, _ := os.ReadDir(e.dest); return len(entries) == 1 })) // only the stopped export's folder

		// the twin: Settings back, the archive back at its path: the retry goes through
		UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = e.out })
		for _, ent := range ents {
			os.Rename(filepath.Join(stash, ent.Name()), filepath.Join(e.out, ent.Name()))
		}
		e.waitPreview(e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string))
	})

	t.Run("another archive folder was moved", func(t *testing.T) {
		e := newWsEnv(t)
		other := filepath.Join(e.dir, "inherited")
		buildArchiveAt(t, other)
		os.Remove(e.tok)
		run, _, _ := stop(e, archSource("archive-other", other), other, entry("bob", false))
		code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
		if out["id"] != "R-o2" {
			t.Fatalf("the retry while the folder is away: %d %v", code, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused),
			assure.Names(other, "isn't available", "Reconnect it", "Export the remaining messages", "can't follow it", "put it back at "+other, "Another archive folder", "every message again", "already exported"))
	})
}

// covers: MA-435, MA-522, R12, R20, S52
// F15 (the wording half): after the archive snapshot is released for 30 idle minutes, the next click says so
// ("You were away from this page for more than 30 minutes ... let go of the archive's folder list") besides the
// R-j2 sentence the existing assertions pin, with the remedy (Load folders again, select again); a page that was
// never loaded (or loaded another archive) keeps the plain R-j2 words, and Load folders brings the count back.
func TestIdleReleasedArchiveSessionSaysYouWereAway(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	other := filepath.Join(e.dir, "inherited")
	buildArchiveAt(t, other)
	arch, oarch := archSource("archive", e.out), archSource("archive-other", other)
	sel := e.archSel(arch, entry("alice", true))

	// never loaded: the plain words, nothing about being away
	code, out := e.post("/api/workspace/count", sel)
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session")
	lacks2 := func(msg string) {
		t.Helper()
		if strings.Contains(msg, "30 minutes") {
			t.Errorf("a page that was never idle says it was: %q", msg)
		}
	}
	lacks2(out["error"].(string))

	e.archFolders(arch)
	e.ok("/api/workspace/count", sel)
	e.d.ws.reap(time.Now().Add(sessionIdle + time.Minute))
	for _, route := range []string{"/api/workspace/count", "/api/workspace/preview"} {
		code, out = e.post(route, sel)
		refusedBody(t, code, out, "R-j2", "wasn't listed in this session", "more than 30 minutes", "Load folders again")
	}
	// Load folders again brings it back, and the note does not outlive the load
	e.archFolders(arch)
	e.ok("/api/workspace/count", sel)

	// another archive folder: the same, and a selection from the other kind of source does not claim it
	e.archFolders(oarch)
	e.d.ws.reap(time.Now().Add(2*sessionIdle + time.Minute))
	code, out = e.post("/api/workspace/count", e.archSel(oarch, entry("bob", false)))
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session", "more than 30 minutes")
	code, out = e.post("/api/workspace/count", sel) // the configured archive was not the one released
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session")
	lacks2(out["error"].(string))
}

// covers: MA-435, MA-420, MA-531, R20, R29, S52
// F2: the preview says, before Start, how many messages the archive gate will refuse (a link where the file
// should be, a file with several hard links), why, and what to do (copy the archive into a plain folder that
// holds real files, and choose that folder), and the run then fails exactly that many. The positive twin: a plain
// archive's preview says nothing of the kind. Nothing is written in the archive or the destination by looking.
func TestArchivePreviewWarnsBeforeStartAboutFilesTheGateWillRefuse(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true), entry("bob", false))
	_, res := e.preview(sel)
	if w := strings.Join(toStrings(res["warnings"]), " | "); strings.Contains(w, "will not be copied") {
		t.Fatalf("the plain archive's preview warns: %q", w)
	}

	// the alice store is a folder linked to another disk
	store := filepath.Join(e.out, "alice")
	elsewhere := filepath.Join(e.dir, "other-disk")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(store, filepath.Join(elsewhere, "alice")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "alice"), store); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	before := dirDigest(t, e.out)
	e.archFolders(arch)
	id, res := e.preview(sel)
	warns := strings.Join(toStrings(res["warnings"]), " | ")
	for _, want := range []string{"3 of the selected messages will not be copied", "a link stands in the archive where the file should be",
		"listed as failed when you start", "copy it into a new plain folder that holds real files, not links", "choose that folder instead"} {
		if !strings.Contains(warns, want) {
			t.Errorf("the preview's warnings lack %q: %q", want, warns)
		}
	}
	if a := res["archive"].(map[string]any); a["blocked"] != float64(3) || a["withOriginal"] != float64(0) {
		t.Errorf("the preview's archive counts: %v, want 3 blocked and none counted as having an original", a)
	}
	if got := dirDigest(t, e.out); got != before {
		t.Error("the preview changed the archive")
	}
	if entries, _ := os.ReadDir(e.dest); len(entries) != 0 {
		t.Errorf("the preview wrote into the destination: %v", entries)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	if last := e.waitRun()["last"].(map[string]any); last["failed"] != float64(3) || last["exported"] != float64(3) {
		t.Errorf("the run: %v, want the 3 the preview said would not be copied to fail, and the other 3 exported", last)
	}
}

// covers: MA-435, MA-531, R12, R20, S52
// F4 (and the desktop half of F17): the words about the search index and about records whose files are gone say
// what is true for the folder they are about. For the owner's own archive they name the button as it is
// labelled (Maintenance → Rebuild search index), and for records whose files are gone they say what Rebuild does
// (it forgets the records, which are the only proof the messages were archived) and to run Maintenance → Check
// integrity first when the loss was not on purpose. For another archive folder, which is read-only and which
// Maintenance cannot act on, they say that, and what can be done (copy it to a folder of your own, or ask for a
// complete copy), and never send the inheritor to a button that cannot help.
func TestArchiveAdviceIsTrueForTheFolderItIsAbout(t *testing.T) {
	type setup struct {
		name  string
		other bool
	}
	for _, s := range []setup{{"the owner's archive", false}, {"another archive folder", true}} {
		t.Run(s.name, func(t *testing.T) {
			e := newWsEnv(t)
			dir := e.out
			kind := "archive"
			if s.other {
				dir, kind = filepath.Join(e.dir, "inherited"), "archive-other"
			}
			f := buildArchiveAt(t, dir)
			src := archSource(kind, dir)
			e.archFolders(src)
			sel := e.archSel(src, entry("alice", false))

			// the twin: a complete archive says nothing about repairs
			if _, out := e.archRows(src, "alice"); out["listUnavailable"] != nil || out["listNote"] != nil {
				t.Fatalf("a complete archive's list: %v", out)
			}
			_, res := e.preview(sel)
			if w := strings.Join(toStrings(res["warnings"]), " | "); strings.Contains(w, "Rebuild") || strings.Contains(w, "Check integrity") {
				t.Fatalf("a complete archive's preview advises: %q", w)
			}

			// records whose files were removed
			if err := os.Remove(f.emlOf("hello")); err != nil {
				t.Fatal(err)
			}
			e.archFolders(src)
			_, res = e.preview(sel)
			w := strings.Join(toStrings(res["warnings"]), " | ")
			if !strings.Contains(w, "1 record whose files were removed is skipped: nothing of it is exported.") {
				t.Errorf("the skipped record: %q", w)
			}
			if s.other {
				for _, want := range []string{"read-only", "ask whoever gave you the archive for a complete copy"} {
					if !strings.Contains(w, want) {
						t.Errorf("another folder's skipped-record words lack %q: %q", want, w)
					}
				}
				lacks(t, "another folder's skipped-record words", w, "Rebuild", "Check integrity")
			} else {
				for _, want := range []string{"Maintenance → Rebuild search index forgets those records", "Maintenance → Check integrity first", "the only proof"} {
					if !strings.Contains(w, want) {
						t.Errorf("the owner's skipped-record words lack %q: %q", want, w)
					}
				}
			}

			// an original with no checksum recorded: who can record one
			ckey := f.key("client-mail")
			f.edit(func(m *state.Manifest) {
				r, _ := m.Get(ckey)
				r.Fixity = nil
				m.Add(ckey, r)
			})
			e.archFolders(src)
			_, res = e.preview(e.archSel(src, entry("alice/Clients", false)))
			infos := strings.Join(toStrings(res["info"]), " | ")
			if s.other {
				if !strings.Contains(infos, "this folder is read-only, so none can be recorded here") || strings.Contains(infos, "Check integrity") {
					t.Errorf("another folder's no-checksum line: %q", infos)
				}
			} else if !strings.Contains(infos, "copied as stored ("+checkButton+" records one)") || strings.Contains(infos, "Verify") {
				t.Errorf("the owner's no-checksum line: %q", infos)
			}

			// no search index at all
			os.Remove(filepath.Join(dir, "search.db"))
			os.Remove(filepath.Join(dir, "search.db-wal"))
			os.Remove(filepath.Join(dir, "search.db-shm"))
			e.archFolders(src)
			_, out := e.archRows(src, "alice")
			note, _ := out["listUnavailable"].(string)
			assure.Reached(t, note, "the list's note")
			if !strings.Contains(note, "Whole-folder selection works") {
				t.Errorf("the list's note: %q", note)
			}
			_, res = e.preview(e.archSel(src, entry("bob", false)))
			w = strings.Join(toStrings(res["warnings"]), " | ")
			if s.other {
				if !strings.Contains(note, "to pick single messages, copy the folder") || !strings.Contains(w, "to get their names back, copy the folder") {
					t.Errorf("each sentence gives the goal it is about: list %q, preview %q", note, w)
				}
				for _, text := range []string{note, w} {
					for _, want := range []string{"read-only", "copy the folder to one of your own", "archive location in Settings"} {
						if !strings.Contains(text, want) {
							t.Errorf("another folder's index words lack %q: %q", want, text)
						}
					}
					lacks(t, "another folder's index words", text, "brings the list back", "fixes the list", "fixes it", "Rebuild search index (Maintenance)")
				}
			} else {
				if !strings.Contains(note, "Maintenance → Rebuild search index brings the list back") {
					t.Errorf("the owner's list note: %q", note)
				}
				for _, want := range []string{"Maintenance → Rebuild search index fixes it", "preview again"} {
					if !strings.Contains(w, want) {
						t.Errorf("the owner's preview warning lacks %q: %q", want, w)
					}
				}
			}
		})
	}
}

// covers: MA-435, R12, S52
// F17: Desktop's labels in the owner's words are the labels the page has.
func TestDesktopAdviceNamesButtonsThePageHas(t *testing.T) {
	e := newWsEnv(t)
	page := e.get("/").Body.String()
	for _, label := range []string{rebuildButton, checkButton} {
		_, name, _ := strings.Cut(label, "→ ")
		if !strings.Contains(page, ">"+name+"<") {
			t.Errorf("the Overview has no %q button, but the advice names %q", name, label)
		}
	}
}

// covers: MA-435, MA-420, R20, S52
// F8 (the part inside the design): an export of one folder of a multi-mailbox archive carries the archive's full
// list of stores in its README and manifest (XC21), and the preview now says so before Start, naming the stores
// that will be named and the ones selected, so the owner who is about to hand one person's folder to that person
// knows what the recipient will learn. Whether the list should be narrowed to the selected stores is the owner's
// (it is XC21's choice) and is not changed here. The twins: a selection that covers every store, and an archive
// with one store, say nothing of the kind.
func TestArchivePreviewSaysWhichStoresTheExportWillName(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	infoOf := func(sel map[string]any) string {
		_, res := e.preview(sel)
		return strings.Join(toStrings(res["info"]), " | ")
	}

	// the twin: both stores are selected
	if got := infoOf(e.archSel(arch, entry("alice", true), entry("bob", false))); strings.Contains(got, "name every store") {
		t.Errorf("a selection of every store says it names more than the selection: %q", got)
	}
	// one store of two
	got := infoOf(e.archSel(arch, entry("bob", false)))
	want := "The README and manifest of this export will name every store the archive holds (alice, bob), not only the one you selected (bob)."
	if !strings.Contains(got, want) {
		t.Errorf("one store selected: %q, want %q", got, want)
	}
	// a selection across folders of one store, picked from the folder of the first
	if got := infoOf(e.archSel(arch, entry("alice", true))); !strings.Contains(got, "not only the one you selected (alice)") {
		t.Errorf("alice's folders selected: %q", got)
	}
}

// covers: MA-446, MA-435, R12, S52
// F9: an Export activity card of an archive-sourced export says which archive it is of: the folder the archive is
// in (for another archive folder, "another archive folder" and its folder), and the stores of the folders that were
// selected, so two exports a week apart, or one of the owner's own archive and one of an inherited one, can be told
// apart without opening them. The page's card subtitle shows that label ahead of the bare source *(script)*. The
// positive twin: a Microsoft 365 card still shows the mailbox and no label.
func TestExportActivityCardNamesTheArchiveItIsOf(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	m365 := e.startExport(e.selection(entry(ids["Sent Items"], false)))
	e.waitRun()
	if c := e.card(m365); c["mailbox"] != "alice@contoso.org" || c["sourceLabel"] != nil {
		t.Fatalf("the Microsoft 365 card: %v", c)
	}

	buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	own := e.ok("/api/exports/start", map[string]any{"previewId": func() string { id, _ := e.preview(e.archSel(arch, entry("bob", false))); return id }()})["runId"].(string)
	e.waitRun()
	other := filepath.Join(e.dir, "inherited")
	buildArchiveAt(t, other)
	oarch := archSource("archive-other", other)
	e.archFolders(oarch)
	inh := e.ok("/api/exports/start", map[string]any{"previewId": func() string { id, _ := e.preview(e.archSel(oarch, entry("alice", true))); return id }()})["runId"].(string)
	e.waitRun()

	if got, _ := e.card(own)["sourceLabel"].(string); got != "your archive, folder archive (bob)" {
		t.Errorf("the owner's archive card: %q, want %q", got, "your archive, folder archive (bob)")
	}
	if got, _ := e.card(inh)["sourceLabel"].(string); got != "another archive folder inherited (alice)" {
		t.Errorf("the inherited folder's card: %q, want %q", got, "another archive folder inherited (alice)")
	}
	if !strings.Contains(exportsJS, "c.sourceLabel||c.mailbox||c.source") {
		t.Error("exports.js does not show the label ahead of the bare source *(script)*")
	}
}

// jsBalanced is a structural check of a script's brackets, with no JavaScript engine to run it in: braces,
// brackets and parentheses balance outside strings, comments and regular expressions' escapes.
func jsBalanced(src string) error {
	var stack []rune
	pairs := map[rune]rune{')': '(', ']': '[', '}': '{'}
	rs := []rune(src)
	line := 1
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\n':
			line++
		case c == '\\':
			i++ // an escape: the next character is not structure
		case c == '\'' || c == '"' || c == '`':
			for i++; i < len(rs) && rs[i] != c; i++ {
				if rs[i] == '\\' {
					i++
				} else if rs[i] == '\n' {
					line++
					if c != '`' {
						return fmt.Errorf("line %d: a string is not closed", line-1)
					}
				}
			}
		case c == '/' && i+1 < len(rs) && rs[i+1] == '/':
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			line++
		case c == '/' && i+1 < len(rs) && rs[i+1] == '*':
			for i += 2; i+1 < len(rs) && !(rs[i] == '*' && rs[i+1] == '/'); i++ {
				if rs[i] == '\n' {
					line++
				}
			}
			i++
		case c == '(' || c == '[' || c == '{':
			stack = append(stack, c)
		case c == ')' || c == ']' || c == '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[c] {
				return fmt.Errorf("line %d: %q closes nothing that is open", line, c)
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 {
		return fmt.Errorf("%d bracket(s) are never closed", len(stack))
	}
	return nil
}

// covers: MA-407, MA-408, R19, R12, S52
// F16: with no Microsoft 365 set up, the first screen of the workspace is not a dead end for someone who was
// handed an archive: the page's script starts on the archive source (the owner's own archive when there is one,
// Another archive folder when not), which needs no Microsoft 365, and its line says so instead of only sending
// them to the command line. A source the person already chose is not overridden. The
// scripts are not run (there is no browser or JavaScript engine here); their structure is checked and their
// source read *(script)*: the owner's click-through of the page (E3's OW-19) is still owed and now covers this.
func TestWorkspacePageStartsOnTheArchiveWhenMicrosoft365IsNotSetUp(t *testing.T) {
	for name, src := range map[string]string{"workspace.js": workspaceJS, "exports.js": exportsJS, "signin.js": signinJS, "dashboard.js": dashboardJS} {
		if err := jsBalanced(src); err != nil {
			t.Errorf("%s is not balanced: %v", name, err)
		}
	}
	for _, want := range []string{
		"To read an archive you were given, or your own, choose it in the Source list above: no Microsoft 365 is needed for that.",
		"if(st.src==='m365'){const k=(st.archAvail&&st.archAvail.state==='ready')?'archive':'archive-other';$('srcKind').value=k;st.src=k;}",
	} {
		if !strings.Contains(workspaceJS, want) {
			t.Errorf("workspace.js does not carry %q", want)
		}
	}
	// the branch is the not-set-up one, and sits before the source is drawn
	i, j, k := strings.Index(workspaceJS, "isn\\'t set up yet"), strings.Index(workspaceJS, "$('srcKind').value=k"), strings.Index(workspaceJS, "renderSource();\n    const s=d.settings")
	if i < 0 || j < i || k < j {
		t.Errorf("the switch to the archive source is not in the branch for Microsoft 365 not set up, before renderSource: %d %d %d", i, j, k)
	}
}

// covers: MA-523, R12, S41
// F13: the Settings-save guard's words and buttons. The ask names the read-only way (Cancel, then Extraction
// workspace → Source → Another archive folder, which changes no setting and touches no backup); the safe button
// (Cancel) is the page's primary one and the harmful one (Save and keep) is not; with nobody signed in the ask
// does not claim an account that "you are signed in as", and a cleared Archive location is not "into ."; a
// removal the scheduler refuses says what was and was not done and what to do, with the scheduler's own text
// after it; and after Save and remove the page says how to get the backup back. The head of the ask the existing
// assertions pin ("Your weekly backup will now archive <account> into <folder>") is unchanged.
func TestSettingsGuardWordsTheReadOnlyWayAndWhatToDoAfter(t *testing.T) {
	e := newWsEnv(t)
	installed := true
	removed := 0
	e.cfg.BackupInstalled = func() bool { return installed }
	e.cfg.RemoveBackup = func() error { removed++; installed = false; return nil }
	e.d.cfg = e.cfg
	newOut, orig := t.TempDir(), t.TempDir()
	save := func(out, backup string) map[string]any {
		_, body := e.post("/api/settings", map[string]any{"out": out, "keepRaw": true, "backup": backup})
		return body
	}
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = orig }); err != nil {
		t.Fatal(err)
	}
	before := readFileString(t, e.cfg.SettingsPath)
	untouched := assure.NoSideEffect(func() bool { return readFileString(t, e.cfg.SettingsPath) == before && removed == 0 && installed })

	// the ask, with the read-only way
	b := save(newOut, "")
	if b["confirm"] != true {
		t.Fatalf("a retargeting save was not asked about: %v", b)
	}
	msg, _ := b["error"].(string)
	assure.Refused(t, 1, msg, assure.Code(1), untouched,
		assure.Names("Your weekly backup will now archive alice@contoso.org into "+newOut, "choose Cancel", "Extraction workspace", "Another archive folder", "without changing any setting"))

	// nobody signed in: no account is claimed
	os.Remove(e.tok)
	b = save(newOut, "")
	msg, _ = b["error"].(string)
	assure.Refused(t, 1, msg, assure.Code(1), untouched, assure.Names("Microsoft 365 account", "nobody is signed in right now"), assure.Forbid("you are signed in as", " into ."))
	e.writeToken("alice@contoso.org", time.Hour)

	// a cleared Archive location, with a launch location in use, archives into that one, and says so (E4-AI6)
	launch := e.cfg.Out
	b = save("", "")
	msg, _ = b["error"].(string)
	if b["confirm"] != true || launch == "" {
		t.Fatalf("clearing the Archive location with a launch location was not asked about: %v", b)
	}
	assure.Refused(t, 1, msg, assure.Code(1), untouched, assure.Names("alice@contoso.org", "will now archive", launch), assure.Forbid(" into ."))

	// a cleared Archive location, with no launch location either, is said as that
	e.cfg.Out = ""
	e.d.cfg = e.cfg
	b = save("", "")
	msg, _ = b["error"].(string)
	if b["confirm"] != true {
		t.Fatalf("clearing the Archive location with a weekly backup installed was not asked about: %v", b)
	}
	assure.Refused(t, 1, msg, assure.Code(1), untouched, assure.Names("alice@contoso.org", "cleared the Archive location", "no folder to archive into"), assure.Forbid(" into ."))

	// the scheduler refuses the removal
	e.cfg.RemoveBackup = func() error { return errors.New("schtasks /Delete failed: Access is denied. (0x80070005)") }
	e.d.cfg = e.cfg
	b = save(newOut, "remove")
	msg, _ = b["error"].(string)
	if b["ok"] != false {
		t.Fatalf("a refused removal saved: %v", b)
	}
	assure.Refused(t, 1, msg, assure.Code(1), untouched,
		assure.Names("could not be removed", "the scheduler said: schtasks /Delete failed: Access is denied. (0x80070005)", "nothing was saved", "Weekly backup card", "Save and keep the backup"))

	// the page: Cancel is the primary button, and what is said after a removal
	page := e.get("/").Body.String()
	for _, want := range []string{`<button class="btn" id="settingsCancel"`, `<button class="btn secondary" id="settingsKeep"`, `<button class="btn secondary" id="settingsRemoveBackup"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the Settings guard's buttons: the page lacks %q", want)
		}
	}
	if !strings.Contains(dashboardJS, "Settings saved, and the weekly backup removed. To run it again, click Install / update on the Weekly backup card.") {
		t.Error("dashboard.js does not say how to get the weekly backup back after it was removed *(script)*")
	}
}

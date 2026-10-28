package desktop

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// copyTree copies the tree at src to dst: the same bytes, a different folder (a remount's new inode).
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// covers: MA-507, R29, R12
// The archive's binding (design §4.3, RR2-C4; the signer's is MA-411): a selection or preview made
// from the configured archive is refused with R-l2 after Settings' archive location changes, in the
// words that name both; one made from another archive folder is refused with R-l2 when that folder is
// no longer at its path; the binding is the path, not the folder's inode, so the same folder remade at
// the same path is still the archive; and a Microsoft 365 selection is never refused for an archive
// change. The positive twins come first.
func TestArchiveSelectionIsBoundToItsArchive(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	sel := e.archSel(arch, entry("alice", true))
	e.ok("/api/workspace/count", sel)
	id, _ := e.preview(sel)

	// Settings points elsewhere now
	elsewhere := filepath.Join(e.dir, "elsewhere")
	os.MkdirAll(elsewhere, 0o755)
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = elsewhere }); err != nil {
		t.Fatal(err)
	}
	names := []string{"was made in the archive at " + e.out, "Settings now points at " + elsewhere, "Load folders again"}
	code, out := e.post("/api/exports/start", map[string]any{"previewId": id})
	refusedBody(t, code, out, "R-l2", names...)
	code, out = e.post("/api/workspace/count", sel)
	refusedBody(t, code, out, "R-l2", names...)
	code, out = e.post("/api/workspace/preview", sel)
	refusedBody(t, code, out, "R-l2", names...)
	code, out = e.post("/api/workspace/messages", map[string]any{"folder": "alice", "source": arch})
	refusedBody(t, code, out, "R-l2", names...)
	if entries, _ := os.ReadDir(e.dest); len(entries) != 0 {
		t.Errorf("a refusal wrote into the destination: %v", entries)
	}

	// a Microsoft 365 selection does not care which archive Settings points at
	ids := e.folders()
	if _, res := e.preview(e.selection(entry(ids["Sent Items"], false))); res["count"] != float64(5) {
		t.Errorf("a Microsoft 365 preview after the archive moved: %v", res)
	}

	// Settings back: a fresh load and preview start
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = e.out }); err != nil {
		t.Fatal(err)
	}
	e.archFolders(arch)
	id2, _ := e.preview(sel)
	e.ok("/api/exports/start", map[string]any{"previewId": id2})
	if last := e.waitRun()["last"].(map[string]any); last["status"] != "complete" {
		t.Fatalf("the export after Settings was put back: %v", last)
	}

	// another archive folder: bound by its path
	other := filepath.Join(e.dir, "inherited")
	buildArchiveAt(t, other)
	oarch := archSource("archive-other", other)
	e.archFolders(oarch)
	osel := e.archSel(oarch, entry("bob", false))
	e.ok("/api/workspace/count", osel)
	oid, _ := e.preview(osel)
	moved := other + ".away"
	if err := os.Rename(other, moved); err != nil {
		t.Fatal(err)
	}
	onames := []string{"was made from the archive at " + other, "no longer at that path", "choose the folder again", "Another archive folder"}
	code, out = e.post("/api/exports/start", map[string]any{"previewId": oid})
	refusedBody(t, code, out, "R-l2", onames...)
	code, out = e.post("/api/workspace/count", osel)
	refusedBody(t, code, out, "R-l2", onames...)
	code, out = e.post("/api/workspace/preview", osel)
	refusedBody(t, code, out, "R-l2", onames...)

	// the same folder remade at the same path (a remount gives it another inode): still the archive
	copyTree(t, moved, other)
	e.ok("/api/workspace/count", osel)
	e.ok("/api/exports/start", map[string]any{"previewId": oid})
	if last := e.waitRun()["last"].(map[string]any); last["status"] != "complete" || last["exported"] != float64(3) {
		t.Errorf("an export from a folder remade at its path: %v", last)
	}

	// a selection that names a folder this session did not load is the page being out of date
	osel2 := e.archSel(archSource("archive-other", e.out), entry("bob", false))
	code, out = e.post("/api/workspace/count", osel2)
	refusedBody(t, code, out, "R-j2", "wasn't listed in this session")
}

// covers: MA-512, R12, S52
// The archive's refusal wording (design §7.6): R-o (no archive at the configured location, or none set),
// R-o3 (another folder with no manifest, or no full path), R-o4 (a manifest over the 1 GiB another-folder
// cap, refused on its size alone), R-p (a manifest or an index a newer MailArchive wrote) and R-l2 (both
// variants), and R-o2 as a stopped run words it, each with its what, why and remedy; none names a token,
// a Graph URL, "panic" or "goroutine".
func TestArchiveRefusalsCarryWhatWhyAndRemedy(t *testing.T) {
	e := newWsEnv(t)
	arch := archSource("archive", e.out)
	forbid := assure.Forbid("panic", "goroutine", "access_token", "refresh_token", "graph.microsoft.com")
	check := func(code int, out map[string]any, id string, names ...string) {
		t.Helper()
		if out["id"] != id {
			t.Fatalf("want %s, got %d %v", id, code, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, code, msg, assure.Code(statusRefused), assure.Names(names...), forbid)
	}

	// R-o: nothing at the configured location yet
	code, out := e.post("/api/workspace/folders", map[string]any{"source": arch})
	check(code, out, "R-o", "no archive at "+e.out, "run Archive now first", "choose Microsoft 365")
	if a := e.ok("/api/workspace/source", nil)["archive"].(map[string]any); a["state"] != "none" || !strings.Contains(a["note"].(string), "run Archive now first") {
		t.Errorf("the Source control's availability line: %v", a)
	}

	// R-o3: another folder with no manifest, and a path that is not a full path
	empty := filepath.Join(e.dir, "empty")
	os.MkdirAll(empty, 0o755)
	code, out = e.post("/api/workspace/source", map[string]any{"otherArchive": empty})
	check(code, out, "R-o3", empty, "isn't a MailArchive archive", ".mailarchive-manifest.json", "index.html and README.txt")
	code, out = e.post("/api/workspace/folders", map[string]any{"source": archSource("archive-other", empty)})
	check(code, out, "R-o3", empty, "isn't a MailArchive archive")
	code, out = e.post("/api/workspace/source", map[string]any{"otherArchive": "relative/folder"})
	check(code, out, "R-o3", "relative/folder", "isn't a full path", "Browse")
	code, out = e.post("/api/workspace/folders", map[string]any{"source": archSource("archive-other", "")})
	check(code, out, "R-o3", "No archive folder is chosen", "Browse")

	// R-o4: a manifest over the cap is refused on its size, before anything is read (a sparse file)
	big := filepath.Join(e.dir, "big")
	os.MkdirAll(big, 0o755)
	f, err := os.Create(filepath.Join(big, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(1<<30 + 1); err != nil {
		f.Close()
		t.Skipf("no sparse files here: %v", err)
	}
	f.Close()
	code, out = e.post("/api/workspace/source", map[string]any{"otherArchive": big})
	check(code, out, "R-o4", big, "larger than MailArchive reads from another archive folder", "the limit is 1 GB", "nothing was written", "archive location in Settings")

	// R-p: a newer manifest, and a newer index
	buildArchiveAt(t, e.out)
	raw, _ := os.ReadFile(filepath.Join(e.out, ".mailarchive-manifest.json"))
	var doc map[string]json.RawMessage
	json.Unmarshal(raw, &doc)
	doc["version"] = json.RawMessage("99")
	newer, _ := json.Marshal(doc)
	os.WriteFile(filepath.Join(e.out, ".mailarchive-manifest.json"), newer, 0o644)
	code, out = e.post("/api/workspace/folders", map[string]any{"source": arch})
	check(code, out, "R-p", "newer MailArchive", "format 99", "update MailArchive Desktop")
	os.WriteFile(filepath.Join(e.out, ".mailarchive-manifest.json"), raw, 0o644)
	e.archFolders(arch)
	setIndexVersion(t, filepath.Join(e.out, "search.db"), 99)
	code, out = e.post("/api/workspace/messages", map[string]any{"folder": "alice", "source": arch})
	check(code, out, "R-p", "search index was written by a newer MailArchive", "format 99", "update MailArchive Desktop")

	// R-o2, as a stopped run words it: the archive is gone when the run starts reading
	setIndexVersion(t, filepath.Join(e.out, "search.db"), 3)
	sel := e.archSel(arch, entry("alice", true))
	id, _ := e.preview(sel)
	stash := filepath.Join(e.dir, "stash")
	os.MkdirAll(stash, 0o755)
	ents, _ := os.ReadDir(e.out)
	for _, ent := range ents {
		os.Rename(filepath.Join(e.out, ent.Name()), filepath.Join(stash, ent.Name()))
	}
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	last := e.waitRun()["last"].(map[string]any)
	msg, _ := last["stop"].(string)
	if last["stopId"] != "R-o2" || last["status"] != "failed" {
		t.Fatalf("an export whose archive is gone: %v", last)
	}
	assure.Refused(t, int(last["outcome"].(float64)), msg, assure.Code(1), assure.Names(e.out, "no longer available", "nothing in the archive is damaged", "Export the remaining messages"), forbid)
}

// setIndexVersion rewrites an index's meta version.
func setIndexVersion(t *testing.T, path string, v int) {
	t.Helper()
	db, err := sqlOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE meta SET version = ?`, v); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-523, R12, S41
// The Settings-save guard (XC22, design §7.4): with a weekly backup installed, a save that changes the
// archive location asks first, naming the signed-in account and the new folder, and writes nothing; "Save
// and keep the backup" saves and leaves it; "Save and remove the scheduled backup" removes it and saves; Cancel
// (not sending the save again) leaves the settings as they were. A save with no backup installed, or one that
// keeps the location, asks nothing. The page offers the three choices. (Worded "scheduled backup", and the
// capture's ask says it follows the Archive location, since 2026-10-28: OR-3, owner-accepted.)
func TestSettingsSaveAsksBeforeRetargetingTheWeeklyBackup(t *testing.T) {
	e := newWsEnv(t)
	installed := false
	removed := 0
	e.cfg.BackupInstalled = func() bool { return installed }
	e.cfg.RemoveBackup = func() error { removed++; installed = false; return nil }
	e.d.cfg = e.cfg
	newOut := t.TempDir() // outside the folder the settings and sign-in live in, which a save refuses
	save := func(out, backup string) map[string]any {
		_, body := e.post("/api/settings", map[string]any{"out": out, "keepRaw": true, "backup": backup})
		return body
	}
	settingsOut := func() string { return LoadSettings(e.cfg.SettingsPath).Out }

	// the positive twins: no backup installed, or the location kept: nothing is asked
	if b := save(newOut, ""); b["ok"] != true || settingsOut() != newOut {
		t.Fatalf("a save with no weekly backup installed: %v", b)
	}
	orig := t.TempDir()
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = orig }); err != nil {
		t.Fatal(err)
	}
	installed = true
	if b := save(orig, ""); b["ok"] != true {
		t.Fatalf("a save that keeps the location, with the backup installed: %v", b)
	}

	// the ask: nothing is written and nothing is removed
	before := readFileString(t, e.cfg.SettingsPath)
	b := save(newOut, "")
	if b["ok"] != false || b["confirm"] != true {
		t.Fatalf("a save that retargets the backup was not asked about: %v", b)
	}
	msg, _ := b["error"].(string)
	assure.Refused(t, 1, msg, assure.Code(1), assure.Names("alice@contoso.org", newOut, "scheduled backup follows the Archive location, so it will now archive"),
		assure.NoSideEffect(func() bool { return readFileString(t, e.cfg.SettingsPath) == before && removed == 0 && installed }))
	if got := b["choices"].([]any); len(got) != 3 || got[0] != "keep" || got[1] != "remove" || got[2] != "cancel" {
		t.Errorf("the choices: %v", got)
	}

	// Cancel: the page sends nothing; the settings stay as they were (checked above)
	// Save and keep the backup
	if b := save(newOut, "keep"); b["ok"] != true || settingsOut() != newOut || removed != 0 || !installed {
		t.Errorf("Save and keep the backup: %v, location %q, removed %d", b, settingsOut(), removed)
	}
	// Save and remove the weekly backup
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = orig }); err != nil {
		t.Fatal(err)
	}
	installed = true
	if b := save(newOut, "remove"); b["ok"] != true || settingsOut() != newOut || removed != 1 || installed {
		t.Errorf("Save and remove the weekly backup: %v, location %q, removed %d", b, settingsOut(), removed)
	}
	// a removal that fails saves nothing
	if err := UpdateSettings(e.cfg.SettingsPath, "test", func(s *Settings) { s.Out = orig }); err != nil {
		t.Fatal(err)
	}
	installed = true
	e.cfg.RemoveBackup = func() error { return os.ErrPermission }
	e.d.cfg = e.cfg
	if b := save(newOut, "remove"); b["ok"] != false || settingsOut() != orig {
		t.Errorf("a removal that failed still saved: %v, location %q", b, settingsOut())
	}

	// the Overview offers the three choices
	page := e.get("/").Body.String()
	for _, want := range []string{"Save and keep the backup", "Save and remove the scheduled backup", ">Cancel<", `id="settingsConfirm"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the Overview's Settings card does not offer %q", want)
		}
	}
	for _, want := range []string{"backup:backup", "d.confirm", "'keep'", "'remove'"} {
		if !strings.Contains(dashboardJS, want) {
			t.Errorf("dashboard.js does not carry the three answers (missing %q)", want)
		}
	}
}

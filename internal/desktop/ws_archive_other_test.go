package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-531, R29, R9, R20, R12, S52
// Another archive folder, read-only [ruling W1], through the workspace's own endpoints: preview and run
// leave every file under the folder byte-identical and add none (no lock, no manifest save, no -wal, -shm
// or journal, no temp), Settings and the installed weekly backup unchanged and no export setting saved
// [RR2-C14] (where the configured archive's export saves its destination, the positive twin); a page with a
// <script> and no policy prefix is copied as .archived-page.txt and a current page stays .html [RR2-C9]; a
// search.db-wal beside the index makes the list say recent messages may be missing while whole-folder
// selection still exports them; a search.db whose docs is a view makes the list unavailable while
// whole-folder selection works [RR2-C11]; read-only media is read all the same; a destination inside the
// folder, or reached through a link into it, is R-f, and so is a Create folder whose parent is such a link,
// each with nothing added under the folder.
func TestAnotherArchiveFolderIsReadOnly(t *testing.T) {
	e := newWsEnv(t)
	removed := 0
	e.cfg.BackupInstalled = func() bool { return true }
	e.cfg.RemoveBackup = func() error { removed++; return nil }
	e.d.cfg = e.cfg
	other := filepath.Join(e.dir, "inherited")
	f := buildArchiveAt(t, other)
	oarch := archSource("archive-other", other)

	// a page with a script and no policy prefix, in the folder, with its fixity put right
	scripted := []byte("<!DOCTYPE html><html><head></head><body><script>alert(document.cookie)</script></body></html>")
	rec := f.rec("plain-page")
	if err := os.WriteFile(f.path(rec.Path), scripted, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(scripted)
	key := f.key("plain-page")
	f.edit(func(m *state.Manifest) {
		r, _ := m.Get(key)
		r.Fixity.HTML = &state.FileDigest{SHA256: hex.EncodeToString(sum[:]), Size: int64(len(scripted))}
		m.Add(key, r)
	})
	before := dirDigest(t, other)
	settingsExisted := fileExists(e.cfg.SettingsPath)

	// the positive twin first: load, list, count, preview, run
	tree := e.archFolders(oarch)
	if len(tree) != 3 || tree["bob"]["count"] != float64(3) {
		t.Fatalf("another archive folder's tree: %v", tree)
	}
	rows, out := e.archRows(oarch, "bob")
	if len(rows) != 3 || out["listUnavailable"] != nil {
		t.Fatalf("the list of bob: %v", out)
	}
	sel := e.archSel(oarch, entry("bob", false))
	id, res := e.preview(sel)
	if a := res["archive"].(map[string]any); a["withoutOriginal"] != float64(3) || a["quarantined"] != float64(1) || a["other"] != true {
		t.Errorf("the preview's archive counts: %v", a)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(3) {
		t.Fatalf("the export from another archive folder: %v", last)
	}
	man := e.manifestOf(last["rootName"].(string))
	if src := man["source"].(map[string]any); src["kind"] != "archive" || src["archive"] != other || src["other_archive_folder"] != true {
		t.Errorf("the manifest's source: %v", src)
	}
	var quarantined, pages int
	for _, m := range man["messages"].([]any) {
		for _, fl := range m.(map[string]any)["files"].([]any) {
			fl := fl.(map[string]any)
			switch fl["role"] {
			case "quarantined-archived-page":
				quarantined++
				if !strings.HasSuffix(fl["path"].(string), ".archived-page.txt") {
					t.Errorf("a quarantined page: %v", fl)
				}
			case "archived-page":
				pages++
				if !strings.HasSuffix(fl["path"].(string), ".html") {
					t.Errorf("a page of the current renderer: %v", fl)
				}
			}
		}
	}
	if quarantined != 1 || pages != 2 {
		t.Errorf("%d quarantined pages and %d pages, want 1 and 2", quarantined, pages)
	}
	if got := dirDigest(t, other); got != before {
		t.Error("the preview and run changed the folder: a file's bytes differ or a file was added")
	}
	if fileExists(e.cfg.SettingsPath) != settingsExisted || (settingsExisted && LoadSettings(e.cfg.SettingsPath).ExportDest != "") {
		t.Errorf("an export from another archive folder saved a setting: %+v", LoadSettings(e.cfg.SettingsPath))
	}
	if removed != 0 {
		t.Errorf("the weekly backup was touched %d time(s)", removed)
	}
	// the positive twin of the settings: the configured archive's export saves its destination
	buildArchiveAt(t, e.out)
	e.archFolders(archSource("archive", e.out))
	id2, _ := e.preview(e.archSel(archSource("archive", e.out), entry("alice", true)))
	e.ok("/api/exports/start", map[string]any{"previewId": id2})
	e.waitRun()
	if s := LoadSettings(e.cfg.SettingsPath); s.ExportDest != e.dest {
		t.Errorf("the configured archive's export did not remember its destination: %+v", s)
	}

	// a -wal beside the index: it is not read, the list says so, and whole-folder selection includes the rest
	walPath := filepath.Join(other, "search.db-wal")
	if err := os.WriteFile(walPath, []byte("not read: a write-ahead log an immutable open ignores"), 0o644); err != nil {
		t.Fatal(err)
	}
	walBefore := dirDigest(t, other)
	e.archFolders(oarch)
	_, out = e.archRows(oarch, "bob")
	if n, _ := out["listNote"].(string); !strings.Contains(n, "Recent messages may be missing from this list; whole-folder selection includes them") {
		t.Errorf("the list beside a -wal: %v", out)
	}
	id3, _ := e.preview(sel)
	e.ok("/api/exports/start", map[string]any{"previewId": id3})
	if l := e.waitRun()["last"].(map[string]any); l["exported"] != float64(3) {
		t.Errorf("whole-folder selection beside a -wal: %v", l)
	}
	if got := dirDigest(t, other); got != walBefore {
		t.Error("an export beside a -wal changed the folder")
	}
	os.Remove(walPath)

	// destinations inside the folder, or through a link into it
	check := func(name, dest string) {
		t.Helper()
		s := e.archSel(oarch, entry("bob", false))
		s["dest"] = dest
		out := e.previewFailure(s)
		if out["id"] != "R-f" {
			t.Errorf("%s: %v, want R-f", name, out)
		}
		msg, _ := out["error"].(string)
		assure.Refused(t, statusRefused, msg, assure.Code(statusRefused), assure.Names("inside your archive", other))
	}
	check("a destination inside the folder", filepath.Join(other, "alice"))
	link := filepath.Join(e.dir, "alias")
	if err := os.Symlink(filepath.Join(other, "bob"), link); err == nil {
		check("a destination reached through a link into the folder", link)
		code, body := e.post("/api/workspace/mkdir", map[string]any{"parent": link, "name": "new"})
		refusedBody(t, code, body, "R-f", "inside your archive", other)
	}
	code, body := e.post("/api/workspace/mkdir", map[string]any{"parent": filepath.Join(other, "alice"), "name": "new"})
	refusedBody(t, code, body, "R-f", "inside your archive", other)
	if got := dirDigest(t, other); got != before {
		t.Error("a refusal changed the folder")
	}
	var dirs []string
	filepath.WalkDir(other, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	for _, d := range dirs {
		if filepath.Base(d) == "new" {
			t.Errorf("Create folder made %s inside the folder", d)
		}
	}
}

func fileExists(p string) bool { _, err := os.Lstat(p); return err == nil }

// covers: MA-531, R29, R9, R20, R12
// A search.db whose docs is a view is not used for the list (RR2-C11): the list is unavailable, in
// words that say whole-folder selection works, and it does; and read-only media (the folder and every file in
// it write-protected) is read all the same, with the list working and nothing added.
func TestAnotherArchiveFolderWithAHostileIndexAndOnReadOnlyMedia(t *testing.T) {
	e := newWsEnv(t)
	hostile := filepath.Join(e.dir, "hostile")
	buildArchiveAt(t, hostile)
	db, err := sqlOpen(filepath.Join(hostile, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`PRAGMA journal_mode=DELETE`, `ALTER TABLE docs RENAME TO docs_real`, `CREATE VIEW docs AS SELECT * FROM docs_real`} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	db.Close()
	for _, side := range []string{"-wal", "-shm"} {
		os.Remove(filepath.Join(hostile, "search.db"+side))
	}
	before := dirDigest(t, hostile)
	oarch := archSource("archive-other", hostile)
	e.archFolders(oarch)
	_, out := e.archRows(oarch, "alice")
	if n, _ := out["listUnavailable"].(string); !strings.Contains(n, "Whole-folder selection works") || len(out["rows"].([]any)) != 0 {
		t.Errorf("the list over an index whose docs is a view: %v", out)
	}
	id, _ := e.preview(e.archSel(oarch, entry("alice", true)))
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	if l := e.waitRun()["last"].(map[string]any); l["status"] != "complete" || l["exported"] != float64(3) {
		t.Errorf("whole-folder selection over a hostile index: %v", l)
	}
	if dirDigest(t, hostile) != before {
		t.Error("reading a hostile index changed the folder")
	}

	// read-only media
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("read-only media is modelled with permission bits, for a user who is not root")
	}
	ro := filepath.Join(e.dir, "readonly")
	buildArchiveAt(t, ro)
	var dirs []string
	filepath.WalkDir(ro, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		} else if err == nil {
			os.Chmod(p, 0o444)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Chmod(dirs[i], 0o555)
	}
	t.Cleanup(func() {
		for _, d := range dirs {
			os.Chmod(d, 0o755)
		}
	})
	roBefore := dirDigest(t, ro)
	rarch := archSource("archive-other", ro)
	e.archFolders(rarch)
	rows, out := e.archRows(rarch, "bob")
	if len(rows) != 3 {
		t.Errorf("the list on read-only media: %v", out)
	}
	id, _ = e.preview(e.archSel(rarch, entry("bob", false)))
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	if l := e.waitRun()["last"].(map[string]any); l["status"] != "complete" {
		t.Errorf("an export from read-only media: %v", l)
	}
	if dirDigest(t, ro) != roBefore {
		t.Error("the read-only folder changed")
	}
}

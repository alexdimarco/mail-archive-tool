package desktop

import (
	"context"
	"database/sql"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// Helpers for the archive source's tests through the dashboard's own handler (extraction design
// §4.2-§4.7, slice E4). The archive is a real one: the real exporter writes it from maildir
// folders, so its records, files, fixity and search index are what a capture leaves.

func putMaildirMsg(t *testing.T, folderDir, name string, raw string) {
	t.Helper()
	cur := filepath.Join(folderDir, "cur")
	if err := os.MkdirAll(cur, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cur, name), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

func archMsg(subject, id, body string) string {
	return "From: alice@example.com\r\nTo: bob@example.com\r\nSubject: " + subject +
		"\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <" + id + "@ex>\r\n\r\n" + body + "\r\n"
}

// archFixture is a real archive with two stores: "alice" (originals kept: hello, second, and
// client mail in a Clients folder) and "bob" (no originals: with-attachment, plain page, third page).
type archFixture struct {
	t   *testing.T
	dir string
	src string
}

// buildArchiveAt writes the fixture archive into dir.
func buildArchiveAt(t *testing.T, dir string) *archFixture {
	t.Helper()
	src := t.TempDir()
	alice, bob := filepath.Join(src, "alice"), filepath.Join(src, "bob")
	putMaildirMsg(t, alice, "a1.eml", archMsg("hello", "id1", "body one"))
	putMaildirMsg(t, alice, "a2.eml", archMsg("second", "id2", "body two"))
	putMaildirMsg(t, filepath.Join(alice, ".Clients"), "c1.eml", archMsg("client mail", "id3", "body three"))
	att := "From: bob@example.com\r\nTo: alice@example.com\r\nSubject: with attachment\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <id4@ex>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BND\"\r\n\r\n--BND\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nsee the attached file\r\n--BND\r\n" +
		"Content-Type: application/pdf; name=\"doc.pdf\"\r\nContent-Disposition: attachment; filename=\"doc.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8gd29ybGQ=\r\n--BND--\r\n"
	putMaildirMsg(t, bob, "b1.eml", att)
	putMaildirMsg(t, bob, "b2.eml", archMsg("plain page", "id5", "body five"))
	putMaildirMsg(t, bob, "b3.eml", archMsg("third page", "id6", "body six"))
	f := &archFixture{t: t, dir: dir, src: src}
	for _, c := range []struct {
		folder string
		raw    bool
	}{{alice, true}, {bob, false}} {
		opts := app.Options{Inputs: []string{c.folder}, Out: dir, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: c.raw}
		if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
			t.Fatalf("build the archive: %v", err)
		}
	}
	os.Remove(filepath.Join(dir, ".mailarchive.lock"))
	return f
}

func (f *archFixture) manifestPath() string {
	return filepath.Join(f.dir, ".mailarchive-manifest.json")
}

// key is the manifest key of the message whose page name holds the subject slug.
func (f *archFixture) key(slug string) string {
	f.t.Helper()
	m, err := state.Load(f.manifestPath())
	if err != nil {
		f.t.Fatal(err)
	}
	for k, r := range m.All() {
		if strings.Contains(r.Path, "_"+slug+"_") {
			return k
		}
	}
	f.t.Fatalf("no record for %q", slug)
	return ""
}

func (f *archFixture) rec(slug string) state.Record {
	m, _ := state.Load(f.manifestPath())
	r, _ := m.Get(f.key(slug))
	return r
}

func (f *archFixture) path(rel string) string { return filepath.Join(f.dir, filepath.FromSlash(rel)) }

// edit loads the manifest, lets fn change it and saves it.
func (f *archFixture) edit(fn func(m *state.Manifest)) {
	f.t.Helper()
	m, err := state.Load(f.manifestPath())
	if err != nil {
		f.t.Fatal(err)
	}
	fn(m)
	if err := m.Save(); err != nil {
		f.t.Fatal(err)
	}
}

// treeDigest is a name-size-bytes digest of every file under dir, skipping the names in skip.
func dirDigest(t *testing.T, dir string, skip ...string) string {
	t.Helper()
	var lines []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		for _, s := range skip {
			if rel == s {
				return nil
			}
		}
		b, _ := os.ReadFile(p)
		lines = append(lines, rel+"\x00"+string(b))
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\x01")
}

func sqlOpen(path string) (*sql.DB, error) { return sql.Open("sqlite", path) }

// archSource is a source description of the configured archive or another folder.
func archSource(kind, path string) map[string]any {
	return map[string]any{"kind": kind, "archive": path}
}

// archSel is a selection of whole folders of an archive.
func (e *wsEnv) archSel(src map[string]any, entries ...map[string]any) map[string]any {
	return map[string]any{"source": src, "dest": e.dest, "options": map[string]any{"preserve": true}, "folders": entries, "messages": []any{}}
}

// archFolders loads the archive's tree and returns its folders by id.
func (e *wsEnv) archFolders(src map[string]any) map[string]map[string]any {
	e.t.Helper()
	out := e.ok("/api/workspace/folders", map[string]any{"source": src})
	byID := map[string]map[string]any{}
	for _, f := range out["folders"].([]any) {
		m := f.(map[string]any)
		byID[m["id"].(string)] = m
	}
	return byID
}

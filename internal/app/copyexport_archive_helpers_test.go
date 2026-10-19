package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// Helpers for the archive source's tests (extraction design §4.6, slice E4). The archive is
// a real one: the real exporter writes it from maildir folders, so its records, files,
// fixity and search index are what a capture leaves.

// ceArchive is a real archive with two stores: "alice", captured with the original .eml of
// each message kept (hello, second, and client-mail in a Clients folder), and "bob", captured
// without (with-attachment, which has a zip, plain-page and third-page).
type ceArchive struct {
	t   *testing.T
	dir string // the archive
	src string // the maildir sources it was captured from
}

// attachMsg is a message with one PDF attachment.
func attachMsg(subject, id string) []byte {
	return []byte("From: bob@example.com\r\nTo: alice@example.com\r\nSubject: " + subject + "\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <" + id + "@ex>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"BND\"\r\n\r\n--BND\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nsee the attached file\r\n--BND\r\n" +
		"Content-Type: application/pdf; name=\"doc.pdf\"\r\nContent-Disposition: attachment; filename=\"doc.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8gd29ybGQ=\r\n--BND--\r\n")
}

func buildCEArchive(t *testing.T) *ceArchive {
	t.Helper()
	src := tmpDir(t)
	alice, bob := filepath.Join(src, "alice"), filepath.Join(src, "bob")
	writeMaildirMsg(t, alice, "a1.eml", msg("hello", "id1", "body one"))
	writeMaildirMsg(t, alice, "a2.eml", msg("second", "id2", "body two"))
	writeMaildirMsg(t, filepath.Join(alice, ".Clients"), "c1.eml", msg("client mail", "id3", "body three"))
	writeMaildirMsg(t, bob, "b1.eml", attachMsg("with attachment", "id4"))
	writeMaildirMsg(t, bob, "b2.eml", msg("plain page", "id5", "body five"))
	writeMaildirMsg(t, bob, "b3.eml", msg("third page", "id6", "body six"))
	a := &ceArchive{t: t, dir: tmpDir(t), src: src}
	a.capture(alice, true, export.Incremental)
	a.capture(bob, false, export.Incremental)
	return a
}

// capture runs the real exporter over one maildir folder into the archive.
func (a *ceArchive) capture(folder string, raw bool, mode export.Mode) {
	a.t.Helper()
	opts := Options{Inputs: []string{folder}, Out: a.dir, Mode: mode, Index: true, Pages: true, KeepRaw: raw}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		a.t.Fatalf("capture %s: %v", filepath.Base(folder), err)
	}
}

func (a *ceArchive) manifestPath() string { return filepath.Join(a.dir, archiveManifestName) }

func (a *ceArchive) abs(rel string) string { return filepath.Join(a.dir, filepath.FromSlash(rel)) }

// rec is the manifest key and record of the message whose page name holds the subject slug.
func (a *ceArchive) rec(slug string) (string, state.Record) {
	a.t.Helper()
	m, err := state.Load(a.manifestPath())
	if err != nil {
		a.t.Fatal(err)
	}
	for k, r := range m.All() {
		if strings.Contains(r.Path, "_"+slug+"_") {
			return k, r
		}
	}
	a.t.Fatalf("no record for %q in the archive", slug)
	return "", state.Record{}
}

func (a *ceArchive) key(slug string) string { k, _ := a.rec(slug); return k }

// siblings are the absolute paths of a message's page, original and zip.
func (a *ceArchive) files(slug string) (page, eml, zip string) {
	_, r := a.rec(slug)
	p, e, z := archiveSiblings(r.Path)
	return a.abs(p), a.abs(e), a.abs(z)
}

// edit loads the manifest, lets fn change it and saves it: a capture's change to the archive.
func (a *ceArchive) edit(fn func(m *state.Manifest)) {
	a.t.Helper()
	m, err := state.Load(a.manifestPath())
	if err != nil {
		a.t.Fatal(err)
	}
	fn(m)
	if err := m.Save(); err != nil {
		a.t.Fatal(err)
	}
}

// setRec changes one record.
func (a *ceArchive) setRec(slug string, fn func(*state.Record)) {
	a.t.Helper()
	k, r := a.rec(slug)
	a.edit(func(m *state.Manifest) {
		fn(&r)
		m.Add(k, r)
	})
}

func digestOf(b []byte) *state.FileDigest {
	h := sha256.Sum256(b)
	return &state.FileDigest{SHA256: hex.EncodeToString(h[:]), Size: int64(len(b))}
}

// replacePage overwrites a record's page and records the new bytes' fixity, so that only the
// archived-page gate, not a fixity check, can tell the page from a good one.
func (a *ceArchive) replacePage(slug string, page []byte) {
	a.t.Helper()
	p, _, _ := a.files(slug)
	if err := os.WriteFile(p, page, 0o644); err != nil {
		a.t.Fatal(err)
	}
	a.setRec(slug, func(r *state.Record) { r.Fixity.HTML = digestOf(page) })
}

func (a *ceArchive) read(path string) []byte {
	a.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		a.t.Fatal(err)
	}
	return b
}

// open opens the archive and fails the test on a refusal.
func (a *ceArchive) open() *ArchiveSource {
	a.t.Helper()
	s, err := OpenArchive(a.dir, ArchiveOptions{})
	if err != nil {
		a.t.Fatalf("OpenArchive: %v", err)
	}
	a.t.Cleanup(func() { s.Close() })
	return s
}

// allKeys is every record of the archive, store by store, folder by folder.
func allKeys(s *ArchiveSource) []string {
	var keys []string
	for _, n := range s.Tree() {
		keys = append(keys, s.Keys(n.ID)...)
	}
	return keys
}

// plan resolves keys and plans their export into a fresh destination.
func (a *ceArchive) plan(e *CopyExportEnv, s *ArchiveSource, keys []string) *CopyPlan {
	a.t.Helper()
	p, _ := a.planWith(e, s, keys, nil)
	return p
}

func (a *ceArchive) planWith(e *CopyExportEnv, s *ArchiveSource, keys []string, meta map[string]index.ListRow) (*CopyPlan, ArchiveResolved) {
	a.t.Helper()
	res, err := s.Messages(context.Background(), keys, meta, nil)
	if err != nil {
		a.t.Fatal(err)
	}
	dest := filepath.Join(tmpDir(a.t), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		a.t.Fatal(err)
	}
	p, err := e.PlanCopyExport(CopyPlanInput{Source: s.Describe(), Messages: res.Messages, Folders: foldersOf(res.Messages), Dest: dest,
		Options: CopyOptions{PreserveFolders: true}, Archives: []string{a.dir}, ArchiveSkipped: res.Skipped})
	if err != nil {
		a.t.Fatalf("PlanCopyExport: %v", err)
	}
	return p, res
}

// run runs a plan through the archive source and returns the result.
func (a *ceArchive) run(e *CopyExportEnv, p *CopyPlan, s *ArchiveSource) *CopyResult {
	a.t.Helper()
	res, err := e.NewCopyRun(p, s).Run(context.Background())
	if err != nil {
		a.t.Fatalf("the run was refused: %v", err)
	}
	return res
}

// entry is a manifest entry of an export, by the message's key.
func entryOf(t *testing.T, man map[string]any, key string) map[string]any {
	t.Helper()
	for _, m := range man["messages"].([]any) {
		if e := m.(map[string]any); e["source_id"] == key {
			return e
		}
	}
	t.Fatalf("no manifest entry for %q", key)
	return nil
}

// fileOf is the file of an entry with a role.
func fileOf(entry map[string]any, role string) map[string]any {
	for _, f := range entry["files"].([]any) {
		if m := f.(map[string]any); m["role"] == role {
			return m
		}
	}
	return nil
}

func notesOf(entry map[string]any) string {
	var b strings.Builder
	for _, n := range entry["notes"].([]any) {
		b.WriteString(n.(string))
		b.WriteString("\n")
	}
	return b.String()
}

// failureListed is the failure listed for a key.
func failureListed(man map[string]any, key string) map[string]any {
	for _, f := range man["failures"].([]any) {
		if m := f.(map[string]any); m["source_id"] == key {
			return m
		}
	}
	return nil
}

// treeDigest is a hash of every name, size and byte under dir, skipping the names in skip:
// two equal digests are two byte-identical trees.
func treeDigest(t *testing.T, dir string, skip ...string) string {
	t.Helper()
	files := treeFiles(t, dir)
	h := sha256.New()
	var names []string
	for n := range files {
		skipped := false
		for _, s := range skip {
			if n == s {
				skipped = true
			}
		}
		if !skipped {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		h.Write([]byte(n + "\x00" + files[n] + "\x00"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

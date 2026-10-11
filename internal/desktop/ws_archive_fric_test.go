package desktop

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

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

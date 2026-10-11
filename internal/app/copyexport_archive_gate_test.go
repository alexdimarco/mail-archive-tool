package app

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-420, MA-531, R19, R29, R4, S34, S52
// The archived-page gate (design §4.11, RR2-C9; built in slice E4): a page of a record that kept no
// original keeps its .html name only if it passes the gate. A page with a <script> and no policy
// prefix, and a page with the renderer's exact prefix that carries a live refresh (rendered before
// the F1 fix), are copied as <stem>.archived-page.txt with a note, role quarantined-archived-page,
// their hash equal to the archive's fixity; a page of the current renderer stays .html (the positive
// twin); the preview counts the quarantined pages.
func TestArchiveSourceQuarantinesAPageThatFailsTheGate(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	scripted := []byte("<!DOCTYPE html><html><head><title>x</title></head><body><script>alert(document.cookie)</script></body></html>")
	good := a.read(mustPagePath(a, "plain-page"))
	refresh := []byte(strings.Replace(string(good), "<meta name=\"viewport\"", "<meta http-equiv=\"refresh\" content=\"0;url=http://evil.example/\">\n<meta name=\"viewport\"", 1))
	if !strings.HasPrefix(string(refresh), "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n") || export.ArchivedPageInert(refresh) || !export.LiveRefreshIn(refresh) {
		t.Fatalf("the fixture page is not a page with the right prefix and a live refresh:\n%.400s", refresh)
	}
	a.replacePage("with-attachment", scripted)
	a.replacePage("plain-page", refresh)

	s := a.open()
	keys := []string{a.key("with-attachment"), a.key("plain-page"), a.key("third-page")}
	p := a.plan(e, s, keys)
	if c := p.Archive; c == nil || c.Quarantined != 2 || c.WithoutOriginal != 3 {
		t.Fatalf("the preview counts %+v, want the two bad pages quarantined of three without an original", c)
	}
	res := a.run(e, p, s)
	if res.Counts.Exported != 3 || res.Counts.Failed != 0 {
		t.Fatalf("the run: %+v", res.Counts)
	}
	man := manifestOf(t, res.Root)
	for _, slug := range []string{"with-attachment", "plain-page"} {
		ent := entryOf(t, man, a.key(slug))
		q := fileOf(ent, "quarantined-archived-page")
		if q == nil || fileOf(ent, "archived-page") != nil || !strings.HasSuffix(q["path"].(string), ".archived-page.txt") {
			t.Errorf("%s: %v, want its page as .archived-page.txt with role quarantined-archived-page", slug, ent)
			continue
		}
		_, rec := a.rec(slug)
		if q["sha256"] != rec.Fixity.HTML.SHA256 {
			t.Errorf("%s: the copy's hash %v is not the archive's fixity %s", slug, q["sha256"], rec.Fixity.HTML.SHA256)
		}
		if !strings.Contains(notesOf(ent), "doesn't begin with the security policy") {
			t.Errorf("%s: no note says why: %q", slug, notesOf(ent))
		}
		if readText(t, res.Root, q["path"].(string)) != string(a.read(mustPagePath(a, slug))) {
			t.Errorf("%s: the quarantined copy is not the archive's bytes", slug)
		}
	}
	ent := entryOf(t, man, a.key("third-page"))
	if f := fileOf(ent, "archived-page"); f == nil || !strings.HasSuffix(f["path"].(string), ".html") || notesOf(ent) != "the archive kept no original: it was captured without 'Keep each message's original .eml', so the archived page and its attachments are copied instead\n" {
		t.Errorf("a page of the current renderer: %v, want it kept as .html with only the no-original note", ent)
	}
	if zipEnt := fileOf(entryOf(t, man, a.key("with-attachment")), "archived-attachments-zip"); zipEnt == nil {
		t.Errorf("the zip of a quarantined page is still copied byte-exact beside it")
	}
}

// covers: MA-420, R19, R29, R4, S52
// A page too large to read for the gate (above export.ArchivedPageMaxBytes) is not trusted: it is
// copied as <stem>.archived-page.txt with a note that says it was too large to check, role
// quarantined-archived-page, its hash still equal to the archive's fixity, and the preview counts it;
// a page of the current renderer just under the bound stays .html (the positive twin).
func TestArchiveSourceQuarantinesAPageTooLargeToCheck(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	good := a.read(mustPagePath(a, "plain-page"))
	pad := func(n int64) []byte { // a page of the current renderer, padded inside a comment to n bytes
		page := append(append([]byte(nil), good...), []byte("\n<!--")...)
		page = append(page, bytes.Repeat([]byte{' '}, int(n)-len(page)-len("-->"))...)
		return append(page, []byte("-->")...)
	}
	big := pad(export.ArchivedPageMaxBytes + 1)
	atBound := pad(export.ArchivedPageMaxBytes)
	if !export.ArchivedPageInert(atBound) || len(big) != export.ArchivedPageMaxBytes+1 {
		t.Fatalf("the fixture pages: a page at the bound is inert %v, the big one is %d bytes", export.ArchivedPageInert(atBound), len(big))
	}
	a.replacePage("with-attachment", big)
	a.replacePage("third-page", atBound)

	s := a.open()
	p := a.plan(e, s, []string{a.key("with-attachment"), a.key("third-page")})
	if c := p.Archive; c == nil || c.Quarantined != 1 || c.WithoutOriginal != 2 {
		t.Fatalf("the preview counts %+v, want the one page too large to check quarantined", c)
	}
	res := a.run(e, p, s)
	if res.Counts.Exported != 2 || res.Counts.Failed != 0 {
		t.Fatalf("the run: %+v", res.Counts)
	}
	man := manifestOf(t, res.Root)
	ent := entryOf(t, man, a.key("with-attachment"))
	q := fileOf(ent, "quarantined-archived-page")
	if q == nil || fileOf(ent, "archived-page") != nil || !strings.HasSuffix(q["path"].(string), ".archived-page.txt") {
		t.Fatalf("a page too large to check: %v, want it as .archived-page.txt with role quarantined-archived-page", ent)
	}
	_, rec := a.rec("with-attachment")
	if q["sha256"] != rec.Fixity.HTML.SHA256 || int64(q["size"].(float64)) != rec.Fixity.HTML.Size {
		t.Errorf("the copy's hash %v and size %v are not the archive's fixity %s and %d", q["sha256"], q["size"], rec.Fixity.HTML.SHA256, rec.Fixity.HTML.Size)
	}
	if n := notesOf(ent); !strings.Contains(n, "too large for MailArchive to check") || !strings.Contains(n, "doesn't begin with the security policy") {
		t.Errorf("no note says why: %q", n)
	}
	if zipEnt := fileOf(ent, "archived-attachments-zip"); zipEnt == nil {
		t.Errorf("the zip of a quarantined page is still copied byte-exact beside it")
	}
	if f := fileOf(entryOf(t, man, a.key("third-page")), "archived-page"); f == nil || !strings.HasSuffix(f["path"].(string), ".html") {
		t.Errorf("a page of the current renderer at the bound is not kept as .html: %v", entryOf(t, man, a.key("third-page")))
	}
}

func mustPagePath(a *ceArchive, slug string) string {
	p, _, _ := a.files(slug)
	return p
}

// covers: MA-420, R20, R29, R4
// An original with no recorded checksum is copied as stored and the entry says so; an Outlook .pst
// store's records say why they have no original in its own words.
func TestArchiveSourceUnattestedAndPSTReasons(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	a.setRec("hello", func(r *state.Record) { r.Fixity.EML = nil })
	a.edit(func(m *state.Manifest) {
		for id, tok := range m.Stores {
			if tok == "bob" {
				delete(m.Stores, id)
				m.Stores["path:/mail/bob.pst"] = "bob"
			}
		}
	})
	s := a.open()
	p := a.plan(e, s, []string{a.key("hello"), a.key("third-page")})
	if p.Archive.Unattested != 1 || p.Archive.WithoutOriginal != 1 {
		t.Fatalf("the preview counts %+v", p.Archive)
	}
	res := a.run(e, p, s)
	man := manifestOf(t, res.Root)
	hello := entryOf(t, man, a.key("hello"))
	_, eml, _ := a.files("hello")
	if f := fileOf(hello, "original"); f == nil || readText(t, res.Root, f["path"].(string)) != string(a.read(eml)) || !strings.Contains(notesOf(hello), "no checksum") {
		t.Errorf("an original with no recorded fixity: %v", hello)
	}
	if n := notesOf(entryOf(t, man, a.key("third-page"))); !strings.Contains(n, "Outlook .pst items never have an original .eml") {
		t.Errorf("a .pst store's record says %q", n)
	}
	_ = os.Remove
}

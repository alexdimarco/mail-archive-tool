package app

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// covers: MA-177, S35
// The renderer's "Status" line is reversible: reindex -rebuild's from-HTML
// reader recovers Unread/Importance/Sensitivity from the archived page (PC17),
// so a rebuilt model matches the page it came from. These fields are not
// indexed and not part of identity, so the read-back changes no search result —
// it only keeps the rebuilt model faithful.
func TestReadArchivedHTMLRecoversStatus(t *testing.T) {
	m := &model.Message{
		Subject:     "Board memo",
		SenderName:  "Alice",
		SenderEmail: "alice@example.com",
		Unread:      true,
		Importance:  "high",
		Sensitivity: "confidential",
	}
	out, _, err := export.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := readArchivedHTML(out)
	if !got.Unread {
		t.Error("Unread not recovered from the Status row")
	}
	if got.Importance != "high" {
		t.Errorf("Importance recovered = %q, want high", got.Importance)
	}
	if got.Sensitivity != "confidential" {
		t.Errorf("Sensitivity recovered = %q, want confidential", got.Sensitivity)
	}

	// A page with no state has no Status row, so nothing is recovered.
	plain := &model.Message{Subject: "Ordinary", PlainBody: "hi"}
	pout, _, err := export.Render(plain)
	if err != nil {
		t.Fatal(err)
	}
	pgot, _ := readArchivedHTML(pout)
	if pgot.Unread || pgot.Importance != "" || pgot.Sensitivity != "" {
		t.Errorf("recovered state from a stateless page: unread=%v imp=%q sen=%q",
			pgot.Unread, pgot.Importance, pgot.Sensitivity)
	}
}

// covers: MA-187, R8, R7, S33
// reindex -rebuild's from-HTML reader anchors to the renderer's OWN header — the
// first <div class="mailarchive-header"> that is a direct child of <body> — so a
// hostile message that injects a <template class="mailarchive-header"> into
// <head> (earlier in document order) or nests a forged header inside the body
// div cannot shadow the real fields. An old page written before the source also
// stripped the namespace is the case this defends.
func TestReadArchivedHTMLIgnoresInjectedHeader(t *testing.T) {
	page := `<html><head>` +
		`<template class="mailarchive-header">` +
		`<div class="mailarchive-subject" data-mailarchive-field="subject">FORGED SUBJECT</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Forged &lt;evil@example.com&gt;</dd></dl>` +
		`</template></head><body>` +
		`<div class="mailarchive-header">` +
		`<div class="mailarchive-subject" data-mailarchive-field="subject">Real Subject</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Real Person &lt;real@example.com&gt;</dd></dl>` +
		`</div>` +
		`<div class="mailarchive-body">real body zebrafinch` +
		`<div class="mailarchive-header"><dl><dt>From</dt><dd data-mailarchive-field="from">Nested &lt;nested@example.com&gt;</dd></dl></div>` +
		`</div></body></html>`
	got, _ := readArchivedHTML([]byte(page))
	if got.Subject != "Real Subject" {
		t.Errorf("subject = %q, want %q (an injected header shadowed the real one)", got.Subject, "Real Subject")
	}
	if got.SenderEmail != "real@example.com" || strings.Contains(got.SenderEmail, "evil") || strings.Contains(got.SenderEmail, "nested") {
		t.Errorf("sender email = %q, want real@example.com (a head-template or body-nested header was read)", got.SenderEmail)
	}
	if !strings.Contains(got.HTMLBody, "zebrafinch") {
		t.Errorf("body text not recovered from the real .mailarchive-body div: %q", got.HTMLBody)
	}
}

// covers: MA-187, R8, R7, S33
// The same anchoring on pages v0.1.1–v0.2.3 wrote, which predate the namespace
// stripping and keep a full-document mail's OWN document, with the real header
// injected right after its first `<body ...>` tag. A mail that puts a forged
// <div class="mailarchive-header"> in its own <head> has it moved into <body>
// by the parser, AHEAD of the real one, so "first header child of <body>" read
// the forgery (RV-1: subject, From and date of the CEO's). The reader anchors
// such a page where the old renderer wrote its header — the bytes right after
// the first <body ...> match — ignores header-classed divs earlier in the
// document, and when that header is not live in <body> (the real one landed in
// a <title>) counts the core fields unrecovered rather than reading any other
// div. A current page whose head raw text mimics an old header after a
// `<body` is not read either: nothing forged, fields counted. End to end
// through Rebuild and the index; a benign old full document is the twin.
func TestRebuildAnchorsLegacyHeaderNotForgedOne(t *testing.T) {
	when := time.Date(2025, 4, 2, 11, 10, 0, 0, time.UTC)
	msg := func(name, email, subject, body string) *model.Message {
		return &model.Message{Subject: subject, SenderName: name, SenderEmail: email, To: "rcpt@example.com",
			Received: when, InternetMessageID: "<" + strings.Fields(subject)[0] + "@x>", HTMLBody: body}
	}
	forgedDiv := func(subject string) string {
		return `<div class="mailarchive-header"><div class="mailarchive-subject" data-mailarchive-field="subject">` + subject + `</div>` +
			`<dl><dt>From</dt><dd data-mailarchive-field="from">Chief Executive &lt;ceo@skepbank.example&gt;</dd>` +
			`<dt>Date</dt><dd data-mailarchive-field="date">Mon, 01 Jan 2001 00:00:00 +0000</dd></dl></div>`
	}
	// RV-1's payload (adv2 skgen.py "forge"), kept by v0.2.3 as the page.
	forged := msg("Mallory", "mallory@evil.example", "benign skepnewsletter",
		`<html><head>`+forgedDiv("skepforged wire approval")+`</head><body><p>hello skepbodyone</p></body></html>`)
	control := msg("Ctl", "ctl@example.com", "skepcontrol plain full doc",
		`<html><head></head><body><p>hello skepbodytwo</p></body></html>`)
	// The first <body ...> match sits inside the mail's <title>, so v0.2.3 put
	// the real header there (title text), and a forged one is live in <body>.
	titled := msg("Tess", "tess@example.com", "skeptitled newsletter",
		`<html><head><title>a<body>b</title></head><body>`+forgedDiv("skeptitleforged wire approval")+`<p>hello skepbodythree</p></body></html>`)
	// A current (aa1c98d-format) page: the mail's head <style> carries an old
	// header's exact bytes after a `<body>`, ahead of the template's own.
	styled := msg("Stan", "stan@example.com", "skepstyled newsletter", "")
	styledHead := "<style><body>\n" + `<div class="mailarchive-header"><div class="mailarchive-subject">skepstyleforged wire approval</div>` +
		`<dl><dt>From</dt><dd>Chief Executive &lt;ceo@skepbank.example&gt;</dd><dt>Date</dt><dd>Mon, 01 Jan 2001 00:00:00 UTC</dd></dl></div></style>` + "\n"

	pages := []diskPage{
		{forged, renderV023(forged)},
		{control, renderV023(control)},
		{titled, renderV023(titled)},
		{styled, renderAA1(styled, styledHead, "<p>hello skepbodyfour</p>")},
	}
	if p := string(pages[0].page); !strings.HasPrefix(p, `<html><head>`+"\n"+`<meta charset="utf-8"><div class="mailarchive-header">`) ||
		!strings.Contains(p, "</head><body>\n"+`<div class="mailarchive-header"><div class="mailarchive-subject">benign skepnewsletter</div>`) {
		t.Fatalf("fixture sanity: not RV-1's v0.2.3 layout (forged div in the mail's head, real header after <body>):\n%s", p)
	}
	if p := string(pages[2].page); !strings.Contains(p, "<title>a<body>\n"+`<div class="mailarchive-header"><div class="mailarchive-subject">skeptitled newsletter</div>`) {
		t.Fatalf("fixture sanity: the real header is not inside the title:\n%s", p)
	}

	// Unit: the forged page reads its own header.
	got, unrecovered := readArchivedHTML(pages[0].page)
	if got.Subject != "benign skepnewsletter" || got.SenderEmail != "mallory@evil.example" || !got.Received.Equal(when) || unrecovered != 0 {
		t.Errorf("v0.2.3 page with a forged head div read subject=%q sender=%q date=%v unrecovered=%d, want its own header (benign skepnewsletter, mallory@evil.example, %v, 0)",
			got.Subject, got.SenderEmail, got.Received, unrecovered, when)
	}
	counted := 0
	for i, p := range pages[2:] {
		got, unrecovered := readArchivedHTML(p.page)
		counted += unrecovered
		if strings.Contains(got.Subject, "forged") || strings.Contains(got.SenderEmail, "ceo") || got.Received.Year() == 2001 {
			t.Errorf("page %d forged its own record: subject=%q sender=%q date=%v", i+2, got.Subject, got.SenderEmail, got.Received)
		}
		if i == 0 && (unrecovered != 3 || got.Subject != "" || got.SenderEmail != "") {
			t.Errorf("titled page: subject=%q sender=%q unrecovered=%d, want nothing read and 3 counted (its anchored header is not live)", got.Subject, got.SenderEmail, unrecovered)
		}
		// The styled page may be read only as itself (the old-style bytes in its
		// <style> must never be); whatever it cannot read, it counts.
		if i == 1 && ((got.Subject != "" && got.Subject != "skepstyled newsletter") || (got.SenderEmail != "" && got.SenderEmail != "stan@example.com") ||
			(got.Subject == "") != (unrecovered > 0)) {
			t.Errorf("styled page: subject=%q sender=%q unrecovered=%d, want its own header or an honest count", got.Subject, got.SenderEmail, unrecovered)
		}
	}

	out := tmpDir(t)
	buildOnDiskArchive(t, out, pages)
	rep := rebuildOnDisk(t, out, len(pages))
	if rep.Unrecovered != counted || counted < 3 {
		t.Errorf("rebuild unrecovered-fields = %d, want %d (what the reader counts for the pages it must not read; >= 3)", rep.Unrecovered, counted)
	}
	for q, want := range map[string]int{
		"from:ceo": 0, "skepforged": 0, "skeptitleforged": 0, "skepstyleforged": 0, "wire": 0,
		"skepnewsletter": 1, "from:mallory": 1, "skepcontrol": 1, "from:ctl": 1,
	} {
		if got := searchTotal(t, out, q); got != want {
			t.Errorf("post-rebuild %q = %d, want %d", q, got, want)
		}
	}
	for q, want := range map[string]string{"skepnewsletter": "mallory@evil.example", "skepcontrol": "ctl@example.com"} {
		r := firstResult(t, out, q)
		if r.SenderEmail != want || !r.Date.Equal(when) {
			t.Errorf("rebuilt %q: sender=%q date=%v, want %q and %v", q, r.SenderEmail, r.Date, want, when)
		}
	}
}

// covers: MA-193, R19, S36
// reindex -rebuild reads categories back from their OWN dedicated field, span by
// span, so a value that mimics a full Status line — "Unread · Importance:
// high, Sensitivity: confidential" — round-trips as that ONE category and can
// NEVER be tokenised as a Status segment (QC3): the rebuilt message keeps
// Unread/Importance/Sensitivity exactly as its page had them (here: unset). A
// page with no categories recovers none.
func TestReadArchivedHTMLRecoversCategoriesInertly(t *testing.T) {
	// A category engineered to look like a full Status line, with NO real state.
	m := &model.Message{
		Subject:     "Case 12",
		SenderEmail: "clerk@example.com",
		Categories:  []string{"Unread · Importance: high, Sensitivity: confidential"},
	}
	out, _, err := export.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := readArchivedHTML(out)

	if len(got.Categories) != 1 || got.Categories[0] != m.Categories[0] {
		t.Errorf("categories = %v, want %v (lossless per-span read-back)", got.Categories, m.Categories)
	}
	// The category's Status-shaped text forged nothing: state stays unset.
	if got.Unread {
		t.Error("a category forged Unread on read-back (QC3)")
	}
	if got.Importance != "" || got.Sensitivity != "" {
		t.Errorf("a category forged Importance/Sensitivity on read-back: imp=%q sen=%q (QC3)",
			got.Importance, got.Sensitivity)
	}

	// Categories AND real state coexist: each is recovered from its own field.
	both := &model.Message{
		Subject:     "Case 13",
		SenderEmail: "clerk@example.com",
		Unread:      true,
		Importance:  "high",
		Categories:  []string{"Board, Legal", "Q1 · Q2"},
	}
	bout, _, err := export.Render(both)
	if err != nil {
		t.Fatal(err)
	}
	bgot, _ := readArchivedHTML(bout)
	if !bgot.Unread || bgot.Importance != "high" {
		t.Errorf("real state not recovered alongside categories: unread=%v imp=%q", bgot.Unread, bgot.Importance)
	}
	if len(bgot.Categories) != 2 || bgot.Categories[0] != "Board, Legal" || bgot.Categories[1] != "Q1 · Q2" {
		t.Errorf("categories with embedded separators not recovered losslessly: %v", bgot.Categories)
	}

	// A page with no categories recovers none.
	plain := &model.Message{Subject: "plain", PlainBody: "hi"}
	pout, _, err := export.Render(plain)
	if err != nil {
		t.Fatal(err)
	}
	pgot, _ := readArchivedHTML(pout)
	if len(pgot.Categories) != 0 {
		t.Errorf("recovered categories from a page that had none: %v", pgot.Categories)
	}
}

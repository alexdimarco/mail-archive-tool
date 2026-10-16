package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// covers: MA-304, R13, R8, S33
// `reindex -rebuild` recovers a page whatever its nesting depth, and indexes a
// shown-as-source page by the mail's words. The deep pages are the bytes the
// releases left on disk, not today's exporter output (which need not write a
// page too deep to parse any more): aa1c98d/033b1e3 rendered a body nested 509
// deep as HTML, but the page around it (html > body > div.mailarchive-body >
// ...) is past the parser's 512-element cap, which once made the rebuild
// recover nothing, so the message vanished from search; v0.7.1 and earlier
// rendered even a 600-deep body as HTML; aa1c98d showed a 600-deep body as
// escaped source, and rebuilding that must index its words, not its markup
// (searching "div" finds nothing, as on the live index). Each rebuilds with its
// subject, sender, date and body text. The positive twin, a 10-deep page as
// today's exporter writes it, has the same hits before and after.
func TestRebuildRecoversDeepAndFallbackPages(t *testing.T) {
	when := time.Date(2025, 4, 2, 9, 30, 0, 0, time.UTC)
	deepBody := func(depth int, needle string) string {
		return strings.Repeat("<div>", depth) + "<p>body words for " + needle + "</p>" + strings.Repeat("</div>", depth)
	}
	mk := func(depth int, subject, needle string) *model.Message {
		return &model.Message{
			Subject: subject, SenderName: "Deep Sender", SenderEmail: "deep@example.com",
			InternetMessageID: "<" + needle + "@x>", Received: when,
			HTMLBody: "<html><body>" + deepBody(depth, needle) + "</body></html>",
		}
	}
	shallow := mk(10, "shallow subject", "needlealpha")
	deep509 := mk(509, "deep509 subject", "needleepsilon")
	deep600 := mk(600, "deep600 subject", "needlegamma")
	shown := mk(600, "shown600 subject", "needlebeta")
	pages := []diskPage{
		{shallow, nil}, // today's exporter
		{deep509, renderAA1(deep509, "", deepBody(509, "needleepsilon"))},
		{deep600, renderAA1(deep600, "", deepBody(600, "needlegamma"))},
		{shown, renderAA1(shown, "", aa1Fallback(shown.HTMLBody, export.FallbackTooDeep))},
	}
	for _, p := range pages[1:3] {
		if _, err := xhtml.Parse(bytes.NewReader(p.page)); err == nil {
			t.Fatalf("fixture sanity: %q parses as a tree; it must be past the parser's cap to exercise the deep path", p.m.Subject)
		}
	}

	out := tmpDir(t)
	layDownArchive(t, out, pages)
	if got := indexTotal(t, out, "needlealpha"); got != 1 {
		t.Fatalf("live-index sanity: needlealpha = %d, want 1", got)
	}
	deleteSearchDB(t, out)
	rep := rebuildOnDisk(t, out, len(pages))
	if rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0 (every page's header recovered)", rep.Unrecovered)
	}
	for q, want := range map[string]int{
		"needlealpha": 1, "needleepsilon": 1, "needlegamma": 1, "needlebeta": 1,
		"deep509": 1, "deep600": 1, "shown600": 1, "deep": 4, "div": 0,
	} {
		if got := indexTotal(t, out, q); got != want {
			t.Errorf("post-rebuild %q = %d, want %d", q, got, want)
		}
	}
	for needle, subject := range map[string]string{"needleepsilon": "deep509 subject", "needlegamma": "deep600 subject"} {
		r := firstResult(t, out, needle)
		if r.Subject != subject || r.SenderEmail != "deep@example.com" || !r.Date.Equal(when) {
			t.Errorf("deep page rebuilt without its header fields: subject=%q sender=%q date=%v", r.Subject, r.SenderEmail, r.Date)
		}
	}
	r := firstResult(t, out, "needlebeta")
	if r.Subject != "shown600 subject" || strings.Contains(r.Snippet, "&lt;") || strings.Contains(strings.ToLower(r.Snippet), "div") {
		t.Errorf("fallback page must be indexed by the mail's words: subject=%q snippet=%q", r.Subject, r.Snippet)
	}
}

// covers: MA-482, R13, R8, S33
// A page too deep for the tree parser is split where the TREE parser would
// split it, never where a bare tokenizer does. The tokenizer treats a <style>
// as raw text even inside <svg>, where the tree does not, so mail head content
// like <template><svg><style><!--</style>…--> showed the tokenizer tags that the
// tree reads as a comment, and let a hostile mail choose the split on a 509-deep
// page (RV-5): "early" split inside the head, losing subject/From/date and
// indexing the real header as body text; "none" (a <plaintext>) found no split
// at all. Both payloads (adv2 skgen.py "split") now rebuild with their subject,
// sender, date and body, nothing forged and no header text in the body; so does
// a head holding thousands of decoy body-div tags in raw text (the split is
// found in a logarithmic number of parses), and a decoy inside the mail body,
// after the real split, does not move it. The same early head on a 10-deep
// page (the tree path) and a 509-deep page with no payload are the twins.
func TestRebuildDeepPageSplitsWhereTheTreeDoes(t *testing.T) {
	when := time.Date(2025, 4, 2, 11, 11, 0, 0, time.UTC)
	early := `<template><svg><style><!--</style><body><div class="mailarchive-header"><div class="mailarchive-subject">SKFORGED</div><dl><dt>From</dt><dd>forged@evil.example</dd></dl></div><div class="mailarchive-body">--></style></svg></template>`
	none := `<template><svg><style><!--</style><plaintext>--></style></svg></template>`
	decoys := strings.Repeat(`<style><div class="mailarchive-body"></style>`+"\n", 4000) + early + "\n"
	deepBody := func(depth int, word, needle string) string {
		return "<p>" + word + "</p>" + strings.Repeat("<div>", depth) + "<p>" + needle + "</p>" + strings.Repeat("</div>", depth)
	}
	page := func(name string, depth int, headExtra, preBody string) diskPage {
		m := &model.Message{
			Subject: "skep" + name + "subj deep", SenderName: name + " Sender", SenderEmail: "sk" + name + "@example.com",
			To: "rcpt@example.com", Received: when, InternetMessageID: "skep-split-" + name + "@example.com",
			TransportHeaders: "From: " + name + " Sender <sk" + name + "@example.com>\r\nX-Note: skeptransportword",
		}
		return diskPage{m, renderAA1(m, headExtra, preBody+deepBody(depth, "skep"+name+"word", "skep"+name+"needle"))}
	}
	pages := []diskPage{
		page("ctl", 509, "", ""),
		page("early", 509, early+"\n", ""),
		page("none", 509, none+"\n", ""),
		page("decoy", 509, decoys, `<p>skepdecoyleadword</p><style><div class="mailarchive-body"></style>`),
		page("shallow", 10, early+"\n", ""),
	}
	for _, p := range pages[:4] {
		if _, err := xhtml.Parse(bytes.NewReader(p.page)); err == nil {
			t.Fatalf("fixture sanity: %q parses as a tree; it must be past the parser's cap", p.m.Subject)
		}
	}

	// Cost guard: 4002 candidates on the decoy page, found by binary search in
	// about log2(4002) ≈ 12 prefix parses (+1 to confirm), not one per decoy.
	parses, parse := 0, parsePrefix
	parsePrefix = func(b []byte) (*xhtml.Node, error) { parses++; return parse(b) }
	got, _ := readArchivedHTML(pages[3].page)
	parsePrefix = parse
	if assure.Reached(t, parses, "prefix parses for the decoy page"); parses > 16 || got.Subject != pages[3].m.Subject {
		t.Errorf("decoy page: %d prefix parses (want <= 16, logarithmic in the 4002 candidates), subject %q", parses, got.Subject)
	}

	for _, p := range pages {
		got, unrecovered := readArchivedHTML(p.page)
		if unrecovered != 0 || got.Subject != p.m.Subject || got.SenderEmail != p.m.SenderEmail || !got.Received.Equal(when) {
			t.Errorf("%q read subject=%q sender=%q date=%v unrecovered=%d, want its own header and 0",
				p.m.Subject, got.Subject, got.SenderEmail, got.Received, unrecovered)
		}
		if strings.Contains(got.HTMLBody, "skeptransportword") || strings.Contains(got.HTMLBody, "SKFORGED") {
			t.Errorf("%q: header or head text indexed as body: %.200q", p.m.Subject, got.HTMLBody)
		}
	}

	out := tmpDir(t)
	buildOnDiskArchive(t, out, pages)
	if rep := rebuildOnDisk(t, out, len(pages)); rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0", rep.Unrecovered)
	}
	want := map[string]int{"skforged": 0, "from:forged": 0, "skeptransportword": 0, "skepdecoyleadword": 1}
	for _, name := range []string{"ctl", "early", "none", "decoy", "shallow"} {
		want["skep"+name+"subj"], want["from:sk"+name], want["skep"+name+"word"], want["skep"+name+"needle"] = 1, 1, 1, 1
	}
	for q, n := range want {
		if got := searchTotal(t, out, q); got != n {
			t.Errorf("post-rebuild %q = %d, want %d", q, got, n)
		}
	}
	r := firstResult(t, out, "skepearlyneedle")
	if r.Subject != "skepearlysubj deep" || r.SenderEmail != "skearly@example.com" || !r.Date.Equal(when) {
		t.Errorf("early page rebuilt as subject=%q sender=%q date=%v", r.Subject, r.SenderEmail, r.Date)
	}
}

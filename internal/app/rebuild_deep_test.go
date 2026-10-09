package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

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

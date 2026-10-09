package app

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
)

// covers: MA-304, R13, R8, S33
// `reindex -rebuild` recovers a page whatever its nesting depth, and indexes a
// shown-as-source page by the mail's words. A body nested 509 deep renders as
// HTML, but the page around it (html > body > div.mailarchive-body > ...) is
// past the parser's 512-element cap, which once made the rebuild recover
// nothing — no subject, sender, date or body — so the message vanished from
// search. A body nested 600 deep is shown as escaped source; rebuilding it must
// index its words, not its markup (searching "div" finds nothing, as on the
// live index). The positive twin, a 10-deep page, rebuilds as before.
func TestRebuildRecoversDeepAndFallbackPages(t *testing.T) {
	out := tmpDir(t)
	mk := func(depth int, subject, needle string) *model.Message {
		return &model.Message{
			Subject: subject, SenderName: "Deep Sender", SenderEmail: "deep@example.com",
			InternetMessageID: "<" + needle + "@x>",
			Received:          time.Date(2025, 4, 2, 9, 30, 0, 0, time.UTC),
			HTMLBody: "<html><body>" + strings.Repeat("<div>", depth) + "<p>body words for " + needle + "</p>" +
				strings.Repeat("</div>", depth) + "</body></html>",
		}
	}
	buildRebuildArchive(t, out, []rbMsg{
		{[]string{"Inbox"}, mk(10, "shallow subject", "needlealpha")},
		{[]string{"Inbox"}, mk(509, "deep509 subject", "needleepsilon")},
		{[]string{"Inbox"}, mk(600, "deep600 subject", "needlebeta")},
	})

	queries := []string{"needlealpha", "needleepsilon", "needlebeta", "deep509", "deep600", "div", "deep"}
	pre := map[string]int{}
	for _, q := range queries {
		pre[q] = indexTotal(t, out, q)
	}
	if pre["needlealpha"] != 1 || pre["needleepsilon"] != 1 || pre["needlebeta"] != 1 || pre["div"] != 0 {
		t.Fatalf("live-index sanity: %v (want each needle 1, div 0)", pre)
	}

	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != 3 {
		t.Fatalf("rebuild report = %+v, want Rebuilt=3", rep)
	}
	for _, q := range queries {
		if got := indexTotal(t, out, q); got != pre[q] {
			t.Errorf("post-rebuild %q = %d, want %d (same hits as the live index)", q, got, pre[q])
		}
	}
	r := firstResult(t, out, "needleepsilon")
	if r.Subject != "deep509 subject" || r.SenderEmail != "deep@example.com" || r.Date.IsZero() {
		t.Errorf("deep page rebuilt without its header fields: subject=%q sender=%q date=%v", r.Subject, r.SenderEmail, r.Date)
	}
	r = firstResult(t, out, "needlebeta")
	if r.Subject != "deep600 subject" || strings.Contains(r.Snippet, "&lt;") || strings.Contains(strings.ToLower(r.Snippet), "div") {
		t.Errorf("fallback page must be indexed by the mail's words: subject=%q snippet=%q", r.Subject, r.Snippet)
	}
}

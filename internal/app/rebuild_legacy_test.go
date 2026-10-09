package app

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// covers: MA-480, R13, R8, S33
// Pages written by v0.1.1–v0.2.3 stamp the header's Date row with the zone's
// ABBREVIATION ("Wed, 02 Apr 2025 10:30:00 UTC"), where later releases write a
// numeric offset. `reindex -rebuild` reads that stamp back, so an old record
// keeps its date (and date-bounded search still finds it) — on the fragment
// template and on a kept full document alike. An abbreviation that names no
// known offset is counted unrecovered, never indexed as a guess hours off. A
// numeric offset (a zone with no name) and today's stamp read as before.
func TestReadArchivedHTMLRecoversLegacyDateStamp(t *testing.T) {
	when := time.Date(2025, 4, 2, 10, 30, 0, 0, time.UTC)
	msg := func(subject string, at time.Time, body string) *model.Message {
		return &model.Message{Subject: subject, SenderName: "Old Sender", SenderEmail: "old@example.com",
			To: "rcpt@example.com", Received: at, HTMLBody: body}
	}
	frag := msg("legacy fragment", when, "<p>fragment body</p>")
	full := msg("legacy full document", when, "<html><head></head><body><p>full body</p></body></html>")

	for _, m := range []*model.Message{frag, full} {
		page := renderV023(m)
		if !strings.Contains(string(page), "<dd>Wed, 02 Apr 2025 10:30:00 UTC</dd>") {
			t.Fatalf("fixture sanity: %q is not a v0.2.3 page with an abbreviated stamp:\n%s", m.Subject, page)
		}
		got, unrecovered := readArchivedHTML(page)
		if !got.Received.Equal(when) || unrecovered != 0 {
			t.Errorf("%q: date = %v (unrecovered=%d), want %v and 0 — the v0.2 stamp was not read back", m.Subject, got.Received, unrecovered, when)
		}
		if got.Subject != m.Subject || got.SenderEmail != "old@example.com" {
			t.Errorf("%q: subject=%q sender=%q, want the page's own header", m.Subject, got.Subject, got.SenderEmail)
		}
	}

	// A zone with no name was written as a bare offset: read exactly.
	offset := msg("legacy offset", time.Date(2025, 4, 2, 12, 30, 0, 0, time.FixedZone("", 2*3600)), "<p>x</p>")
	if page := renderV023(offset); !strings.Contains(string(page), "+0200</dd>") {
		t.Fatalf("fixture sanity: no numeric offset in %s", page)
	} else if got, u := readArchivedHTML(page); !got.Received.Equal(when) || u != 0 {
		t.Errorf("numeric-offset stamp: date = %v (unrecovered=%d), want %v", got.Received, u, when)
	}

	// An abbreviation with no known offset is honest-unrecovered, not a guess.
	odd := msg("legacy odd zone", time.Date(2025, 4, 2, 15, 30, 0, 0, time.FixedZone("ZZT", 5*3600)), "<p>x</p>")
	if page := renderV023(odd); !strings.Contains(string(page), "15:30:00 ZZT</dd>") {
		t.Fatalf("fixture sanity: no ZZT stamp in %s", page)
	} else if got, u := readArchivedHTML(page); !got.Received.IsZero() || u != 1 {
		t.Errorf("unknown abbreviation: date = %v (unrecovered=%d), want zero and 1 (counted, never guessed at offset 0)", got.Received, u)
	}

	// Positive twin: today's renderer's stamp still reads.
	cur, _, err := export.Render(msg("current page", when, "<p>x</p>"))
	if err != nil {
		t.Fatal(err)
	}
	if got, u := readArchivedHTML(cur); !got.Received.Equal(when) || u != 0 {
		t.Errorf("current page: date = %v (unrecovered=%d), want %v", got.Received, u, when)
	}

	// End to end: the rebuilt index carries the date, so a date-bounded search
	// finds the old record.
	out := tmpDir(t)
	buildOnDiskArchive(t, out, []diskPage{{frag, renderV023(frag)}})
	rep := rebuildOnDisk(t, out, 1)
	if rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0", rep.Unrecovered)
	}
	r := firstResult(t, out, "fragment")
	if !r.Date.Equal(when) {
		t.Errorf("rebuilt record date = %v, want %v", r.Date, when)
	}
	assure.Reached(t, searchTotal(t, out, "after:2025-04-02 before:2025-04-03 fragment"), "date-bounded hits for the old record")
}

// covers: MA-481, R13, R8, S33
// A page shown as source carries the mail's HTML as escaped text in a
// <pre data-mailarchive-fallback>, and `reindex -rebuild` indexes that text
// (the mail's words), not the page's markup. The attribute is honoured only on
// the layout the renderer writes: the <pre> is the ONLY non-whitespace child of
// the body div, its class is "plain", and its value is a known reason. Pages
// v0.1.1–v0.2.3 wrote put a mail fragment raw into the body div, so a mail can
// carry the attribute there itself; its decoy <pre> no longer hides every other
// word of the mail from the rebuilt index (RV-6). A real shown-as-source page,
// for every reason, still indexes by the mail's words.
func TestRebuildHonoursFallbackOnlyOnRendererLayout(t *testing.T) {
	when := time.Date(2025, 4, 2, 11, 20, 0, 0, time.UTC)
	msg := func(subject, body string) *model.Message {
		return &model.Message{Subject: subject, SenderName: "Frag Sender", SenderEmail: "skfrag@example.com",
			To: "rcpt@example.com", Received: when, InternetMessageID: "<" + strings.Fields(subject)[0] + "@x>", HTMLBody: body}
	}
	// RV-6's payload (skgen.py "old"), kept raw by v0.2.3 in the body div.
	decoy := msg("skepfragsubj", `<pre data-mailarchive-fallback="x">skepdecoy only</pre><p>the real message says skeprealword</p>`)
	decoyPage := renderV023(decoy)
	if !strings.Contains(string(decoyPage), "<div class=\"mailarchive-body\">\n<pre data-mailarchive-fallback=\"x\">skepdecoy only</pre><p>") {
		t.Fatalf("fixture sanity: not RV-6's v0.2.3 layout:\n%s", decoyPage)
	}

	// Each renderer-layout condition, alone, decides: a decoy that misses any
	// one is indexed as an ordinary body (its markup, as before aa1c98d).
	for name, body := range map[string]string{
		"not the only child": `<pre class="plain" data-mailarchive-fallback="too-deeply-nested">skepdecoy</pre><p>skeprealtwo</p>`,
		"text beside it":     `<pre class="plain" data-mailarchive-fallback="too-deeply-nested">skepdecoy</pre>skeprealthree`,
		"no class plain":     `<pre data-mailarchive-fallback="too-deeply-nested">skepdecoy</pre>`,
		"unknown reason":     `<pre class="plain" data-mailarchive-fallback="x">skepdecoy</pre>`,
	} {
		got, _ := readArchivedHTML(renderV023(msg("decoy "+name, body)))
		if !strings.Contains(got.HTMLBody, "<pre") || !strings.Contains(got.HTMLBody, "skepdecoy") {
			t.Errorf("%s: HTMLBody = %q, want the body div's own markup (the attribute not honoured)", name, got.HTMLBody)
		}
	}

	// Real shown-as-source pages (aa1c98d's layout), one per reason, are
	// honoured: the indexed body is the mail's source text, not the <pre>.
	// "too-large" is the reason being added alongside this change (RV-8).
	source := "<html><body>" + strings.Repeat("<div>", 600) + "<p>body words for skepfallbackneedle</p>" + strings.Repeat("</div>", 600) + "</body></html>"
	for _, reason := range []string{export.FallbackTooDeep, export.FallbackUnparseable, export.FallbackHostile, "too-large"} {
		got, _ := readArchivedHTML(renderAA1(msg("fallback "+reason, source), "", aa1Fallback(source, reason)))
		if got.HTMLBody != source {
			t.Errorf("fallback %q: HTMLBody = %.80q..., want the mail's source text", reason, got.HTMLBody)
		}
	}

	out := tmpDir(t)
	fallback := msg("skepfallbacksubj", source)
	buildOnDiskArchive(t, out, []diskPage{
		{decoy, decoyPage},
		{fallback, renderAA1(fallback, "", aa1Fallback(source, export.FallbackTooDeep))},
	})
	rebuildOnDisk(t, out, 2)
	for q, want := range map[string]int{
		"skeprealword": 1, "skepdecoy": 1, // every word of the old page's mail
		"skepfallbackneedle": 1, "div": 0, // the fallback page: the mail's words, not its markup
	} {
		if got := searchTotal(t, out, q); got != want {
			t.Errorf("post-rebuild %q = %d, want %d", q, got, want)
		}
	}
	assure.Reached(t, firstResult(t, out, "skepdecoy").SenderEmail, "the decoy page's rebuilt sender")
}

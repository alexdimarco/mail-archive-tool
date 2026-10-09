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

// covers: MA-480, R13, R8, S33
// v0.1.1–v0.2.3 converted every time to UTC before rendering (each source set
// Received/Sent to ….UTC()), so the Date stamp they wrote always ends "UTC".
// Any other abbreviation is not theirs and names no offset the reader can
// trust — not even one this machine's own zone uses, which time.Parse would
// resolve through time.Local ("CST" is +8 in Shanghai and -6 in Chicago) — so
// it is counted unrecovered whatever zone the rebuild runs in, never indexed
// hours off. This host's own abbreviations (January and July) are among those
// checked, so wherever the host is not on UTC a reader that trusts the local
// zone fails here. The page's other fields still read; a "UTC" stamp still
// reads (TestReadArchivedHTMLRecoversLegacyDateStamp is the twin).
func TestReadArchivedHTMLLegacyStampReadsOnlyUTC(t *testing.T) {
	abbrevs := map[string]int{"GMT": 0, "GMT+3": 3 * 3600, "EST": -5 * 3600, "PDT": -7 * 3600, "CST": 8 * 3600, "CET": 3600, "BST": 3600}
	for _, month := range []time.Month{time.January, time.July} {
		if name, off := time.Date(2025, month, 15, 12, 0, 0, 0, time.Local).Zone(); name != "UTC" {
			abbrevs[name] = off
		}
	}
	var pages []diskPage
	for name, off := range abbrevs {
		m := &model.Message{Subject: "skzone " + name, SenderName: "Zone Sender", SenderEmail: "skzone@example.com", To: "rcpt@example.com",
			Received: time.Date(2025, 4, 2, 10, 30, 0, 0, time.FixedZone(name, off)), InternetMessageID: "<skzone-" + name + "@x>", HTMLBody: "<p>skzoneword</p>"}
		page := renderV023(m)
		if !strings.Contains(string(page), "10:30:00 "+name+"</dd>") {
			t.Fatalf("fixture sanity: no %q stamp in %s", name, page)
		}
		got, unrecovered := readArchivedHTML(page)
		if !got.Received.IsZero() || unrecovered != 1 {
			t.Errorf("%q stamp: date = %v (unrecovered=%d), want zero and 1 (not a stamp those releases wrote; counted, never guessed)", name, got.Received, unrecovered)
		}
		if got.Subject != m.Subject || got.SenderEmail != "skzone@example.com" {
			t.Errorf("%q stamp: subject=%q sender=%q, want the page's own header", name, got.Subject, got.SenderEmail)
		}
		pages = append(pages, diskPage{m, page})
	}

	out := tmpDir(t)
	buildOnDiskArchive(t, out, pages)
	if rep := rebuildOnDisk(t, out, len(pages)); rep.Unrecovered != len(pages) {
		t.Errorf("rebuild unrecovered-fields = %d, want %d (one date per page)", rep.Unrecovered, len(pages))
	}
	if got := searchTotal(t, out, "skzoneword"); got != len(pages) {
		t.Errorf("post-rebuild \"skzoneword\" = %d, want %d (every record still indexed)", got, len(pages))
	}
	assure.Reached(t, searchTotal(t, out, "from:skzone@example.com"), "rebuilt records by sender")
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
	// one is indexed as an ordinary body (its markup, as before aa1c98d). On a
	// v0.2.3 page the attribute is never honoured at all (those releases wrote
	// no fallback <pre>); the same bodies laid into aa1c98d's template, where
	// the body div is the renderer's, are decided by the layout alone.
	for name, body := range map[string]string{
		"not the only child": `<pre class="plain" data-mailarchive-fallback="too-deeply-nested">skepdecoy</pre><p>skeprealtwo</p>`,
		"text beside it":     `<pre class="plain" data-mailarchive-fallback="too-deeply-nested">skepdecoy</pre>skeprealthree`,
		"no class plain":     `<pre data-mailarchive-fallback="too-deeply-nested">skepdecoy</pre>`,
		"unknown reason":     `<pre class="plain" data-mailarchive-fallback="x">skepdecoy</pre>`,
	} {
		m := msg("decoy "+name, body)
		for layout, page := range map[string][]byte{"v0.2.3": renderV023(m), "aa1c98d": renderAA1(m, "", body)} {
			got, _ := readArchivedHTML(page)
			if !strings.Contains(got.HTMLBody, "<pre") || !strings.Contains(got.HTMLBody, "skepdecoy") {
				t.Errorf("%s (%s page): HTMLBody = %q, want the body div's own markup (the attribute not honoured)", name, layout, got.HTMLBody)
			}
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

// covers: MA-483, R13, R8, S33
// Pages v0.1.1–v0.2.3 wrote for a full-document mail have NO body div: the
// header was injected into the mail's own <body>, and the mail's content
// follows it. `reindex -rebuild` reads that layout in both readers (RV-7): a
// shallow page indexes <body>'s content after the header (it once indexed no
// body words at all), and a page too deep for the tree parser is split right
// after the anchored header (it once recovered nothing). Either way the
// record keeps its subject, sender and date and its words; RV-1's forged head
// div, at depth 600, still forges nothing and adds no words. A 600-deep
// fragment page (the template, with a body div) is the twin.
func TestRebuildReadsLegacyPageWithoutBodyDiv(t *testing.T) {
	when := time.Date(2025, 4, 2, 11, 30, 0, 0, time.UTC)
	deep := func(inner string, d int) string {
		return strings.Repeat("<div>", d) + inner + strings.Repeat("</div>", d)
	}
	msg := func(name, subject, body string) *model.Message {
		return &model.Message{Subject: subject, SenderName: name + " Sender", SenderEmail: "sk" + strings.ToLower(name) + "@example.com",
			To: "rcpt@example.com", Received: when, InternetMessageID: "<" + subject + "@x>", HTMLBody: body}
	}
	forgedDiv := `<div class="mailarchive-header"><div class="mailarchive-subject" data-mailarchive-field="subject">skepforged wire approval</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Chief Executive &lt;ceo@skepbank.example&gt;</dd></dl></div>`
	// adv2 skgen.py "old", plus RV-1's payload made deep.
	full := msg("Full", "skepfullsubj", "<html><head></head><body><p>skepfullword</p>"+deep("<p>skepfullneedle</p>", 10)+"</body></html>")
	deepOld := msg("Deepold", "skepdeepoldsubj", "<html><head></head><body><p>skepdeepoldword</p>"+deep("<p>skepdeepoldneedle</p>", 600)+"</body></html>")
	forged := msg("Mallory", "skepdeepforgesubj", "<html><head>"+forgedDiv+"</head><body><p>skepdeepforgeword</p>"+deep("<p>skepdeepforgeneedle</p>", 600)+"</body></html>")
	frag := msg("Deepfrag", "skepdeepfragsubj", "<p>skepdeepfragword</p>"+deep("<p>skepdeepfragneedle</p>", 600))
	pages := []diskPage{{full, renderV023(full)}, {deepOld, renderV023(deepOld)}, {forged, renderV023(forged)}, {frag, renderV023(frag)}}
	for _, p := range pages[:3] {
		if strings.Contains(string(p.page), `class="mailarchive-body"`) || !strings.HasPrefix(string(p.page), "<html><head>") {
			t.Fatalf("fixture sanity: %q is not a v0.2.3 full-document page (no body div):\n%.300s", p.m.Subject, p.page)
		}
	}
	for _, p := range pages[1:] {
		if _, err := xhtml.Parse(bytes.NewReader(p.page)); err == nil {
			t.Fatalf("fixture sanity: %q parses as a tree; it must be past the parser's cap", p.m.Subject)
		}
	}

	for _, p := range pages {
		got, unrecovered := readArchivedHTML(p.page)
		if unrecovered != 0 || got.Subject != p.m.Subject || got.SenderEmail != p.m.SenderEmail || !got.Received.Equal(when) {
			t.Errorf("%q read subject=%q sender=%q date=%v unrecovered=%d, want its own header and 0",
				p.m.Subject, got.Subject, got.SenderEmail, got.Received, unrecovered)
		}
		if needle := strings.TrimSuffix(p.m.Subject, "subj") + "needle"; !strings.Contains(got.HTMLBody, needle) || strings.Contains(got.HTMLBody, "mailarchive-subject") {
			t.Errorf("%q: HTMLBody (%d bytes) lacks %q or carries a header: %.120q", p.m.Subject, len(got.HTMLBody), needle, got.HTMLBody)
		}
	}

	out := tmpDir(t)
	buildOnDiskArchive(t, out, pages)
	if rep := rebuildOnDisk(t, out, len(pages)); rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0", rep.Unrecovered)
	}
	want := map[string]int{"from:ceo": 0, "skepforged": 0, "wire": 0}
	for _, name := range []string{"skepfull", "skepdeepold", "skepdeepforge", "skepdeepfrag"} {
		want[name+"subj"], want[name+"word"], want[name+"needle"] = 1, 1, 1
	}
	for _, p := range pages {
		want["from:"+p.m.SenderEmail] = 1
	}
	for q, n := range want {
		if got := searchTotal(t, out, q); got != n {
			t.Errorf("post-rebuild %q = %d, want %d", q, got, n)
		}
	}
	r := firstResult(t, out, "skepdeepoldneedle")
	if r.Subject != "skepdeepoldsubj" || r.SenderEmail != "skdeepold@example.com" || !r.Date.Equal(when) {
		t.Errorf("deep old page rebuilt as subject=%q sender=%q date=%v", r.Subject, r.SenderEmail, r.Date)
	}
}

// covers: MA-481, MA-483, R13, R8, S33
// A page v0.1.1–v0.2.3 wrote (it carries their header span) never has its
// fallback attribute honoured, nor a body div taken as the renderer's: those
// releases never wrote a fallback <pre> and kept the mail's own classes, so
// on either of their layouts a mail could wear both. The body is all of
// <body>'s content but the header, so no mail-supplied element hides the rest
// of the mail from the rebuilt index (RV-6 beyond the fragment decoy): (a) a
// full document whose content starts with its own <div class="mailarchive-body">,
// with or without a decoy fallback <pre> in it, keeps every word after it; (b) a
// fragment that is one renderer-shaped fallback <pre> is indexed by its visible
// text, so words after an escaped `<!--` or inside an escaped `<style>` are not
// re-read as markup and lost. Each record keeps its own header. A plain <div>
// and a <pre> without the attribute are the twins (adv2 fvgen.py "fulldecoy",
// "oldpre").
func TestRebuildLegacyPageNeverHonoursFallback(t *testing.T) {
	when := time.Date(2025, 4, 2, 11, 50, 0, 0, time.UTC)
	msg := func(name, body string) *model.Message {
		return &model.Message{Subject: name + "subj", SenderName: name + " Sender", SenderEmail: name + "@example.com",
			To: "rcpt@example.com", Received: when, InternetMessageID: "<" + name + "@x>", HTMLBody: body}
	}
	full := func(content string) string { return `<html><head></head><body>` + content + `</body></html>` }
	cases := []struct {
		m    *model.Message
		want []string // words the rebuilt index must find for this mail
	}{
		{msg("skfdecoy", full(`<div class="mailarchive-body"><pre class="plain" data-mailarchive-fallback="too-deeply-nested">skfdecoyshown</pre></div><p>the real message says skfdecoyreal</p>`)),
			[]string{"skfdecoyshown", "skfdecoyreal"}},
		{msg("skfdiv", full(`<div class="mailarchive-body">skfdivshown</div><p>the real message says skfdivreal</p>`)),
			[]string{"skfdivshown", "skfdivreal"}},
		{msg("skfctl", full(`<div>skfctlshown</div><p>the real message says skfctlreal</p>`)),
			[]string{"skfctlshown", "skfctlreal"}},
		{msg("skopre", `<pre class="plain" data-mailarchive-fallback="too-deeply-nested">skoprevisible &lt;!-- skoprehidden words --&gt;</pre>`),
			[]string{"skoprevisible", "skoprehidden"}},
		{msg("skoprestyle", `<pre class="plain" data-mailarchive-fallback="unparseable">&lt;style&gt;skoprestylehidden&lt;/style&gt; skoprestylevisible</pre>`),
			[]string{"skoprestylevisible", "skoprestylehidden"}},
		{msg("skoprectl", `<pre class="plain">skoprectlvisible &lt;!-- skoprectlhidden --&gt;</pre>`),
			[]string{"skoprectlvisible", "skoprectlhidden"}},
	}
	var pages []diskPage
	for _, c := range cases {
		p := renderV023(c.m)
		if _, _, legacy := legacyHeaderSpan(p); !legacy {
			t.Fatalf("fixture sanity: %q carries no v0.2.3 header span:\n%s", c.m.Subject, p)
		}
		pages = append(pages, diskPage{c.m, p})
	}
	if p := string(pages[0].page); !strings.Contains(p, "</dl></div><div class=\"mailarchive-body\"><pre class=\"plain\" data-mailarchive-fallback=") {
		t.Fatalf("fixture sanity: the full-document decoy does not follow the injected header:\n%s", p)
	}

	for i, p := range pages {
		got, unrecovered := readArchivedHTML(p.page)
		if unrecovered != 0 || got.Subject != p.m.Subject || got.SenderEmail != p.m.SenderEmail || !got.Received.Equal(when) {
			t.Errorf("%q read subject=%q sender=%q date=%v unrecovered=%d, want its own header and 0",
				p.m.Subject, got.Subject, got.SenderEmail, got.Received, unrecovered)
		}
		if strings.Contains(got.HTMLBody, "mailarchive-subject") {
			t.Errorf("%q: header indexed as body: %.200q", p.m.Subject, got.HTMLBody)
		}
		for _, w := range cases[i].want {
			if !strings.Contains(got.HTMLBody, w) {
				t.Errorf("%q: HTMLBody lacks %q: %.200q", p.m.Subject, w, got.HTMLBody)
			}
		}
	}

	out := tmpDir(t)
	buildOnDiskArchive(t, out, pages)
	if rep := rebuildOnDisk(t, out, len(pages)); rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0", rep.Unrecovered)
	}
	for _, c := range cases {
		for _, w := range append([]string{c.m.Subject, "from:" + c.m.SenderEmail}, c.want...) {
			if got := searchTotal(t, out, w); got != 1 {
				t.Errorf("post-rebuild %q = %d, want 1", w, got)
			}
		}
	}
}

// covers: MA-483, R13, R8, S33
// On a v0.1.1–v0.2.3 full-document page the parser puts whatever the mail had
// between its </head> and its <body> tag — text, a hidden preheader div — into
// <body> AHEAD of the injected header. That is the mail's content too, and
// `reindex -rebuild` indexes it, shallow and on a page too deep for the tree
// parser, alongside the content after the header. A header-classed div there
// (RV-1's forged one, moved in from the mail's <head>) is still skipped: it
// forges nothing and adds no words (adv2 fvgen.py "before").
func TestRebuildLegacyPageIndexesContentBeforeHeader(t *testing.T) {
	when := time.Date(2025, 4, 2, 11, 55, 0, 0, time.UTC)
	deep := func(inner string, d int) string {
		return strings.Repeat("<div>", d) + inner + strings.Repeat("</div>", d)
	}
	msg := func(name, body string) *model.Message {
		return &model.Message{Subject: name + "subj", SenderName: name + " Sender", SenderEmail: name + "@example.com",
			To: "rcpt@example.com", Received: when, InternetMessageID: "<" + name + "@x>", HTMLBody: body}
	}
	forgedDiv := `<div class="mailarchive-header"><div class="mailarchive-subject" data-mailarchive-field="subject">skbforged wire approval</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Chief Executive &lt;ceo@skbbank.example&gt;</dd></dl></div>`
	pages := []diskPage{}
	for _, m := range []*model.Message{
		msg("skbtext", `<html><head></head><p>skbtextbefore</p><body><p>skbtextafter</p></body></html>`),
		msg("skbpre", `<html><head><title>t</title></head><div style="display:none">skbprebefore</div><body style="margin:0"><p>skbpreafter</p></body></html>`),
		msg("skbforge", `<html><head>`+forgedDiv+`</head><div style="display:none">skbforgebefore</div><body><p>skbforgeafter</p></body></html>`),
		msg("skbdeep", `<html><head></head><div style="display:none">skbdeepbefore</div><body><p>skbdeepafter</p>`+deep("<p>skbdeepneedle</p>", 600)+`</body></html>`),
		msg("skbdeepforge", `<html><head>`+forgedDiv+`</head><div style="display:none">skbdeepforgebefore</div><body><p>skbdeepforgeafter</p>`+deep("<p>skbdeepforgeneedle</p>", 600)+`</body></html>`),
	} {
		pages = append(pages, diskPage{m, renderV023(m)})
	}
	for i, p := range pages {
		_, err := xhtml.Parse(bytes.NewReader(p.page))
		if deep := i >= 3; deep != (err != nil) {
			t.Fatalf("fixture sanity: %q parse error = %v; the deep pages must be past the parser's cap, the others not", p.m.Subject, err)
		}
		if _, _, legacy := legacyHeaderSpan(p.page); !legacy || strings.Contains(string(p.page), `class="mailarchive-body"`) {
			t.Fatalf("fixture sanity: %q is not a v0.2.3 full-document page:\n%.300s", p.m.Subject, p.page)
		}
	}

	for _, p := range pages {
		got, unrecovered := readArchivedHTML(p.page)
		if unrecovered != 0 || got.Subject != p.m.Subject || got.SenderEmail != p.m.SenderEmail || !got.Received.Equal(when) {
			t.Errorf("%q read subject=%q sender=%q date=%v unrecovered=%d, want its own header and 0",
				p.m.Subject, got.Subject, got.SenderEmail, got.Received, unrecovered)
		}
		name := strings.TrimSuffix(p.m.Subject, "subj")
		if !strings.Contains(got.HTMLBody, name+"before") || !strings.Contains(got.HTMLBody, name+"after") {
			t.Errorf("%q: HTMLBody lacks %q or %q: %.200q", p.m.Subject, name+"before", name+"after", got.HTMLBody)
		}
		if strings.Contains(got.HTMLBody, "mailarchive-subject") || strings.Contains(got.HTMLBody, "skbforged") {
			t.Errorf("%q: a header indexed as body: %.200q", p.m.Subject, got.HTMLBody)
		}
	}

	out := tmpDir(t)
	buildOnDiskArchive(t, out, pages)
	if rep := rebuildOnDisk(t, out, len(pages)); rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0", rep.Unrecovered)
	}
	want := map[string]int{"from:ceo": 0, "skbforged": 0, "wire": 0, "skbdeepneedle": 1, "skbdeepforgeneedle": 1}
	for _, p := range pages {
		name := strings.TrimSuffix(p.m.Subject, "subj")
		want[p.m.Subject], want["from:"+p.m.SenderEmail], want[name+"before"], want[name+"after"] = 1, 1, 1, 1
	}
	for q, n := range want {
		if got := searchTotal(t, out, q); got != n {
			t.Errorf("post-rebuild %q = %d, want %d", q, got, n)
		}
	}
}

package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-187, MA-482, R13, R8, S33
// A CURRENT page (v0.3.0 onward; here the bytes aa1c98d wrote) whose mail head
// raw text — a <style>, a <noscript> (raw text to the scripting-on parser the
// exporter and the reader use) or a comment — carries an old (v0.1.1–v0.2.3)
// header's exact bytes right after a `<body` looks, byte for byte, like an old
// page whose real header landed somewhere not live. The reader cannot tell
// which header the renderer wrote (MA-187's open gap), so it reads neither:
// nothing the mail wrote is indexed as its subject, sender or date, and what is
// not read is counted. The record keeps every body word, shallow and 509 deep:
// on a page too deep for the tree parser the split is still made at the body
// div after the first header div directly in <body>, where the tree would read
// the body (before, no split was found and the whole record, body words too,
// left search). No header text is indexed as body. The same pages without the
// payload, shallow and deep, rebuild fully (the twins).
func TestRebuildCurrentPageWithOldHeaderBytesInHead(t *testing.T) {
	when := time.Date(2025, 4, 2, 11, 40, 0, 0, time.UTC)
	oldHeader := func(from string) string {
		return `<div class="mailarchive-header"><div class="mailarchive-subject">skmimicforged wire approval</div>` +
			`<dl><dt>From</dt><dd>` + from + `</dd><dt>Date</dt><dd>Mon, 01 Jan 2001 00:00:00 UTC</dd></dl></div>`
	}
	// headExtra exactly as aa1c98d's sanitizer serialized each mail head (each
	// child followed by "\n"; checked against that exporter's output, and the
	// reviewers' fvgen.py "erase" pages). A comment keeps `<` but escapes `&`,
	// so its payload's From carries no entity.
	heads := map[string]string{
		"style":    "<style><body>\n" + oldHeader("Chief Executive &lt;ceo@skmimbank.example&gt;") + "</style>\n",
		"noscript": "<noscript><body>\n" + oldHeader("Chief Executive &lt;ceo@skmimbank.example&gt;") + "</noscript>\n",
		"comment":  "<!--<body>\n" + oldHeader("ceo@skmimbank.example") + "-->\n",
		"ctl":      "<style>p{color:red}</style>\n",
	}
	type tc struct {
		name  string
		depth int
		head  string
	}
	var cases []tc
	for _, h := range []string{"style", "noscript", "comment", "ctl"} {
		cases = append(cases, tc{"skmim" + h, 10, h}, tc{"skmimdeep" + h, 509, h})
	}
	var pages []diskPage
	for _, c := range cases {
		m := &model.Message{
			Subject: c.name + "subj", SenderName: c.name + " Sender", SenderEmail: c.name + "@example.com",
			To: "rcpt@example.com", Received: when, InternetMessageID: "<" + c.name + "@example.com>",
			TransportHeaders: "From: " + c.name + "@example.com\r\nX-Note: skmimtransportword",
		}
		inner := "<p>" + c.name + "word</p>" + strings.Repeat("<div>", c.depth) + "<p>" + c.name + "needle</p>" + strings.Repeat("</div>", c.depth)
		pages = append(pages, diskPage{m, renderAA1(m, heads[c.head], inner)})
	}
	for i, p := range pages {
		_, err := xhtml.Parse(bytes.NewReader(p.page))
		if deep := cases[i].depth > 10; deep != (err != nil) {
			t.Fatalf("fixture sanity: %q parse error = %v; the 509-deep pages must be past the parser's cap, the others not", p.m.Subject, err)
		}
		if cases[i].head != "ctl" {
			if _, _, legacy := legacyHeaderSpan(p.page); !legacy {
				t.Fatalf("fixture sanity: %q carries no old header bytes after its first <body match", p.m.Subject)
			}
		}
	}

	counted := 0
	for i, p := range pages {
		got, unrecovered := readArchivedHTML(p.page)
		counted += unrecovered
		if strings.Contains(got.Subject, "forged") || strings.Contains(got.SenderEmail, "ceo") || got.Received.Year() == 2001 {
			t.Errorf("%q forged its own record: subject=%q sender=%q date=%v", p.m.Subject, got.Subject, got.SenderEmail, got.Received)
		}
		own := unrecovered == 0 && got.Subject == p.m.Subject && got.SenderEmail == p.m.SenderEmail && got.Received.Equal(when)
		none := unrecovered == 3 && got.Subject == "" && got.SenderEmail == "" && got.Received.IsZero()
		if !own && !none || cases[i].head == "ctl" && !own {
			t.Errorf("%q read subject=%q sender=%q date=%v unrecovered=%d, want its own header and 0, or (payload pages) nothing and 3",
				p.m.Subject, got.Subject, got.SenderEmail, got.Received, unrecovered)
		}
		name := cases[i].name
		if !strings.Contains(got.HTMLBody, name+"word") || !strings.Contains(got.HTMLBody, name+"needle") {
			t.Errorf("%q: HTMLBody (%d bytes) lacks %q or %q", p.m.Subject, len(got.HTMLBody), name+"word", name+"needle")
		}
		if strings.Contains(got.HTMLBody, "skmimtransportword") || strings.Contains(got.HTMLBody, "skmimicforged") || strings.Contains(got.HTMLBody, "Sender") {
			t.Errorf("%q: header or head text indexed as body: %.200q", p.m.Subject, got.HTMLBody)
		}
	}

	out := tmpDir(t)
	buildOnDiskArchive(t, out, pages)
	if rep := rebuildOnDisk(t, out, len(pages)); rep.Unrecovered != counted {
		t.Errorf("rebuild unrecovered-fields = %d, want %d (what the reader counts)", rep.Unrecovered, counted)
	}
	want := map[string]int{"from:ceo": 0, "skmimicforged": 0, "wire": 0, "skmimtransportword": 0}
	for _, c := range cases {
		want[c.name+"word"], want[c.name+"needle"] = 1, 1
		if c.head == "ctl" {
			want[c.name+"subj"], want["from:"+c.name] = 1, 1
		}
	}
	for q, n := range want {
		if got := searchTotal(t, out, q); got != n {
			t.Errorf("post-rebuild %q = %d, want %d", q, got, n)
		}
	}
	assure.Reached(t, firstResult(t, out, "skmimdeepstyleneedle").Path, "the deep styled page's rebuilt record")
}

// covers: MA-187, R19, R8, S33
// OPEN GAP in MA-187's "cannot erase" half, pending an OPERATOR ruling — encoded
// the kit's way (a t.Skip that FAILS if un-skipped today), never a loosened
// assertion. A CURRENT page (today's exporter) whose mail head raw text mimics
// an old (v0.1.1–v0.2.3) header's exact bytes after a `<body` is, byte for byte,
// indistinguishable from an old page whose real header is not live, so the
// reader counts subject/From/date unrecovered — it ERASES the page's real
// header rather than risk forging the mimicked one. (It never forges and keeps
// the body: TestRebuildCurrentPageWithOldHeaderBytesInHead pins that.) MA-187's
// invariant is "cannot forge OR erase"; the erase half is open here.
//
// The settled resolution is an out-of-band writer-version signal on NEW pages
// (internal/state + internal/export, not this branch's files), so a v0.3+ page
// is never read as a legacy one and its real header is recovered; pages already
// on disk from v0.3.0 to aa1c98d carry no such signal and stay an open gap.
// This test asserts the invariant that ruling must restore for a new page — the
// real subject, From and date recovered, unrecovered 0 — and SKIPS pending the
// ruling. When a writer signal lands and this passes, REMOVE the Skip so it
// becomes a permanent guard.
func TestRebuildCurrentPageRecoversRealHeaderDespiteMimicInHead(t *testing.T) {
	t.Skip("MA-187 open gap: a current page's real subject/From/date are erased when its head raw text mimics the old header bytes; pending an operator ruling on an out-of-band writer-version signal (docs/review-sanitizer-roundtrip-adversarial.md)")

	when := time.Date(2025, 4, 2, 11, 40, 0, 0, time.UTC)
	oldHeader := `<div class="mailarchive-header"><div class="mailarchive-subject">mimicforged wire approval</div>` +
		`<dl><dt>From</dt><dd>Chief Executive &lt;ceo@mimbank.example&gt;</dd><dt>Date</dt><dd>Mon, 01 Jan 2001 00:00:00 UTC</dd></dl></div>`
	head := "<style><body>\n" + oldHeader + "</style>\n"
	m := &model.Message{
		Subject: "realmimicsubj", SenderName: "Real Sender", SenderEmail: "real@example.com",
		Received: when, InternetMessageID: "<realmimic@example.com>",
	}
	page := renderAA1(m, head, "<p>realmimicword</p>")
	if _, _, legacy := legacyHeaderSpan(page); !legacy {
		t.Fatalf("fixture sanity: the head mimic must match legacyHeaderSpan")
	}
	got, unrecovered := readArchivedHTML(page)
	if unrecovered != 0 || got.Subject != m.Subject || got.SenderEmail != m.SenderEmail || !got.Received.Equal(when) {
		t.Errorf("a current page's real header must be recovered despite the head mimic: subject=%q sender=%q date=%v unrecovered=%d, want %q/%q/%v and 0",
			got.Subject, got.SenderEmail, got.Received, unrecovered, m.Subject, m.SenderEmail, when)
	}
}

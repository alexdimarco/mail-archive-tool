package app

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-492, R8, R13, S33
// The live index (BodyText on the original mail) and `reindex -rebuild`
// (BodyText on the archived page's body div) read the same words. RV-3: the
// SVG style/comment trick (adv2-scripts/mkhide.py) hid the words after it from
// both, though the page shows them. RV-4: a mail without `</head>`
// (adv2-scripts/mknohead.py) had no body words in the live index, but did after
// a rebuild. Each word is now found once, on the live index and after a
// rebuild, at ordinary depth and nested past the tree parser's cap (a page
// shown as source, read by the fallback extractor on both sides). So are the
// words after an SVG that a stray `</p>` or `</br>` closes (adv2 rv5/mkbr.py):
// the parser leaves foreign content there, so the <style> that follows is HTML
// and the comment trick hides nothing; the fallback once missed that. Positive
// twins: an ordinary mail's title, style and script words are found on neither
// side, nor are a noscript's tag and attribute words, while its text is; a
// noscript in the mail's explicit <head> (which the page keeps in its own head,
// out of the rebuild's reach) is found on neither side.
func TestLiveAndRebuiltIndexReadTheSameVisibleWords(t *testing.T) {
	out := tmpDir(t)
	date := time.Date(2025, 4, 2, 12, 30, 0, 0, time.UTC)
	n := 0
	mk := func(subject, body string) rbMsg {
		n++
		return rbMsg{[]string{"Inbox"}, &model.Message{
			Subject: subject, SenderName: "Parity Sender", SenderEmail: "parity@example.com",
			InternetMessageID: "<parity-" + strconv.Itoa(n) + "@example.com>",
			Received:          date.Add(time.Duration(n) * time.Minute),
			HTMLBody:          body,
		}}
	}
	deep := func(inner string) string {
		return strings.Repeat("<div>", 600) + inner + strings.Repeat("</div>", 600)
	}
	buildRebuildArchive(t, out, []rbMsg{
		mk("quarterly update", `<html><body><p>Hello team</p><svg width="0" height="0"><style><!--</style><script>--></style></svg><p>please wire funds to account kestreleleven today</p></body></html>`),
		mk("skeptic hide", `<p>Hello skepvisible</p><svg width=0 height=0><style><!--</style><script>--></style></svg><p>please send to account skephidden today</p>`),
		mk("nohead ordinary", `<html><head><meta name="viewport" content="width=device-width"><body><p>hello kestreltwelve</p></body></html>`),
		mk("withhead ordinary", `<html><head><meta name="viewport" content="width=device-width"></head><body><p>hello kestrelthirteen</p></body></html>`),
		mk("nohead title", `<head><title>t</title><body><p>hello skepnoheadtitleword</p>`),
		mk("ordinary twin", `<html><head><title>parititleword</title><style>.x{} paristyleword</style><script>pariscriptword()</script></head>`+
			`<body><p>paribodyword</p><style>paribodystyle</style><script>paribodyscript</script>`+
			`<noscript><p>parinoscriptword</p><img alt="parinsaltword"></noscript></body></html>`),
		mk("head noscript", `<html><head><noscript><p>parheadnsword</p></noscript></head><body><p>parheadnsbody</p></body></html>`),
		mk("deep hide", `<html><body>`+deep(`<p>Hello</p><svg><style><!--</style><script>--></style></svg><p>account deepkestreleleven today</p>`)+`</body></html>`),
		mk("deep nohead", `<html><head><meta name="viewport" content="x"><body>`+deep(`<p>hello deepkestreltwelve</p>`)+`</body></html>`),
		mk("deep p breakout", `<html><body>`+deep(`<p>Hello brvisibleone</p><svg width=0 height=0></p><style><!--</style><p>please wire funds to brhiddenp today</p>--></style></svg><p>brtailone</p>`)+`</body></html>`),
		mk("deep br breakout", `<html><body>`+deep(`<p>Hello brvisibletwo</p><svg width=0 height=0></br><style><!--</style><p>please wire funds to brhiddenbr today</p>--></style></svg><p>brtailtwo</p>`)+`</body></html>`),
		mk("shallow p breakout", `<html><body><p>Hello</p><svg width=0 height=0></p><style><!--</style><p>please wire funds to brshallowp today</p>--></style></svg></body></html>`),
		mk("shallow br breakout", `<html><body><p>Hello</p><svg width=0 height=0></br><style><!--</style><p>please wire funds to brshallowbr today</p>--></style></svg></body></html>`),
	})

	want := map[string]int{
		"kestreleleven": 1, "wire": 5, "skephidden": 1, "skepvisible": 1,
		"kestreltwelve": 1, "kestrelthirteen": 1, "skepnoheadtitleword": 1,
		"paribodyword": 1, "parinoscriptword": 1, "parheadnsbody": 1, "parheadnsword": 0,
		"parititleword": 0, "paristyleword": 0, "pariscriptword": 0, "paribodystyle": 0, "paribodyscript": 0, "parinsaltword": 0,
		"deepkestreleleven": 1, "deepkestreltwelve": 1,
		"brvisibleone": 1, "brhiddenp": 1, "brtailone": 1, "brvisibletwo": 1, "brhiddenbr": 1, "brtailtwo": 1,
		"brshallowp": 1, "brshallowbr": 1,
	}
	live := map[string]int{}
	for q := range want {
		live[q] = indexTotal(t, out, q)
	}

	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != 13 {
		t.Fatalf("rebuild report = %+v, want Rebuilt=13 (every page re-derived)", rep)
	}
	for q, w := range want {
		rebuilt := indexTotal(t, out, q)
		if live[q] != w || rebuilt != w {
			t.Errorf("search %q: live=%d rebuilt=%d, want %d on both", q, live[q], rebuilt, w)
		}
	}
	r := firstResult(t, out, "kestreltwelve")
	if r.Subject != "nohead ordinary" || !strings.Contains(r.Snippet, "kestreltwelve") {
		t.Errorf("rebuilt nohead record: subject=%q snippet=%q", r.Subject, r.Snippet)
	}
}

// parityArchive exports bodies as HTML mails (the live index reads each
// original mail), then deletes search.db and rebuilds it from the pages. It
// returns each query's hit count on both sides.
func parityArchive(t *testing.T, bodies map[string]string, queries []string) (live, rebuilt map[string]int) {
	t.Helper()
	out := tmpDir(t)
	date := time.Date(2025, 4, 3, 9, 0, 0, 0, time.UTC)
	var msgs []rbMsg
	n := 0
	for subject, body := range bodies {
		n++
		msgs = append(msgs, rbMsg{[]string{"Inbox"}, &model.Message{
			Subject: subject, SenderName: "Parity Sender", SenderEmail: "parity@example.com",
			InternetMessageID: "<parity2-" + strconv.Itoa(n) + "@example.com>",
			Received:          date.Add(time.Duration(n) * time.Minute),
			HTMLBody:          body,
		}})
	}
	buildRebuildArchive(t, out, msgs)
	live = map[string]int{}
	for _, q := range queries {
		live[q] = indexTotal(t, out, q)
	}
	deleteSearchDB(t, out)
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != len(bodies) {
		t.Fatalf("rebuild report = %+v, want Rebuilt=%d (every page re-derived)", rep, len(bodies))
	}
	rebuilt = map[string]int{}
	for _, q := range queries {
		rebuilt[q] = indexTotal(t, out, q)
	}
	return live, rebuilt
}

// parityProblems lists each query whose count is not the wanted one, on the
// live index or after the rebuild.
func parityProblems(want map[string]int, live, rebuilt map[string]int) []string {
	var problems []string
	for q, w := range want {
		if live[q] != w || rebuilt[q] != w {
			problems = append(problems, fmt.Sprintf("search %q: live=%d rebuilt=%d, want %d on both", q, live[q], rebuilt[q], w))
		}
	}
	sort.Strings(problems)
	return problems
}

func keysOf(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// covers: MA-492, R8, R13
// Which head is kept out of the index must match what the rebuild can read.
// Every head is skipped now, explicit OR implied, and the rebuild wraps the
// body div's inner HTML in <body> so its content is body (not an implied head).
// A head written the usual way, on its own line after <html> (and after a
// doctype and a comment, Litmus-style), is explicit: the exporter keeps it in
// the page's own <head>, so its noscript text is found on neither side. A mail
// whose explicit <body> starts with a noscript keeps that noscript at the start
// of the page's body div, which the rebuild wraps in <body>, so its text is
// found on both sides. And the old implied-head drift is closed: a fragment
// that leads with a text-bearing <noscript> or <noframes> — bare, after
// whitespace, or after a <meta> — is an implied head the exporter moves into
// the page's <head>; it was indexed live only (live=1 rebuilt=0), and is now
// found on NEITHER side, equal.
func TestLiveAndRebuiltIndexAgreeOnHeadsAroundNoscript(t *testing.T) {
	bodies := map[string]string{
		"newline head": "<html>\n<head>\n<noscript><p>parnlheadns</p></noscript>\n</head>\n<body>\n<p>parnlbody</p>\n</body>\n</html>",
		"litmus head": "<!DOCTYPE html>\n<!-- preheader -->\n<html lang=\"en\" xmlns=\"http://www.w3.org/1999/xhtml\">\n  <head>\n    <meta charset=\"utf-8\">\n" +
			"    <noscript><p>parlitheadns</p></noscript>\n  </head>\n  <body>\n    <p>parlitbody</p>\n  </body>\n</html>",
		"body starts with noscript": `<html><head><title>t</title></head><body><noscript><p>parbodystartns</p></noscript><p>parbodystartbody</p></body></html>`,
		"fragment leading noscript": `<noscript><p>parfragns</p></noscript><p>parfragbody</p>`,
		"fragment leading noframes": `<noframes><p>parfragnf</p></noframes><p>parfragnfbody</p>`,
		"noscript after whitespace": "\n  <noscript><p>parwsns</p></noscript><p>parwsbody</p>",
		"noscript after a meta":     `<meta charset="utf-8"><noscript><p>parmetans</p></noscript><p>parmetabody</p>`,
	}
	want := map[string]int{
		"parnlheadns": 0, "parnlbody": 1,
		"parlitheadns": 0, "parlitbody": 1,
		"parbodystartns": 1, "parbodystartbody": 1,
		"parfragns": 0, "parfragbody": 1,
		"parfragnf": 0, "parfragnfbody": 1,
		"parwsns": 0, "parwsbody": 1,
		"parmetans": 0, "parmetabody": 1,
	}
	live, rebuilt := parityArchive(t, bodies, keysOf(want))
	assure.Reached(t, live["parnlbody"]+rebuilt["parnlbody"], "hits for an indexed body word")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
}

// covers: MA-492, R8, R13
// A head <noscript> that holds a charset <meta> and text (adv2 rv5 corpus
// 46_head_noscript_charset_text): with scripting on, the parser keeps the
// noscript's content in the head as raw text; with scripting off, the <p>
// leaves the head and becomes body content. Which of the two the page shows
// is the exporter's choice: today the dual-mode re-check sends this mail to
// the source fallback, and a scripting-off retry would render the text in
// the body. Whatever it shows, the live index and the rebuilt one must find
// the same words. So this pins parity, not a count, for the noscript text,
// and the body word is found once on both sides.
func TestLiveAndRebuiltIndexAgreeOnHeadNoscriptMarkup(t *testing.T) {
	bodies := map[string]string{
		"head noscript charset": `<html><head><noscript><meta charset="utf-8"><p>parheadnstext</p></noscript></head><body><p>parheadnsbodyword</p></body></html>`,
	}
	same := []string{"parheadnstext"}
	want := map[string]int{"parheadnsbodyword": 1}
	live, rebuilt := parityArchive(t, bodies, append(keysOf(want), same...))
	assure.Reached(t, live["parheadnsbodyword"]+rebuilt["parheadnsbodyword"], "hits for the body word")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
	for _, q := range same {
		if live[q] != rebuilt[q] {
			t.Errorf("search %q: live=%d rebuilt=%d, want the same count on both (R8)", q, live[q], rebuilt[q])
		}
	}
}

// covers: MA-492, R8, R13
// A <noscript> nested in another is read the same on both sides. The live
// index reads the outer one's raw text as a reader with JavaScript off does
// (the nested noscript an element, its text text); the page the exporter's
// in-place retry writes (the outer one carries a charset meta) must hold the
// same, and `reindex -rebuild` reads it back. The retry used to parse the
// nested noscript's text a second time: the reviewer's
// adv_noscript2_body_esc_d turned its escaped refresh into a <meta> element,
// so "meta"/"equiv"/"refresh" were found live only; a doubly escaped tag
// became an element and its name left the rebuilt index; a nested <p> beside
// text was dropped; and an SVG <noscript>'s escaped text was re-parsed into
// an element. Each word is now found once on both sides.
func TestLiveAndRebuiltIndexAgreeOnNestedNoscripts(t *testing.T) {
	bodies := map[string]string{
		"nest case alpha":   `<p>nsrvwords</p><noscript><meta charset=utf-8><noscript>&lt;meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/nsrvbeacon"&gt;</noscript></noscript><p>nsrvtail</p>`,
		"nest case bravo":   `<p>nsa</p><noscript><meta charset=utf-8><noscript>nestedtextword<p>nestedkeptword</p></noscript></noscript><p>nstail</p>`,
		"nest case charlie": `<p>nsdbl</p><noscript><meta charset=utf-8><noscript>&amp;lt;dbltag&amp;gt;dblword&amp;lt;/dbltag&amp;gt;</noscript></noscript>`,
		"nest case delta":   `<p>nssvg</p><noscript><meta charset=utf-8></noscript><svg><noscript>&lt;svgtag&gt;svgword&lt;/svgtag&gt;</noscript></svg>`,
	}
	want := map[string]int{
		"nsrvwords": 1, "nsrvtail": 1, "meta": 1, "equiv": 1, "refresh": 1, "nsrvbeacon": 1,
		"nestedtextword": 1, "nestedkeptword": 1, "nstail": 1,
		"dblword": 1, "dbltag": 1, "lt": 1,
		"svgword": 1, "svgtag": 1,
	}
	live, rebuilt := parityArchive(t, bodies, keysOf(want))
	assure.Reached(t, live["nestedtextword"]+rebuilt["nestedtextword"], "hits for a nested noscript word")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
}

// covers: MA-492, R8
// OPEN GAP in R8, NOT accepted (pending an owner ruling on MA-476's escaped
// twin), encoded the kit's way: a t.Skip that FAILS if un-skipped today. The
// in-place noscript retry writes the OUTER noscript's own text children back
// literally (x/net renders a noscript's text as it is), so escaped markup in
// that text — decoded once by the scripting-off parse that rebuilt the
// content — is read as markup by a reader with JavaScript off and by
// `reindex -rebuild`: `&lt;b&gt;outeresctext&lt;/b&gt;` beside a charset meta
// renders as a <b> element, and the tag's name is found live only (live=1
// rebuilt=0). A nested noscript's text is written escaped and is at parity
// (TestLiveAndRebuiltIndexAgreeOnNestedNoscripts); writing the outer text the
// same way closes this, but MA-476's escaped twin beside a real charset meta
// would then render with its refresh as inert text instead of falling back as
// unsafe-after-reparse, which changes what MA-476 asserts: the owner's call.
// When it is ruled and this passes, REMOVE the Skip.
func TestLiveAndRebuiltIndexAgreeOnOuterNoscriptEscapedText(t *testing.T) {
	t.Skip("R8 open gap: the noscript retry writes the outer noscript's escaped text back literally, so it is read as markup (the tag name is found live only); pending an owner ruling on MA-476's escaped twin (docs/review-sanitizer-roundtrip-adversarial.md)")

	bodies := map[string]string{
		"outer escaped case": `<p>outerescbody</p><noscript>&lt;outeresctag&gt;outeresctext&lt;/outeresctag&gt;<meta charset=utf-8></noscript>`,
	}
	want := map[string]int{"outerescbody": 1, "outeresctext": 1, "outeresctag": 1}
	live, rebuilt := parityArchive(t, bodies, keysOf(want))
	assure.Reached(t, live["outerescbody"]+rebuilt["outerescbody"], "hits for the body word")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
}

// covers: MA-473, MA-492, R8, R13
// Table parts that arrive while a table the flattener kept is open but the
// current node is past flattenDepth (startTablePart's dropped-current-node
// branch, a397085): a cell kept below the cap (a table at depth 471), dropped
// markup in it past 480, and then a <td>, a <tr>, a nested table's own cells
// (alone, and followed by the kept table's next <td>), or a <td> inside an SVG
// foreignObject. Each part drops with the region it arrives in, leaving a word
// boundary. Credited to the kept table instead (through the kept-stack path,
// which clears the drop stack), the nested table's parts were written as the
// kept table's, its </table> closed the kept table, and the kept table's next
// <td> was a stray tag the parser ignores: "tpthenafter" and "tpthennext" ran
// together on the page and in the rebuilt index only. Each mail renders as
// HTML, its page parses in both scripting readings with the words on either
// side of a part apart on the page, and every word is found once live and once
// after a rebuild, the glued pair on neither side.
func TestLiveAndRebuiltIndexAgreeOnTablePartsPastTheCap(t *testing.T) {
	divs := func(n int, inner string) string {
		return strings.Repeat("<div>", n) + inner + strings.Repeat("</div>", n)
	}
	kept := func(inner string) string {
		return `<html><body>` + divs(470, `<table><tr><td>`+divs(10, inner)+`</td></tr></table>`) + `</body></html>`
	}
	bodies := map[string]string{
		"tp cell after dropped": kept(`tpcellalpha<td>tpcellbeta`),
		"tp row after dropped":  kept(`tprowalpha<tr><td>tprowbeta`),
		"tp nested table":       kept(`<table><tr><td>tpnestalpha</td><td>tpnestbeta</td></tr></table>tpnestafter`),
		"tp nested then cell":   kept(`<table><tr><td>tpthenalpha</td><td>tpthenbeta</td></tr></table>tpthenafter<td>tpthennext`),
		"tp foreignobject cell": kept(`<svg><foreignObject>tpfoalpha<td>tpfobeta</foreignObject></svg>`),
	}
	pairs := [][2]string{
		{"tpcellalpha", "tpcellbeta"}, {"tprowalpha", "tprowbeta"},
		{"tpnestalpha", "tpnestbeta"}, {"tpnestbeta", "tpnestafter"},
		{"tpthenalpha", "tpthenbeta"}, {"tpthenafter", "tpthennext"}, {"tpfoalpha", "tpfobeta"},
	}
	want := map[string]int{}
	for _, p := range pairs {
		want[p[0]], want[p[1]], want[p[0]+p[1]] = 1, 1, 0
	}

	out := tmpDir(t)
	date := time.Date(2025, 4, 4, 9, 0, 0, 0, time.UTC)
	var msgs []rbMsg
	for subject, body := range bodies {
		msgs = append(msgs, rbMsg{[]string{"Inbox"}, &model.Message{
			Subject: subject, SenderName: "Parity Sender", SenderEmail: "parity@example.com",
			InternetMessageID: "<" + strings.ReplaceAll(subject, " ", "-") + "@example.com>",
			Received:          date.Add(time.Duration(len(msgs)) * time.Minute), HTMLBody: body,
		}})
	}
	rels := buildRebuildArchive(t, out, msgs)
	for subject := range bodies {
		page, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(assure.Reached(t, rels[subject], "page of "+subject))))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(page), "data-mailarchive-fallback") {
			t.Errorf("%s: must render as HTML (flattened), not fall back", subject)
		}
		for _, scripting := range []bool{true, false} {
			if _, err := xhtml.ParseWithOptions(bytes.NewReader(page), xhtml.ParseOptionEnableScripting(scripting)); err != nil {
				t.Errorf("%s: the page does not parse (scripting=%v): %v", subject, scripting, err)
			}
		}
		for _, p := range pairs {
			if strings.Contains(string(page), p[0]+p[1]) {
				t.Errorf("%s: the page runs %q and %q together", subject, p[0], p[1])
			}
		}
	}
	live := map[string]int{}
	for q := range want {
		live[q] = indexTotal(t, out, q)
	}
	deleteSearchDB(t, out)
	rebuildOnDisk(t, out, len(bodies))
	rebuilt := map[string]int{}
	for q := range want {
		rebuilt[q] = indexTotal(t, out, q)
	}
	assure.Reached(t, live["tpcellbeta"]+rebuilt["tpcellbeta"], "hits for a word after a table part past the cap")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
}

// covers: MA-492, R8
// OPEN GAP in R8, NOT accepted, encoded the kit's way: a t.Skip that FAILS if
// un-skipped today. The fresh-eyes corpus run (rv3/corpus-adv, 372 vectors)
// leaves 8 mails whose words differ live and rebuilt, all of them already at
// 8f900c9 (the pre-integration merge), in two shapes. (1) A <plaintext> that
// runs to the end of the mail (adv_plaintext_{body,head}_{esc,lnk,ref}_b) also
// swallows the page's own closing `</div></body></html>`, which the rebuild
// then indexes as the words "div", "body" and "html" (rebuilt only). (2) A
// <noscript> nested past flattenDepth (adv_deep_xmp, adv_deep2_template): the
// flattener reads noscript content as markup and drops the noscript's own
// tags past the cap, so what the scripting-on reading of the mail held as the
// noscript's raw text — and an <xmp> or <template> straddling its end — reads
// differently on the page: the refresh's words are found rebuilt only, and so
// is a word after a <template> whose </template> sat in the noscript's raw
// text (in the mail it is template content, which no reader sees). Fixing
// either changes what the page or the flattener writes, which is for the
// round-3 pass and the owner; until then this stays skipped. When it passes,
// REMOVE the Skip.
func TestLiveAndRebuiltIndexAgreeOnCorpusResidualShapes(t *testing.T) {
	t.Skip("R8 open gap: a mail-final <plaintext> swallows the page's closing tags, and a noscript dropped past flattenDepth changes the scripting-on reading (rv3/corpus-adv residuals, pre-existing at 8f900c9)")

	divs := func(n int, inner string) string {
		return strings.Repeat("<div>", n) + inner + strings.Repeat("</div>", n)
	}
	const refresh = `<meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/resid">`
	bodies := map[string]string{
		"resid plaintext tail": `<p>residplainwords</p><plaintext><noscript><meta charset=utf-8>residtailword</noscript><p>tail</p>`,
		"resid deep xmp":       divs(500, `<noscript><meta charset=utf-8><xmp></noscript>`+refresh+`</xmp></noscript>`) + `<p>residxmpafter</p>`,
		"resid deep template":  divs(500, `<template><noscript><meta charset=utf-8></template>&lt;meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/residtpl"&gt;</noscript>`) + `<p>residtplafter</p>`,
	}
	same := []string{"div", "body", "html", "resid", "residtpl", "refresh", "equiv", "residplainwords", "residtailword", "residxmpafter", "residtplafter"}
	live, rebuilt := parityArchive(t, bodies, same)
	assure.Reached(t, live["residplainwords"]+rebuilt["residplainwords"], "hits for a body word")
	for _, q := range same {
		if live[q] != rebuilt[q] {
			t.Errorf("search %q: live=%d rebuilt=%d, want the same count on both (R8)", q, live[q], rebuilt[q])
		}
	}
}

// covers: MA-492, R8, R13, S33
// Deep mails, at and past the depth where an exporter would flatten them (480)
// and past the tree parser's cap (512), keep their word boundaries and their
// words on both sides: block-spaced text 490 divs deep (adv2 rv5 corpus
// 53_deep_480_blocks), one line per level 600 deep (31_divs_600_deep), a
// 150-deep table layout (28_tables_150_deep), and the RV-3 hide payload at
// depths 480, 500 and 509. Each word is found once live and once after a
// rebuild; the words glued together are found on neither side. However the
// page renders such a mail (as HTML, flattened, or as source), the rebuild
// must read the same words the live index read from the mail.
func TestLiveAndRebuiltIndexAgreeOnDeepBlockText(t *testing.T) {
	divs := func(n int, inner string) string {
		return strings.Repeat("<div>", n) + inner + strings.Repeat("</div>", n)
	}
	var lines strings.Builder
	for i := 0; i < 600; i++ {
		lines.WriteString("<div><p>deepline" + strconv.Itoa(i) + "</p>")
	}
	lines.WriteString(strings.Repeat("</div>", 600))
	hide := func(word string) string {
		return `<p>Hello</p><svg width=0 height=0><style><!--</style><script>--></style></svg><p>account ` + word + ` today</p>`
	}
	bodies := map[string]string{
		"blocks 490 deep": `<html><body>` + divs(490, `<p>blkalpha</p><p>blkbravo</p><div>blkcharlie</div>blkdelta<br>blkecho<li>blkfoxtrot</li>`+
			`<table><tr><td>blkgolf</td><td>blkhotel</td></tr></table>`) + `</body></html>`,
		"lines 600 deep": `<div dir="ltr">Top reply line.` + lines.String() + `</div>`,
		"tables 150 deep": `<html><body>` + strings.Repeat(`<table role="presentation"><tr><td align="center">`, 150) +
			`<p>One hundred fifty layer table.</p><div>tblsecond block</div><div>tblthird block</div>` +
			strings.Repeat(`</td></tr></table>`, 150) + `</body></html>`,
		"hide 480 deep": `<html><body>` + divs(480, hide("hidefoureighty")) + `</body></html>`,
		"hide 500 deep": `<html><body>` + divs(500, hide("hidefivehundred")) + `</body></html>`,
		"hide 509 deep": `<html><body>` + divs(509, hide("hidefiveohnine")) + `</body></html>`,
	}
	want := map[string]int{
		"blkalpha": 1, "blkbravo": 1, "blkcharlie": 1, "blkdelta": 1, "blkecho": 1, "blkfoxtrot": 1, "blkgolf": 1, "blkhotel": 1,
		"blkalphablkbravo": 0, "blkdeltablkecho": 0, "blkgolfblkhotel": 0,
		"deepline0": 1, "deepline300": 1, "deepline477": 1, "deepline478": 1, "deepline479": 1, "deepline599": 1, "deepline478deepline479": 0,
		"tblsecond": 1, "tblthird": 1, "blocktblthird": 0,
		"hidefoureighty": 1, "hidefivehundred": 1, "hidefiveohnine": 1,
	}
	live, rebuilt := parityArchive(t, bodies, keysOf(want))
	assure.Reached(t, live["blkalpha"]+rebuilt["blkalpha"], "hits for a deep block word")
	for _, p := range parityProblems(want, live, rebuilt) {
		t.Error(p)
	}
}

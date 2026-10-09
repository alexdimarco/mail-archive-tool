package export

import (
	"archive/zip"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// explodingBody is the RV-8 reproduction: <p>, 400 distinct <b id=i>, </p>,
// then n paragraphs. x/net's reconstruct-active-formatting step re-opens all
// 400 b elements inside every later paragraph, so a 16 KB mail (n=2000)
// serializes to about 12 MB, and every re-parse of that output costs
// O(paragraphs x 400^2) Noah's-ark attribute comparisons.
func explodingBody(n int, tail string) string {
	var b strings.Builder
	b.WriteString("<html><body><p>")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "<b id=%d>", i)
	}
	b.WriteString("</p>")
	b.WriteString(strings.Repeat("<p>x</p>", n))
	b.WriteString(tail)
	b.WriteString("</body></html>")
	return b.String()
}

type parseCount struct{ on, off int }

// countReparses wraps the real re-parse (it still runs) and counts the calls
// per scripting mode, for the cost guards below.
func countReparses(t *testing.T) *parseCount {
	t.Helper()
	real := reparse
	t.Cleanup(func() { reparse = real })
	n := &parseCount{}
	reparse = func(doc string, scripting bool) (*xhtml.Node, error) {
		if scripting {
			n.on++
		} else {
			n.off++
		}
		return real(doc, scripting)
	}
	return n
}

// countRenders wraps the exporter's real renderer and counts the calls.
func countRenders(t *testing.T) *int {
	t.Helper()
	real := renderMessage
	t.Cleanup(func() { renderMessage = real })
	n := new(int)
	renderMessage = func(m *model.Message, ctx RenderContext) (RenderResult, error) {
		*n++
		return real(m, ctx)
	}
	return n
}

// covers: MA-471, R19, S21
// The scripting-off re-parse runs only when the serialized output can hold a
// <noscript> start tag (the bytes "<noscript" in any ASCII case): the parser's
// scripting flag changes only how a noscript start tag is handled, so without
// one both readings are the same tree and the second parse is skipped exactly.
// Ordinary mail is re-parsed once; mail whose noscript is only text or an
// attribute value (escaped on output) is too; a <NoScRiPt> in the body or the
// head is still read both ways (the safety half: MA-300's noscript vectors
// stay inert).
func TestRecheckReparsesScriptingOffOnlyForNoscript(t *testing.T) {
	once := map[string]string{
		"ordinary":           `<p>Hello <b>there</b></p><table><tr><td>cell</td></tr></table>`,
		"noscript as text":   `<p>&lt;noscript&gt;not a tag&lt;/noscript&gt;</p>`,
		"noscript attribute": `<p title="<noscript>">attribute only</p>`,
	}
	for name, body := range once {
		n := countReparses(t)
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Fatalf("%s: must render, got fallback %q", name, out.BodyFallback)
		}
		if n.on == 0 || n.off != 0 {
			t.Errorf("%s: want the scripting-on re-parse only (no <noscript> tag can be in the output), got on=%d off=%d", name, n.on, n.off)
		}
	}
	both := map[string]string{
		"body noscript": `<p>x</p><NoScRiPt><img src="data:image/gif;base64,R0lGOD=" alt=""></NoScRiPt>`,
		"head noscript": `<html><head><noscript><style>p{}</style></noscript></head><body><p>x</p></body></html>`,
		// Raw text is written literally, so these bytes reach the output; the
		// byte test is a superset (read twice though the trees agree), never
		// the reverse.
		"bytes in style": `<style>/* <noscript> */ p{color:red}</style><p>styled</p>`,
	}
	for name, body := range both {
		n := countReparses(t)
		if _, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{}); err != nil {
			t.Fatal(err)
		}
		if n.on == 0 || n.off == 0 {
			t.Errorf("%s: a <noscript> in the output must be re-parsed with scripting on AND off, got on=%d off=%d", name, n.on, n.off)
		}
	}
}

// covers: MA-471, R1, R7
// The exporter renders each message ONCE. The page names the zip only beside
// attachments it lists (those not embedded inline), which is exactly when the
// exporter writes the zip, so the link can be offered up front: a message whose
// every attachment is an inline image renders once with no zip link and no zip
// (it used to be rendered twice, the second time only to drop a link that was
// never shown); the twin with one more, ordinary attachment renders once with
// the link and its zip. Counted twice over: by the exporter's render seam, and
// by what one render costs whoever calls it — the sanitizer's re-check and the
// page check, one scripting-on parse each (no noscript tag: none with
// scripting off) — so a second render that bypasses the seam is seen too.
func TestExportRendersEachMessageOnce(t *testing.T) {
	png := []byte("PNGDATA-once")
	inline := `<html><body><p>logo <img src="cid:img1@host"></p></body></html>`

	out := t.TempDir()
	e := incExporter(out, mustManifest(t))
	renders := countRenders(t)
	parses := countReparses(t)
	m := &model.Message{Subject: "all inline", SenderEmail: "a@example.com", InternetMessageID: "<inl@x>",
		Received: testDate, HTMLBody: inline, Attachments: []model.Attachment{inlinePNG(png)}}
	if _, err := e.Export("Store", []string{"Inbox"}, m); err != nil {
		t.Fatal(err)
	}
	page := assure.Reached(t, readOnly(t, out, ".html"), "all-inline page")
	if *renders != 1 {
		t.Errorf("a message whose attachments are all inline must be rendered once, got %d renders", *renders)
	}
	if parses.on != 2 || parses.off != 0 {
		t.Errorf("all-inline: one render is two scripting-on re-parses (re-check, page check), got on=%d off=%d", parses.on, parses.off)
	}
	if !strings.Contains(page, "data:image/png;base64,") || strings.Contains(page, "download zip") || countFiles(t, out, ".zip") != 0 {
		t.Errorf("all-inline: want the image embedded, no zip link and no zip (zips=%d):\n%s", countFiles(t, out, ".zip"), bodyPart(page))
	}

	out = t.TempDir()
	e = incExporter(out, mustManifest(t))
	renders = countRenders(t)
	parses = countReparses(t)
	b := &blob{data: []byte("report bytes")}
	m = &model.Message{Subject: "with attachment", SenderEmail: "a@example.com", InternetMessageID: "<att@x>",
		Received: testDate, HTMLBody: inline, Attachments: []model.Attachment{inlinePNG(png), b.att("report.pdf")}}
	if _, err := e.Export("Store", []string{"Inbox"}, m); err != nil {
		t.Fatal(err)
	}
	page = assure.Reached(t, readOnly(t, out, ".html"), "page with an attachment")
	if *renders != 1 {
		t.Errorf("a message with an ordinary attachment must be rendered once, got %d renders", *renders)
	}
	if parses.on != 2 || parses.off != 0 {
		t.Errorf("with attachment: one render is two scripting-on re-parses (re-check, page check), got on=%d off=%d", parses.on, parses.off)
	}
	if !strings.Contains(page, "report.pdf") || !strings.Contains(page, `">download zip</a>`) || countFiles(t, out, ".zip") != 1 {
		t.Errorf("with attachment: want report.pdf listed with the zip link and one zip (zips=%d)", countFiles(t, out, ".zip"))
	}
}

// covers: MA-472, R1, R7, R19
// Cost guard on the RV-8 body: a mail whose serialization inflates past the
// size cap (max(16 x its input, 1 MiB)) is shown as its escaped source with the
// reason "too-large" -- the page stays about the size of the mail instead of
// megabytes, the inflated output is never re-parsed (the two re-check parses
// were the O(N^2) part), the message is rendered once, and the fallback is
// never silent (counted, reported, recorded) with its inline image in the zip.
func TestExportInflatingMailIsBoundedAndRenderedOnce(t *testing.T) {
	png := []byte("PNGDATA-explode")
	body := explodingBody(2000, `<img src="cid:img1@host">`)
	m := &model.Message{Subject: "explode", SenderEmail: "x@example.com", InternetMessageID: "<explode@x>",
		Received: testDate, HTMLBody: body, Attachments: []model.Attachment{inlinePNG(png)}}

	out := t.TempDir()
	man := mustManifest(t)
	e := incExporter(out, man)
	renders := countRenders(t)
	parses := countReparses(t)
	if _, err := e.Export("Store", []string{"Inbox"}, m); err != nil {
		t.Fatal(err)
	}
	page := assure.Reached(t, readOnly(t, out, ".html"), "explode page")
	if *renders != 1 {
		t.Errorf("want one render per message, got %d", *renders)
	}
	if parses.on+parses.off != 0 {
		t.Errorf("the inflated output must never be re-parsed, got on=%d off=%d", parses.on, parses.off)
	}
	if limit := 2*len(body) + 64<<10; len(page) > limit {
		t.Errorf("page is %d bytes for a %d-byte mail, want at most %d (output must be bounded)", len(page), len(body), limit)
	}
	if !strings.Contains(page, `<pre class="plain" data-mailarchive-fallback="`+FallbackTooLarge+`">`) || !strings.Contains(page, "cid:img1@host") {
		t.Errorf("want the escaped original source, reason %q:\n%s", FallbackTooLarge, bodyPart(page))
	}
	if e.Stats.HTMLFallback != 1 {
		t.Errorf("Stats.HTMLFallback = %d, want 1", e.Stats.HTMLFallback)
	}
	recorded := false
	for _, r := range man.Entries {
		recorded = recorded || r.BodyFallback == FallbackTooLarge
	}
	if !recorded {
		t.Errorf("the manifest record must carry BodyFallback=%q", FallbackTooLarge)
	}
	zr, err := zip.OpenReader(assure.Reached(t, listFiles(t, out, ".zip"), "attachment zips")[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 || zr.File[0].Name != "logo.png" {
		t.Errorf("the inline image must go into the zip when the body falls back")
	}
}

// inflatingPart is explodingBody's inflating markup alone (no document
// around it), for a head <template> as well as a body: x/net re-opens the 400
// formatting elements in each of the n paragraphs inside a template too.
func inflatingPart(n int) string {
	var b strings.Builder
	b.WriteString("<p>")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "<b id=%d>", i)
	}
	b.WriteString("</p>")
	b.WriteString(strings.Repeat("<p>x</p>", n))
	return b.String()
}

// covers: MA-472, R7, R19
// The size cap is ONE budget for the sanitized head and body together, so a
// mail cannot double it by splitting the inflation between them: a head
// <template> and a body that each serialize to about 0.6 MiB — each alone
// under the cap the whole mail gets (its 1 MiB floor), together past it — fall
// back as too-large, never re-parsed. The positive twins: the head half alone
// and the body half alone render as HTML.
func TestSizeCapIsSharedByHeadAndBody(t *testing.T) {
	const n = 100
	head := "<template>" + inflatingPart(n) + "</template>"
	body := inflatingPart(n)
	doc := "<html><head>" + head + "</head><body>" + body + "</body></html>"

	// Precondition, measured on the parsed mail: each half fits the cap the
	// whole mail gets, the two together do not.
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	size := func(name string) int {
		var sb strings.Builder
		for c := findElem(root, func(x *xhtml.Node) bool { return x.Data == name }).FirstChild; c != nil; c = c.NextSibling {
			if err := xhtml.Render(&sb, c); err != nil {
				t.Fatal(err)
			}
		}
		return sb.Len()
	}
	limit, hs, bs := serializedCap(len(doc)), size("head"), size("body")
	if hs >= limit || bs >= limit || hs+bs <= limit {
		t.Fatalf("precondition: want each half under the cap and both past it, got head=%d body=%d cap=%d", hs, bs, limit)
	}

	parses := countReparses(t)
	out, err := RenderWith(&model.Message{Subject: "split inflation", HTMLBody: doc}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if out.BodyFallback != FallbackTooLarge || parses.on+parses.off != 0 {
		t.Errorf("head and body inflating together past the cap must fall back as %q, never re-parsed: got %q (on=%d off=%d)",
			FallbackTooLarge, out.BodyFallback, parses.on, parses.off)
	}
	if len(out.HTML) > 2*len(doc)+64<<10 {
		t.Errorf("the fallback page is %d bytes for a %d-byte mail (output must be bounded)", len(out.HTML), len(doc))
	}

	for name, half := range map[string]string{
		"head half": "<html><head>" + head + "</head><body><p>body words</p></body></html>",
		"body half": "<html><body>" + body + "</body></html>",
	} {
		twin, err := RenderWith(&model.Message{Subject: name, HTMLBody: half}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if twin.BodyFallback != "" {
			t.Errorf("positive twin %s: one half under the cap must render as HTML, got %q", name, twin.BodyFallback)
		}
	}
}

// covers: MA-472, R7
// No ordinary mail trips the size cap: serialization of real-world shapes --
// entity-heavy text and attributes (x/net writes " and ' as 5-byte
// references), unclosed <p>/<li>/<td>, implied tbody, Word-style unclosed
// <font> spanning many paragraphs (reconstructed in each), attribute-only
// flags, a large inline data: image -- stays far below 16 x the input, and each
// renders as HTML.
func TestOrdinaryMailNeverTooLarge(t *testing.T) {
	img := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 3<<20))
	corpus := map[string]string{
		"quotes and ampersands": "<p>" + strings.Repeat(`"'&`, 50000) + "</p>",
		"quoted attributes":     strings.Repeat(`<span title="&quot;&quot;&quot;'''">q</span>`, 5000),
		"unclosed paragraphs":   strings.Repeat("<p>para", 5000),
		"unclosed list items":   "<ul>" + strings.Repeat("<li>item", 5000) + "</ul>",
		"implied tbody":         strings.Repeat("<table><td>c<td>d</table>", 3000),
		"word unclosed fonts":   `<font face="Calibri" size="2"><span style="color:#1f497d">` + strings.Repeat("<p class=MsoNormal>text of a paragraph</p>", 5000),
		"attribute flags":       strings.Repeat("<input type=checkbox checked disabled readonly>", 5000),
		"large inline image":    `<p>see <img src="` + img + `" alt="chart"></p>`,
		"newsletter":            strings.Repeat(`<table width="100%" cellpadding=0 cellspacing=0 border=0><tr><td align=center><a href="https://example.com/a?b=1&c=2"><img src="data:image/gif;base64,R0lGOD=" width=1 height=1></a></td></tr></table>`, 2000),
	}
	worst, worstName := 0.0, ""
	for name, body := range corpus {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("%s: ordinary mail must render as HTML, got fallback %q", name, out.BodyFallback)
			continue
		}
		if r := float64(len(out.HTML)) / float64(len(body)); r > worst {
			worst, worstName = r, name
		}
	}
	t.Logf("largest page/input ratio in the ordinary corpus: %.2f (%s); the cap is 16x", worst, worstName)
	if worst <= 0 || worst > 8 {
		t.Errorf("ordinary mail must stay well below the 16x cap, worst ratio %.2f (%s)", worst, worstName)
	}
}

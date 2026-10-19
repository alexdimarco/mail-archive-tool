package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

func nestedHTML(depth int, inner string) string {
	return strings.Repeat("<div>", depth) + inner + strings.Repeat("</div>", depth)
}

func nestedTables(depth int, inner string) string {
	return strings.Repeat("<table><tr><td>", depth) + inner + strings.Repeat("</td></tr></table>", depth)
}

// reopenedBold is a mail the flattener's model reads as shallow but the tree
// builder does not: 40 distinct <b> left open in a closed div are re-opened
// (reconstruct-active-formatting) inside the text of the next one, nested
// `divs` deep. With 470 the mail parses at exactly x/net's 512 cap — so the
// sanitizer's own parse and its re-check accept it — and the page, one level
// deeper (div.mailarchive-body), is past it.
func reopenedBold(divs int, words string) string {
	var b strings.Builder
	b.WriteString("<div>")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "<b id=r%d>", i)
	}
	b.WriteString("</div>")
	b.WriteString(nestedHTML(divs, words))
	return b.String()
}

func inlinePNG(payload []byte) model.Attachment {
	return model.Attachment{
		Filename:  "logo.png",
		MimeType:  "image/png",
		ContentID: "<img1@host>",
		WriteTo:   func(w io.Writer) (int64, error) { n, err := w.Write(payload); return int64(n), err },
	}
}

// pageTrees parses an exported page as a browser and `reindex -rebuild` will
// — x/net's default parser, scripting on and off — failing the test if either
// reading refuses it (a page too deep for the tree parser cannot be read back).
func pageTrees(t *testing.T, what, page string) []*xhtml.Node {
	t.Helper()
	var roots []*xhtml.Node
	for _, scripting := range []bool{true, false} {
		root, err := xhtml.ParseWithOptions(strings.NewReader(page), xhtml.ParseOptionEnableScripting(scripting))
		if err != nil {
			t.Fatalf("%s: the exported page does not parse (scripting=%v): %v", what, scripting, err)
		}
		roots = append(roots, root)
	}
	return roots
}

func treeDepth(n *xhtml.Node) int {
	d := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if x := treeDepth(c); x > d {
			d = x
		}
	}
	if n.Type == xhtml.ElementNode {
		d++
	}
	return d
}

func findElem(n *xhtml.Node, match func(*xhtml.Node) bool) *xhtml.Node {
	if n.Type == xhtml.ElementNode && match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findElem(c, match); f != nil {
			return f
		}
	}
	return nil
}

// countElems counts the elements named name below n (n itself excluded).
func countElems(n *xhtml.Node, name string) int {
	k := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == name {
			k++
		}
		k += countElems(c, name)
	}
	return k
}

func attrOf(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// mailBody is the renderer's div.mailarchive-body in a parsed page.
func mailBody(t *testing.T, root *xhtml.Node) *xhtml.Node {
	t.Helper()
	b := findElem(root, func(n *xhtml.Node) bool { return n.Data == "div" && attrOf(n, "class") == "mailarchive-body" })
	if b == nil {
		t.Fatal("no div.mailarchive-body in the page")
	}
	return b
}

// liveText reports whether words appear in a text node of the rendered body
// that is markup, not the shown-as-source fallback <pre>.
func liveText(body *xhtml.Node, words string) bool {
	var walk func(n *xhtml.Node, inFallback bool) bool
	walk = func(n *xhtml.Node, inFallback bool) bool {
		if n.Type == xhtml.ElementNode && attrOf(n, "data-mailarchive-fallback") != "" {
			inFallback = true
		}
		if n.Type == xhtml.TextNode && !inFallback && strings.Contains(n.Data, words) {
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c, inFallback) {
				return true
			}
		}
		return false
	}
	return walk(body, false)
}

// covers: MA-473, R7, R13, R19
// Mail nested deeper than the parser's 512-element cap (browsers render it:
// Chromium keeps attaching content past its own cap) renders as HTML: past
// flattenDepth its container tags are dropped while its text, void elements
// (the inline image) and the nesting above the cut are kept — 150 nested
// tables, 600- and 1000-deep divs, a 600-deep SVG. Each page parses with
// x/net's default cap in both scripting readings (no tool ever needs the
// tokenizer to read it back), the words are live text and the cid: image is
// embedded. The positive twin: ordinary mail, and mail 470 deep, passes the
// flattener as the very same string and keeps every level.
func TestRenderDeepMailRendersFlattened(t *testing.T) {
	img := `<img src="cid:img1@host">`
	cases := []struct {
		name, body, words, elem string
		minKept                 int
		image                   bool
	}{
		{"150 nested tables", nestedTables(150, "<p>deep table words "+img+"</p>"), "deep table words", "table", 115, true},
		{"600-deep divs", nestedHTML(600, "<p>deep div words "+img+"</p>"), "deep div words", "div", 470, true},
		{"1000-deep divs", nestedHTML(1000, "<p>very deep words "+img+"</p>"), "very deep words", "div", 470, true},
		{"600-deep svg", "<svg>" + strings.Repeat("<g>", 600) + "<text>deep svg words</text>" + strings.Repeat("</g>", 600) + "</svg>", "deep svg words", "g", 470, false},
	}
	for _, c := range cases {
		m := &model.Message{Subject: c.name, HTMLBody: "<html><body>" + c.body + "</body></html>",
			Attachments: []model.Attachment{inlinePNG([]byte("PNGDATA-flat"))}}
		out, err := RenderWith(m, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("%s: must render as HTML, got fallback %q", c.name, out.BodyFallback)
			continue
		}
		for i, root := range pageTrees(t, c.name, string(out.HTML)) {
			body := mailBody(t, root)
			if !liveText(body, c.words) {
				t.Errorf("%s (reading %d): the words %q must be live text in the body", c.name, i, c.words)
			}
			if d := treeDepth(root); d > 512 {
				t.Errorf("%s: page depth %d is past the parser's cap", c.name, d)
			}
			if k := countElems(body, c.elem); k < c.minKept {
				t.Errorf("%s: only %d <%s> kept, want at least %d (nesting above the cut is kept)", c.name, k, c.elem, c.minKept)
			}
			if c.image && findElem(body, func(n *xhtml.Node) bool {
				return n.Data == "img" && strings.HasPrefix(attrOf(n, "src"), "data:image/png;base64,")
			}) == nil {
				t.Errorf("%s: the inline cid: image must be embedded as a live <img>", c.name)
			}
		}
		if c.image && !out.Consumed[0] {
			t.Errorf("%s: the inline image must be consumed (embedded), not sent to the zip", c.name)
		}
	}

	ordinary := []string{
		nestedHTML(470, "<p>470 deep</p>"),
		nestedTables(100, "<p>100 tables</p>"),
		`<html><head><style>p{color:red}</style></head><body><p>Hi<br>there</p><ul><li>a<li>b</ul></body></html>`,
		strings.Repeat("<p>unclosed paragraph", 2000),
		"<table>" + strings.Repeat("<tr><td>a<td>b", 2000) + "</table>",
		`<svg width="10" height="10"><style>.a{fill:red}</style><rect class="a"/></svg><math><mi>x</mi></math>`,
		strings.Repeat("<p><font face=Arial>Word-style unclosed font", 2000),
		"<noscript>" + nestedHTML(300, "fallback") + "</noscript><p>after</p>",
	}
	for i, doc := range ordinary {
		if got := flattenDeep(doc); got != doc {
			t.Errorf("positive twin %d: mail that never passes flattenDepth must come back unchanged", i)
		}
	}
	out, err := RenderWith(&model.Message{Subject: "470", HTMLBody: nestedHTML(470, "<p>470 deep</p>")}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if out.BodyFallback != "" {
		t.Fatalf("positive twin: a 470-deep mail must render, got %q", out.BodyFallback)
	}
	for _, root := range pageTrees(t, "470 deep", string(out.HTML)) {
		if k := countElems(mailBody(t, root), "div"); k != 470 {
			t.Errorf("positive twin: a 470-deep mail keeps all 470 levels, got %d", k)
		}
	}
}

// covers: MA-473, R7
// The flattener keeps the mail's content, only its deep container tags go:
// with a small limit, the text (CRLF, NUL and entity references included, as
// written), comments, void elements and raw-text elements (style, script,
// textarea and their contents, markup-looking text included) all come through
// in order, an unterminated tag at the end is kept as it was, and the result
// parses no deeper than the limit (plus html, body and a leaf). The only bytes
// it adds are the word boundaries of dropped block-level tags (MA-477): one
// newline for the run of <div>s dropped before "deep" and one for their end
// tags after "text", none for the dropped inline <span>, <b> and <i>.
func TestFlattenKeepsTextAndLeaves(t *testing.T) {
	doc := "<div><section><div><span>one\r\ntwo &amp; \x00 three<!-- c1 --><img src=a.png>" +
		"<div><div><b><i>deep <br> text</i></b></div></div><style>p > b { color: red } </div></style>" +
		"<script>if (a < b) { x = '</div>' }</script><textarea><div>typed</div></textarea>" +
		"<input type=checkbox></span></div></section></div><p>tail &lt;not a tag&gt;</p><div class=\"unterminated"
	const limit = 3
	flat := flattenAt(doc, limit)
	if flat == doc {
		t.Fatal("the test mail must be deeper than the limit")
	}
	type sig struct{ text, comments, leaves string }
	read := func(s string) sig {
		var g sig
		z := xhtml.NewTokenizer(strings.NewReader(s))
		for {
			tt := z.Next()
			if tt == xhtml.ErrorToken {
				return g
			}
			raw := string(z.Raw())
			switch tt {
			case xhtml.TextToken:
				g.text += raw
			case xhtml.CommentToken:
				g.comments += raw
			case xhtml.StartTagToken, xhtml.SelfClosingTagToken, xhtml.EndTagToken:
				name, _ := z.TagName()
				switch string(name) {
				case "img", "br", "input", "style", "script", "textarea":
					g.leaves += raw
				}
			}
		}
	}
	in, got := read(doc), read(flat)
	want := strings.Replace(strings.Replace(in.text, "three", "three\n", 1), " text", " text\n", 1)
	if got.text != want {
		t.Errorf("text changed (beyond the two block boundaries):\n  in: %q\nwant: %q\n out: %q", in.text, want, got.text)
	}
	if got.comments != in.comments || got.leaves != in.leaves {
		t.Errorf("comments or leaf elements changed:\n in: %q %q\nout: %q %q", in.comments, in.leaves, got.comments, got.leaves)
	}
	if !strings.HasSuffix(flat, `<div class="unterminated`) {
		t.Errorf("the unterminated tag at the end must be kept as written: %q", flat[max(0, len(flat)-40):])
	}
	root, err := xhtml.Parse(strings.NewReader(flat))
	if err != nil {
		t.Fatal(err)
	}
	if d := treeDepth(root); d > 2+limit+1 {
		t.Errorf("flattened at %d, the tree is %d deep (want at most %d)", limit, d, 2+limit+1)
	}
	if !bytes.Contains([]byte(flat), []byte("<div><section><div>")) {
		t.Errorf("the nesting above the limit must be kept: %q", flat[:60])
	}
}

// covers: MA-474, R13, R7, R19
// A page the exporter writes is never too deep for the tree parser (INT-1(b)):
// the assembled page — html > body > div.mailarchive-body around the body — is
// itself parsed, in both scripting readings, before it is accepted. The mail
// that proves it is one the flattener's model reads as shallow (reopenedBold:
// the tree builder re-opens 40 formatting elements it cannot see) and that
// parses at exactly the 512 cap, so the sanitizer's own parse and re-check
// accept it; only the page is too deep, and it falls back as
// too-deeply-nested with a page that parses. Wrapped in a <noscript> (469
// deep) the page is too deep only to a reader with JavaScript off, and falls
// back too: the page check reads the scripting-off page as well. Across shapes
// around the cap (divs, tables, noscript, head template, the reopened case
// bare and in a noscript), every page written — rendered or fallen back —
// parses. The twin, 460 divs deep, renders.
func TestRenderNeverWritesAPageTooDeepToParse(t *testing.T) {
	doc := reopenedBold(470, "reopened words")
	if _, err := xhtml.Parse(strings.NewReader(flattenDeep(doc))); err != nil {
		t.Fatalf("precondition: the mail itself must parse (only the page is too deep): %v", err)
	}
	out, err := RenderWith(&model.Message{Subject: "reopened", HTMLBody: doc}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	page := string(out.HTML)
	pageTrees(t, "reopened 470", page)
	if out.BodyFallback != FallbackTooDeep || !strings.Contains(page, `data-mailarchive-fallback="`+FallbackTooDeep+`"`) || !strings.Contains(page, "reopened words") {
		t.Errorf("a page too deep to parse must fall back as %q with the words shown, got %q", FallbackTooDeep, out.BodyFallback)
	}

	// The page is parsed in EVERY scripting reading: inside a <noscript> the
	// same kind of mail (469 deep) is one text node to the scripting-on reading
	// of the page and one level past the cap to a reader with JavaScript off.
	// The sanitizer's own parse and its re-check accept it (the re-check's doc
	// has no div.mailarchive-body), so only the page check's scripting-off
	// parse makes it fall back.
	ns := "<p>shown words</p><noscript>" + reopenedBold(469, "noscript words") + "</noscript>"
	if _, _, _, f := sanitizeHTML(ns); f != "" {
		t.Fatalf("precondition: the sanitizer must accept the noscript mail (only its page is too deep), got %q", f)
	}
	out, err = RenderWith(&model.Message{Subject: "noscript reopened", HTMLBody: ns}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	pageTrees(t, "noscript reopened 469", string(out.HTML))
	if out.BodyFallback != FallbackTooDeep || !strings.Contains(string(out.HTML), "shown words") {
		t.Errorf("a page too deep only with scripting off must fall back as %q with the words shown, got %q", FallbackTooDeep, out.BodyFallback)
	}

	twin, err := RenderWith(&model.Message{Subject: "reopened 460", HTMLBody: reopenedBold(460, "twin words")}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if twin.BodyFallback != "" {
		t.Errorf("positive twin: 460 deep must render, got %q", twin.BodyFallback)
	}
	for _, root := range pageTrees(t, "reopened 460", string(twin.HTML)) {
		if !liveText(mailBody(t, root), "twin words") {
			t.Errorf("positive twin: the words must be live text")
		}
	}

	shapes := map[string]string{}
	for _, n := range []int{478, 479, 480, 481, 505, 509, 510, 511, 512, 600} {
		shapes[fmt.Sprintf("divs %d", n)] = nestedHTML(n, "w")
	}
	for _, n := range []int{119, 120, 121, 127, 128} {
		shapes[fmt.Sprintf("tables %d", n)] = nestedTables(n, "w")
	}
	for _, n := range []int{440, 460, 465, 470, 480} {
		shapes[fmt.Sprintf("reopened %d", n)] = reopenedBold(n, "w")
	}
	shapes["noscript 600"] = "<p>x</p><noscript>" + nestedHTML(600, "y") + "</noscript>"
	for _, n := range []int{466, 468, 469, 470} {
		shapes[fmt.Sprintf("noscript reopened %d", n)] = "<p>x</p><noscript>" + reopenedBold(n, "y") + "</noscript>"
	}
	shapes["head template 700"] = "<html><head><template>" + nestedHTML(700, "t") + "</template></head><body><p>b</p></body></html>"
	shapes["table text 600"] = "<table>" + nestedHTML(600, "fostered") + "</table>"
	for name, body := range shapes {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		pageTrees(t, name, string(out.HTML))
	}
}

// covers: MA-303, R1, R7, R19
// A mail whose HTML cannot be shown as HTML is shown as its escaped ORIGINAL
// source — never silently: the run counts it (Stats.HTMLFallback), the
// verification issues name it (html-shown-as-source, with the reason), and the
// manifest record carries the reason, so the report regenerated from the
// manifest keeps saying so. Its inline image is not lost: the source shows the
// cid: reference (not a base64 blob), and the image goes into the attachment
// zip. Driven by a fallback that still happens without fault injection: mail
// too deep for the parser even after flattening (reopenedBold, whose 40
// formatting elements the tree builder re-opens out of the flattener's sight;
// too-deeply-nested). The positive twins, the same kind of mail nested 10 and
// 600 deep, render as HTML with the image embedded, nothing counted and no zip
// (before the flattener, MA-473, the 600-deep mail was this test's fallback).
func TestExportHTMLFallbackIsVisibleAndKeepsImages(t *testing.T) {
	payload := []byte("PNGDATA-deep")
	mk := func(html, subject string) *model.Message {
		return &model.Message{
			Subject: subject, SenderEmail: "a@example.com", InternetMessageID: "<" + subject + "@x>",
			Received:    testDate,
			HTMLBody:    "<html><body>" + html + "</body></html>",
			Attachments: []model.Attachment{inlinePNG(payload)},
		}
	}
	words := func(subject string) string { return `<p>deep words ` + subject + ` <img src="cid:img1@host"></p>` }

	// Positive twins first: they render as HTML, image inline, no zip.
	for _, twin := range []struct {
		depth   int
		subject string
	}{{10, "shallowmsg"}, {600, "deepmsg"}} {
		out := t.TempDir()
		e := incExporter(out, mustManifest(t))
		if _, err := e.Export("Store", []string{"Inbox"}, mk(nestedHTML(twin.depth, words(twin.subject)), twin.subject)); err != nil {
			t.Fatal(err)
		}
		page := readOnly(t, out, ".html")
		if strings.Contains(page, "data-mailarchive-fallback") || !strings.Contains(page, "data:image/png;base64,") {
			t.Fatalf("positive twin %s: must render as HTML with the image embedded:\n%s", twin.subject, bodyPart(page))
		}
		if e.Stats.HTMLFallback != 0 || countFiles(t, out, ".zip") != 0 {
			t.Fatalf("positive twin %s: no fallback and no zip expected (fallback=%d zips=%d)", twin.subject, e.Stats.HTMLFallback, countFiles(t, out, ".zip"))
		}
	}

	out := t.TempDir()
	man := mustManifest(t)
	e := incExporter(out, man)
	if _, err := e.Export("Store", []string{"Inbox"}, mk(reopenedBold(470, words("fallbackmsg")), "fallbackmsg")); err != nil {
		t.Fatal(err)
	}
	page := assure.Reached(t, readOnly(t, out, ".html"), "fallback page")
	if !strings.Contains(page, `<pre class="plain" data-mailarchive-fallback="`+FallbackTooDeep+`">`) {
		t.Errorf("the mail must be shown as source with reason %q:\n%s", FallbackTooDeep, bodyPart(page))
	}
	if !strings.Contains(page, "cid:img1@host") || strings.Contains(page, "base64,") {
		t.Errorf("the fallback must show the original source (cid: reference), not an embedded blob:\n%s", bodyPart(page))
	}
	if !strings.Contains(page, "deep words fallbackmsg") {
		t.Errorf("the mail's words must still be on the page")
	}
	if e.Stats.HTMLFallback != 1 {
		t.Errorf("Stats.HTMLFallback = %d, want 1 (the fallback must be counted)", e.Stats.HTMLFallback)
	}
	var issue *Issue
	for i := range e.Issues {
		if e.Issues[i].Kind == "html-shown-as-source" {
			issue = &e.Issues[i]
		}
	}
	if issue == nil || issue.Detail != FallbackTooDeep {
		t.Errorf("expected an html-shown-as-source issue with detail %q, got %+v", FallbackTooDeep, e.Issues)
	}
	found := false
	for _, r := range man.Entries {
		if r.BodyFallback == FallbackTooDeep && r.HasIssues() && r.Subject == "fallbackmsg" {
			found = true
		}
	}
	if !found {
		t.Errorf("the manifest record must carry BodyFallback=%q (and the subject, so the report can name it): %+v", FallbackTooDeep, man.Entries)
	}
	zips := assure.Reached(t, listFiles(t, out, ".zip"), "attachment zips")
	zr, err := zip.OpenReader(zips[0])
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if len(names) != 1 || names[0] != "logo.png" {
		t.Errorf("the inline image must be archived in the zip when the body falls back, got %v", names)
	}
}

func listFiles(t *testing.T, root, suffix string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, suffix) && !strings.Contains(p, string(filepath.Separator)+".") && filepath.Base(p) != "index.html" {
			out = append(out, p)
		}
		return nil
	})
	return out
}

func countFiles(t *testing.T, root, suffix string) int { return len(listFiles(t, root, suffix)) }

func readOnly(t *testing.T, root, suffix string) string {
	t.Helper()
	files := listFiles(t, root, suffix)
	if len(files) != 1 {
		t.Fatalf("expected exactly one %s under %s, got %v", suffix, root, files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func clipped(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

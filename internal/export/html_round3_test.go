package export

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// classCount counts, in a parsed page, the elements (whatever their name or
// namespace) wearing the class token tok. Written apart from the product's own
// check so the test does not grade itself.
func classCount(root *xhtml.Node, tok string) int {
	n := 0
	var walk func(x *xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.ElementNode {
			for _, f := range strings.Fields(attrOf(x, "class")) {
				if f == tok {
					n++
				}
			}
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return n
}

// fieldMarkers counts the elements carrying a data-mailarchive-field attribute.
func fieldMarkers(root *xhtml.Node) int {
	n := 0
	var walk func(x *xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.ElementNode {
			for _, a := range x.Attr {
				if strings.EqualFold(a.Key, "data-mailarchive-field") {
					n++
				}
			}
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return n
}

// covers: MA-300, MA-474, R19, S21
// R3-AO1. A stray `</div>` inside a body <noscript> is raw text to the
// scripting-on parse the sanitizer works on, and to the re-check, which parses
// the output in a bare <body> without the page's own wrapper div. A reader with
// JavaScript off reads the noscript as markup, and there the `</div>` pops the
// noscript AND the page's div.mailarchive-body, which the check never had: the
// following `</noscript>` no longer closes an <svg> (or <math>, or a textarea
// in it) opened inside the noscript, a later <plaintext> or <xmp> becomes a
// foreign element and swallows nothing, and what the mail hid there — a
// `<meta http-equiv=refresh>`, a `<link rel=preconnect>`, a second charset, a
// forged tool-styled header — comes out live. The page the exporter writes must
// be read in every reading: with scripting on AND off, no hostile meta, no live
// resource hint, the page's own charset the only one, exactly one header div,
// one body div and the same data-mailarchive-field markers as the same message
// with a plain body.
func TestRenderStrayDivInNoscriptCannotMakeBreakoutLive(t *testing.T) {
	const (
		refresh = `<meta http-equiv=refresh content='0;url=http://e.test/r'>`
		hint    = `<link rel=preconnect href=http://e.test>`
		charset = `<meta charset=windows-1252>`
		header  = `<i></i><div class=mailarchive-header data-mailarchive-field=from>`
	)
	cases := map[string]string{
		"svg plaintext":   `<p>x</p><noscript></div><svg></noscript><plaintext>` + refresh,
		"math plaintext":  `<p>x</p><noscript></div><math></noscript><plaintext>` + refresh,
		"svg textarea":    `<p>x</p><noscript></div><svg><textarea></noscript><plaintext>` + refresh,
		"svg xmp":         `<p>x</p><noscript></div><svg></noscript><xmp>` + refresh,
		"three divs":      `<p>x</p><noscript></div></div></div><svg></noscript><plaintext>` + refresh,
		"esp document":    `<html><head><title>t</title></head><body><p>x</p><noscript></div><svg></noscript><plaintext>` + refresh + `</body></html>`,
		"resource hint":   `<p>x</p><noscript></div><svg></noscript><plaintext>` + hint,
		"second charset":  `<p>x</p><noscript></div><svg></noscript><plaintext>` + charset,
		"forged header":   `<p>x</p><noscript></div><svg></noscript><plaintext>` + header,
		"forged and meta": `<p>x</p><noscript></div><svg></noscript><plaintext>` + header + refresh,
	}
	control, err := RenderWith(&model.Message{Subject: "stray div", HTMLBody: `<p>x</p>`}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	ctlRoots := pageTrees(t, "control", string(control.HTML))

	for name, body := range cases {
		out, err := RenderWith(&model.Message{Subject: "stray div", HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		page := assure.Reached(t, string(out.HTML), name+" exported page")
		if live := liveHostileMetas(t, page); len(live) > 0 {
			t.Errorf("%s: a live hostile meta (fallback %q): %v", name, out.BodyFallback, live)
		}
		if hints := liveResourceHints(t, page); len(hints) > 0 {
			t.Errorf("%s: a live resource hint (fallback %q): %v", name, out.BodyFallback, hints)
		}
		if n := liveCharsetMetas(t, page); n != [2]int{1, 1} {
			t.Errorf("%s: the page's own charset must be the only one, got %v (scripting on, off)", name, n)
		}
		for i, root := range pageTrees(t, name, page) {
			if h, b := classCount(root, "mailarchive-header"), classCount(root, "mailarchive-body"); h != 1 || b != 1 {
				t.Errorf("%s (reading %d): want exactly one header div and one body div, got %d and %d", name, i, h, b)
			}
			if got, want := fieldMarkers(root), fieldMarkers(ctlRoots[i]); got != want {
				t.Errorf("%s (reading %d): %d data-mailarchive-field markers, want the control's %d", name, i, got, want)
			}
			if header, body := pageLayout(root); header == nil || body == nil {
				t.Errorf("%s (reading %d): the header and body div must be live directly in <body>", name, i)
			}
		}
	}

	// Positive twins: a stray </div> that stays inside its own noscript is not
	// hostile, and an ordinary noscript fallback still renders as HTML.
	for name, body := range map[string]string{
		"noscript pixel":  `<p>x</p><noscript><img src="data:image/gif;base64,R0lGODlhAQABAAAAACw="></noscript><p>tail twin</p>`,
		"noscript markup": `<p>x</p><noscript><div><p>Please enable JavaScript</p></div></noscript><p>tail twin</p>`,
	} {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("positive twin %s: must render as HTML, got fallback %q", name, out.BodyFallback)
		}
	}
}

// shownWords reports whether words are on the page as the mail showed them: in
// a text node inside the body div that is neither script nor style data (a
// <plaintext>'s text is shown), or, for a body shown as source, in the text of
// the fallback <pre>.
func shownWords(body *xhtml.Node, fellBack bool, words string) bool {
	if fellBack {
		pre := findElem(body, func(n *xhtml.Node) bool { return n.Data == "pre" && attrOf(n, "data-mailarchive-fallback") != "" })
		return pre != nil && strings.Contains(textOf(pre), words)
	}
	found := false
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && n.Namespace == "" && (n.Data == "script" || n.Data == "style") {
			return
		}
		if n.Type == xhtml.TextNode && strings.Contains(n.Data, words) {
			found = true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(body)
	return found
}

// onlyWhitespaceAfter reports whether nothing but whitespace text follows n in
// its parent.
func onlyWhitespaceAfter(n *xhtml.Node) bool {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type != xhtml.TextNode || strings.TrimSpace(s.Data) != "" {
			return false
		}
	}
	return true
}

// covers: MA-303, R1, R7
// R3-AF3, R3-AO3, R3-AC4. A mail whose HTML document is a <frameset> has no
// <body> in the tree (the parser replaces it with the frameset), and the
// renderer used to write only the body's children: an empty body div, no
// fallback, no issue and no Verification line, so the mail's words — its
// <noframes> alternate text is the only readable part — were silently lost from
// the page and from the rebuilt index. A mail with no body to show is shown as
// its escaped source, counted and recorded like any other fallback, and its
// words are on the page. Positive twins: a frameset that follows body content
// (the parser ignores it) and an ordinary document render as HTML.
func TestRenderFramesetMailIsShownAsSource(t *testing.T) {
	bodies := map[string]string{
		"frameset document": `<html><head><title>t</title></head><frameset cols="*"><frame src="http://x.example/"><noframes>framesetword alternate text</noframes></frameset></html>`,
		"bare frameset":     `<frameset rows="50%,50%"><frame src=x><noframes><p>f2beta words</p></noframes></frameset>`,
	}
	for name, body := range bodies {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		page := assure.Reached(t, string(out.HTML), name+" exported page")
		if out.BodyFallback == "" {
			t.Errorf("%s: a mail with no body must be shown as source, not as an empty page:\n%s", name, bodyPart(page))
		}
		for i, root := range pageTrees(t, name, page) {
			if _, b := pageLayout(root); b == nil || !shownWords(b, out.BodyFallback != "", "alternate text") && !shownWords(b, out.BodyFallback != "", "f2beta words") {
				t.Errorf("%s (reading %d): the mail's words must be on the page:\n%s", name, i, bodyPart(page))
			}
		}
	}
	for name, body := range map[string]string{
		"frameset after content": `<p>twin words</p><frameset><frame src=x></frameset>`,
		"ordinary document":      `<html><head><title>t</title></head><body><p>twin words</p></body></html>`,
		"empty body":             `<html><head></head><body></body></html>`,
	} {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("positive twin %s: must render as HTML, got fallback %q", name, out.BodyFallback)
		}
	}
}

// covers: MA-474, MA-492, R8, R13, R7
// R3-AC2, R3-AF4, R3-AC3. The sanitizer's serialization is not a fixed point
// for some malformed markup, and the page is where that shows. Nested li or
// button around a table with foster-parented text and a stray `</div>`
// (`<li><table><div><li></div> tail`): the tree serializes to markup whose
// re-parse inside the page lets the mail's own `</div>` close the tool's
// div.mailarchive-body early, so the rest of the mail lands after it as
// siblings in <body> — shown, but outside the container `reindex -rebuild`
// reads, so those words left the rebuilt index (live=1 rebuilt=0). A script left
// in an escape state beside a table that foster-parenting reorders
// (`<table><tr><td> w <thead> w <menuitem> w <script><!-- w <script><!--`)
// swallows the table in the page's reading and hides its first cell. A page is
// written as HTML only if, in the scripting-on reading the index reads, its
// body div holds exactly what was sanitized (a page may swallow its own closing
// tags in a <plaintext> or a script escape at the end of the mail, which is the
// mail's own and is read back without them) and, in every reading, nothing
// follows the body div; otherwise the mail is shown as source. Each word the
// mail shows is on the page, in the body div, in both readings.
func TestRenderKeepsTheWholeMailInsideItsBodyDiv(t *testing.T) {
	cases := []struct {
		name, body string
		words      []string
	}{
		{"li table div li", `<li><table><div><li></div> escapeword visible tail`, []string{"escapeword"}},
		{"button table div button", `<button><table><div><button></div> endmarker`, []string{"endmarker"}},
		{"f3 li colgroup", `<li> f3zero <table> f3one <div> f3two <li> f3three <colgroup> f3four`, []string{"f3zero", "f3one", "f3two", "f3three", "f3four"}},
		{"f4 button table", `<button> f4zero <table/> f4one <div> f4two <button> f4three </div> f4four`, []string{"f4zero", "f4one", "f4two", "f4three", "f4four"}},
		{"f1 script escape table", `<table><tr><td> f1zero <thead> f1one <menuitem> f1two <script><!-- f1three <script><!-- f1four </div>`, []string{"f1zero", "f1one", "f1two"}},
	}
	for _, c := range cases {
		out, err := RenderWith(&model.Message{Subject: c.name, HTMLBody: c.body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		page := assure.Reached(t, string(out.HTML), c.name+" exported page")
		for i, root := range pageTrees(t, c.name, page) {
			header, body := pageLayout(root)
			if header == nil || body == nil {
				t.Errorf("%s (reading %d): the header and body div must be live directly in <body>", c.name, i)
				continue
			}
			if !onlyWhitespaceAfter(body) {
				t.Errorf("%s (reading %d, fallback %q): content follows div.mailarchive-body in <body>; the rebuild never reads it:\n%s", c.name, i, out.BodyFallback, bodyPart(page))
			}
			for _, w := range c.words {
				if !shownWords(body, out.BodyFallback != "", w) {
					t.Errorf("%s (reading %d, fallback %q): %q must be shown in the body div:\n%s", c.name, i, out.BodyFallback, w, bodyPart(page))
				}
			}
		}
	}

	// Positive twins render as HTML: ordinary markup, and a <plaintext> or a
	// script escape at the very end of the mail, which swallows only the page's
	// own closing tags.
	for name, body := range map[string]string{
		"table cells":     `<table><tr><td>one</td><td>two</td></tr></table><p>after</p>`,
		"nested lists":    `<ul><li>a<ul><li>b</li></ul></li><li>c</li></ul>`,
		"button and form": `<form><button type=submit>go</button></form><p>x</p>`,
		"pre and textarea": `<pre>
  keep   space</pre><textarea>t</textarea>`,
		"plaintext at end": `<p>before</p><plaintext>rest words`,
		"script escape":    `<p>before words</p><script><!--<script>`,
		"unclosed inline":  `<p><b>bold <i>italic</p><p>next`,
	} {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("positive twin %s: must render as HTML, got fallback %q", name, out.BodyFallback)
		}
	}
}

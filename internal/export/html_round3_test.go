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

// covers: MA-473, MA-492, R7, R13
// R3-AC5 and san-Q2 (2). The flattener drops the container tags of mail nested
// past flattenDepth and keeps their text, which is exact for a container that
// only groups content. It is not for one whose job is to hide or to scope what
// is inside it: a <template> (its content is inert, dropped it is shown), an
// SVG or MathML root (the <style>, <script> and <textarea> inside are foreign
// elements, dropped they are HTML raw text or visible text, and a CDATA section
// becomes a bogus comment), an SVG <style> or <script> (not rendered, indexed
// by no one), and a <noscript> (raw text to the scripting-on reading, markup to
// the scripting-off reading: dropped, an <xmp> or <template> straddling its raw
// text's end reads differently). Such a start tag that would be dropped makes
// the mail too deep to flatten: it is shown as source, `too-deeply-nested`, as
// it was before the flattener existed, and its words are the same on both
// indexes. The positive twins still flatten: nested tables, divs, an SVG whose
// plain groups nest past the cap, and the same elements below the cap.
func TestFlattenerRefusesWhatItCouldNotDropWithoutChangingTheMail(t *testing.T) {
	divs := func(n int, inner string) string {
		return strings.Repeat("<div>", n) + inner + strings.Repeat("</div>", n)
	}
	refused := map[string]string{
		"template":        divs(485, `<p>shown f5visible</p><template><p>f5secret words</p></template>`),
		"svg style":       strings.Repeat("<svg><g>", 505) + `<style> f6hidden </style>` + strings.Repeat("</g></svg>", 505),
		"noscript":        strings.Repeat("<noscript>", 481) + `</noscript> f7shown ` + strings.Repeat("</noscript>", 481),
		"noscript in div": divs(500, `<noscript><meta charset=utf-8><xmp></noscript><p>x</p></xmp></noscript>`) + `<p>residxmpafter</p>`,
		"svg style kept":  `<svg>` + strings.Repeat("<g>", 600) + `<style> f6deep </style>` + strings.Repeat("</g>", 600) + `</svg>`,
	}
	for name, body := range refused {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != FallbackTooDeep {
			t.Errorf("%s: a mail whose %s tag would be dropped past the cap must be shown as source (%q), got %q", name, name, FallbackTooDeep, out.BodyFallback)
		}
		pageTrees(t, name, string(out.HTML))
	}

	// An SVG or MathML root dropped past the cap is still dropped, as the
	// flattener always did, but what its scope held for the leaves written in
	// its place is kept: a self-closing <textarea/> would be an unclosed HTML
	// textarea that swallows the rest of the page (and its closing tags, indexed
	// as 480 extra "div" terms), and a CDATA section would be a bogus comment
	// that hides its text (R3-AC5 (c), (e)).
	leaves := map[string]struct{ body, words string }{
		"svg textarea": {divs(485, `<p>f8before</p><svg><textarea/><p>f8after</p><p>f8tail</p>`), "f8tail"},
		"svg cdata":    {divs(482, `<svg><![CDATA[ f9cdata ]]></svg>`), "f9cdata"},
		"math cdata":   {divs(482, `<math><![CDATA[ f9mathcdata ]]></math>`), "f9mathcdata"},
	}
	for name, c := range leaves {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: c.body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("%s: must render flattened, got fallback %q", name, out.BodyFallback)
		}
		for i, root := range pageTrees(t, name, string(out.HTML)) {
			_, b := pageLayout(root)
			if b == nil || !shownWords(b, false, c.words) {
				t.Errorf("%s (reading %d): %q must be live text in the body div", name, i, c.words)
				continue
			}
			if findElem(b, func(n *xhtml.Node) bool { return n.Data == "textarea" || n.Type == xhtml.CommentNode }) != nil ||
				strings.Contains(textOf(b), "</div>") {
				t.Errorf("%s (reading %d): an HTML textarea or a comment swallowed the mail, or its closing tags became text:\n%s", name, i, clipped(bodyPart(string(out.HTML))))
			}
		}
	}

	flattened := map[string]string{
		"600 divs":              nestedHTML(600, "<p>plain deep words</p>"),
		"150 tables":            nestedTables(150, "<p>table deep words</p>"),
		"svg groups":            "<svg>" + strings.Repeat("<g>", 600) + "<text>group words</text>" + strings.Repeat("</g>", 600) + "</svg>",
		"template below cap":    nestedHTML(100, `<template><p>inert</p></template><p>tw words</p>`),
		"noscript below cap":    nestedHTML(100, `<noscript><p>ns</p></noscript><p>nw words</p>`),
		"svg below cap":         nestedHTML(100, `<svg><style>.a{}</style><rect/></svg><p>sw words</p>`),
		"svg before the cap":    `<svg><style>.a{}</style></svg>` + nestedHTML(600, "<p>bw words</p>"),
		"template then deep":    `<template><p>t</p></template>` + nestedHTML(600, "<p>tdw words</p>"),
		"noscript then deep":    `<noscript><p>n</p></noscript>` + nestedHTML(600, "<p>ndw words</p>"),
		"deep then plain texts": nestedHTML(600, "<p>a</p><textarea>t</textarea><style>x{}</style><script>y</script><p>b</p>"),
	}
	for name, body := range flattened {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("positive twin %s: must render as HTML, got fallback %q", name, out.BodyFallback)
		}
	}
}

// covers: MA-472, R1, R7, R19
// R3-AC6. The serialization cap scaled with the input without bound: 16x a
// 10 MB mail is 160 MB, so a small inflating mail (400 unclosed `<b id=i>` and
// thousands of paragraphs, which falls back as too-large in 1.3 s on its own)
// plus a 10 MB harmless comment was rendered, serialized to a 126 MB page and
// re-parsed in both readings: 18.6 s and 3.2 GB while the archive lock was
// held, linear in the padding. The cap is now 16x the input AND at most a fixed
// headroom (32 MiB) beyond it: ordinary mail never grows by more than a few
// times (MA-472's ~5x worst case is 8 MB of input against the headroom), a big
// inline image (output about its size) is far inside it, and padding cannot lift
// the guard.
func TestSerializedCapDoesNotScaleWithPadding(t *testing.T) {
	const mib = 1 << 20
	for _, c := range []struct{ n, want int }{
		{0, mib},                // the floor
		{1000, mib},             // small mail: the floor
		{100 << 10, 1600 << 10}, // 16x
		{1 * mib, 16 * mib},     // 16x
		{2 * mib, 32 * mib},     // 16x still binds (the headroom would allow 34)
		{3 * mib, 35 * mib},     // the headroom takes over past about 2.1 MiB
		{10 * mib, 42 * mib},    // the padded RV-8 mail: not 160 MiB
		{64 * mib, 96 * mib},    // a very large mail grows by the headroom only
	} {
		if got := serializedCap(c.n); got != c.want {
			t.Errorf("serializedCap(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

// covers: MA-472, R1, R7
// R3-AC6, end to end with the headroom lowered (the real one is 32 MiB, too big
// to inflate past in a unit test): a harmless 2 MiB comment pads the RV-8 body,
// which alone is far over the 1 MiB floor and falls back, and whose 16x cap
// with the padding (32 MiB) would let its ~12 MiB serialization through. The
// headroom is what refuses it, as too-large and never re-parsed. The positive
// twin: the same padding with ordinary markup renders as HTML.
func TestPaddingCannotLiftTheSerializationCap(t *testing.T) {
	old := serializedHeadroom
	serializedHeadroom = 4 << 20
	t.Cleanup(func() { serializedHeadroom = old })

	pad := "<!--" + strings.Repeat("x", 2<<20) + "-->"
	doc := pad + inflatingPart(2000)
	if got, was := serializedCap(len(doc)), 16*len(doc); got >= was {
		t.Fatalf("precondition: the headroom must bind, cap %d vs 16x %d", got, was)
	}
	parses := countReparses(t)
	out, err := RenderWith(&model.Message{Subject: "padded explode", HTMLBody: doc}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if out.BodyFallback != FallbackTooLarge || parses.on+parses.off != 0 {
		t.Errorf("a padded inflating mail must fall back as %q, never re-parsed: got %q (on=%d off=%d)", FallbackTooLarge, out.BodyFallback, parses.on, parses.off)
	}

	twin, err := RenderWith(&model.Message{Subject: "padded ordinary", HTMLBody: pad + `<p>ordinary words</p>`}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if twin.BodyFallback != "" {
		t.Errorf("positive twin: padding with ordinary markup must render, got %q", twin.BodyFallback)
	}
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

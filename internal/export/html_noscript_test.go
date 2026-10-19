package export

import (
	"fmt"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/model"
)

// deepNoscript is the RV-9 body: shallow to the sanitizer's scripting-on parse
// (noscript content is one text node) and n+3 deep to a reader with
// JavaScript off, who reads it as markup.
func deepNoscript(n int) string {
	return "<p>shown words</p><noscript>" + nestedHTML(n, "noscript words") + "</noscript>"
}

// covers: MA-475, R7, R1, R19, S21
// The re-parse check names its own failures truthfully. When a reading of the
// output cannot be parsed at all, nothing is known to be safe and the body must
// fall back — but the reason is the parse failure's: a depth-cap error in
// either reading is too-deeply-nested (a benign mail reaches it in the
// scripting-off reading alone, through deep markup inside a <noscript>), not
// unsafe-after-reparse. Only a live hostile meta or link hint is reported
// unsafe; shallow ordinary output is not flagged (positive twin).
func TestReparseCheckNamesItsFailureTruthfully(t *testing.T) {
	body := deepNoscript(600)
	if _, err := xhtml.Parse(strings.NewReader(body)); err != nil {
		t.Fatalf("precondition: the scripting-on reading must parse: %v", err)
	}
	cases := []struct{ name, head, body, want string }{
		{"deep only with scripting off", "", body, FallbackTooDeep},
		{"deep head noscript, scripting off", "<noscript>" + nestedHTML(600, "h") + "</noscript>", "<p>x</p>", FallbackTooDeep},
		{"deep in both readings", "", nestedHTML(600, "z"), FallbackTooDeep},
		{"live refresh", "", `<meta http-equiv="refresh" content="0;url=http://evil.example/">`, FallbackHostile},
		{"live refresh, scripting off only", "", `<noscript><meta http-equiv="refresh" content="0;url=http://evil.example/"></noscript>`, FallbackHostile},
		{"ordinary", `<style>p{color:red}</style>`, `<p>fine <b>words</b></p><noscript><img src="data:image/gif;base64,R0lGOD=" alt=""></noscript>`, ""},
	}
	for _, c := range cases {
		if got := hostileSurvives(c.head, c.body); got != c.want {
			t.Errorf("%s: hostileSurvives = %q, want %q", c.name, got, c.want)
		}
	}
}

// covers: MA-475, R7, R19, S21
// A benign mail whose <noscript> wraps markup nested 100 to 600 deep renders as
// HTML (it was shown as source and reported unsafe-after-reparse): the
// flattener reads noscript content as markup, so the scripting-off reading is
// flattened too, and the page parses in both readings with the noscript
// markup kept as elements for a reader with JavaScript off.
func TestRenderDeepNoscriptRenders(t *testing.T) {
	for _, n := range []int{100, 505, 509, 510, 600} {
		name := fmt.Sprintf("noscript %d", n)
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: deepNoscript(n)}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		if out.BodyFallback != "" {
			t.Errorf("%s: a benign deep noscript must render as HTML, got fallback %q", name, out.BodyFallback)
			continue
		}
		trees := pageTrees(t, name, string(out.HTML))
		for i, root := range trees {
			if !liveText(mailBody(t, root), "shown words") {
				t.Errorf("%s (reading %d): the mail's words must be live text", name, i)
			}
		}
		ns := findElem(mailBody(t, trees[1]), func(n *xhtml.Node) bool { return n.Data == "noscript" })
		if ns == nil || countElems(ns, "div") < min(n, 470) {
			t.Errorf("%s: with scripting off the noscript markup must be kept as elements (want at least %d divs)", name, min(n, 470))
		}
	}
}

// neutralizeOwnAttrs and neutralizeOwnClass are the reserved markers
// neutralize itself adds to mail elements — a disarmed meta's
// data-mailarchive-neutralized, a renamed link's
// data-mailarchive-neutralized-href, a dropped inline image's
// data-mailarchive-missing and class mailarchive-missing-image — written out
// literally so the test's oracle is its own, not the product's
// foreignReservedMarker.
var neutralizeOwnAttrs = map[string]bool{
	"data-mailarchive-neutralized":      true,
	"data-mailarchive-neutralized-href": true,
	"data-mailarchive-missing":          true,
}

const neutralizeOwnClass = "mailarchive-missing-image"

// reservedMarkerElems lists, for each scripting reading of a sanitized
// head+body, every reserved marker on a live element that neutralize did not
// add, as "tag:marker": a class token beginning "mailarchive-" (in any ASCII
// case, as stripReservedNamespace reads it) and a "data-mailarchive-*"
// attribute outside neutralizeOwnAttrs. The tool's own header is NOT in
// sanitizeHTML's output (the renderer adds it later), so any hit here is a
// mail-supplied forgery that survived stripping.
func reservedMarkerElems(t *testing.T, head, body string) [2][]string {
	t.Helper()
	doc := "<!DOCTYPE html><html><head>" + head + "</head><body>" + body + "</body></html>"
	var out [2][]string
	for i, scripting := range []bool{true, false} {
		root, err := xhtml.ParseWithOptions(strings.NewReader(doc), xhtml.ParseOptionEnableScripting(scripting))
		if err != nil {
			t.Fatalf("reading %d: %v", i, err)
		}
		var walk func(x *xhtml.Node)
		walk = func(x *xhtml.Node) {
			if x.Type == xhtml.ElementNode {
				for _, a := range x.Attr {
					key := strings.ToLower(a.Key)
					if strings.HasPrefix(key, "data-mailarchive-") && !neutralizeOwnAttrs[key] {
						out[i] = append(out[i], x.Data+":"+key)
					}
					if key == "class" {
						for _, tok := range strings.Fields(a.Val) {
							if strings.HasPrefix(strings.ToLower(tok), "mailarchive-") && tok != neutralizeOwnClass {
								out[i] = append(out[i], x.Data+":class="+tok)
							}
						}
					}
				}
			}
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}
	return out
}

// covers: MA-496, R19, R8, R7, S21
// A mail cannot keep the tool's private namespace on an element in either
// reading of the exported page, even hidden in a head <noscript> whose content
// is raw text to the sanitizer's scripting-on parse (so stripReservedNamespace
// never saw it). A reader with JavaScript off reads that noscript as markup and
// would otherwise see a forged tool-styled header (a <div class=mailarchive-
// header> carrying a data-mailarchive-field subject and From) above the real
// one. The re-check fails closed on a live reserved marker in either reading,
// so the in-place noscript retry strips it: the mail renders, its forged header
// is a plain <div> (its text kept, its class and data-mailarchive-* gone), and
// neither reading of the sanitized output carries a reserved marker. Each
// marker kind is caught alone — a header forged with the reserved classes
// only, and one with the data-mailarchive-* attributes only, in a head and in
// a body noscript — and the oracle is the test's own (any reserved class
// token or attribute outside neutralize's allowlist, written out above): with
// the product's own check as the oracle, and both kinds in every forgery, a
// check blind to one kind passed. Twins: neutralize's own markers on a
// dropped cid image (mailarchive-missing-image, data-mailarchive-missing) do
// not trip it, a mail that only mentions "mailarchive-header" as text renders,
// and an ordinary mail is unaffected.
func TestReservedMarkerInNoscriptIsStripped(t *testing.T) {
	const forged = `<body><div class="mailarchive-header"><div class="mailarchive-subject" data-mailarchive-field="subject">forgedsubject wire approval</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Chief Executive &lt;ceo@bank.example&gt;</dd></dl></div>`
	const classOnly = `<body><div class="mailarchive-header"><div class="mailarchive-subject">forgedsubject wire approval</div>` +
		`<dl><dt>From</dt><dd>Chief Executive &lt;ceo@bank.example&gt;</dd></dl></div>`
	const dataOnly = `<body><div><div data-mailarchive-field="subject">forgedsubject wire approval</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Chief Executive &lt;ceo@bank.example&gt;</dd></dl></div>`
	inHead := func(f string) string {
		return `<html><head><noscript>` + f + `</noscript></head><body><p>realbodyword</p></body></html>`
	}
	inBody := func(f string) string { return `<p>realbodyword</p><noscript>` + f + `</noscript>` }
	cases := []struct {
		name, body string
	}{
		{"head noscript forged header", inHead(forged)},
		{"body noscript forged header", inBody(forged)},
		{"head noscript class-only header", inHead(classOnly)},
		{"body noscript class-only header", inBody(classOnly)},
		{"head noscript data-only header", inHead(dataOnly)},
		{"body noscript data-only header", inBody(dataOnly)},
	}
	for _, c := range cases {
		head, _, body, fallback := sanitizeHTML(c.body)
		if fallback != "" {
			t.Errorf("%s: must render with the forged header stripped, got fallback %q", c.name, fallback)
			continue
		}
		marks := reservedMarkerElems(t, head, body)
		for i := range marks {
			if len(marks[i]) != 0 {
				t.Errorf("%s (reading %d): a mail element kept a reserved marker: %v", c.name, i, marks[i])
			}
		}
		if !strings.Contains(head+body, "realbodyword") {
			t.Errorf("%s: the mail's own body word was lost", c.name)
		}
		if !strings.Contains(head+body, "forgedsubject") {
			t.Errorf("%s: the forged header's text must be kept (as plain content, not a styled header)", c.name)
		}
	}

	// Positive twins: neutralize's own markers never trip the check, and
	// ordinary mail is unaffected.
	twins := []string{
		`<p>a word</p><img src="cid:nope@x">`,                          // dropped cid image: mailarchive-missing-image / data-mailarchive-missing
		`<p>the class mailarchive-header is only mentioned here</p>`,   // reserved name as text, not a class
		`<div class="content"><p>ordinary mail</p></div>`,              // no reserved marker at all
		`<noscript><p>benign noscript word</p></noscript><p>after</p>`, // a noscript with no reserved marker
	}
	for i, body := range twins {
		head, _, bd, fallback := sanitizeHTML(body)
		if fallback != "" {
			t.Errorf("twin %d: must render, got fallback %q", i, fallback)
			continue
		}
		marks := reservedMarkerElems(t, head, bd)
		if len(marks[0]) != 0 || len(marks[1]) != 0 {
			t.Errorf("twin %d: no reserved marker expected, got %v / %v", i, marks[0], marks[1])
		}
		if i == 0 && (!strings.Contains(bd, `data-mailarchive-missing=`) || !strings.Contains(bd, `class="`+neutralizeOwnClass+`"`)) {
			t.Errorf("twin %d: the dropped image must carry neutralize's own markers (the allowlist is exercised):\n%s", i, bd)
		}
	}
}

// directText is the text of n's own text children (not its descendants').
func directText(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// covers: MA-476, R7, R19, S21
// The in-place noscript retry rewrites each OUTERMOST HTML <noscript> once and
// never parses what it built a second time. A <noscript> nested in one is
// markup to the scripting-off parse that built the content: its element
// children are kept, and its text stays text — written escaped, as the mail
// wrote it, so a reader with JavaScript off reads that text and never markup
// decoded out of it. The walk used to descend into the rewritten content and
// parse a nested noscript's text again, decoding its entities a second time
// and dropping its element children: the reviewer's adv_noscript2_body_esc_d
// turned its escaped refresh into a (neutralized) <meta> element, a doubly
// escaped `&amp;lt;b&amp;gt;` became a <b> element, and a <p> beside nested
// text was lost. An SVG <noscript> is an ordinary foreign element whose
// children were markup to the main parse: its escaped text stays text too,
// never re-parsed into a <b> that leaves the <svg>. Each renders as HTML, with
// no hostile meta live in either reading and no element made of escaped text.
func TestNoscriptRetryRewritesEachNoscriptOnce(t *testing.T) {
	cases := []struct {
		name, body string
		nestedText string // the nested noscript's own text to a reader with JavaScript off
		kept       string // the text of the nested noscript's <p> child ("": none)
		svgText    string // the SVG noscript's text in both readings ("": no SVG)
	}{
		{"nested escaped refresh",
			`<p>adv words</p><noscript><meta charset=utf-8><noscript>&lt;meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/adv"&gt;</noscript></noscript><p>tail</p>`,
			`<meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/adv">`, "", ""},
		{"nested element children",
			`<p>nsa</p><noscript><meta charset=utf-8><noscript>nestedtextword<p>nestedkeptword</p></noscript></noscript><p>nstail</p>`,
			"nestedtextword", "nestedkeptword", ""},
		{"nested doubly escaped",
			`<p>nsa</p><noscript><meta charset=utf-8><noscript>&amp;lt;b&amp;gt;dblword&amp;lt;/b&amp;gt;</noscript></noscript>`,
			"&lt;b&gt;dblword&lt;/b&gt;", "", ""},
		{"svg noscript",
			`<p>nsa</p><noscript><meta charset=utf-8></noscript><svg><noscript>&lt;b&gt;svgword&lt;/b&gt;</noscript></svg>`,
			"", "", "<b>svgword</b>"},
	}
	for _, c := range cases {
		out, err := RenderWith(&model.Message{Subject: c.name, HTMLBody: c.body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		page := string(out.HTML)
		if out.BodyFallback != "" {
			t.Errorf("%s: must render as HTML, got fallback %q", c.name, out.BodyFallback)
			continue
		}
		if live := liveHostileMetas(t, page); len(live) > 0 {
			t.Errorf("%s: a live hostile meta survived: %v\n%s", c.name, live, bodyPart(page))
		}
		trees := pageTrees(t, c.name, page)
		for i, root := range trees {
			body := mailBody(t, root)
			if n := countElems(body, "meta") + countElems(body, "b"); n != 0 {
				t.Errorf("%s (reading %d): escaped text became %d element(s):\n%s", c.name, i, n, bodyPart(page))
			}
			if c.svgText != "" {
				ns := findElem(body, func(n *xhtml.Node) bool { return n.Data == "noscript" && n.Namespace == "svg" })
				if ns == nil || directText(ns) != c.svgText {
					t.Errorf("%s (reading %d): the SVG noscript's text must stay %q", c.name, i, c.svgText)
				}
			}
		}
		if c.nestedText == "" {
			continue
		}
		off := mailBody(t, trees[1])
		inner := findElem(off, func(n *xhtml.Node) bool {
			return n.Data == "noscript" && n.Parent != nil && n.Parent.Data == "noscript"
		})
		if inner == nil {
			t.Errorf("%s: with scripting off the nested noscript must be an element inside the outer one:\n%s", c.name, bodyPart(page))
			continue
		}
		if got := directText(inner); got != c.nestedText {
			t.Errorf("%s: with scripting off the nested noscript's text must stay text, %q; got %q", c.name, c.nestedText, got)
		}
		if c.kept != "" {
			p := findElem(inner, func(n *xhtml.Node) bool { return n.Data == "p" })
			if p == nil || !strings.Contains(directText(p), c.kept) {
				t.Errorf("%s: the nested noscript's element child (<p>%s</p>) must be kept:\n%s", c.name, c.kept, bodyPart(page))
			}
		}
	}
}

// pageLayout finds, in a parsed page, the renderer's header div — the first
// div directly in <body> with the class token mailarchive-header — and the
// body div that must be the next element after it (nil when either is not
// live there): the anchor `reindex -rebuild` reads a record by. Written apart
// from the product's own check so the test does not grade itself.
func pageLayout(root *xhtml.Node) (header, body *xhtml.Node) {
	hasToken := func(n *xhtml.Node, tok string) bool {
		for _, f := range strings.Fields(attrOf(n, "class")) {
			if f == tok {
				return true
			}
		}
		return false
	}
	bodyEl := findElem(root, func(n *xhtml.Node) bool { return n.Data == "body" })
	if bodyEl == nil {
		return nil, nil
	}
	for c := bodyEl.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == "div" && hasToken(c, "mailarchive-header") {
			header = c
			break
		}
	}
	if header == nil {
		return nil, nil
	}
	for c := header.NextSibling; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			if c.Data == "div" && hasToken(c, "mailarchive-body") {
				return header, c
			}
			break
		}
	}
	return header, nil
}

// shownIn reports whether the mail's words are on the page body as the mail
// showed them: in a live HTML <p> or <xmp> (markup that nothing before it
// swallowed or moved — raw text in a <noscript>, a <plaintext> or a script is
// not, nor an SVG element the words were moved into), or, when the body fell
// back, in the text of the shown-as-source <pre>.
func shownIn(body *xhtml.Node, fellBack bool, words string) bool {
	if !fellBack {
		return findElem(body, func(n *xhtml.Node) bool {
			return n.Namespace == "" && (n.Data == "p" || n.Data == "xmp") && strings.Contains(directText(n), words)
		}) != nil
	}
	pre := findElem(body, func(n *xhtml.Node) bool { return n.Data == "pre" && attrOf(n, "data-mailarchive-fallback") != "" })
	return pre != nil && strings.Contains(directText(pre), words)
}

// covers: MA-476, MA-474, R7, R8, R13, R19, S21
// No page the exporter writes hides its own header or body. The in-place
// noscript retry (d787013) wrote pages whose head <noscript> never closed: its
// rewritten content ended in a <plaintext> (x/net's Render writes nothing
// after one, so the </noscript> was never written, and the rest of the page —
// the tool's header and the mail — became noscript raw text in the head), or
// in a script left in an escape state by `<!--<script>` (with scripting off it
// ran past its </script> and the </noscript> to the end of the page). The
// re-check read no live meta in either and the page check only parsed the
// page, so a hostile mail blanked its own archived page and, on `reindex
// -rebuild`, erased its own subject, sender and date. These are the
// reviewer's nine (rv3/corpus-adv: adv_plaintext_head_{esc,lnk,ref}_{a,d},
// adv_scriptesc_head_{esc,lnk,ref}_a). A rewritten noscript is now kept only
// where it is written as one closed noscript in both readings (closesItself),
// and every pass is accepted only with a page whose header div and body div are
// live in every reading (pageFallback). In the body the same tricks moved or
// swallowed the mail's words after the noscript: a <plaintext>, a script
// escape (with scripting off only), text that decodes to
// `</noscript><plaintext>` or to a `</div>` that closes the body div, a
// noscript nested in a <div> whose `</div>` the scripting-on reading meets
// after the outer noscript's raw text ends (it closed the page's body div),
// and, under an SVG foreignObject inside an SVG <noscript>, a nested
// noscript's end tag left over after the raw text, which the foreign-content
// rules read as the SVG noscript's (the HTML <xmp> after it became an SVG xmp
// and its text an SVG <style>). Two heads blanked the page with no retry to
// blame: a head <script> left in an escape state at the end of the mail (both
// readings), and a head <noscript> holding a <plaintext> (scripting off) —
// they fall back to the escaped source as unsafe-after-reparse, whose header
// is live. In every reading of every page here the header (with the subject)
// and the body div are live, the mail's words are shown as it showed them
// (live, or in the source), the page's own charset is the only charset meta,
// and no hostile meta or hint is live.
//
// Positive twins, rendered as HTML with the header, the body div and the words
// live: a body <plaintext> or script escape at the end of the mail (it
// swallows only the page's closing tags), an AMP head noscript, a head
// noscript with a charset meta and text; a head <noscript> whose raw text
// leaves an element open around the header for a reader with JavaScript off (a
// <b>, a <div>) or a <frameset> that drops the body — the first pass's page
// fails, and the in-place retry writes the content closed (or, for the
// frameset, empty, as a body fragment parse reads it); and a refused rewrite
// beside an accepted one — the refused noscript keeps its raw text, the
// accepted one's charset meta is gone, and the page renders.
func TestRenderKeepsThePageHeaderAndBodyLive(t *testing.T) {
	const (
		esc = `&lt;meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/adv"&gt;`
		lnk = `<link rel="preconnect" href="http://127.0.0.1:8799/b/adv">`
		ref = `<meta http-equiv="refresh" content="0;url=http://127.0.0.1:8799/b/adv">`
	)
	head := func(ns string) string {
		return `<html><head><noscript><meta charset=utf-8>` + ns + `</noscript></head><body><p>adv words</p></body></html>`
	}
	cases := []struct {
		name, body, words string
		fallback          string // "" = any (the retry or the page check decides); else the reason it must fall back as
	}{
		{"adv_plaintext_head_esc_a", head(`<plaintext></noscript>` + esc), "adv words", ""},
		{"adv_plaintext_head_esc_d", head(`<plaintext>` + esc), "adv words", ""},
		{"adv_plaintext_head_lnk_a", head(`<plaintext></noscript>` + lnk), "adv words", ""},
		{"adv_plaintext_head_lnk_d", head(`<plaintext>` + lnk), "adv words", ""},
		{"adv_plaintext_head_ref_a", head(`<plaintext></noscript>` + ref), "adv words", ""},
		{"adv_plaintext_head_ref_d", head(`<plaintext>` + ref), "adv words", ""},
		{"adv_scriptesc_head_esc_a", head(`<script><!--<script></noscript>` + esc + `</script>`), "adv words", ""},
		{"adv_scriptesc_head_lnk_a", head(`<script><!--<script></noscript>` + lnk + `</script>`), "adv words", ""},
		{"adv_scriptesc_head_ref_a", head(`<script><!--<script></noscript>` + ref + `</script>`), "adv words", ""},
		{"body noscript plaintext", `<p>nsbefore</p><noscript><meta charset=utf-8><plaintext>x</noscript><p>after words</p>`, "after words", ""},
		{"body noscript script escape", `<p>nsbefore</p><noscript><meta charset=utf-8><script><!--<script></noscript><p>after words</p>`, "after words", ""},
		{"body noscript decoded plaintext", `<p>nsbefore</p><noscript>a&lt;/noscript&gt;&lt;plaintext&gt;b<meta charset=utf-8></noscript><p>after words</p>`, "after words", ""},
		{"body noscript decoded div end", `<p>nsbefore</p><noscript>&lt;/noscript&gt;&lt;/div&gt;<meta charset=utf-8></noscript><p>after words</p>`, "after words", ""},
		{"body noscript nested in a div", `<p>nsbefore</p><noscript><meta charset=utf-8><div><noscript>inner</div>outer</noscript><p>after words</p>`, "after words", ""},
		{"svg noscript stray end tag", `<p>nsbefore</p><svg><noscript><foreignObject><noscript><meta charset=utf-8><noscript>y</noscript>more<xmp>q<style>fo words</style>z</xmp></foreignObject></noscript></svg><p>after</p>`, "fo words", ""},
		{"head script escape at the end", `<html><head><title>t</title><script><!--<script></head><body><p>after words</p></body></html>`, "after words", FallbackHostile},
		{"head noscript plaintext", `<html><head><noscript><plaintext>nsheadword</noscript></head><body><p>after words</p></body></html>`, "after words", FallbackHostile},
	}
	for _, c := range cases {
		subject := "pagesubj " + c.name
		out, err := RenderWith(&model.Message{Subject: subject, HTMLBody: c.body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		page := string(out.HTML)
		if c.fallback != "" && out.BodyFallback != c.fallback {
			t.Errorf("%s: must fall back as %q (its page would hide the header), got %q", c.name, c.fallback, out.BodyFallback)
		}
		for i, root := range pageTrees(t, c.name, page) {
			header, body := pageLayout(root)
			if header == nil || body == nil {
				t.Errorf("%s (reading %d): the page's header and body div must be live directly in <body> (header %v, body div %v):\n%s",
					c.name, i, header != nil, body != nil, bodyPart(page))
				continue
			}
			if !strings.Contains(textOf(header), subject) {
				t.Errorf("%s (reading %d): the live header must carry the subject", c.name, i)
			}
			if !shownIn(body, out.BodyFallback != "", c.words) {
				t.Errorf("%s (reading %d): the mail's words %q must be on the page as the mail showed them (fallback %q):\n%s", c.name, i, c.words, out.BodyFallback, bodyPart(page))
			}
		}
		if live := liveHostileMetas(t, page); len(live) > 0 {
			t.Errorf("%s: a live hostile meta: %v", c.name, live)
		}
		if hints := liveResourceHints(t, page); len(hints) > 0 {
			t.Errorf("%s: a live resource hint: %v", c.name, hints)
		}
		if n := liveCharsetMetas(t, page); n != [2]int{1, 1} {
			t.Errorf("%s: the page's own charset must be the only one, got %v (scripting on, off)", c.name, n)
		}
	}

	twins := []struct{ name, body, words string }{
		{"body plaintext at the end", `<p>before</p><plaintext>rest words`, "rest words"},
		{"body script escape at the end", `<p>before words</p><script><!--<script>`, "before words"},
		{"amp head noscript", `<!doctype html><html amp4email><head><meta charset="utf-8"><style amp4email-boilerplate>body{visibility:hidden}</style><noscript><meta charset="utf-8"><style>body{visibility:visible}</style></noscript></head><body><p>marker words</p></body></html>`, "marker words"},
		{"head noscript charset text", `<html><head><noscript><meta charset="utf-8"><p>headnstext</p></noscript></head><body><p>head twin words</p></body></html>`, "head twin words"},
		{"head noscript open b", `<html><head><noscript><b>Please enable JavaScript</noscript></head><body><p>open b words</p></body></html>`, "open b words"},
		{"head noscript open div", `<html><head><noscript><div class="banner">Please enable JavaScript</noscript></head><body><p>open div words</p></body></html>`, "open div words"},
		{"head noscript frameset", `<html><head><noscript><frameset></noscript></head><body><p>frameset words</p></body></html>`, "frameset words"},
		{"refused beside accepted", `<p>m5before</p><noscript><meta charset=utf-8></noscript><noscript><div><noscript>m5inner</div>m5outer</noscript><p>m5 after words</p>`, "m5 after words"},
	}
	for _, c := range twins {
		out, err := RenderWith(&model.Message{Subject: c.name, HTMLBody: c.body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		page := string(out.HTML)
		if out.BodyFallback != "" {
			t.Errorf("positive twin %s: must render as HTML, got fallback %q", c.name, out.BodyFallback)
			continue
		}
		for i, root := range pageTrees(t, c.name, page) {
			if header, body := pageLayout(root); header == nil || body == nil || !liveText(body, c.words) {
				t.Errorf("positive twin %s (reading %d): header and body div live, the words live text:\n%s", c.name, i, page)
			}
		}
		if n := liveCharsetMetas(t, page); n != [2]int{1, 1} {
			t.Errorf("positive twin %s: the page's own charset must be the only one, got %v (scripting on, off)", c.name, n)
		}
	}
}

// textOf is the text of n and its descendants.
func textOf(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(x *xhtml.Node)
	walk = func(x *xhtml.Node) {
		if x.Type == xhtml.TextNode {
			b.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// liveCharsetMetas counts the <meta charset> elements in an exported page in
// each scripting reading (the page's own utf-8 charset is one of them).
func liveCharsetMetas(t *testing.T, page string) [2]int {
	t.Helper()
	var n [2]int
	for i, root := range pageTrees(t, "charset count", page) {
		var walk func(x *xhtml.Node)
		walk = func(x *xhtml.Node) {
			if x.Type == xhtml.ElementNode && strings.EqualFold(x.Data, "meta") {
				for _, a := range x.Attr {
					if strings.EqualFold(a.Key, "charset") {
						n[i]++
					}
				}
			}
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}
	return n
}

// covers: MA-476, MA-300, R7, R19, S21
// A harmless <meta charset> or Content-Type meta inside <noscript> — in the
// body, the head, an AMP-style head, or a whole ESP document wrapped in
// noscript — renders as HTML (it was always shown as source): the re-check
// trips on the scripting-off reading, so the main parse is retried once with
// scripting off, where neutralize can reach noscript content, and the result
// passes the same dual-mode re-check. Under both readings no hostile meta is
// live and the page's own utf-8 charset is the only charset meta. The MA-300
// noscript refreshes now render with the refresh neutralized, still inert;
// the escaped twin renders as text; and an escaped twin beside a real
// charset meta — whose scripting-off serialization writes the escaped meta
// back raw — is rejected by the re-check and falls back (the retry is
// accepted only when the re-check passes again). The retry keeps the mail's
// head pieces: the AMP head's styles are on the page in both readings.
func TestRenderNoscriptMetasRenderNeutralized(t *testing.T) {
	const refresh = `<meta http-equiv="refresh" content="0;url=http://evil.example/beacon">`
	render := map[string]string{
		"body content-type":     `<p>marker-text</p><noscript><meta http-equiv=Content-Type content="text/html; charset=UTF-8"><img src=cid:px@x></noscript>`,
		"body charset":          `<p>marker-text</p><noscript><meta charset=utf-8></noscript>`,
		"head charset":          `<html><head><noscript><meta charset=utf-8></noscript></head><body><p>marker-text</p></body></html>`,
		"head content-type":     `<html><head><noscript><meta http-equiv="Content-Type" content="text/html; charset=utf-8"></noscript></head><body><p>marker-text</p></body></html>`,
		"amp head":              `<!doctype html><html amp4email data-css-strict><head><meta charset="utf-8"><script async src="https://cdn.ampproject.org/v0.js"></script><style amp4email-boilerplate>body{visibility:hidden}</style><noscript><meta charset="utf-8"><style>body{visibility:visible}</style></noscript></head><body><p>marker-text</p></body></html>`,
		"esp doc in noscript":   `<p>marker-text</p><noscript><html><head><meta http-equiv="Content-Type" content="text/html; charset=utf-8"><title>News</title></head><body><table><tr><td>fallback newsletter</td></tr></table></body></html></noscript>`,
		"noscript refresh":      `<p>marker-text</p><noscript>` + refresh + `</noscript>`,
		"head noscript refresh": `<html><head><noscript>` + refresh + `</noscript></head><body><p>marker-text</p></body></html>`,
		"nested refresh":        `<div><span><noscript>` + refresh + `</noscript></span></div><p>marker-text</p>`,
		"escaped twin":          `<p>marker-text</p><noscript>&lt;meta http-equiv="refresh" content="0;url=http://evil.example/beacon"&gt;</noscript>`,
	}
	for name, body := range render {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatal(err)
		}
		page := string(out.HTML)
		if out.BodyFallback != "" {
			t.Errorf("%s: must render as HTML, got fallback %q", name, out.BodyFallback)
		}
		if live := liveHostileMetas(t, page); len(live) > 0 {
			t.Errorf("%s: a live hostile meta survived: %v\n%s", name, live, bodyPart(page))
		}
		if n := liveCharsetMetas(t, page); n != [2]int{1, 1} {
			t.Errorf("%s: the page's own charset must be the only one, got %v (scripting on, off)", name, n)
		}
		if !strings.Contains(page, "marker-text") {
			t.Errorf("%s: the mail's own text was lost", name)
		}
	}

	mixed := `<p>marker-text</p><noscript>&lt;meta http-equiv="refresh" content="0;url=http://evil.example/beacon"&gt;<meta charset=utf-8></noscript>`
	out, err := RenderWith(&model.Message{Subject: "mixed", HTMLBody: mixed}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if live := liveHostileMetas(t, string(out.HTML)); len(live) > 0 || out.BodyFallback != FallbackHostile {
		t.Errorf("escaped twin beside a real meta: the retry must be rejected by the re-check and fall back (got %q, live %v)", out.BodyFallback, live)
	}

	// The retry keeps the mail's head pieces: the AMP head's boilerplate style
	// is a live style in the page's head in both readings, and to a reader
	// with JavaScript off its noscript style is live there too.
	amp, err := RenderWith(&model.Message{Subject: "amp", HTMLBody: render["amp head"]}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if amp.BodyFallback != "" {
		t.Fatalf("amp head: must render, got %q", amp.BodyFallback)
	}
	for i, root := range pageTrees(t, "amp head", string(amp.HTML)) {
		head := findElem(root, func(n *xhtml.Node) bool { return n.Data == "head" })
		var styles []string
		var walk func(n *xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && n.Data == "style" && n.FirstChild != nil {
				styles = append(styles, n.FirstChild.Data)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(head)
		joined := strings.Join(styles, "|")
		if !strings.Contains(joined, "body{visibility:hidden}") {
			t.Errorf("amp head (reading %d): the mail's head style must be kept in the page's head, got styles %q", i, styles)
		}
		if scriptingOff := i == 1; scriptingOff && !strings.Contains(joined, "body{visibility:visible}") {
			t.Errorf("amp head, scripting off: the noscript style must be live in the page's head, got styles %q", styles)
		}
	}
}

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

// reservedMarkerElems lists, for each scripting reading of a sanitized
// head+body, the tag names of elements carrying a reserved marker neutralize
// did not add (what foreignReservedMarker catches). The tool's own header is
// NOT in sanitizeHTML's output (the renderer adds it later), so any hit here is
// a mail-supplied forgery that survived stripping.
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
			if x.Type == xhtml.ElementNode && foreignReservedMarker(x) {
				out[i] = append(out[i], x.Data)
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
// neither reading of the sanitized output carries a reserved marker. Twins:
// neutralize's own markers on a dropped cid image (mailarchive-missing-image,
// data-mailarchive-missing) do not trip it, a mail that only mentions
// "mailarchive-header" as text renders, and an ordinary mail is unaffected.
func TestReservedMarkerInNoscriptIsStripped(t *testing.T) {
	const forged = `<body><div class="mailarchive-header"><div class="mailarchive-subject" data-mailarchive-field="subject">forgedsubject wire approval</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Chief Executive &lt;ceo@bank.example&gt;</dd></dl></div>`
	cases := []struct {
		name, body string
		where      string // "head" or "body"
	}{
		{"head noscript forged header", `<html><head><noscript>` + forged + `</noscript></head><body><p>realbodyword</p></body></html>`, "head"},
		{"body noscript forged header", `<p>realbodyword</p><noscript>` + forged + `</noscript>`, "body"},
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

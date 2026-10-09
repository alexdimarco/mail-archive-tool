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

package export

import (
	"fmt"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// resourceHintRels are the link types that make a browser open a connection,
// resolve a name or fetch ahead of use. preconnect and dns-prefetch are not
// governed by any CSP directive, so the archive policy cannot stop them.
var resourceHintRels = []string{"preconnect", "dns-prefetch", "prefetch", "preload", "modulepreload", "prerender"}

// liveResourceHints parses an exported page the way a browser will, with
// scripting on and off (the flag decides whether <noscript> content is text or
// markup), and returns every <link> element — whatever its namespace — that has
// an href and a resource-hint rel token, plus any live <meta http-equiv=Link>
// (the Link pragma, which can carry a preconnect). rel is split on ASCII
// whitespace and compared ASCII case-insensitively, as browsers do. Written
// separately from the product's own check so the test does not grade itself.
func liveResourceHints(t *testing.T, page string) []string {
	t.Helper()
	var found []string
	for _, scripting := range []bool{true, false} {
		root, err := xhtml.ParseWithOptions(strings.NewReader(page), xhtml.ParseOptionEnableScripting(scripting))
		if err != nil {
			t.Fatalf("exported page does not parse (scripting=%v): %v", scripting, err)
		}
		var walk func(n *xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && strings.EqualFold(n.Data, "meta") {
				for _, a := range n.Attr {
					if strings.EqualFold(a.Key, "http-equiv") && strings.EqualFold(strings.TrimSpace(a.Val), "link") {
						found = append(found, fmt.Sprintf("Link pragma meta (scripting=%v)", scripting))
					}
				}
			}
			if n.Type == xhtml.ElementNode && strings.EqualFold(n.Data, "link") {
				href, rel, hasHref := "", "", false
				for _, a := range n.Attr {
					switch strings.ToLower(a.Key) {
					case "href":
						href, hasHref = a.Val, true
					case "rel":
						rel = a.Val
					}
				}
				for _, tok := range strings.FieldsFunc(rel, func(r rune) bool { return strings.ContainsRune(" \t\n\f\r", r) }) {
					for _, h := range resourceHintRels {
						if hasHref && strings.EqualFold(tok, h) {
							found = append(found, fmt.Sprintf("%s %s (scripting=%v)", h, href, scripting))
						}
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}
	return found
}

// covers: MA-470, R19, S21
// A mail-supplied <link> resource hint (preconnect, dns-prefetch, prefetch,
// preload, modulepreload, prerender) never comes out of the renderer with a
// live href, in the head, the body, a <template>, an SVG foreignObject or a
// <noscript> (whose copy is live only to a reader with JavaScript off), however
// the rel is cased or combined: CSP has no directive for preconnect or
// dns-prefetch, so an archived page opened from disk would otherwise open a
// connection to the sender's host (an open-tracking / IP-leak beacon). The
// mail's text is kept and the page still renders as HTML — a noscript copy
// through the scripting-off retry (MA-476), which neutralize can reach.
// The positive twin: ordinary mail keeps its hyperlinks and data: images, and a
// mail <link rel=stylesheet> (already blocked by the policy) is renamed inert
// without the body falling back.
func TestRenderResourceHintLinksAreInert(t *testing.T) {
	const beacon = "http://evil.example/beacon"
	cases := map[string]string{
		"head preconnect":     `<html><head><link rel="preconnect" href="` + beacon + `"></head><body><p>marker-text</p></body></html>`,
		"body preconnect":     `<p>marker-text</p><link rel="preconnect" href="` + beacon + `">`,
		"body dns-prefetch":   `<p>marker-text</p><link rel="dns-prefetch" href="//evil.example">`,
		"body prefetch":       `<p>marker-text</p><link rel="prefetch" href="` + beacon + `">`,
		"body preload":        `<p>marker-text</p><link rel="preload" as="image" href="` + beacon + `">`,
		"body modulepreload":  `<p>marker-text</p><link rel="modulepreload" href="` + beacon + `">`,
		"body prerender":      `<p>marker-text</p><link rel="prerender" href="` + beacon + `">`,
		"mixed case, tokens":  `<p>marker-text</p><LINK REL="Stylesheet  PreConnect" HREF="` + beacon + `">`,
		"tab separated":       "<p>marker-text</p><link rel=\"icon\tdns-prefetch\" href=\"" + beacon + "\">",
		"body template":       `<p>marker-text</p><template><link rel="preconnect" href="` + beacon + `"></template>`,
		"head template":       `<html><head><template><link rel="preconnect" href="` + beacon + `"></template></head><body><p>marker-text</p></body></html>`,
		"svg foreignObject":   `<p>marker-text</p><svg><foreignObject><link rel="preconnect" href="` + beacon + `"></foreignObject></svg>`,
		"noscript body":       `<p>marker-text</p><noscript><link rel="preconnect" href="` + beacon + `"></noscript>`,
		"noscript head":       `<html><head><noscript><link rel="preconnect" href="` + beacon + `"></noscript></head><body><p>marker-text</p></body></html>`,
		"noscript nested div": `<div><span><noscript><link rel="dns-prefetch" href="//evil.example"></noscript></span></div><p>marker-text</p>`,
		"head Link pragma":    `<html><head><meta http-equiv="Link" content="<` + beacon + `>; rel=preconnect"></head><body><p>marker-text</p></body></html>`,
	}
	for name, body := range cases {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: body}, RenderContext{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		page := assure.Reached(t, string(out.HTML), name+" exported page")
		if live := liveResourceHints(t, page); len(live) > 0 {
			t.Errorf("%s: a live resource-hint link survived the renderer: %v\n%s", name, live, bodyPart(page))
		}
		if !strings.Contains(page, "marker-text") {
			t.Errorf("%s: the mail's own text was lost", name)
		}
		if out.BodyFallback != "" {
			t.Errorf("%s: the body must render with the hint renamed inert, not fall back (got %q)", name, out.BodyFallback)
		}
	}

	ordinary := `<p>Hello <a href="https://example.com/offer" rel="noopener noreferrer">see the offer</a></p>` +
		`<img src="data:image/png;base64,iVBORw0KGgo=" alt="logo">` +
		`<link rel="stylesheet" href="https://cdn.example.com/mail.css">`
	out, err := RenderWith(&model.Message{Subject: "ordinary", HTMLBody: ordinary}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	page := string(out.HTML)
	if out.BodyFallback != "" {
		t.Fatalf("positive twin: ordinary mail must render as HTML, got fallback %q", out.BodyFallback)
	}
	for _, want := range []string{
		`<a href="https://example.com/offer" rel="noopener noreferrer">see the offer</a>`,
		`<img src="data:image/png;base64,iVBORw0KGgo=" alt="logo"/>`,
		`<link rel="stylesheet" data-mailarchive-neutralized-href="https://cdn.example.com/mail.css"/>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("positive twin: want %s in the page:\n%s", want, bodyPart(page))
		}
	}
}

// covers: MA-470, R19, S21
// The re-parse check fails closed on a live resource-hint <link href> in
// EITHER scripting reading, independently of neutralize: that covers a copy
// inside <noscript>, which the sanitizer's own scripting-on parse reads as text
// and neutralize can never reach. Positive twins: a stylesheet link, a hint with
// no href or with its href already renamed, a hint that is only text, and a rel
// token joined by a non-ASCII space (one token to a browser) are not hostile.
func TestHostileSurvivesCatchesLiveResourceHints(t *testing.T) {
	hostile := map[string][2]string{
		"body preconnect":     {"", `<link rel="preconnect" href="http://evil.example/">`},
		"head dns-prefetch":   {`<link rel="DNS-Prefetch" href="//evil.example">`, `<p>x</p>`},
		"noscript copy (off)": {"", `<noscript><link rel="preconnect" href="http://evil.example/"></noscript>`},
		"multi-token rel":     {"", "<link rel=\"icon\fpreload\" href=\"http://evil.example/\">"},
	}
	for name, hb := range hostile {
		if reason := hostileSurvives(hb[0], hb[1]); reason != FallbackHostile {
			t.Errorf("%s: the re-parse check must report a live resource hint as hostile (%q), got %q", name, FallbackHostile, reason)
		}
	}
	benign := map[string][2]string{
		"stylesheet":        {"", `<link rel="stylesheet" href="https://cdn.example.com/a.css">`},
		"hint without href": {"", `<link rel="preconnect">`},
		"neutralized hint":  {"", `<link rel="preconnect" data-mailarchive-neutralized-href="http://evil.example/">`},
		"hint as text":      {"", `<p>&lt;link rel="preconnect" href="http://evil.example/"&gt;</p>`},
		"non-ascii space":   {"", "<link rel=\"x\u00a0preconnect\" href=\"http://evil.example/\">"},
	}
	for name, hb := range benign {
		if reason := hostileSurvives(hb[0], hb[1]); reason != "" {
			t.Errorf("%s: positive twin must not be reported hostile, got %q", name, reason)
		}
	}
}

// bodyPart is the page from the renderer's body div on (clipped), for failure
// messages that should show the mail's markup rather than the page template.
func bodyPart(page string) string {
	if i := strings.Index(page, `<div class="mailarchive-body"`); i >= 0 {
		page = page[i:]
	}
	return clipped(page)
}

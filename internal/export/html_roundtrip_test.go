package export

import (
	"io"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// liveHostileMetas parses an exported page the way a browser will and returns
// every meta element whose http-equiv would act (refresh, CSP, set-cookie,
// content-type), whatever namespace it ended up in. It is written separately
// from the product's own check so the test does not grade itself.
func liveHostileMetas(t *testing.T, page string) []string {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("exported page does not parse: %v", err)
	}
	var found []string
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode && strings.EqualFold(n.Data, "meta") {
			for _, a := range n.Attr {
				if !strings.EqualFold(a.Key, "http-equiv") {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(a.Val)) {
				case "refresh", "set-cookie", "content-type":
					found = append(found, a.Val)
				case "content-security-policy":
					// The archive's own policy meta is the one allowed CSP.
					ours := false
					for _, b := range n.Attr {
						if strings.EqualFold(b.Key, "content") && b.Val == ArchiveCSP {
							ours = true
						}
					}
					if !ours {
						found = append(found, a.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return found
}

// covers: MA-300, R19, S21
// A meta refresh smuggled as escaped text inside an SVG or MathML raw-text
// element (style, noscript, xmp, iframe, noembed, noframes) must not come out
// of the renderer live. The parser decodes the character references into that
// element's text, so neutralize never sees a <meta>; a serializer that writes
// foreign-content raw text unescaped (x/net before v0.13.0, GO-2023-1988) then
// hands the browser a live refresh, which no CSP directive can stop. The
// positive twin proves benign SVG styling still renders as markup (the check
// does not fail closed on ordinary mail).
func TestRenderForeignContentRawTextStaysInert(t *testing.T) {
	smuggled := `&lt;/%[1]s&gt;&lt;meta http-equiv="refresh" content="0;url=http://evil.example/beacon"&gt;`
	cases := map[string]string{
		"svg style":     `<svg><style>` + strings.ReplaceAll(smuggled, "%[1]s", "style") + `</style></svg>`,
		"math style":    `<math><style>` + strings.ReplaceAll(smuggled, "%[1]s", "style") + `</style></math>`,
		"svg noscript":  `<svg><noscript>` + strings.ReplaceAll(smuggled, "%[1]s", "noscript") + `</noscript></svg>`,
		"svg xmp":       `<svg><xmp>` + strings.ReplaceAll(smuggled, "%[1]s", "xmp") + `</xmp></svg>`,
		"svg iframe":    `<svg><iframe>` + strings.ReplaceAll(smuggled, "%[1]s", "iframe") + `</iframe></svg>`,
		"svg noembed":   `<svg><noembed>` + strings.ReplaceAll(smuggled, "%[1]s", "noembed") + `</noembed></svg>`,
		"svg noframes":  `<svg><noframes>` + strings.ReplaceAll(smuggled, "%[1]s", "noframes") + `</noframes></svg>`,
		"svg unclosed":  `<svg><style>&lt;meta http-equiv="refresh" content="0;url=http://evil.example/beacon"&gt;`,
		"full document": `<html><head></head><body><p>hi</p><svg><style>` + strings.ReplaceAll(smuggled, "%[1]s", "style") + `</style></svg></body></html>`,
	}
	for name, body := range cases {
		out, err := RenderWith(&model.Message{Subject: name, HTMLBody: "<p>marker-text</p>" + body}, RenderContext{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		page := assure.Reached(t, string(out.HTML), name+" exported page")
		if live := liveHostileMetas(t, page); len(live) > 0 {
			t.Errorf("%s: a live hostile meta survived the renderer: %v\n%s", name, live, page)
		}
		if !strings.Contains(page, "marker-text") {
			t.Errorf("%s: the mail's own text was lost", name)
		}
	}

	benign := `<p>logo</p><svg width="10" height="10"><style>.a{fill:red}</style><rect class="a" width="10" height="10"></rect></svg>`
	out, err := RenderWith(&model.Message{Subject: "benign svg", HTMLBody: benign}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	page := string(out.HTML)
	if strings.Contains(page, `<pre class="plain">`) || !strings.Contains(page, `<rect class="a"`) {
		t.Errorf("benign SVG styling must render as markup, not fall back to text:\n%s", page)
	}
}

// covers: MA-301, R19, S21
// If the serialized output ever re-parses to a live hostile meta — whatever the
// cause — the body fails closed to escaped text: no live refresh, the mail's
// words still visible. Fault injection: a renderer that writes a raw refresh
// after every <style> stands in for a serializer bug. The positive twin is the
// same message under the real renderer, which keeps its markup.
func TestRenderFailsClosedWhenOutputReparsesHostile(t *testing.T) {
	m := &model.Message{Subject: "styled", HTMLBody: `<html><head></head><body><style>p{color:red}</style><p>visible-words</p></body></html>`}

	out, err := RenderWith(m, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	healthy := string(out.HTML)
	if strings.Contains(healthy, `<pre class="plain">`) || !strings.Contains(healthy, "<p>visible-words</p>") {
		t.Fatalf("positive twin: the real renderer must keep the markup:\n%s", healthy)
	}

	real := renderNode
	t.Cleanup(func() { renderNode = real })
	renderNode = func(w io.Writer, n *xhtml.Node) error {
		if err := real(w, n); err != nil {
			return err
		}
		if n.Type == xhtml.ElementNode && n.Data == "style" {
			_, err := io.WriteString(w, `<meta http-equiv="refresh" content="0;url=http://evil.example/injected">`)
			return err
		}
		return nil
	}

	out, err = RenderWith(m, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	page := assure.Reached(t, string(out.HTML), "fail-closed page")
	if live := liveHostileMetas(t, page); len(live) > 0 {
		t.Errorf("a non-round-tripping renderer let a live meta through: %v\n%s", live, page)
	}
	if !strings.Contains(page, `<pre class="plain">`) {
		t.Errorf("expected the escaped-text fallback body:\n%s", page)
	}
	if !strings.Contains(page, "visible-words") {
		t.Errorf("the fallback must still show the mail's words:\n%s", page)
	}
}

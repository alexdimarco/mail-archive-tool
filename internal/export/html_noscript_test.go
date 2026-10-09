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

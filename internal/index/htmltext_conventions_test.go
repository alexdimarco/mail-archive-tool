package index

import (
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// covers: MA-493, R8
// The tree-based extractor keeps the conventions the search snippet and the
// FTS rows were built on: whitespace collapses to single spaces, entities are
// decoded, block elements (and <br>) separate words while inline elements join
// them, attribute values and comments are not text, the text of <textarea> and
// <xmp> is shown, and a plain-text body wins over HTML (RTF last). Two things
// change on purpose, because no reading of the page shows them: <template>
// content (inert) and a <title> placed in the body (display:none) are no longer
// indexed.
func TestBodyTextKeepsExtractorConventions(t *testing.T) {
	cases := []struct {
		name string
		m    *model.Message
		want string
	}{
		{"whitespace collapses", &model.Message{HTMLBody: "<p>  a \n\t b  </p>"}, "a b"},
		{"entities decode", &model.Message{HTMLBody: "<p>Fish &amp; chips&nbsp;now &lt;3 &#x41;</p>"}, "Fish & chips now <3 A"},
		{"block boundaries separate words", &model.Message{HTMLBody: "<div>one</div><div>two</div><p>three</p><br>four<li>five</li>"}, "one two three four five"},
		{"inline elements join", &model.Message{HTMLBody: "<p>wi<b>r</b>e <span>fu</span>nds</p>"}, "wire funds"},
		{"attributes are not text", &model.Message{HTMLBody: `<a href="https://x.example/attrword" title="titleattr">link</a><img alt="altword">`}, "link"},
		{"comments are not text", &model.Message{HTMLBody: "<p>a<!-- commentword -->b</p>"}, "ab"},
		{"textarea and xmp text is shown", &model.Message{HTMLBody: "<p><textarea>typed &amp; kept</textarea></p><p><xmp>raw <b>xmp</b></xmp></p>"}, "typed & kept raw <b>xmp</b>"},
		{"full document", &model.Message{HTMLBody: `<!DOCTYPE html><html><head><title>T</title></head><body><h1>Head</h1><p>Para one.</p><table><tr><td>c1</td><td>c2</td></tr></table></body></html>`}, "Head Para one. c1 c2"},
		{"plain body wins", &model.Message{PlainBody: "  plain\n\ntext ", HTMLBody: "<p>html</p>"}, "plain text"},
		{"rtf last", &model.Message{RTFBody: " rtf\ttext "}, "rtf text"},
		{"template content is inert", &model.Message{HTMLBody: "<p>a</p><template><p>tplword</p></template><p>b</p>"}, "a b"},
		{"a title in the body is not shown", &model.Message{HTMLBody: "<p>a</p><title>bodytitle</title><p>b</p>"}, "a b"},
	}
	for _, c := range cases {
		got := BodyText(c.m)
		assure.Reached(t, got, c.name)
		if got != c.want {
			t.Errorf("%s: BodyText = %q, want %q", c.name, got, c.want)
		}
	}
}

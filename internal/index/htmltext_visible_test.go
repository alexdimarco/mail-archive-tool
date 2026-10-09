package index

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/net/html"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// These tests pin what the full-text extractor indexes from an HTML body: the
// words a reader of the archived page can see. They were written against the
// review findings RV-3 (a foreign-content raw-text trick hid visible words) and
// RV-4 (a <head> left without </head> swallowed the whole body), in
// docs/review-sanitizer-roundtrip-adversarial.md.

func htmlText(h string) string { return BodyText(&model.Message{HTMLBody: h}) }

// ftsWords splits text the way the FTS index does (unicode61: runs of letters
// and digits), so a test sees what `search` would match — markup indexed as
// text counts as its words ("alt", "src").
func ftsWords(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func hasWord(text, w string) bool {
	for _, f := range ftsWords(text) {
		if strings.EqualFold(f, w) {
			return true
		}
	}
	return false
}

// deepWrap nests inner n <div>s deep. Past 512 open elements the x/net tree
// parser refuses the document, so this is how a test reaches the fallback.
func deepWrap(n int, inner string) string {
	return strings.Repeat("<div>", n) + inner + strings.Repeat("</div>", n)
}

// requireTooDeep proves the input really takes the fallback path: the tree
// parser must refuse it.
func requireTooDeep(t *testing.T, h string) {
	t.Helper()
	if _, err := html.Parse(strings.NewReader(h)); err == nil {
		t.Fatalf("fixture is not deep enough to reach the fallback extractor (the tree parser accepted it)")
	}
}

type visCase struct {
	name   string
	html   string
	want   []string // words that must be indexed
	absent []string // words that must not be
}

// visibleProblems extracts a case's text and lists every expectation it
// breaks.
func visibleProblems(c visCase) (got string, problems []string) {
	got = htmlText(c.html)
	for _, w := range c.want {
		if !hasWord(got, w) {
			problems = append(problems, fmt.Sprintf("%s: %q is visible on the page but not indexed; text=%q", c.name, w, clipText(got)))
		}
	}
	for _, w := range c.absent {
		if hasWord(got, w) {
			problems = append(problems, fmt.Sprintf("%s: %q is not visible on the page but was indexed; text=%q", c.name, w, clipText(got)))
		}
	}
	return got, problems
}

func clipText(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// covers: MA-488, R8
// RV-3: an SVG <style> is not a raw-text element (the tree parser and browsers
// read its content as markup), so `<svg><style><!--</style><script>--></style>`
// is one comment inside one SVG style. The old extractor tokenized it as HTML:
// it left the style at the comment's `</style>`, entered script data at
// `<script>` and never left it, so every word after it — which the archived
// page shows — was missing from the index. Those words now count, and so do
// the words after the other foreign-content raw-text tricks. Positive twin: the
// content of HTML and SVG <style>/<script> and of <title> is still not indexed.
func TestBodyTextKeepsWordsAfterForeignRawTextTrick(t *testing.T) {
	cases := []visCase{
		{
			name: "finder payload (adv2-scripts/mkhide.py)",
			html: `<html><body><p>Hello team</p><svg width="0" height="0"><style><!--</style><script>--></style></svg><p>please wire funds to account kestreleleven today</p></body></html>`,
			want: []string{"Hello", "team", "please", "wire", "funds", "kestreleleven", "today"},
		},
		{
			name: "skeptic payload",
			html: `<p>Hello skepvisible</p><svg width=0 height=0><style><!--</style><script>--></style></svg><p>please wire funds to account skephidden today</p>`,
			want: []string{"skepvisible", "skephidden", "wire"},
		},
		{
			name: "svg title is not RCDATA in foreign content",
			html: `<svg><title><!--</title><style>--></title></svg><p>aftersvgtitle</p>`,
			want: []string{"aftersvgtitle"},
		},
		{
			name: "math style is not raw text either",
			html: `<math><style><!--</style><script>--></style></math><p>aftermathstyle</p>`,
			want: []string{"aftermathstyle"},
		},
		{
			name: "an html end tag closes the foreign content it is open around",
			html: `<div><svg><style></div><p>afterdivclose</p>`,
			want: []string{"afterdivclose"},
		},
		{
			name: "positive twin: style, script and title content stay out",
			html: `<html><head><title>titleword</title><style>.a{color:red} styleword</style><script>scriptword()</script></head>` +
				`<body><p>bodyword</p><style>bodystyleword</style><script>bodyscriptword</script>` +
				`<svg><style>.c{fill:red} svgstyleword</style><script>svgscriptword</script><text>svgtextword</text></svg><p>tailword</p></body></html>`,
			want:   []string{"bodyword", "svgtextword", "tailword"},
			absent: []string{"titleword", "styleword", "scriptword", "bodystyleword", "bodyscriptword", "svgstyleword", "svgscriptword"},
		},
	}
	for _, c := range cases {
		got, problems := visibleProblems(c)
		assure.Reached(t, got, c.name+": extracted text")
		for _, p := range problems {
			t.Error(p)
		}
	}
}

// covers: MA-489, R8
// RV-4: `</head>` is optional, and the parser ends the head at `<body>` or at
// the first body-content start tag or text. The old extractor skipped from
// `<head>` until a `</head>`, `</style>` or `</script>`, so an ordinary,
// valid mail without `</head>` had no body words in the live index (while
// `reindex -rebuild`, which reads the body div, had them). Positive twin: the
// head's title, style and script are still not indexed, with or without
// `</head>`.
func TestBodyTextIndexesBodyAfterUnclosedHead(t *testing.T) {
	cases := []visCase{
		{
			name: "finder payload without </head> (adv2-scripts/mknohead.py)",
			html: `<html><head><meta name="viewport" content="width=device-width"><body><p>hello kestreltwelve</p></body></html>`,
			want: []string{"hello", "kestreltwelve"},
		},
		{
			name: "finder payload with </head>",
			html: `<html><head><meta name="viewport" content="width=device-width"></head><body><p>hello kestrelthirteen</p></body></html>`,
			want: []string{"hello", "kestrelthirteen"},
		},
		{
			name: "skeptic: title then body, no </head>",
			html: `<head><title>t</title><body><p>hello skepnoheadtitleword</p>`,
			want: []string{"skepnoheadtitleword"},
		},
		{
			name: "head closed by body content, no <body> tag",
			html: `<html><head><meta charset="utf-8"><p>impliedbodyword</p>`,
			want: []string{"impliedbodyword"},
		},
		{
			name: "head closed by text",
			html: `<head><style>p{}</style>headtextword <b>boldword</b>`,
			want: []string{"headtextword", "boldword"},
		},
		{
			name:   "positive twin: head metadata stays out (with </head>)",
			html:   `<html><head><title>headtitleword</title><style>headstyleword{}</style><script>headscriptword()</script><meta name="x" content="metaword"></head><body><p>bodyword</p></body></html>`,
			want:   []string{"bodyword"},
			absent: []string{"headtitleword", "headstyleword", "headscriptword", "metaword"},
		},
		{
			name:   "positive twin: head metadata stays out (without </head>)",
			html:   `<head><title>headtitleword2</title><style>headstyleword2{}</style><script>headscriptword2()</script><body><p>bodyword2</p>`,
			want:   []string{"bodyword2"},
			absent: []string{"headtitleword2", "headstyleword2", "headscriptword2"},
		},
	}
	for _, c := range cases {
		got, problems := visibleProblems(c)
		assure.Reached(t, got, c.name+": extracted text")
		for _, p := range problems {
			t.Error(p)
		}
	}
}

// covers: MA-490, R8
// A body nested past the tree parser's 512-element cap is read by the fallback
// tokenizer, which must not repeat either bug: one skip counter that any
// `</style>`, `</script>` or `</head>` decremented, raw-text mode entered on
// <style>/<script> inside SVG/MathML, and a head skip that only `</head>` ended.
// It keeps a per-element stack (only `</style>` ends an SVG style, only
// `</script>` an SVG script, and a head ends at body content), tracks foreign
// content (integration points, breakout tags, HTML end tags that close it,
// including a stray `</p>` or `</br>`, which x/net's in-body rules turn into a
// breakout start tag) and tells the tokenizer when a raw-text name inside
// foreign content is not raw text. Each fixture is proven to reach the
// fallback (the tree parser refuses it).
func TestBodyTextFallbackForDeepBodiesKeepsVisibleWords(t *testing.T) {
	const depth = 600
	cases := []visCase{
		{
			name: "RV-3 payload, deep",
			html: `<html><body>` + deepWrap(depth, `<p>Hello team</p><svg width="0" height="0"><style><!--</style><script>--></style></svg><p>please wire funds to account deepkestrel today</p>`) + `</body></html>`,
			want: []string{"Hello", "wire", "deepkestrel", "today"},
		},
		{
			name: "RV-4 payload, deep",
			html: `<html><head><meta name="viewport" content="width=device-width"><body>` + deepWrap(depth, `<p>hello deepnohead</p>`) + `</body></html>`,
			want: []string{"hello", "deepnohead"},
		},
		{
			name:   "head closed by body content, deep",
			html:   `<html><head><title>deeptitleword</title><p>deepimplied</p>` + deepWrap(depth, `x`),
			want:   []string{"deepimplied"},
			absent: []string{"deeptitleword"},
		},
		{
			name:   "only </style> ends an SVG style",
			html:   deepWrap(depth, `<svg><style>svgcssone</script>svgcsstwo</style><text>svgshownone</text></svg>`),
			want:   []string{"svgshownone"},
			absent: []string{"svgcssone", "svgcsstwo"},
		},
		{
			name:   "only </script> ends an SVG script",
			html:   deepWrap(depth, `<svg><script>svgjsone</style>svgjstwo</script><text>svgshowntwo</text></svg>`),
			want:   []string{"svgshowntwo"},
			absent: []string{"svgjsone", "svgjstwo"},
		},
		{
			name: "an HTML end tag closes the foreign content, deep",
			html: deepWrap(depth, `<div><svg><style></div><p>deepdivclose</p>`),
			want: []string{"deepdivclose"},
		},
		{
			name: "foreignObject is an HTML integration point",
			html: deepWrap(depth, `<svg><foreignObject><style><!--</style><p>foshown</p>--></style></foreignObject></svg>`),
			want: []string{"foshown"},
		},
		{
			name:   "MathML text integration point",
			html:   deepWrap(depth, `<math><mi><style>mathcss</style>mathshown</mi></math><p>mathafter</p>`),
			want:   []string{"mathshown", "mathafter"},
			absent: []string{"mathcss"},
		},
		{
			// x/net hands an unmatched `</p>` in foreign content to the in-body
			// rules, which imply a <p>: a breakout tag. So the SVG closes, the
			// <style> after it is HTML, and the paragraph is shown.
			name: "`</p>` with no <p> open breaks out of SVG, deep (adv2 rv5/mkbr.py)",
			html: deepWrap(depth, `<p>Hello brvisibleone</p><svg width=0 height=0></p><style><!--</style><p>please wire funds to brhiddenp today</p>--></style></svg><p>brtailone</p>`),
			want: []string{"brvisibleone", "wire", "brhiddenp", "today", "brtailone"},
		},
		{
			// `</br>` is read as a <br> start tag, also a breakout tag.
			name: "`</br>` breaks out of SVG, deep (adv2 rv5/mkbr.py)",
			html: deepWrap(depth, `<p>Hello brvisibletwo</p><svg width=0 height=0></br><style><!--</style><p>please wire funds to brhiddenbr today</p>--></style></svg><p>brtailtwo</p>`),
			want: []string{"brvisibletwo", "brhiddenbr", "brtailtwo"},
		},
		{
			// A breakout tag, or an HTML end tag, pops a hiding SVG <style>, and
			// the block boundary of that tag is shown: the words on either side
			// stay two words, as in the tree.
			name:   "a tag that closes a hiding SVG style keeps its word boundary, deep",
			html:   deepWrap(depth, `<p>alpha<svg><style>brcss</br>bravo</p><p>charlie<svg><style>brcss2<p>delta</p><div>echo<svg><style>brcss3</div>foxtrot`),
			want:   []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot"},
			absent: []string{"brcss", "brcss2", "brcss3", "alphabravo", "charliedelta", "echofoxtrot"},
		},
		{
			// Positive twins: an unmatched end tag that is not p or br stays in
			// the SVG, so its style still hides; and `</p>` at a foreignObject
			// (an integration point) does not leave the outer SVG.
			name:   "positive twin: other unmatched end tags stay in SVG, deep",
			html:   deepWrap(depth, `<svg></div><style>svgdivcss</style><text>svgdivshown</text></svg><svg><foreignObject><svg></p><style>focss</style>fotextword</svg></foreignObject><style>outercss</style></svg>`),
			want:   []string{"svgdivshown", "fotextword"},
			absent: []string{"svgdivcss", "focss", "outercss"},
		},
		{
			name:   "positive twin: HTML style, script and title stay out, deep",
			html:   `<html><head><title>dtitleword</title><style>dstyleword</style></head><body>` + deepWrap(depth, `<p>dbodyword</p><style>dbodystyle</style><script>dbodyscript</script>`) + `</body></html>`,
			want:   []string{"dbodyword"},
			absent: []string{"dtitleword", "dstyleword", "dbodystyle", "dbodyscript"},
		},
	}
	for _, c := range cases {
		requireTooDeep(t, c.html)
		got, problems := visibleProblems(c)
		assure.Reached(t, got, c.name+": extracted text")
		for _, p := range problems {
			t.Error(p)
		}
	}
}

// covers: MA-491, R8
// <noscript> is read both ways: the archive ships no script, so a reader with
// JavaScript off (and the mail client that first showed the mail, which never
// runs scripts) sees its content as markup, while one with JavaScript on does
// not see it at all. Its content is indexed as the scripting-off reading parses
// it — the words, never its tags or attribute values as words — and appended
// after the scripting-on text, so it never splits a word that reading shows
// (`wi<noscript>x</noscript>re` reads "wire" with JavaScript on). A noscript
// inside an explicit <head> is not indexed, like the rest of that head: the
// exporter moves the mail's head into the page's <head>, which `reindex
// -rebuild` does not read, so indexing it would make the live index and the
// rebuilt one disagree (R8). The fallback extractor reads noscript the same way.
func TestBodyTextReadsNoscriptAsMarkup(t *testing.T) {
	cases := []visCase{
		{
			name:   "body noscript: words, not markup",
			html:   `<p>before</p><noscript><p>nsbodyword</p><img src="https://t.example/p.gif" alt="nsaltword"></noscript><p>after</p>`,
			want:   []string{"before", "nsbodyword", "after"},
			absent: []string{"nsaltword", "src", "gif", "img"},
		},
		{
			name: "noscript never splits a word the scripting-on reading shows",
			html: `<p>wi<noscript>x</noscript>re</p>`,
			want: []string{"wire", "x"},
		},
		{
			name:   "noscript in an explicit head stays out (the rebuild cannot see it)",
			html:   `<html><head><noscript><p>headnsword</p></noscript></head><body><p>bodyns</p></body></html>`,
			want:   []string{"bodyns"},
			absent: []string{"headnsword"},
		},
		{
			name:   "style inside noscript stays out",
			html:   `<p>v</p><noscript><style>nsstyleword</style><p>nsshown</p></noscript>`,
			want:   []string{"nsshown"},
			absent: []string{"nsstyleword"},
		},
		{
			name:   "deep body noscript (fallback extractor)",
			html:   deepWrap(600, `<p>before</p><noscript><p>nsdeepword</p><img alt="nsdeepalt"></noscript>`),
			want:   []string{"before", "nsdeepword"},
			absent: []string{"nsdeepalt", "img"},
		},
	}
	for _, c := range cases {
		got, problems := visibleProblems(c)
		assure.Reached(t, got, c.name+": extracted text")
		for _, p := range problems {
			t.Error(p)
		}
	}
}

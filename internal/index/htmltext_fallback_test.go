package index

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// fallbackCorpus is markup both extractors can read (shallow enough for the
// tree parser): the review payloads, their positive twins and ordinary mail.
var fallbackCorpus = []string{
	`<html><body><p>Hello team</p><svg width="0" height="0"><style><!--</style><script>--></style></svg><p>please wire funds to account kestreleleven today</p></body></html>`,
	`<p>Hello skepvisible</p><svg width=0 height=0><style><!--</style><script>--></style></svg><p>please wire funds to account skephidden today</p>`,
	`<svg><title><!--</title><style>--></title></svg><p>aftersvgtitle</p>`,
	`<math><style><!--</style><script>--></style></math><p>aftermathstyle</p>`,
	`<div><svg><style></div><p>afterdivclose</p>`,
	`<svg><style>svgcssone</script>svgcsstwo</style><text>svgshownone</text></svg>`,
	`<svg><script>svgjsone</style>svgjstwo</script><text>svgshowntwo</text></svg>`,
	`<svg><foreignObject><style><!--</style><p>foshown</p>--></style></foreignObject></svg>`,
	`<svg><foreignObject><div><p>in fo</p></div></foreignObject><text>svg text</text></svg><p>after</p>`,
	`<math><mi><style>mathcss</style>mathshown</mi></math><p>mathafter</p>`,
	`<math><annotation-xml encoding="text/html"><p>annot html</p><style>annotcss</style></annotation-xml></math>`,
	`<svg><desc>a <b>bold</b> desc</desc><p>breakout</p>tail</svg>`,
	`<svg><font color="red">font breaks out</font></svg><p>x</p>`,
	`<svg><![CDATA[cdata text]]></svg>`,
	`<p>Hello brvisibleone</p><svg width=0 height=0></p><style><!--</style><p>please wire funds to brhiddenp today</p>--></style></svg><p>brtailone</p>`,
	`<p>Hello brvisibletwo</p><svg width=0 height=0></br><style><!--</style><p>please wire funds to brhiddenbr today</p>--></style></svg><p>brtailtwo</p>`,
	`<p>in scope<svg></p><style><!--</style><p>pinscope</p>--></style></svg>`,
	`<math><mi></p><style>mcss</style>mtext</mi></math><math><annotation-xml></br>annotbr</annotation-xml></math>`,
	`<p>alpha<svg><style>brcss</br>bravo</p><p>charlie<svg><style>brcss2<p>delta</p><div>echo<svg><style>brcss3</div>foxtrot`,
	`<svg></div><style>svgdivcss</style><text>svgdivshown</text></svg><svg><foreignObject><svg></p><style>focss</style>fotextword</svg></foreignObject><style>outercss</style></svg>`,
	`<p><button><svg></p><style>bcss</style>btext</svg></button>`,
	`<html><head><meta name="viewport" content="width=device-width"><body><p>hello kestreltwelve</p></body></html>`,
	`<html><head><meta name="viewport" content="width=device-width"></head><body><p>hello kestrelthirteen</p></body></html>`,
	`<head><title>t</title><body><p>hello skepnoheadtitleword</p>`,
	`<html><head><meta charset="utf-8"><p>impliedbodyword</p>`,
	`<head><style>p{}</style>headtextword <b>boldword</b>`,
	`<head><template><p>in head template</p></template><title>t</title></head><p>body</p>`,
	`<html><head><title>headtitleword</title><style>headstyleword{}</style><script>headscriptword()</script><meta name="x" content="metaword"></head><body><p>bodyword</p></body></html>`,
	`<!DOCTYPE html><!-- c --><html lang="en"><head><noscript><p>headns</p></noscript></head><body><p>b</p></body></html>`,
	`<noscript><p>implied head noscript</p></noscript><p>body</p>`,
	"<html>\n<head>\n<noscript><p>nlheadnsword</p></noscript>\n</head>\n<body>\n<p>nlbodyword</p>\n</body>\n</html>",
	"<!DOCTYPE html>\n<!-- preheader -->\n<html lang=\"en\">\n  <head>\n    <meta charset=\"utf-8\">\n    <noscript><p>litheadnsword</p></noscript>\n  </head>\n  <body>\n    <p>litbodyword</p>\n  </body>\n</html>",
	"\n<noscript><p>divstartnsword</p></noscript><p>divstartbody</p>",
	`<p>before</p><noscript><p>nsbodyword</p><img src="https://t.example/p.gif" alt="nsaltword"></noscript><p>after</p>`,
	`<p>wi<noscript>x</noscript>re</p>`,
	`<p>v</p><noscript><style>nsstyleword</style><p>nsshown</p></noscript>`,
	`<p>a</p><template><p>tplword</p></style></template><p>b</p><title>bodytitle</title>`,
	"<p>  a \n\t b  </p><p>Fish &amp; chips&nbsp;now &lt;3 &#x41;</p>",
	"<div>one</div><div>two</div><p>three</p><br>four<li>five</li><p>wi<b>r</b>e <span>fu</span>nds</p>",
	`<a href="https://x.example/attrword" title="titleattr">link</a><img alt="altword"><p>a<!-- commentword -->b</p>`,
	"<p><textarea>typed &amp; kept</textarea></p><p><xmp>raw <b>xmp</b></xmp></p><iframe><p>iframe raw</p></iframe>",
	`<!DOCTYPE html><html><head><title>Digest</title><style>body{margin:0}</style></head><body><center>` +
		`<table role="presentation" width="100%"><tr><td style="padding:12px"><h2>Weekly digest &amp; news</h2>` +
		`<p>We shipped <b>faster search</b>, <a href="https://example.com/x">new filters</a> and a&nbsp;redesigned inbox.</p>` +
		`<ul><li>Item one with <em>emphasis</em></li><li>Item two</li></ul><!--[if mso]><table><tr><td><![endif]-->` +
		`<img src="https://example.com/i.png" alt="banner"></td></tr></table></center></body></html>`,
}

// covers: MA-490, R8
// The fallback extractor (for bodies past the tree parser's 512-element cap)
// reads markup the way the tree walk does: on every corpus entry — the RV-3
// and RV-4 payloads, the foreign-content integration points, breakout tags,
// CDATA, explicit and implied heads, noscript, template and ordinary mail — the
// two produce the same text. So a mail nested one level too deep for the
// parser loses none of its words and gains none (the same words would be found
// at depth 10).
func TestFallbackExtractorAgreesWithTree(t *testing.T) {
	for _, h := range fallbackCorpus {
		tree, ok := treeText(h)
		if !ok {
			t.Fatalf("corpus entry is too deep for the tree parser: %q", h)
		}
		assure.Reached(t, tree, "tree text")
		if fb := tokenText(h, true); fb != tree {
			t.Errorf("fallback and tree disagree on %q:\n tree     %q\n fallback %q", clipText(h), tree, fb)
		}
	}
	// The same through htmlToText, nested past the cap: the deep copy reads as
	// the shallow one does, plus nothing.
	for _, h := range fallbackCorpus {
		if strings.Contains(strings.ToLower(h), "<head") || strings.Contains(strings.ToLower(h), "<html") || strings.HasPrefix(h, "<noscript") {
			continue // a document prologue cannot be nested
		}
		shallow := htmlToText(h)
		deep := deepWrap(600, h)
		requireTooDeep(t, deep)
		if got := htmlToText(deep); got != shallow {
			t.Errorf("deep copy reads differently from %q:\n shallow %q\n deep    %q", clipText(h), shallow, got)
		}
	}
}

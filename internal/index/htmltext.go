package index

import (
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"mail-archive-tool/internal/model"
)

// BodyText returns the best plain-text representation of a message for indexing
// and snippets: the plain body if present, otherwise text extracted from the
// HTML body, otherwise the decoded RTF.
func BodyText(m *model.Message) string {
	if s := strings.TrimSpace(m.PlainBody); s != "" {
		return normalizeWS(s)
	}
	if s := strings.TrimSpace(m.HTMLBody); s != "" {
		return htmlToText(s)
	}
	return normalizeWS(m.RTFBody)
}

// blockElements introduce a word boundary so adjacent text doesn't run
// together. The set is the flattener's flatBlock (internal/export): a browser
// shows each on its own line, and the exporter writes a newline where it drops
// one past the depth cap, so the live index (which reads the original mail) and
// `reindex -rebuild` (which reads the flattened page) split words at the same
// names — including dt/dd/dl/pre/address/..., which an earlier, narrower set
// ran together (`<dt>a</dt><dd>b</dd>` now reads "a b", a better ordinary-mail
// word boundary and exact live/rebuilt parity for those names; R8).
var blockElements = map[string]bool{
	// the browser block-level containers the flattener also treats as block
	"p": true, "br": true, "div": true, "li": true, "tr": true, "td": true, "th": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true,
	"table": true, "blockquote": true, "section": true, "article": true, "header": true, "footer": true,
	"address": true, "aside": true, "caption": true, "center": true, "dd": true, "details": true,
	"dialog": true, "dir": true, "dl": true, "dt": true, "fieldset": true, "figcaption": true,
	"figure": true, "form": true, "hgroup": true, "legend": true, "listing": true, "main": true,
	"menu": true, "nav": true, "optgroup": true, "option": true, "pre": true, "search": true,
	"summary": true, "tbody": true, "tfoot": true, "thead": true,
}

// BlockElementNames lists the names blockElements splits words at, sorted. It
// exists for one test: the set must be the exporter's flatBlock (listed by
// internal/export FlatBlockNames), and neither package can see the other's, so
// an external test of internal/export, which imports both, compares the two
// lists (MA-477).
func BlockElementNames() []string {
	var names []string
	for name, block := range blockElements {
		if block {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// htmlToText returns the collapsed text a reader of the archived page sees,
// for the full-text index and the snippet. It reads the body the way the
// exporter and browsers do — the x/net tree parser, scripting on — so that what
// the index holds is what the page shows (R8): a bare tokenizer reads
// <style>/<script>/<title> inside SVG or MathML as raw text, which the parser
// does not, and a hostile mail used that to hide words the page renders (RV-3).
//
// Not indexed, because no reading of the page shows them: <title>, <style>,
// <script> (HTML or SVG) and <template>. The head is not indexed either,
// explicit OR implied, because the exporter moves the mail's head (its implied
// head included) into the page's own <head>, which `reindex -rebuild` does not
// read; so a fragment that starts with a text-bearing <noscript> or <noframes>
// is skipped on both sides, not indexed live only (the old implied-head drift).
// The rebuild wraps the body div's inner HTML in <body> before handing it here
// (internal/app/htmlheader.go), so a mail whose own <body> starts with a
// <noscript> is body content on both sides, indexed on both. A <noscript>'s
// content in the body IS indexed, as markup: the archive ships no script, so a
// reader with JavaScript off — and the mail client the mail was first read in
// — sees it (see noscriptText). The conventions are those of the tokenizer this
// replaces: whitespace collapses, entities are decoded, block elements
// separate words, attribute values and comments are not text.
//
// The tree parser refuses documents nested past 512 open elements; those (and
// only those) go to tokenText, a stack-tracking tokenizer reading them the
// same way.
func htmlToText(h string) string {
	if s, ok := treeText(h); ok {
		return s
	}
	return tokenText(h, true)
}

// textSink collects a body's visible text. main is the scripting-on reading,
// in document order. extra holds each <noscript>'s content as the
// scripting-off reading parses it, kept apart and appended so that it never
// splits a word the scripting-on reading shows (`wi<noscript>x</noscript>re`
// reads "wire" with JavaScript on).
type textSink struct {
	main, extra strings.Builder
}

func (s *textSink) String() string {
	if s.extra.Len() == 0 {
		return normalizeWS(s.main.String())
	}
	return normalizeWS(s.main.String() + " " + s.extra.String())
}

func (s *textSink) noscript(raw string) {
	if t := noscriptText(raw); t != "" {
		s.extra.WriteByte(' ')
		s.extra.WriteString(t)
	}
}

// treeText extracts the visible text from the parse tree; ok=false when the
// parser refuses the input.
func treeText(h string) (string, bool) {
	doc, err := html.Parse(strings.NewReader(h))
	if err != nil {
		return "", false
	}
	var s textSink
	walkChildren(doc, &s, true, true) // skip the head, explicit or implied (see htmlToText)
	return s.String(), true
}

// walkChildren writes n's visible descendants into s. scripting names the
// reading that built the tree: with it on, a <noscript> holds one raw text
// node, read again by noscriptText; with it off (inside noscriptText) the
// noscript's children are markup and are walked as they are. skipHead says to
// skip a <head> element, explicit or implied (the extractor always does, for
// the whole document; see htmlToText).
func walkChildren(n *html.Node, s *textSink, scripting, skipHead bool) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.TextNode:
			s.main.WriteString(c.Data)
		case html.ElementNode:
			walkElement(c, s, scripting, skipHead)
		}
	}
}

func walkElement(e *html.Node, s *textSink, scripting, skipHead bool) {
	switch e.Namespace {
	case "":
		switch e.DataAtom {
		case atom.Head:
			if skipHead {
				return
			}
		case atom.Title, atom.Style, atom.Script, atom.Template:
			return
		case atom.Noscript:
			if scripting {
				var raw strings.Builder
				for c := e.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.TextNode {
						raw.WriteString(c.Data)
					}
				}
				s.noscript(raw.String())
				return
			}
		}
	case "svg":
		// SVG <style> is CSS and SVG <script> is code; neither is rendered.
		// MathML has no such elements: a <style> in MathML is an unknown
		// element whose text a browser may show, so it is indexed.
		if e.Data == "style" || e.Data == "script" {
			return
		}
	}
	block := blockElements[e.Data]
	if block {
		s.main.WriteByte(' ')
	}
	walkChildren(e, s, scripting, skipHead)
	if block {
		s.main.WriteByte(' ')
	}
}

// noscriptText reads a <noscript>'s raw content (what the scripting-on
// reading holds) the way a reader with JavaScript off parses it: as markup in
// a body, scripting off. The scripting-on reading of the rest of the page is
// unchanged by this; the content's words are what is added.
func noscriptText(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragmentWithOptions(strings.NewReader(raw), ctx, html.ParseOptionEnableScripting(false))
	if err != nil {
		return tokenText(raw, false)
	}
	var s textSink
	for _, n := range nodes {
		switch n.Type {
		case html.TextNode:
			s.main.WriteString(n.Data)
		case html.ElementNode:
			walkElement(n, &s, false, false)
		}
	}
	return s.String()
}

// htmlSpace reports whether b is only HTML whitespace (space, tab, LF, FF, CR).
func htmlSpace(b []byte) bool {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\n', '\f', '\r':
		default:
			return false
		}
	}
	return true
}

// --- the fallback: a tokenizer that tracks the parser's stack ---
//
// tokenText reads markup the tree parser refuses: x/net stops at 512 open
// elements, browsers do not, and such a mail is shown on its page as escaped
// source (export.FallbackTooDeep), so the live index and `reindex -rebuild`
// both reach this with the mail's own bytes. It follows the same rules as the
// tree walk, on a stack of open elements kept the way x/net keeps its own:
//
//   - only an element's own end tag (or a parser rule that pops it) ends what
//     it hides: `</script>` never ends an SVG <style>, a stray `</style>` never
//     ends anything, and an explicit <head> ends at `</head>` or at the first
//     token that is not head content (a body start tag, text, `</body>`);
//   - SVG and MathML are tracked as foreign content, with x/net's integration
//     points and breakout tags; inside it, a raw-text name (<style>, <title>,
//     <noscript>…) is an ordinary element, so the tokenizer is told
//     NextIsNotRawText, as the parser tells it, and CDATA is allowed;
//   - an HTML end tag closes the foreign content open above its element when
//     the parser's in-body rules would (default scope for block end tags; the
//     nearest special element for the rest), and `</br>` and `</p>` always
//     leave it: the in-body rules read `</br>` as a <br> start tag and let a
//     `</p>` with no <p> open imply one, and both are breakout tags;
//   - a block tag's word boundary is written after the pops it causes, so a
//     tag that closes a hiding element (an SVG <style>) shows its boundary;
//   - `</form>` takes the form off the stack and leaves what is above it open,
//     and a misnested formatting end tag pops what the adoption agency pops
//     (see detach), so foreign content above them stays or goes as in x/net.
//
// What it does not model — table insertion modes, implied end tags other than
// `</form>`'s, the reconstruction of formatting elements, the adoption
// agency's reparenting, <frameset> — mostly moves text or closes ordinary
// elements. In contrived markup it can still change what is hidden: a random
// differential against the tree walk finds words hidden only here around a
// <frameset> and in table modes around foreign content. It also finds them
// where x/net drops a <template> opened in an SVG or MathML integration point
// and shows its text, which browsers keep inert. Tracking stops at
// maxOpenElements open elements.
const maxOpenElements = 1 << 20

func tokenText(h string, scripting bool) string {
	w := &tokenWalker{
		z:          html.NewTokenizer(strings.NewReader(h)),
		scripting:  scripting,
		htmlPos:    map[string][]int{},
		foreignPos: map[string][]int{},
	}
	if !scripting {
		// noscript content: a body fragment, which has no head phase.
		w.phase = phaseBody
	}
	w.run()
	return w.sink.String()
}

type elemNS uint8

const (
	nsHTML elemNS = iota
	nsSVG
	nsMath
)

type openElem struct {
	name    string
	ns      elemNS
	hides   bool // <template>, an explicit <head>, SVG <style>/<script>: the content is never shown
	special bool // x/net's "special" category: an in-body "any other end tag" walk stops here
	scope   bool // a default-scope boundary: a scoped end tag cannot reach below it
	ip      bool // HTML integration point: start tags and text inside follow HTML rules
	mtip    bool // MathML text integration point (mi, mo, mn, ms, mtext)
	annXML  bool // MathML annotation-xml: an <svg> start tag inside follows HTML rules
	runRoot bool // the lowest element of a contiguous run of foreign elements
	gone    bool // taken off x/net's stack (see detach) but kept here as a placeholder
}

const (
	phaseInitial = iota // before the document's head is known
	phaseHead           // inside an explicit <head>
	phaseBody
)

type rawKind uint8

const (
	rawNone     rawKind = iota
	rawHidden           // the raw text of <title>, <style>, <script>
	rawShown            // <textarea>, <xmp>, <iframe>, <noembed>, <noframes>, <plaintext>: kept as the tree keeps it
	rawNoscript         // a scripting-on <noscript>'s content
)

type tokenWalker struct {
	z          *html.Tokenizer
	scripting  bool
	phase      int
	stack      []openElem
	htmlPos    map[string][]int // per tag name, the stack indexes of open HTML elements
	foreignPos map[string][]int // the same for SVG/MathML elements
	specials   []int            // stack indexes of special elements (a placeholder's may linger; see topSpecial)
	scopes     []int            // stack indexes of default-scope boundaries
	runs       []int            // stack indexes of foreign run roots
	hidden     int              // open elements that hide their content
	raw        rawKind          // what the next text token is, after a raw-text start tag
	sink       textSink
}

func (w *tokenWalker) run() {
	for {
		top := w.top()
		w.z.AllowCDATA(top != nil && top.ns != nsHTML)
		tt := w.z.Next()
		raw := w.raw
		w.raw = rawNone
		switch tt {
		case html.ErrorToken:
			return
		case html.TextToken:
			w.text(raw)
		case html.StartTagToken, html.SelfClosingTagToken:
			w.startTag(tt == html.SelfClosingTagToken)
		case html.EndTagToken:
			w.endTag()
		}
	}
}

func (w *tokenWalker) top() *openElem {
	if len(w.stack) == 0 {
		return nil
	}
	return &w.stack[len(w.stack)-1]
}

func lastIndex(s []int) int {
	if len(s) == 0 {
		return -1
	}
	return s[len(s)-1]
}

func (w *tokenWalker) push(e openElem) {
	if len(w.stack) >= maxOpenElements {
		return
	}
	i := len(w.stack)
	if e.ns != nsHTML && (i == 0 || w.stack[i-1].ns == nsHTML) {
		e.runRoot = true
		w.runs = append(w.runs, i)
	}
	w.stack = append(w.stack, e)
	if e.ns == nsHTML {
		w.htmlPos[e.name] = append(w.htmlPos[e.name], i)
	} else {
		w.foreignPos[e.name] = append(w.foreignPos[e.name], i)
	}
	if e.special {
		w.specials = append(w.specials, i)
	}
	if e.scope {
		w.scopes = append(w.scopes, i)
	}
	if e.hides {
		w.hidden++
	}
}

func (w *tokenWalker) pop() {
	w.popOne()
	// A placeholder never becomes the current node: x/net's stack has no such
	// element, so the one below it is current.
	for t := w.top(); t != nil && t.gone; t = w.top() {
		w.popOne()
	}
}

func (w *tokenWalker) popOne() {
	i := len(w.stack) - 1
	e := w.stack[i]
	if !e.gone {
		pos := w.htmlPos
		if e.ns != nsHTML {
			pos = w.foreignPos
		}
		p := pos[e.name]
		pos[e.name] = p[:len(p)-1]
	}
	if n := len(w.specials); e.special && n > 0 && w.specials[n-1] == i {
		w.specials = w.specials[:n-1]
	}
	if e.scope {
		w.scopes = w.scopes[:len(w.scopes)-1]
	}
	if e.runRoot {
		w.runs = w.runs[:len(w.runs)-1]
	}
	if e.hides {
		w.hidden--
	}
	w.stack = w.stack[:i]
}

// detach takes the HTML element at stack index j off x/net's stack without
// closing what is above it, as the parser does for `</form>` and for the
// formatting element the adoption agency moves. j must be the last open
// element of its name. The entry stays in w.stack as a placeholder (gone), so
// the indexes above it keep their meaning, but it no longer counts as open,
// special, or the current node. Its entry in w.specials (a form's) is dropped
// lazily, by the readers, so a run of detaches costs no copying.
func (w *tokenWalker) detach(j int) {
	e := &w.stack[j]
	p := w.htmlPos[e.name]
	w.htmlPos[e.name] = p[:len(p)-1]
	e.gone = true
	if j == len(w.stack)-1 {
		w.pop()
	}
}

// topSpecial returns the stack index of the topmost special element still on
// x/net's stack, or -1. A placeholder's entry it passes is dropped, once.
func (w *tokenWalker) topSpecial() int {
	for n := len(w.specials); n > 0; n = len(w.specials) {
		if i := w.specials[n-1]; !w.stack[i].gone {
			return i
		}
		w.specials = w.specials[:n-1]
	}
	return -1
}

// specialsAbove counts the special elements above stack index j, up to limit.
// It drops the placeholders' entries it reads on the way, so each is read
// once, and it reads at most limit live ones: the cost stays linear in the
// input however the detaches and the queries interleave.
func (w *tokenWalker) specialsAbove(j, limit int) int {
	n := len(w.specials)
	k, i, count := n, n-1, 0
	for ; i >= 0 && w.specials[i] > j && count < limit; i-- {
		if w.stack[w.specials[i]].gone {
			continue
		}
		k--
		w.specials[k] = w.specials[i]
		count++
	}
	// [k, n) holds the live entries read, in order; [i+1, k) is free.
	if k > i+1 {
		copy(w.specials[i+1:], w.specials[k:n])
		w.specials = w.specials[:i+1+n-k]
	}
	return count
}

// popTo pops the element at index i and everything above it.
func (w *tokenWalker) popTo(i int) {
	for len(w.stack) > i {
		w.pop()
	}
}

func (w *tokenWalker) topIsHead() bool {
	t := w.top()
	return w.phase == phaseHead && t != nil && t.ns == nsHTML && t.name == "head"
}

func (w *tokenWalker) endHead() {
	if w.topIsHead() {
		w.pop()
	}
	w.phase = phaseBody
}

// inForeign mirrors x/net's inForeignContent for the current token.
func (w *tokenWalker) inForeign(tt html.TokenType, name string) bool {
	n := w.top()
	if n == nil || n.ns == nsHTML {
		return false
	}
	if n.mtip && (tt == html.TextToken || tt == html.StartTagToken && name != "mglyph" && name != "malignmark") {
		return false
	}
	if n.annXML && tt == html.StartTagToken && name == "svg" {
		return false
	}
	if n.ip && (tt == html.StartTagToken || tt == html.TextToken) {
		return false
	}
	return true
}

func (w *tokenWalker) text(raw rawKind) {
	t := w.z.Text() // consumes the token's data: read it once
	if raw == rawNone && (w.phase == phaseInitial || w.topIsHead()) {
		if htmlSpace(t) {
			return
		}
		w.endHead() // text implies (or ends) the head and starts the body
	}
	if w.hidden > 0 || raw == rawHidden {
		return
	}
	if raw == rawNoscript {
		w.sink.noscript(string(t))
		return
	}
	w.sink.main.Write(t)
}

func tagName(z *html.Tokenizer) (string, bool) {
	b, hasAttr := z.TagName()
	if a := atom.Lookup(b); a != 0 {
		return a.String(), hasAttr
	}
	return string(b), hasAttr
}

// headContent names the start tags x/net's inHeadIM keeps in the head.
var headContent = map[string]bool{
	"base": true, "basefont": true, "bgsound": true, "link": true, "meta": true,
	"title": true, "noscript": true, "noframes": true, "style": true, "script": true,
	"template": true, "head": true, "html": true,
}

func (w *tokenWalker) startTag(selfClosing bool) {
	name, hasAttr := tagName(w.z)
	var attrs map[string]string
	if hasAttr && (name == "font" || name == "annotation-xml") {
		attrs = map[string]string{}
		for more := true; more; {
			var k, v []byte
			k, v, more = w.z.TagAttr()
			attrs[string(k)] = string(v)
		}
	}
	switch {
	case w.phase == phaseInitial:
		switch name {
		case "html":
			return
		case "head":
			w.push(openElem{name: "head", hides: true, special: true})
			w.phase = phaseHead
			return
		}
		if headContent[name] {
			// Leading head content opens the implied head the parser creates;
			// the extractor skips every head (hides), explicit or implied, so
			// this matches the tree walk. The tag is processed in the head below.
			w.push(openElem{name: "head", hides: true, special: true})
			w.phase = phaseHead
		} else {
			w.phase = phaseBody
		}
	case w.topIsHead():
		if name == "html" || name == "head" {
			return
		}
		if !headContent[name] {
			w.endHead()
		}
	}
	if w.inForeign(html.StartTagToken, name) {
		_, color := attrs["color"]
		_, face := attrs["face"]
		_, size := attrs["size"]
		if !breakout[name] && !(name == "font" && (color || face || size)) {
			w.blockSpace(name)
			w.foreignStart(name, selfClosing, attrs)
			return
		}
		w.breakOut()
	}
	w.blockSpace(name)
	w.htmlStart(name, selfClosing)
}

// blockSpace writes the word boundary of a block element's tag, unless an open
// element hides it. It is called after the tag's pops, so a boundary that
// closes (or breaks out of) a hiding element is shown, as the tree shows it.
func (w *tokenWalker) blockSpace(name string) {
	if w.hidden == 0 && blockElements[name] {
		w.sink.main.WriteByte(' ')
	}
}

// breakOut pops the open foreign elements down to the nearest HTML element or
// integration point, as x/net does before it reads a breakout tag.
func (w *tokenWalker) breakOut() {
	for t := w.top(); t != nil && t.ns != nsHTML && !t.ip && !t.mtip; t = w.top() {
		w.pop()
	}
}

func (w *tokenWalker) foreignStart(name string, selfClosing bool, attrs map[string]string) {
	ns := w.top().ns
	// Inside foreign content no tag is raw text (an SVG <style> or <title> is
	// an ordinary element); x/net tells its tokenizer so for every element it
	// adds there, self-closing or not.
	w.z.NextIsNotRawText()
	if selfClosing {
		return
	}
	e := openElem{name: name, ns: ns}
	switch ns {
	case nsSVG:
		switch name {
		case "style", "script":
			e.hides = true
		case "foreignobject", "desc", "title":
			e.ip, e.special, e.scope = true, true, true
		}
	case nsMath:
		switch name {
		case "mi", "mo", "mn", "ms", "mtext":
			e.mtip, e.special, e.scope = true, true, true
		case "annotation-xml":
			e.annXML, e.special, e.scope = true, true, true
			enc := strings.ToLower(attrs["encoding"])
			e.ip = enc == "text/html" || enc == "application/xhtml+xml"
		}
	}
	w.push(e)
}

func (w *tokenWalker) htmlStart(name string, selfClosing bool) {
	switch name {
	case "svg", "math":
		if !selfClosing {
			ns := nsSVG
			if name == "math" {
				ns = nsMath
			}
			w.push(openElem{name: name, ns: ns})
		}
	case "html", "body", "head", "frameset":
		// ignored in the body
	case "title", "style", "script":
		w.raw = rawHidden
	case "textarea", "xmp", "iframe", "noembed", "noframes", "plaintext":
		w.raw = rawShown
	case "noscript":
		if w.scripting {
			w.raw = rawNoscript
			return
		}
		w.z.NextIsNotRawText()
		w.push(openElem{name: name, special: true})
	case "template":
		w.push(openElem{name: name, hides: true, special: true, scope: true})
	case "area", "base", "basefont", "bgsound", "br", "col", "embed", "frame", "hr", "image",
		"img", "input", "keygen", "link", "meta", "param", "source", "track", "wbr":
		// void
	case "caption", "colgroup", "tbody", "td", "tfoot", "th", "thead", "tr":
		// x/net ignores these outside a table
		if len(w.htmlPos["table"]) > 0 {
			w.push(openElem{name: name, special: true, scope: name == "caption" || name == "td" || name == "th"})
		}
	default:
		w.push(openElem{name: name, special: specialHTML[name], scope: scopeHTML[name]})
	}
}

func (w *tokenWalker) endTag() {
	name, _ := tagName(w.z)
	switch {
	case w.phase == phaseInitial:
		switch name {
		case "head", "body", "html", "br":
			w.phase = phaseBody
		default:
			return
		}
	case w.topIsHead():
		switch name {
		case "head":
			w.endHead()
			return
		case "body", "html", "br":
			w.endHead()
		case "template":
		default:
			return
		}
	}
	if w.inForeign(html.EndTagToken, name) {
		// x/net: pop to the nearest element of that name within the top run of
		// foreign elements; otherwise the in-body rules decide. For `</br>` and
		// `</p>` those leave the foreign content: `</br>` is read as a <br>
		// start tag, and `</p>` either pops to an open <p> (below the foreign
		// content) or implies a <p> start tag. <br> and <p> are breakout tags,
		// so no foreign element ever has either name.
		if j := lastIndex(w.foreignPos[name]); j >= 0 && j >= lastIndex(w.runs) {
			w.popTo(j)
			w.blockSpace(name)
			return
		}
		if name == "br" || name == "p" {
			w.breakOut()
		}
	}
	w.htmlEnd(name)
	w.blockSpace(name)
}

func (w *tokenWalker) htmlEnd(name string) {
	switch name {
	case "template":
		if j := lastIndex(w.htmlPos["template"]); j >= 0 {
			w.popTo(j)
		}
	case "head", "body", "html", "br":
		// ignored in the body (`</br>` is a <br>: a word boundary, written by
		// the caller)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		j := -1
		for _, h := range [...]string{"h1", "h2", "h3", "h4", "h5", "h6"} {
			j = max(j, lastIndex(w.htmlPos[h]))
		}
		w.popInScope(j)
	case "a", "b", "big", "code", "em", "font", "i", "nobr", "s", "small", "strike", "strong", "tt", "u":
		// The adoption agency. Out of scope, the end tag is ignored. With no
		// special element above the formatting element, it pops to it. With
		// some, each pass of its outer loop (eight at most) reparents across
		// the next one up; the pass that finds none above pops everything over
		// the topmost, and the formatting element itself leaves the stack.
		// With eight or more, the loop ends before that pop, and this model
		// leaves the stack alone. Only the pops matter for what is hidden:
		// the elements between are HTML, since any foreign content there
		// would put an integration point, a scope boundary, in the way.
		j := lastIndex(w.htmlPos[name])
		if j < 0 {
			w.popOther(name)
			return
		}
		if lastIndex(w.scopes) > j {
			return
		}
		switch above := w.specialsAbove(j, 8); {
		case above == 0:
			w.popTo(j)
		case above < 8:
			w.popTo(w.topSpecial() + 1)
			w.detach(j)
		}
	case "form":
		// x/net, with no <template> open: when the form is in scope, the
		// implied end tags are popped and the form is taken off the stack;
		// what is above it, foreign content included, stays open. Inside a
		// template it pops to the form instead.
		j := lastIndex(w.htmlPos["form"])
		if j < 0 || lastIndex(w.scopes) > j {
			return
		}
		if len(w.htmlPos["template"]) > 0 {
			w.popTo(j)
			return
		}
		for t := w.top(); t != nil && t.ns == nsHTML && impliedEnd[t.name]; t = w.top() {
			name := t.name
			w.pop()
			w.blockSpace(name)
		}
		w.detach(j)
	default:
		if scopedEnd[name] {
			w.popInScope(lastIndex(w.htmlPos[name]))
			return
		}
		w.popOther(name)
	}
}

// impliedEnd names the elements x/net's generateImpliedEndTags pops.
var impliedEnd = map[string]bool{
	"dd": true, "dt": true, "li": true, "optgroup": true, "option": true, "p": true,
	"rb": true, "rp": true, "rt": true, "rtc": true,
}

// popInScope pops to the element at j when it is in the default scope.
func (w *tokenWalker) popInScope(j int) {
	if j >= 0 && lastIndex(w.scopes) <= j {
		w.popTo(j)
	}
}

// popOther is x/net's inBodyEndTagOther: pop to the nearest element of that
// name unless a special element is open above it.
func (w *tokenWalker) popOther(name string) {
	if j := lastIndex(w.htmlPos[name]); j >= 0 && w.topSpecial() <= j {
		w.popTo(j)
	}
}

// breakout is x/net's list of start tags that end foreign content.
var breakout = map[string]bool{
	"b": true, "big": true, "blockquote": true, "body": true, "br": true, "center": true,
	"code": true, "dd": true, "div": true, "dl": true, "dt": true, "em": true, "embed": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "head": true,
	"hr": true, "i": true, "img": true, "li": true, "listing": true, "menu": true, "meta": true,
	"nobr": true, "ol": true, "p": true, "pre": true, "ruby": true, "s": true, "small": true,
	"span": true, "strong": true, "strike": true, "sub": true, "sup": true, "table": true,
	"tt": true, "u": true, "ul": true, "var": true,
}

// specialHTML is x/net's special category for HTML elements (void and
// raw-text ones never reach the stack here).
var specialHTML = map[string]bool{
	"address": true, "applet": true, "article": true, "aside": true, "blockquote": true,
	"button": true, "center": true, "dd": true, "details": true, "dir": true, "div": true,
	"dl": true, "dt": true, "fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hgroup": true, "li": true, "listing": true, "main": true, "marquee": true,
	"menu": true, "nav": true, "object": true, "ol": true, "p": true, "pre": true,
	"section": true, "select": true, "summary": true, "table": true, "ul": true,
}

// scopeHTML is x/net's default-scope boundary set for HTML elements (td, th,
// caption and template are pushed with it set directly).
var scopeHTML = map[string]bool{
	"applet": true, "marquee": true, "object": true, "table": true, "select": true,
}

// scopedEnd names the end tags x/net's in-body rules pop only when the element
// is in the default scope (and the table end tags, approximated the same way).
var scopedEnd = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "button": true,
	"center": true, "details": true, "dialog": true, "dir": true, "div": true, "dl": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true, "header": true,
	"hgroup": true, "listing": true, "main": true, "menu": true, "nav": true, "ol": true,
	"pre": true, "search": true, "section": true, "select": true, "summary": true, "ul": true,
	"p": true, "li": true, "dd": true, "dt": true, "applet": true, "marquee": true,
	"object": true, "table": true, "tbody": true, "thead": true, "tfoot": true, "tr": true,
	"td": true, "th": true, "caption": true, "colgroup": true,
}

// Snippet returns a collapsed, length-bounded preview of text.
func Snippet(text string, maxRunes int) string {
	text = normalizeWS(text)
	r := []rune(text)
	if len(r) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(r[:maxRunes])) + "…"
}

// normalizeWS collapses all runs of whitespace to single spaces and trims.
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

package export

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"mime"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// ArchiveCSP is the Content-Security-Policy every exported document carries as
// a <meta> (browsers enforce it on file://, where no header exists) and that
// `serve` sends as a header for archived files: no scripts, no remote loads
// (tracking pixels, remote CSS/fonts/frames), no <base> rewriting, no form
// submission. Inline styles and data: images/fonts — what real mail needs to
// render — stay allowed. One constant, so the two surfaces cannot drift (R19).
const ArchiveCSP = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'"

// inertMetas are the first children of every exported document's <head>: the
// archive policy (enforced by browsers on file://) and a no-referrer rule. The
// head is OURS — the mail's document is parsed and its pieces placed after
// these — so nothing the mail supplies can precede the policy (R19).
const inertMetas = `<meta http-equiv="Content-Security-Policy" content="` + ArchiveCSP + `">` + "\n" +
	`<meta name="referrer" content="no-referrer">`

// docTemplate: %s = title, mail head extras (styles etc.), our header, body attrs, body.
const docTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + inertMetas + `
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
body{font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;margin:0;padding:0;color:#1a1a1a;background:#fff}
.mailarchive-header{border-bottom:1px solid #ddd;padding:16px 20px;background:#f7f7f8;font-size:14px}
.mailarchive-header dl{display:grid;grid-template-columns:max-content 1fr;gap:2px 12px;margin:0}
.mailarchive-header dt{font-weight:600;color:#555}
.mailarchive-header dd{margin:0;word-break:break-word}
.mailarchive-subject{font-size:18px;font-weight:700;margin:0 0 10px}
.mailarchive-nav{font-size:13px;margin:0 0 8px}
.mailarchive-attachments,.mailarchive-raw{margin-top:8px;font-size:13px}
.mailarchive-body{padding:20px}
.mailarchive-body pre.plain{white-space:pre-wrap;word-wrap:break-word;font-family:ui-monospace,Consolas,monospace}
.mailarchive-missing-image{display:inline-block;font-size:12px;color:#888;border:1px dashed #bbb;padding:2px 6px}
</style>
%s</head>
<body>
%s
<div class="mailarchive-body"%s>
%s
</div>
</body>
</html>
`

// RenderContext tells the renderer where the page will live, so the header can
// carry navigation and links an inheritor can follow without the tool: the
// relative path back to the archive root ("../../"), the folder page
// ("index.html"), the sibling attachment zip and the preserved raw message
// (both file names, empty when absent). A zero context renders no links.
type RenderContext struct {
	RootRel        string
	FolderIndexRel string
	ZipName        string
	RawName        string
}

// RenderResult is a rendered page plus what the renderer learned about it.
type RenderResult struct {
	HTML       []byte
	Consumed   map[int]bool // attachment indices embedded inline as data: URIs
	Unresolved []string     // cid: tokens with no matching part (dangling references)
	// BodyFallback is non-empty when the mail's HTML could not be shown as HTML
	// and the page shows its escaped source instead: FallbackTooDeep,
	// FallbackUnparseable, FallbackHostile or FallbackTooLarge. The exporter
	// records it (R1, R7): the fallback is never silent.
	BodyFallback string
}

// Reasons an HTML body is shown as escaped source rather than rendered. The
// strings are recorded on the manifest (body_fallback) and in the report, and
// `reindex -rebuild` recognises a fallback page by them: never rename one.
const (
	FallbackTooDeep     = "too-deeply-nested"    // the parser's nesting cap (x/net: 512 open elements)
	FallbackUnparseable = "unparseable"          // the parser or serializer refused the mail
	FallbackHostile     = "unsafe-after-reparse" // the re-parse check found a live hostile meta or link hint
	FallbackTooLarge    = "too-large"            // the serialization would pass serializedCap (RV-8)
)

// Render builds a self-contained HTML document for the message with no
// navigation context. Kept for callers that only need the bytes.
func Render(m *model.Message) ([]byte, map[int]bool, error) {
	rr, err := RenderWith(m, RenderContext{})
	if err != nil {
		return nil, nil, err
	}
	return rr.HTML, rr.Consumed, nil
}

// RenderWith builds a self-contained HTML document for the message. Inline
// images referenced via cid: are embedded as data: URIs. The mail's HTML is
// PARSED with the same algorithm browsers use (golang.org/x/net/html), its
// hostile metas defanged on the parsed tree, its head pieces (styles) and body
// placed into OUR document after the policy metas — so what the browser sees
// is exactly what was checked (R19).
func RenderWith(m *model.Message, ctx RenderContext) (RenderResult, error) {
	source, isHTML := selectBody(m)
	consumed := map[int]bool{}
	body := embedInlineImages(source, m.Attachments, consumed)

	var headExtra, bodyAttrs, inner, fallback string
	var unresolved []string
	if isHTML {
		unresolved = unresolvedInlineRefs([]byte(body))
		headExtra, bodyAttrs, inner, fallback = sanitizeHTML(body)
	} else {
		inner = `<pre class="plain">` + html.EscapeString(body) + `</pre>`
	}
	page := assemblePage(m, ctx, consumed, headExtra, bodyAttrs, inner)
	if isHTML && fallback == "" {
		// The page wraps the body one level deeper than the tree sanitizeHTML
		// checked (html > body > div.mailarchive-body), and a page too deep for
		// the tree parser cannot be read back by `reindex -rebuild` (INT-1).
		// Parse the page itself, in every reading a browser can make.
		fallback = pageFallback(page)
	}
	if fallback != "" {
		// Show the mail's ORIGINAL source (cid: references, not megabytes of
		// base64), and put every inline image back in the attachment zip so
		// nothing is lost when the body cannot be rendered. This page needs no
		// parse check: around the template's shallow, closed markup every byte
		// of the mail is escaped text inside one <pre>.
		consumed = map[int]bool{}
		page = assemblePage(m, ctx, consumed, "", "", plainFallback(source, fallback))
	}
	return RenderResult{HTML: []byte(page), Consumed: consumed, Unresolved: unresolved, BodyFallback: fallback}, nil
}

// assemblePage puts our header and the mail's sanitized pieces into the page
// template.
func assemblePage(m *model.Message, ctx RenderContext, consumed map[int]bool, headExtra, bodyAttrs, inner string) string {
	return fmt.Sprintf(docTemplate, html.EscapeString(displaySubject(m)), headExtra, renderHeader(m, ctx, consumed), bodyAttrs, inner)
}

// pageFallback reports why an assembled page must not be written as HTML — ""
// when it parses with x/net's default 512-element cap in every reading (the
// scripting flag on, and off when the page holds a noscript tag) — so a page
// the exporter writes is never too deep for the tree parser.
func pageFallback(page string) string {
	for _, scripting := range scriptingModes(page) {
		if _, err := reparse(page, scripting); err != nil {
			return parseFailure(err)
		}
	}
	return ""
}

// parseFailure maps a parse error to the fallback reason: the parser's
// nesting cap ("open stack of elements exceeds 512 nodes"), or anything else.
func parseFailure(err error) string {
	if strings.Contains(err.Error(), "exceeds") {
		return FallbackTooDeep
	}
	return FallbackUnparseable
}

// sanitizeHTML parses the mail's HTML (a full document or a fragment — the
// parser builds html/head/body either way) and returns the serialized head
// extras (styles, harmless metas), the body element's own attributes, and the
// body content, with every hostile element neutralized on the PARSED tree:
//
//   - <meta http-equiv=refresh|content-security-policy|set-cookie|content-type>
//     → the attribute is renamed (the tag stays, inert; nothing is silently
//     dropped);
//   - <meta charset> → removed (our UTF-8 charset is first and must win; the
//     exporter always writes UTF-8);
//   - <title> → removed (ours names the page);
//   - <img src="cid:…"> with no matching part → src removed, a caption added;
//   - <link href> → renamed data-mailarchive-neutralized-href, on EVERY
//     <link> in the parsed tree (see neutralizeLink; a <noscript> copy is
//     text to a scripting-on parse, see hostileSurvives).
//
// Scripts, remote loads, <base> and forms need no rewriting: the policy meta
// that precedes everything makes them inert. A meta refresh is the exception —
// CSP has no directive that governs it — and so are the preconnect and
// dns-prefetch link hints, so the serialized result is re-parsed
// the way a browser will read it, and if any meta or link hint neutralize must
// disarm is live there, the body falls back to escaped text (fail closed). The parse →
// serialize round trip is not guaranteed to be a fixed point: x/net once wrote
// SVG/MathML raw-text children unescaped (GO-2023-1988), turning an inert text
// node back into a live <meta refresh>. When the body is shown as source, the
// fallback reason is returned (and body is empty; RenderWith builds the
// fallback from the un-embedded source).
//
// The main parse runs with scripting on, like the browser that reads most
// archives, so <noscript> content is raw text that neutralize cannot reach,
// while the re-check also reads the output with scripting off. A harmless
// <meta charset> or Content-Type meta inside <noscript> therefore tripped the
// re-check and could never be rendered (DIFF-FP-1). When the re-check trips
// and the mail has a noscript tag, the main parse is retried ONCE with
// scripting off — noscript content is then markup neutralize renames or
// removes — and that result is accepted only if it passes the same dual-mode
// re-check; otherwise the body falls back as before. (Parsing only with
// scripting off is not an option: its serialization writes noscript text
// children back raw, so an escaped <meta> there would come out live — the
// re-check is what rejects that.)
func sanitizeHTML(doc string) (headExtra, bodyAttrs, body, fallback string) {
	// Markup nested past flattenDepth loses its container tags (not its text)
	// first, as a browser flattens past its own cap, so deep mail renders
	// instead of failing the parser's 512-element cap (DIFF-FP-2).
	flat := flattenDeep(doc)
	headExtra, bodyAttrs, body, fallback = sanitizeParsed(flat, len(doc), true)
	if fallback == FallbackHostile && hasNoscriptTag(flat) {
		// Without a noscript tag the scripting-off parse is the same tree
		// (scriptingModes), so the retry could change nothing.
		if h, a, b, f := sanitizeParsed(flat, len(doc), false); f == "" {
			return h, a, b, ""
		}
	}
	return headExtra, bodyAttrs, body, fallback
}

// sanitizeParsed is one sanitizing pass over doc (already flattened), with the
// main parse's scripting flag as given; inputLen sizes the serialization cap.
func sanitizeParsed(doc string, inputLen int, scripting bool) (headExtra, bodyAttrs, body, fallback string) {
	root, err := xhtml.ParseWithOptions(strings.NewReader(doc), xhtml.ParseOptionEnableScripting(scripting))
	if err != nil {
		// Unparseable bytes (or nesting the flattener's model missed): show
		// them as text rather than guess.
		return "", "", "", parseFailure(err)
	}
	var headNode, bodyNode *xhtml.Node
	var find func(n *xhtml.Node)
	find = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode {
				switch c.DataAtom {
				case atom.Head:
					if headNode == nil {
						headNode = c
					}
				case atom.Body:
					if bodyNode == nil {
						bodyNode = c
					}
				}
			}
			find(c)
		}
	}
	find(root)
	neutralize(root)

	// The serialization is capped (RV-8): x/net's reconstruct-active-
	// formatting step can turn a small mail into a huge tree (400 unclosed
	// <b id=i> re-opened in every later paragraph: 16 KB in, 12 MB out), and
	// the checks below re-parse that output, each parse quadratic in the open
	// formatting elements. Past the cap the body falls back to its source
	// before the output is complete, let alone re-parsed.
	left := serializedCap(inputLen)
	hb, bb := &capWriter{left: &left}, &capWriter{left: &left}
	if headNode != nil {
		for c := headNode.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && c.DataAtom == atom.Title {
				continue
			}
			if err := renderNode(hb, c); err != nil {
				return "", "", "", renderFailure(err)
			}
			if err := hb.WriteByte('\n'); err != nil {
				return "", "", "", renderFailure(err)
			}
		}
	}
	if bodyNode != nil {
		for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
			if err := renderNode(bb, c); err != nil {
				return "", "", "", renderFailure(err)
			}
		}
		bodyAttrs = bodyAttributes(bodyNode)
	}
	if reason := hostileSurvives(hb.String(), bb.String()); reason != "" {
		return "", "", "", reason
	}
	return hb.String(), bodyAttrs, bb.String(), ""
}

// serializedCap bounds the sanitized head+body of a mail whose HTML (inline
// images already embedded) is n bytes: 16x the input, and at least 1 MiB.
// Serializing real mail grows it by at most about 5x — x/net writes " and '
// as 5-byte references, and implied end tags and tbody add little — so 16x
// leaves 3x headroom, and only the super-linear growth of reconstructed
// formatting elements trips it (the RV-8 body grows about 630x). The floor
// keeps small mail, which implied tags can double, far from the cap. Every
// check after it re-parses at most 16x the input, so its cost stays linear in
// the mail's size.
func serializedCap(n int) int { return max(16*n, 1<<20) }

// errTooLarge stops the serializer once its output passes the size cap.
var errTooLarge = errors.New("serialized mail HTML exceeds the size cap")

// renderFailure maps a serialization error to the fallback reason: the size
// cap, or any other refusal to serialize (fail closed: never write a body the
// checks have not read).
func renderFailure(err error) string {
	if errors.Is(err, errTooLarge) {
		return FallbackTooLarge
	}
	return FallbackUnparseable
}

// capWriter collects serialized output and refuses to grow past a budget
// shared with its siblings (one mail's head and body draw on one cap). It has
// the Write, WriteByte and WriteString methods x/net's Render writes through.
type capWriter struct {
	b    strings.Builder
	left *int
}

func (w *capWriter) spend(n int) error {
	if n > *w.left {
		*w.left = 0
		return errTooLarge
	}
	*w.left -= n
	return nil
}

func (w *capWriter) Write(p []byte) (int, error) {
	if err := w.spend(len(p)); err != nil {
		return 0, err
	}
	return w.b.Write(p)
}

func (w *capWriter) WriteString(s string) (int, error) {
	if err := w.spend(len(s)); err != nil {
		return 0, err
	}
	return w.b.WriteString(s)
}

func (w *capWriter) WriteByte(c byte) error {
	if err := w.spend(1); err != nil {
		return err
	}
	return w.b.WriteByte(c)
}

func (w *capWriter) String() string { return w.b.String() }

// flattenDepth is how deep flattenDeep lets mail markup nest — open elements,
// not counting html/head/body — before it stops writing container tags. The
// page puts the mail body under html > body > div.mailarchive-body, so an
// element at mail depth d sits at page depth d+3 and a void or raw-text leaf
// inside it at d+4: with 480 the deepest page element is at 484, leaving 28 of
// x/net's 512 levels for elements the tree builder adds that the flattener's
// model does not track (reconstructed formatting elements, mostly). The page
// check in RenderWith (pageFallback) is what guarantees an exported page
// parses; flattening only makes deep mail render instead of falling back.
const flattenDepth = 480

// flattenDeep returns the mail's HTML with every container start and end tag
// past flattenDepth dropped, keeping all text, comments, void elements and
// raw-text elements (script, style, textarea, title, ...: one text token each,
// so they never nest) — as Chromium keeps attaching content past its own 512
// cap instead of refusing the page (DIFF-FP-2). Where a dropped tag is a
// block-level one (flatBlock: p, div, td, li, ...) a newline takes its place,
// so the words on either side stay apart — on the page, and for
// `reindex -rebuild`, which reads the words back from it. Mail that never gets
// that deep is returned unchanged, the same string. It is a single tokenizer
// pass with a model of the tree builder's open-element stack (implied end
// tags, implied tbody/tr, foreign content and its integration points,
// noscript read as markup so the scripting-off reading is flattened too). The
// model is not the tree builder and carries no safety: what it writes is
// parsed, neutralized and re-checked like any mail, and an exported page is
// parsed once more as a whole before it is accepted.
func flattenDeep(doc string) string { return flattenAt(doc, flattenDepth) }

func flattenAt(doc string, limit int) string {
	z := xhtml.NewTokenizer(strings.NewReader(doc))
	f := &flattener{limit: limit}
	var out *strings.Builder
	var last byte // the last byte written to out
	pos := 0      // bytes of doc covered by the tokens read so far (tokens are contiguous)
	for {
		top := f.top()
		z.AllowCDATA(top != nil && top.ns != "") // as the parser does: CDATA only in foreign content
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			break
		}
		raw := z.Raw()
		keep, name := true, ""
		switch tt {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			nameB, hasAttr := z.TagName()
			name = string(nameB)
			keep = f.start(z, atom.Lookup(nameB), name, hasAttr, tt == xhtml.SelfClosingTagToken)
		case xhtml.EndTagToken:
			nameB, _ := z.TagName()
			name = string(nameB)
			keep = f.end(atom.Lookup(nameB), name)
		}
		if !keep && out == nil {
			out = &strings.Builder{}
			out.Grow(len(doc))
			out.WriteString(doc[:pos])
			if pos > 0 {
				last = doc[pos-1]
			}
		}
		switch {
		case keep && out != nil && len(raw) > 0:
			out.Write(raw)
			last = raw[len(raw)-1]
		case !keep && flatBlock[name] && !isASCIIWhitespace(rune(last)):
			// The dropped tag separated words (a browser shows them on
			// separate lines; the index tokenizes them apart): keep a word
			// boundary. Whitespace is inert wherever it lands — in table
			// context the tree builder inserts it in place, it is never
			// foster-parented — and one is enough, so a run of dropped tags
			// writes a single newline.
			out.WriteByte('\n')
			last = '\n'
		}
		pos += len(raw)
	}
	if out == nil {
		return doc
	}
	out.WriteString(doc[pos:]) // bytes the tokenizer returned no token for (an unterminated tag at EOF)
	return out.String()
}

// flatEl is one open element in flattener's model.
type flatEl struct {
	a       atom.Atom
	name    string // lower-case tag name
	ns      string // "" (HTML), "svg" or "math"
	htmlIP  bool   // an HTML integration point: svg foreignObject/desc/title, math annotation-xml text/html
	mathTIP bool   // a MathML text integration point: mi, mo, mn, ms, mtext
}

// flattener models the tree builder's stack of open elements for flattenAt.
// stack holds the elements whose start tag was written (plus the tbody/tr the
// tree builder adds itself); drop holds, in the same order, the open elements
// whose start tag was NOT written, which are always above every written one
// (once one is open nothing else is written until it closes, and a pop of a
// written element closes everything above it). The drop stack is kept in full
// — not just counted — so the current node's namespace is known in the dropped
// region too: an SVG/MathML container dropped past the depth cap still makes a
// <style>/<script>/<title> inside it an ordinary foreign element (read as
// markup, not raw text) rather than an HTML raw-text element that would swallow
// the rest of the body when the page is re-parsed (and the index reads it
// back). open and dropOpen count stack and drop by name, so a tag with nothing
// of its name open costs O(1) rather than a walk of a deep stack, and a stray
// end tag pops nothing (a mail of 480 divs and a million stray tags stays
// linear; the drop stack's memory is linear in the input, as x/net's own would
// be below its cap).
type flattener struct {
	limit    int
	stack    []flatEl       // written (kept) open elements
	open     map[string]int // count of stack by name
	drop     []flatEl       // dropped open elements, above every kept one
	dropOpen map[string]int // count of drop by name
}

// top is the current node: the innermost open element, dropped or kept.
func (f *flattener) top() *flatEl {
	if n := len(f.drop); n > 0 {
		return &f.drop[n-1]
	}
	if n := len(f.stack); n > 0 {
		return &f.stack[n-1]
	}
	return nil
}

// popTo closes stack[i] and everything above it, dropped elements included
// (the dropped elements are all above every kept one).
func (f *flattener) popTo(i int) {
	f.clearDrop()
	for _, el := range f.stack[i:] {
		f.open[el.name]--
	}
	f.stack = f.stack[:i]
}

// clearDrop closes every open dropped element.
func (f *flattener) clearDrop() {
	if len(f.drop) == 0 {
		return
	}
	f.drop = f.drop[:0]
	clear(f.dropOpen)
}

// popDropTo closes drop[i] and every dropped element above it.
func (f *flattener) popDropTo(i int) {
	for _, el := range f.drop[i:] {
		f.dropOpen[el.name]--
	}
	f.drop = f.drop[:i]
}

// push opens el (after the implied elements the tree builder adds before it)
// and reports whether its start tag is written: not when an element is already
// dropped or el would pass the limit. A dropped element goes on the drop stack
// so its namespace and nesting stay known.
func (f *flattener) push(el flatEl, implied ...flatEl) bool {
	if len(f.drop) > 0 || len(f.stack)+len(implied)+1 > f.limit {
		if f.dropOpen == nil {
			f.dropOpen = map[string]int{}
		}
		for _, p := range implied {
			f.dropOpen[p.name]++
			f.drop = append(f.drop, p)
		}
		f.dropOpen[el.name]++
		f.drop = append(f.drop, el)
		return false
	}
	if f.open == nil {
		f.open = map[string]int{}
	}
	for _, p := range implied {
		f.open[p.name]++
	}
	f.open[el.name]++
	f.stack = append(f.stack, implied...)
	f.stack = append(f.stack, el)
	return true
}

// closeDropped closes the nearest open dropped element named name (and every
// dropped element above it), if there is one.
func (f *flattener) closeDropped(name string) bool {
	if f.dropOpen[name] == 0 {
		return false
	}
	for i := len(f.drop) - 1; i >= 0; i-- {
		if f.drop[i].name == name {
			f.popDropTo(i)
			return true
		}
	}
	return false
}

// closeIn closes the nearest open HTML element named a unless a scope
// boundary comes first (the tree builder's "has an element in scope").
func (f *flattener) closeIn(a atom.Atom, boundary func(flatEl) bool) {
	if f.closeDropped(a.String()) || f.open[a.String()] == 0 {
		return
	}
	for i := len(f.stack) - 1; i >= 0; i-- {
		el := f.stack[i]
		if el.ns == "" && el.a == a {
			f.popTo(i)
			return
		}
		if boundary(el) {
			return
		}
	}
}

func (f *flattener) start(z *xhtml.Tokenizer, a atom.Atom, name string, hasAttr, selfClosing bool) bool {
	if top := f.top(); top != nil && top.ns != "" && f.foreignRules(top, a) {
		breaks := flatBreakout[name]
		if a == atom.Font && hasAttr {
			for more := true; more && !breaks; {
				var k []byte
				k, _, more = z.TagAttr()
				switch string(k) {
				case "color", "face", "size":
					breaks = true
				}
			}
		}
		if !breaks {
			// A foreign element: a raw-text name is an element here, and a
			// self-closing tag closes itself.
			z.NextIsNotRawText()
			if selfClosing {
				return true
			}
			el := flatEl{a: a, name: name, ns: top.ns}
			switch {
			case el.ns == "svg" && (a == atom.Foreignobject || a == atom.Desc || a == atom.Title):
				el.htmlIP = true
			case el.ns == "math" && (a == atom.Mi || a == atom.Mo || a == atom.Mn || a == atom.Ms || a == atom.Mtext):
				el.mathTIP = true
			case el.ns == "math" && a == atom.AnnotationXml && hasAttr:
				for more := true; more; {
					var k, v []byte
					k, v, more = z.TagAttr()
					if string(k) == "encoding" && (strings.EqualFold(string(v), "text/html") || strings.EqualFold(string(v), "application/xhtml+xml")) {
						el.htmlIP = true
					}
				}
			}
			return f.push(el)
		}
		// A breakout tag leaves foreign content: close up to the nearest HTML
		// element or integration point, then treat the tag as HTML.
		f.breakoutForeign()
	}
	return f.startHTML(z, a, name, selfClosing)
}

// breakoutForeign closes the open foreign elements down to the nearest HTML
// element or integration point, across the drop stack and the kept stack (the
// dropped elements are all above the kept ones), as x/net does before it reads
// a breakout tag.
func (f *flattener) breakoutForeign() {
	for {
		t := f.top()
		if t == nil || t.ns == "" || t.htmlIP || t.mathTIP {
			return
		}
		if n := len(f.drop); n > 0 {
			f.popDropTo(n - 1)
		} else {
			f.popTo(len(f.stack) - 1)
		}
	}
}

// foreignRules reports whether a start tag a is handled by the tree builder's
// foreign-content rules when top is the current node (parse.go
// inForeignContent).
func (f *flattener) foreignRules(top *flatEl, a atom.Atom) bool {
	switch {
	case top.mathTIP && a != atom.Mglyph && a != atom.Malignmark:
		return false
	case top.ns == "math" && top.a == atom.AnnotationXml && a == atom.Svg:
		return false
	case top.htmlIP:
		return false
	}
	return true
}

func (f *flattener) startHTML(z *xhtml.Tokenizer, a atom.Atom, name string, selfClosing bool) bool {
	switch a {
	case atom.Html, atom.Head, atom.Body, atom.Frameset:
		return true // merged into the document's own elements, or ignored: nothing opens
	case atom.Svg, atom.Math:
		if selfClosing {
			return true
		}
		return f.push(flatEl{a: a, name: name, ns: name})
	}
	f.impliedEnds(a)
	if flatVoid[a] || flatRawText[a] {
		return true // a leaf: void, or one text token the tokenizer reads to its end tag
	}
	if a == atom.Noscript {
		// Read noscript content as markup (the scripting-off reading, where its
		// depth counts); to the scripting-on reading it stays one text node.
		z.NextIsNotRawText()
	}
	el := flatEl{a: a, name: name}
	switch a {
	case atom.Form:
		if f.open["form"] > 0 {
			for _, open := range f.stack {
				if open.ns == "" && open.a == atom.Form {
					return true // a nested form start tag is ignored
				}
			}
		}
	case atom.Table:
		// A table directly in a table (not in a cell) closes the outer one.
		// Skip the restructuring in the dropped region: a dropped table drops
		// the new one too (push routes to drop), and popTo here would wrongly
		// clear the drop stack.
		for i := len(f.stack) - 1; len(f.drop) == 0 && i >= 0 && f.open["table"] > 0; i-- {
			s := f.stack[i]
			if s.ns != "" {
				continue
			}
			if s.a == atom.Td || s.a == atom.Th || s.a == atom.Caption || s.a == atom.Template {
				break
			}
			if s.a == atom.Table || s.a == atom.Tbody || s.a == atom.Thead || s.a == atom.Tfoot || s.a == atom.Tr {
				for j := i; j >= 0; j-- {
					if f.stack[j].ns == "" && f.stack[j].a == atom.Table {
						f.popTo(j)
						break
					}
				}
				break
			}
		}
	case atom.Td, atom.Th, atom.Tr, atom.Tbody, atom.Thead, atom.Tfoot, atom.Caption, atom.Colgroup:
		return f.startTablePart(el)
	}
	return f.push(el)
}

// startTablePart opens a table part where the tree builder would put it:
// closing the open cell or row first and adding the implied tbody/tr. Outside
// a table (or template) the tree builder ignores the tag.
func (f *flattener) startTablePart(el flatEl) bool {
	if f.open["table"] == 0 && f.open["template"] == 0 {
		if f.dropOpen["table"] > 0 || f.dropOpen["template"] > 0 {
			// The enclosing table/template is dropped: the part drops too,
			// leaving a block word boundary, never a stray <tr>/<td> that a
			// re-parse ignores for lack of a table around it (so the words in
			// adjacent cells would glue together, on the page and in the index).
			return f.push(el)
		}
		return true // ignored by the tree builder: written, opens nothing
	}
	if len(f.drop) > 0 {
		// A kept table is open but the current node is dropped: the part drops
		// into the dropped region (a boundary), never through the kept-stack
		// path below, which would clear the drop stack.
		return f.push(el)
	}
	ti := -1
	for i := len(f.stack) - 1; i >= 0; i-- {
		if s := f.stack[i]; s.ns == "" && (s.a == atom.Table || s.a == atom.Template) {
			ti = i
			break
		}
	}
	if ti < 0 {
		return true // ignored by the tree builder: written, opens nothing
	}
	if f.stack[ti].a == atom.Template {
		return f.push(el) // template content takes table parts as they come
	}
	// The nearest row-group / row the new part goes into.
	si := ti
	for i := len(f.stack) - 1; i > ti; i-- {
		s := f.stack[i]
		if s.ns != "" {
			continue
		}
		if (el.a == atom.Td || el.a == atom.Th) && (s.a == atom.Tr || s.a == atom.Tbody || s.a == atom.Thead || s.a == atom.Tfoot) ||
			el.a == atom.Tr && (s.a == atom.Tbody || s.a == atom.Thead || s.a == atom.Tfoot) {
			si = i
			break
		}
	}
	if si+1 < len(f.stack) {
		f.popTo(si + 1)
	}
	parent := f.stack[si].a
	switch {
	case (el.a == atom.Td || el.a == atom.Th) && parent == atom.Table:
		return f.push(el, flatEl{a: atom.Tbody, name: "tbody"}, flatEl{a: atom.Tr, name: "tr"})
	case (el.a == atom.Td || el.a == atom.Th) && parent != atom.Tr:
		return f.push(el, flatEl{a: atom.Tr, name: "tr"})
	case el.a == atom.Tr && parent == atom.Table:
		return f.push(el, flatEl{a: atom.Tbody, name: "tbody"})
	}
	return f.push(el)
}

// impliedEnds closes what the start tag a implies the end of: an open p, the
// previous li / dd / dt / heading / option, an open a / nobr / button /
// select.
func (f *flattener) impliedEnds(a atom.Atom) {
	if flatClosesP[a] {
		f.closeIn(atom.P, flatButtonScope)
	}
	switch a {
	case atom.Li:
		f.closeItem(atom.Li, atom.Li)
	case atom.Dd, atom.Dt:
		f.closeItem(atom.Dd, atom.Dt)
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		if top := f.top(); top != nil && top.ns == "" && flatHeading[top.a] {
			if len(f.drop) > 0 {
				f.popDropTo(len(f.drop) - 1)
			} else {
				f.popTo(len(f.stack) - 1)
			}
		}
	case atom.A, atom.Nobr, atom.Button, atom.Select:
		f.closeIn(a, flatDefaultScope)
	case atom.Option:
		f.closeTop(atom.Option)
	case atom.Optgroup:
		f.closeTop(atom.Option)
		f.closeTop(atom.Optgroup)
	}
}

// closeItem closes the nearest open item (li, or dd/dt) unless a special
// element other than address, div or p comes first.
func (f *flattener) closeItem(x, y atom.Atom) {
	if f.closeDropped(x.String()) || f.closeDropped(y.String()) || f.open[x.String()]+f.open[y.String()] == 0 {
		return
	}
	for i := len(f.stack) - 1; i >= 0; i-- {
		el := f.stack[i]
		if el.ns == "" && (el.a == x || el.a == y) {
			f.popTo(i)
			return
		}
		if flatSpecial(el) && el.a != atom.Address && el.a != atom.Div && el.a != atom.P {
			return
		}
	}
}

func (f *flattener) closeTop(a atom.Atom) {
	if f.closeDropped(a.String()) {
		return
	}
	if top := f.top(); top != nil && top.ns == "" && top.a == a {
		f.popTo(len(f.stack) - 1)
	}
}

func (f *flattener) end(a atom.Atom, name string) bool {
	if f.closeDropped(name) {
		return false // its start tag was not written
	}
	if a == atom.P || a == atom.Br {
		// </p> and </br> leave foreign content: the in-body rules read </br> as
		// a <br> start tag and let a </p> with no <p> open imply one, and both
		// are breakout tags, so no foreign element is ever named p or br. Without
		// this, an SVG dropped past the cap stays open and a <style> after the
		// stray </p> is read as a foreign (not raw-text) element, hiding the body
		// after it on the page and in the rebuilt index (RV-3 on the deep path).
		if top := f.top(); top != nil && top.ns != "" {
			f.breakoutForeign()
		}
	}
	switch a {
	case atom.Html, atom.Head, atom.Body, atom.Br:
		return true // </br> is a <br>; the others close nothing in the model
	}
	if f.open[name] == 0 {
		return true // nothing of that name is open: the tree builder ignores it
	}
	tablePart := a == atom.Table || a == atom.Tbody || a == atom.Thead || a == atom.Tfoot || a == atom.Tr || a == atom.Td || a == atom.Th || a == atom.Caption || a == atom.Colgroup
	for i := len(f.stack) - 1; i >= 0; i-- {
		el := f.stack[i]
		if el.name == name {
			f.popTo(i)
			return true
		}
		switch {
		case tablePart:
			if el.ns == "" && (el.a == atom.Table || el.a == atom.Template || el.a == atom.Html) {
				return true
			}
		case flatFormatting[a] || flatSpecial(flatEl{a: a}):
			if flatDefaultScope(el) {
				return true
			}
		default:
			if flatSpecial(el) {
				return true // "any other end tag" stops at a special element
			}
		}
	}
	return true // unmatched: ignored by the tree builder (a </p> makes an empty p)
}

func flatDefaultScope(el flatEl) bool {
	switch el.ns {
	case "":
		switch el.a {
		case atom.Applet, atom.Caption, atom.Html, atom.Table, atom.Td, atom.Th, atom.Marquee, atom.Object, atom.Template, atom.Select:
			return true
		}
		return false
	case "math":
		switch el.a {
		case atom.AnnotationXml, atom.Mi, atom.Mn, atom.Mo, atom.Ms, atom.Mtext:
			return true
		}
		return false
	}
	return el.a == atom.Desc || el.a == atom.Foreignobject || el.a == atom.Title
}

func flatButtonScope(el flatEl) bool {
	return flatDefaultScope(el) || el.ns == "" && el.a == atom.Button
}

// flatSpecial mirrors x/net's isSpecialElement.
func flatSpecial(el flatEl) bool {
	switch el.ns {
	case "":
		return flatSpecialHTML[el.a]
	case "math":
		switch el.a {
		case atom.Mi, atom.Mo, atom.Mn, atom.Ms, atom.Mtext, atom.AnnotationXml:
			return true
		}
		return false
	}
	return el.a == atom.Foreignobject || el.a == atom.Desc || el.a == atom.Title
}

func atomSet(as ...atom.Atom) map[atom.Atom]bool {
	m := make(map[atom.Atom]bool, len(as))
	for _, a := range as {
		m[a] = true
	}
	return m
}

var (
	flatVoid = atomSet(atom.Area, atom.Base, atom.Basefont, atom.Bgsound, atom.Br, atom.Col, atom.Embed, atom.Frame,
		atom.Hr, atom.Image, atom.Img, atom.Input, atom.Keygen, atom.Link, atom.Meta, atom.Param, atom.Source, atom.Track, atom.Wbr)
	flatRawText = atomSet(atom.Iframe, atom.Noembed, atom.Noframes, atom.Plaintext, atom.Script, atom.Style,
		atom.Textarea, atom.Title, atom.Xmp)
	flatClosesP = atomSet(atom.Address, atom.Article, atom.Aside, atom.Blockquote, atom.Center, atom.Details, atom.Dialog,
		atom.Dir, atom.Div, atom.Dl, atom.Fieldset, atom.Figcaption, atom.Figure, atom.Footer, atom.Header, atom.Hgroup,
		atom.Main, atom.Menu, atom.Nav, atom.Ol, atom.P, atom.Search, atom.Section, atom.Summary, atom.Ul,
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Pre, atom.Listing, atom.Form, atom.Plaintext,
		atom.Xmp, atom.Hr, atom.Li, atom.Dd, atom.Dt)
	flatHeading    = atomSet(atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6)
	flatFormatting = atomSet(atom.A, atom.B, atom.Big, atom.Code, atom.Em, atom.Font, atom.I, atom.Nobr, atom.S,
		atom.Small, atom.Strike, atom.Strong, atom.Tt, atom.U)
	flatSpecialHTML = atomSet(atom.Address, atom.Applet, atom.Area, atom.Article, atom.Aside, atom.Base, atom.Basefont,
		atom.Bgsound, atom.Blockquote, atom.Body, atom.Br, atom.Button, atom.Caption, atom.Center, atom.Col,
		atom.Colgroup, atom.Dd, atom.Details, atom.Dir, atom.Div, atom.Dl, atom.Dt, atom.Embed, atom.Fieldset,
		atom.Figcaption, atom.Figure, atom.Footer, atom.Form, atom.Frame, atom.Frameset, atom.H1, atom.H2, atom.H3,
		atom.H4, atom.H5, atom.H6, atom.Head, atom.Header, atom.Hgroup, atom.Hr, atom.Html, atom.Iframe, atom.Img,
		atom.Input, atom.Keygen, atom.Li, atom.Link, atom.Listing, atom.Main, atom.Marquee, atom.Menu, atom.Meta,
		atom.Nav, atom.Noembed, atom.Noframes, atom.Noscript, atom.Object, atom.Ol, atom.P, atom.Param, atom.Plaintext,
		atom.Pre, atom.Script, atom.Section, atom.Select, atom.Source, atom.Style, atom.Summary, atom.Table,
		atom.Tbody, atom.Td, atom.Template, atom.Textarea, atom.Tfoot, atom.Th, atom.Thead, atom.Title, atom.Tr,
		atom.Track, atom.Ul, atom.Wbr, atom.Xmp)
	// flatBlock names the tags whose drop leaves a word boundary: every name
	// the search index treats as one (internal/index blockElements: its live
	// index tokenizes the mail, `reindex -rebuild` the page, so a boundary the
	// flattener loses is a word neither finds again), and HTML's other
	// block-level containers, which a browser shows on lines of their own.
	// Keyed by name, whatever the namespace, as the index's tokenizer reads
	// them. Inline names (b, span, a, font, ...) are not here: a word split by
	// them is one word, and a dropped inline tag writes nothing.
	flatBlock = map[string]bool{
		// the index's blockElements
		"p": true, "br": true, "div": true, "li": true, "tr": true, "td": true, "th": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true,
		"table": true, "blockquote": true, "section": true, "article": true, "header": true, "footer": true,
		// HTML's other block-level containers
		"address": true, "aside": true, "caption": true, "center": true, "dd": true, "details": true,
		"dialog": true, "dir": true, "dl": true, "dt": true, "fieldset": true, "figcaption": true,
		"figure": true, "form": true, "hgroup": true, "legend": true, "listing": true, "main": true,
		"menu": true, "nav": true, "optgroup": true, "option": true, "pre": true, "search": true,
		"summary": true, "tbody": true, "tfoot": true, "thead": true,
	}
	// flatBreakout: start tags that leave foreign content (x/net foreign.go).
	flatBreakout = map[string]bool{"b": true, "big": true, "blockquote": true, "body": true, "br": true,
		"center": true, "code": true, "dd": true, "div": true, "dl": true, "dt": true, "em": true, "embed": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "head": true, "hr": true, "i": true,
		"img": true, "li": true, "listing": true, "menu": true, "meta": true, "nobr": true, "ol": true, "p": true,
		"pre": true, "ruby": true, "s": true, "small": true, "span": true, "strong": true, "strike": true,
		"sub": true, "sup": true, "table": true, "tt": true, "u": true, "ul": true, "var": true}
)

// renderNode serializes one parsed node. It is a variable only so a test can
// inject a renderer that does not round-trip (fault injection for the
// hostileSurvives fail-closed path); production always uses xhtml.Render.
var renderNode = xhtml.Render

// plainFallback shows mail HTML as escaped text: nothing in it is live. The
// reserved data-mailarchive-fallback attribute (mail can never carry one:
// stripReservedNamespace removes them) tells `reindex -rebuild` that the
// <pre> holds HTML source, so it indexes the mail's words, not its markup.
func plainFallback(doc, reason string) string {
	return `<pre class="plain" data-mailarchive-fallback="` + html.EscapeString(reason) + `">` + html.EscapeString(doc) + `</pre>`
}

// hostileEquiv reports whether a meta http-equiv value is one neutralize
// disarms. "link" is the Link pragma: an HTTP Link header carried in a meta,
// which can name rel=preconnect. No browser is required to honour it, but
// nothing is lost by disarming it — the one thing such a header could load in
// an archived page, a stylesheet, the policy already refuses (RV-2).
func hostileEquiv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "refresh", "content-security-policy", "set-cookie", "content-type", "link":
		return true
	}
	return false
}

// resourceHintRel reports whether a <link rel> value names a resource hint —
// a link type that makes the browser open a connection to, resolve or fetch
// from the href's host ahead of use. rel is split on ASCII whitespace and
// matched ASCII case-insensitively, as browsers read it (strings.ToLower folds
// no non-ASCII rune onto a letter these names use, so it is exact here).
func resourceHintRel(rel string) bool {
	for _, tok := range strings.FieldsFunc(rel, isASCIIWhitespace) {
		switch strings.ToLower(tok) {
		case "preconnect", "dns-prefetch", "prefetch", "preload", "modulepreload", "prerender":
			return true
		}
	}
	return false
}

func isASCIIWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	}
	return false
}

// liveResourceHint reports whether a <link> element is a resource hint a
// browser would act on: it has an href and a resource-hint rel (any rel
// attribute counts, so a duplicate cannot hide one).
func liveResourceHint(n *xhtml.Node) bool {
	hasHref, hint := false, false
	for _, a := range n.Attr {
		switch {
		case strings.EqualFold(a.Key, "href"):
			hasHref = true
		case strings.EqualFold(a.Key, "rel"):
			hint = hint || resourceHintRel(a.Val)
		}
	}
	return hasHref && hint
}

// hostileSurvives re-parses sanitized head and body output as a browser will
// and reports whether any meta that neutralize must disarm (a live hostile
// http-equiv, or a competing charset) or any live resource-hint <link href>
// (preconnect and dns-prefetch are governed by no CSP directive) is present in
// it. Every meta and link element is
// checked whatever its namespace, so a renderer that lets markup escape a
// foreign-content raw-text element is caught here rather than trusted. The page
// is re-parsed TWICE, with the scripting flag on and off: it decides whether
// <noscript> content is text or markup, and a viewer with JavaScript disabled
// (the archive needs none) reads it as markup, while the sanitizer's own parse
// read it as text. Hostile in either reading fails closed. The second parse is
// skipped when the output holds no noscript start tag (see scriptingModes).
//
// It returns the fallback reason, "" when the output is safe: FallbackHostile
// for a live meta or hint; and, when a reading cannot be parsed at all (so
// nothing is known to be safe and the body must fall back), the parse
// failure's own reason — FallbackTooDeep for the nesting cap, which a benign
// mail can reach in the scripting-off reading alone (deep markup inside a
// <noscript>), FallbackUnparseable otherwise — never "unsafe" (RV-9).
func hostileSurvives(head, body string) string {
	doc := "<!DOCTYPE html><html><head>" + head + "</head><body>" + body + "</body></html>"
	for _, scripting := range scriptingModes(head, body) {
		if reason := hostileIn(doc, scripting); reason != "" {
			return reason
		}
	}
	return ""
}

// scriptingModes is the scripting flags a re-parse of s must use to read it
// as every browser will: on, plus off when s can hold a <noscript> start tag.
// Skipping the second parse otherwise is exact, not a heuristic: in x/net the
// scripting flag is read only when a noscript start tag is processed (in head
// and in body), and the tokenizer names a tag by the bytes after "<", folded
// to lower case in ASCII only (NUL becomes U+FFFD), so a noscript start tag
// needs the bytes "<noscript" in some ASCII case. Without them both readings
// build the same tree (RV-8: the second parse doubled the check's cost).
func scriptingModes(s ...string) []bool {
	for _, part := range s {
		if hasNoscriptTag(part) {
			return []bool{true, false}
		}
	}
	return []bool{true}
}

// hasNoscriptTag reports whether s contains "<noscript" in any ASCII case.
func hasNoscriptTag(s string) bool {
	const tag = "<noscript"
	for i := strings.IndexByte(s, '<'); i >= 0; {
		if len(s)-i < len(tag) {
			return false
		}
		if asciiEqualFold(s[i:i+len(tag)], tag) {
			return true
		}
		j := strings.IndexByte(s[i+1:], '<')
		if j < 0 {
			return false
		}
		i += 1 + j
	}
	return false
}

// asciiEqualFold compares a and b (b lower-case) folding ASCII letters only,
// as the tokenizer does.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		c := a[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != b[i] {
			return false
		}
	}
	return true
}

// reparse parses serialized output for the checks that read it back as a
// browser will. It is a variable only so a test can count the parses (the
// RV-8 cost guard); production always uses xhtml.ParseWithOptions.
var reparse = func(doc string, scripting bool) (*xhtml.Node, error) {
	return xhtml.ParseWithOptions(strings.NewReader(doc), xhtml.ParseOptionEnableScripting(scripting))
}

func hostileIn(doc string, scripting bool) string {
	root, err := reparse(doc, scripting)
	if err != nil {
		return parseFailure(err)
	}
	var live func(n *xhtml.Node) bool
	live = func(n *xhtml.Node) bool {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && strings.EqualFold(c.Data, "meta") {
				if metaIsCharset(c) {
					return true
				}
				for _, a := range c.Attr {
					if strings.EqualFold(a.Key, "http-equiv") && hostileEquiv(a.Val) {
						return true
					}
				}
			}
			if c.Type == xhtml.ElementNode && strings.EqualFold(c.Data, "link") && liveResourceHint(c) {
				return true
			}
			if live(c) {
				return true
			}
		}
		return false
	}
	if live(root) {
		return FallbackHostile
	}
	return ""
}

// neutralize rewrites hostile nodes in place (see sanitizeHTML).
// stripReservedNamespace removes the tool's private markers from ONE
// mail-supplied element: class tokens beginning "mailarchive-" and attributes
// keyed "data-mailarchive-*". neutralize's own additions (it marks a dropped
// inline image class="mailarchive-missing-image") run after this on the same
// element and are preserved.
func stripReservedNamespace(n *xhtml.Node) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		if strings.HasPrefix(strings.ToLower(a.Key), "data-mailarchive-") {
			continue // drop the tool's private data attributes
		}
		if strings.EqualFold(a.Key, "class") {
			var toks []string
			for _, t := range strings.Fields(a.Val) {
				if !strings.HasPrefix(strings.ToLower(t), "mailarchive-") {
					toks = append(toks, t)
				}
			}
			if len(toks) == 0 {
				continue // the class held only reserved tokens: drop it entirely
			}
			a.Val = strings.Join(toks, " ")
		}
		kept = append(kept, a)
	}
	n.Attr = kept
}

func neutralize(n *xhtml.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == xhtml.ElementNode {
			// Mail may not wear the tool's private namespace. Strip any
			// class token beginning "mailarchive-" and any attribute whose key
			// begins "data-mailarchive-" from every mail-supplied element (head
			// children included — neutralize runs over the whole parsed tree).
			// The renderer writes its own header/body/subject/field markers
			// AFTER sanitizeHTML, so this removes no legitimate output; it stops
			// a hostile <head><style class="mailarchive-header"> (or a
			// data-mailarchive-field) from shadowing a real field when
			// reindex -rebuild re-derives the record from this page.
			stripReservedNamespace(c)
			switch c.DataAtom {
			case atom.Meta:
				if metaIsCharset(c) {
					n.RemoveChild(c)
					c = next
					continue
				}
				for i, a := range c.Attr {
					if strings.EqualFold(a.Key, "http-equiv") && hostileEquiv(a.Val) {
						c.Attr[i].Key = "data-mailarchive-neutralized"
					}
				}
			case atom.Link:
				neutralizeLink(c)
			case atom.Img:
				for i, a := range c.Attr {
					if strings.EqualFold(a.Key, "src") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(a.Val)), "cid:") {
						c.Attr[i].Key = "data-mailarchive-missing"
						c.Attr = append(c.Attr,
							xhtml.Attribute{Key: "alt", Val: "[inline image not included in the archived message]"},
							xhtml.Attribute{Key: "class", Val: "mailarchive-missing-image"})
					}
				}
			}
		}
		neutralize(c)
		c = next
	}
}

// neutralizeLink renames the href of a mail-supplied <link> — every one
// neutralize reaches, not only resource hints. ArchiveCSP allows nothing remote
// (only data: images and fonts and inline styles), so every network fetch a
// link could start (stylesheet, icon, preload, prefetch, modulepreload,
// manifest) is already refused and renaming the href costs the page nothing it
// showed — at most a data: favicon. The two hints the policy does not govern,
// preconnect and dns-prefetch, would otherwise open a connection to (or
// resolve) the sender's host when the archived page is opened (RV-2). Renaming
// every href is simpler than a rel list and cannot be bypassed by a link type
// the list misses. A <noscript> copy that only a reader with JavaScript off
// reads as markup is out of reach of the scripting-on parse: the re-check fails
// closed on it when it is a resource hint (and the scripting-off retry then
// renames it); any other link type there keeps its href in that reading, its
// fetch refused by the policy.
func neutralizeLink(n *xhtml.Node) {
	for i, a := range n.Attr {
		if strings.EqualFold(a.Key, "href") {
			n.Attr[i].Key = "data-mailarchive-neutralized-href"
		}
	}
}

func metaIsCharset(n *xhtml.Node) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, "charset") {
			return true
		}
	}
	return false
}

// bodyAttributes carries the mail body's presentation attributes onto our
// content wrapper (a plain <div>): style, class, dir and lang as they are,
// bgcolor as a background colour.
func bodyAttributes(body *xhtml.Node) string {
	var b strings.Builder
	for _, a := range body.Attr {
		switch strings.ToLower(a.Key) {
		case "style", "class", "dir", "lang":
			b.WriteString(` ` + strings.ToLower(a.Key) + `="` + html.EscapeString(a.Val) + `"`)
		case "bgcolor":
			b.WriteString(` style="background-color:` + html.EscapeString(a.Val) + `"`)
		}
	}
	return b.String()
}

// selectBody picks the richest available body and reports whether it is HTML.
func selectBody(m *model.Message) (string, bool) {
	if strings.TrimSpace(m.HTMLBody) != "" {
		return m.HTMLBody, true
	}
	if strings.TrimSpace(m.PlainBody) != "" {
		return m.PlainBody, false
	}
	if strings.TrimSpace(m.RTFBody) != "" {
		return m.RTFBody, false
	}
	return "", false
}

// timeLayout shows a time with its ORIGINAL UTC offset — the offset is part of
// the record (a reader must not have to guess which zone "09:30" was in).
const timeLayout = "Mon, 02 Jan 2006 15:04:05 -0700"

// fmtTime renders a time with its original offset and, when the offset is not
// UTC, the same instant in UTC — what file names and folder tables use.
func fmtTime(t time.Time) string {
	s := t.Format(timeLayout)
	if _, off := t.Zone(); off != 0 {
		s += " (" + t.UTC().Format("2006-01-02 15:04 UTC") + ")"
	}
	return s
}

// statusLine renders the message's capture-time state — read flag, importance,
// sensitivity — as one human line for the "Status" row, e.g.
// "Unread · Importance: high · Sensitivity: confidential". It is empty when none
// is set (no row is shown). The format is reversible: `reindex -rebuild`'s
// from-HTML reader (internal/app/htmlheader.go) parses it back into the model
// fields. The separator is a middle dot (U+00B7), which html.EscapeString leaves
// untouched, so the reader recovers the parts exactly.
func statusLine(m *model.Message) string {
	var parts []string
	if m.Unread {
		parts = append(parts, "Unread")
	}
	if m.Importance != "" {
		parts = append(parts, "Importance: "+m.Importance)
	}
	if m.Sensitivity != "" {
		parts = append(parts, "Sensitivity: "+m.Sensitivity)
	}
	return strings.Join(parts, " · ")
}

// Category display bounds (QC4): a pathological set of categories must not bloat
// the page or the rebuild parse. The count is capped at maxCategories and the
// total rendered category text at maxCategoriesLen; exceeding either appends a
// visible truncation note.
const (
	maxCategories    = 64
	maxCategoriesLen = 4096
)

// categoriesField renders the message's categories as the inner HTML of the
// dedicated "Categories" dd: each value in its OWN escaped, control-stripped
// <span class="mailarchive-category">, joined for display by ", " but recovered
// per-span (so a value containing ", " or " · " round-trips losslessly and can
// never be read as a Status segment — QC3). The number and total length are
// bounded with a visible truncation note (QC4). ok is false when, after
// cleaning, there is nothing to show — then no row is emitted.
func categoriesField(cats []string) (inner string, ok bool) {
	cleaned := make([]string, 0, len(cats))
	for _, c := range cats {
		if c = strings.TrimSpace(util.StripControl(c)); c != "" {
			cleaned = append(cleaned, c)
		}
	}
	if len(cleaned) == 0 {
		return "", false
	}
	truncated := false
	if len(cleaned) > maxCategories {
		cleaned = cleaned[:maxCategories]
		truncated = true
	}
	var b strings.Builder
	total, shown := 0, 0
	for _, c := range cleaned {
		if total+len(c) > maxCategoriesLen {
			truncated = true
			break
		}
		if shown > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`<span class="mailarchive-category">` + html.EscapeString(c) + `</span>`)
		total += len(c)
		shown++
	}
	if shown == 0 {
		// The first value alone exceeded the length cap: show nothing but the note.
		return " … (truncated)", true
	}
	if truncated {
		b.WriteString(` … (truncated)`)
	}
	return b.String(), true
}

func renderHeader(m *model.Message, ctx RenderContext, consumed map[int]bool) string {
	var b strings.Builder
	b.WriteString(`<div class="mailarchive-header">`)
	if ctx.RootRel != "" || ctx.FolderIndexRel != "" {
		b.WriteString(`<div class="mailarchive-nav">`)
		if ctx.RootRel != "" {
			b.WriteString(`<a href="` + html.EscapeString(ctx.RootRel+"index.html") + `">All folders</a>`)
		}
		if ctx.FolderIndexRel != "" {
			if ctx.RootRel != "" {
				b.WriteString(` · `)
			}
			b.WriteString(`<a href="` + html.EscapeString(ctx.FolderIndexRel) + `">This folder</a>`)
		}
		b.WriteString(`</div>`)
	}
	// The subject div and the From/To/Cc/Date/Sent/Received dds carry an inert
	// data-mailarchive-field attribute so `reindex -rebuild` can recover the
	// index fields from an archived page by attribute rather than by structure
	// (PC3). It changes no visible output — it is a machine tag on the tool's own
	// header markup, escaped mail content is unaffected.
	b.WriteString(`<div class="mailarchive-subject" data-mailarchive-field="subject">` + html.EscapeString(displaySubject(m)) + `</div>`)
	b.WriteString(`<dl>`)
	// row emits a labelled dd; field, when non-empty, tags that dd for rebuild.
	row := func(label, value, field string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		b.WriteString(`<dt>` + html.EscapeString(label) + `</dt>`)
		if field != "" {
			b.WriteString(`<dd data-mailarchive-field="` + field + `">` + html.EscapeString(value) + `</dd>`)
		} else {
			b.WriteString(`<dd>` + html.EscapeString(value) + `</dd>`)
		}
	}
	row("From", formatSender(m), "from")
	row("Reply-To", m.ReplyTo, "")
	row("To", m.To, "to")
	row("Cc", m.Cc, "cc")
	row("Bcc", m.Bcc, "")
	// Sent and Received are shown separately when both are known and differ;
	// otherwise the one known time is the Date.
	switch {
	case !m.Sent.IsZero() && !m.Received.IsZero() && !m.Sent.Equal(m.Received):
		row("Sent", fmtTime(m.Sent), "sent")
		row("Received", fmtTime(m.Received), "received")
	default:
		if d := m.Date(); !d.IsZero() {
			row("Date", fmtTime(d), "date")
		}
	}
	row("Message-ID", m.InternetMessageID, "")
	row("In-Reply-To", m.InReplyTo, "")
	// Capture-time message state (read flag, importance, sensitivity), shown as
	// one line when any is set. row() escapes it and tags the dd so `reindex
	// -rebuild` can read it back (statusLine's format is reversible).
	row("Status", statusLine(m), "status")
	// Capture-time categories (the operator/source classification), in their OWN
	// dedicated row — NEVER folded into the Status line (QC3) — so a category
	// value can never be tokenised as a Status segment to forge Unread/
	// Importance/Sensitivity on read-back. Each value rides in its own escaped,
	// control-stripped <span class="mailarchive-category"> (untrusted text; the
	// spans let reindex -rebuild recover the values losslessly with no separator
	// ambiguity), capped in number and total length (QC4).
	if inner, ok := categoriesField(m.Categories); ok {
		b.WriteString(`<dt>Categories</dt><dd data-mailarchive-field="categories">` + inner + `</dd>`)
	}
	b.WriteString(`</dl>`)

	// The transport-header block as stored by the source, in a collapsed panel.
	// It is sender-influenced text (Received/Authentication-Results can be
	// forged), so it is labelled "unverified", escaped, and shown inside <pre>
	// where the CSP already makes any script inert — it is never executed. The
	// block is capped at 64 KiB with a visible truncation note; when empty
	// (a PST item that never crossed the internet, a body-only blob) the panel
	// is omitted entirely.
	if strings.TrimSpace(m.TransportHeaders) != "" {
		const headerCap = 64 << 10 // 64 KiB
		text := m.TransportHeaders
		truncated := false
		if len(text) > headerCap {
			text = text[:headerCap]
			// Trim a rune split by the byte cap so escaping stays well-formed.
			for len(text) > 0 {
				if r, size := utf8.DecodeLastRuneInString(text); r == utf8.RuneError && size <= 1 {
					text = text[:len(text)-1]
					continue
				}
				break
			}
			truncated = true
		}
		b.WriteString(`<details class="mailarchive-headers"><summary>Transport headers as stored (unverified)</summary>`)
		b.WriteString(`<p class="mailarchive-headers-note">These lines are supplied by the sending and relaying servers and can be forged — shown as stored, not proof of origin.</p>`)
		b.WriteString(`<pre>`)
		b.WriteString(html.EscapeString(text))
		if truncated {
			b.WriteString(" … (truncated)")
		}
		b.WriteString(`</pre></details>`)
	}

	// Archived attachments (those not embedded inline), with the zip link, so
	// the page alone tells the reader what came with the message.
	var names []string
	for i, a := range m.Attachments {
		if consumed[i] {
			continue
		}
		names = append(names, attachmentLabel(a, i))
	}
	if len(names) > 0 {
		b.WriteString(`<div class="mailarchive-attachments"><b>Attachments:</b> `)
		for i, n := range names {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(html.EscapeString(n))
		}
		if ctx.ZipName != "" {
			b.WriteString(` — <a href="` + html.EscapeString(ctx.ZipName) + `">download zip</a>`)
		}
		b.WriteString(`</div>`)
	}
	if ctx.RawName != "" {
		b.WriteString(`<div class="mailarchive-raw"><a href="` + html.EscapeString(ctx.RawName) + `">Original message (.eml)</a></div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func displaySubject(m *model.Message) string {
	if s := strings.TrimSpace(m.Subject); s != "" {
		return s
	}
	return "(no subject)"
}

func formatSender(m *model.Message) string {
	name := strings.TrimSpace(m.SenderName)
	email := strings.TrimSpace(m.SenderEmail)
	switch {
	case name != "" && email != "" && !strings.EqualFold(name, email):
		return fmt.Sprintf("%s <%s>", name, email)
	case email != "":
		return email
	default:
		return name
	}
}

// embedInlineImages replaces cid: references to attachments with base64 data
// URIs and records which attachments were consumed.
func embedInlineImages(body string, atts []model.Attachment, consumed map[int]bool) string {
	if !strings.Contains(strings.ToLower(body), "cid:") {
		return body
	}
	for i := range atts {
		cid := strings.Trim(atts[i].ContentID, "<>")
		if cid == "" {
			continue
		}
		ref := regexp.MustCompile(`(?i)cid:` + regexp.QuoteMeta(cid))
		if !ref.MatchString(body) {
			continue
		}
		data, err := drain(atts[i])
		if err != nil || len(data) == 0 {
			continue
		}
		mt := atts[i].MimeType
		if mt == "" {
			mt = mime.TypeByExtension(filepath.Ext(atts[i].Filename))
		}
		if mt == "" {
			mt = "application/octet-stream"
		}
		dataURI := "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data)
		body = ref.ReplaceAllString(body, dataURI)
		consumed[i] = true
	}
	return body
}

func drain(a model.Attachment) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := a.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

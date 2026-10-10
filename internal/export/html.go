package export

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
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
	FallbackHostile     = "unsafe-after-reparse" // a re-parse found a live hostile meta, link hint or reserved marker, or the page's own header or body not live
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
		// Each sanitizing pass is accepted only with the page it makes: the
		// page wraps the body one level deeper than the tree the sanitizer
		// checked (html > body > div.mailarchive-body), and a page too deep for
		// the tree parser cannot be read back by `reindex -rebuild` (INT-1); and
		// the mail's head pieces come before the tool's header, so mail markup
		// that runs on past its own end can swallow the header and the body.
		// The page itself is parsed, in every reading a browser can make.
		headExtra, bodyAttrs, inner, fallback = sanitizeChecked(body, func(h, a, b string) string {
			return pageFallback(assemblePage(m, ctx, consumed, h, a, b), b)
		})
	} else {
		inner = `<pre class="plain">` + html.EscapeString(body) + `</pre>`
	}
	page := assemblePage(m, ctx, consumed, headExtra, bodyAttrs, inner)
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
// when, in every reading (the scripting flag on, and off when the page holds a
// noscript tag), it parses with x/net's default 512-element cap AND its own
// layout is live (anchoredPage) — so a page the exporter writes is never too
// deep for the tree parser, and never one whose mail markup hides the tool's
// header and body. Mail head markup that runs on past its own end — a head
// <script> left in an escape state, or what a reader with JavaScript off reads
// out of a head <noscript>: a <plaintext>, an element left open around the
// header — would put them in raw text or inside the mail's own markup: the
// page would show the reader no header and no body, and `reindex -rebuild`,
// which anchors on them, would erase the mail's own subject, sender, date and
// words (MA-187, R8). Such a page is unsafe-after-reparse (and, when the mail
// has a noscript tag, gets the in-place noscript retry; see sanitizeChecked);
// the fallback page keeps the header, so the record stays recoverable.
//
// The page is also read as a whole, not only as the check document the
// sanitizer parsed (a bare <body> with no div around the mail): in every
// reading the mail must stay INSIDE div.mailarchive-body (R3-AO1, AF4, AC2) —
// a stray `</div>` that a reader with JavaScript off meets inside a <noscript>
// closes the tool's own div, which the check document never had, and what the
// mail hid after it (a refresh, a preconnect, a second charset, a forged
// header) then comes out live; and in the scripting-on reading `reindex
// -rebuild` reads, the body div must hold exactly the markup that was
// sanitized, so a serialization that is not a fixed point (a script left in an
// escape state beside a foster-parented table, a stray end tag that closes the
// div early) cannot hide words from the page or from the rebuilt index (R3-AC3).
// And the page's own meta, link and header markers are counted: nothing the
// mail holds adds a charset or hostile meta beyond the template's, a resource
// hint, a second header or a second body div.
func pageFallback(page, inner string) string {
	for _, scripting := range scriptingModes(page) {
		root, err := reparse(page, scripting)
		if err != nil {
			return parseFailure(err)
		}
		header, bodyDiv := pageAnchors(root)
		if header == nil || bodyDiv == nil {
			return FallbackHostile
		}
		if !mailStaysInBodyDiv(bodyDiv, inner, scripting) || !pageMarkersAreOurs(root) {
			return FallbackHostile
		}
	}
	return ""
}

// pageAnchors finds, in a parsed page, the renderer's own layout: the first
// div.mailarchive-header directly in the first <body>, and the next element
// after it, which must be a div.mailarchive-body — exactly the anchor `reindex
// -rebuild` reads a current page's record by (internal/app readArchivedTree:
// findFirst body, firstChildDiv, nextElementSibling). Either is nil when it is
// not live there.
func pageAnchors(root *xhtml.Node) (header, bodyDiv *xhtml.Node) {
	body := firstElement(root, "body")
	if body == nil {
		return nil, nil
	}
	for c := body.FirstChild; c != nil && header == nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == "div" && classToken(c, "mailarchive-header") {
			header = c
		}
	}
	if header == nil {
		return nil, nil
	}
	next := header.NextSibling
	for next != nil && next.Type != xhtml.ElementNode {
		next = next.NextSibling
	}
	if next == nil || next.Data != "div" || !classToken(next, "mailarchive-body") {
		return header, nil
	}
	return header, next
}

// PageClosers are the bytes the template writes after the mail's body: what a
// <plaintext>, or a script left in an escape state, at the very end of the mail
// swallows as its own text. `reindex -rebuild` removes them from a final
// <plaintext>'s text before it indexes the page (they are the page's, not the
// mail's words; R8).
const PageClosers = "\n</div>\n</body>\n</html>\n"

// mailStaysInBodyDiv reports whether the mail is contained in div.mailarchive-
// body: nothing but whitespace follows the div in <body> in any reading, and,
// with scripting on (the reading the index and the rebuild take), the div's
// children serialize to exactly the sanitized markup between the template's
// newlines. The one deviation allowed is the page's own closing tags read as
// the text of a final <plaintext> (written with no end tag), or of a final
// script left in an escape state (written with its </script>): inert text the
// mail's own raw-text element swallowed, which the reader removes again.
func mailStaysInBodyDiv(bodyDiv *xhtml.Node, inner string, scripting bool) bool {
	for s := bodyDiv.NextSibling; s != nil; s = s.NextSibling {
		if s.Type != xhtml.TextNode || strings.TrimSpace(s.Data) != "" {
			return false
		}
	}
	if !scripting {
		return true
	}
	var b strings.Builder
	for c := bodyDiv.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&b, c); err != nil {
			return false
		}
	}
	// A rewritten noscript may end in the end tags of noscripts nested at its
	// end (closesItself); with scripting on they are stray end tags the parser
	// ignores, so they are in the sanitized markup and not in the page's.
	const strayEnd = "</noscript>"
	got, want := strings.ReplaceAll(b.String(), strayEnd, ""), strings.ReplaceAll("\n"+inner, strayEnd, "")
	switch {
	case got == want+"\n", got == want+PageClosers:
		return true
	case strings.HasSuffix(want, "</script>") && got == want+PageClosers+"</script>":
		return true
	}
	return false
}

// templateOwnMetas is how many metas the template itself writes that
// pageMarkersAreOurs counts (the charset and the policy).
var templateOwnMetas = sync.OnceValue(func() int {
	root, err := xhtml.Parse(strings.NewReader(fmt.Sprintf(docTemplate, "", "", "", "", "")))
	if err != nil {
		return -1
	}
	n, _ := countPageMarkers(root)
	return n
})

// pageMarkersAreOurs reports whether a parsed page holds exactly the metas the
// template wrote (no mail charset or hostile http-equiv meta beyond them), no
// live resource-hint link, one header marker element and one body marker
// element, in this reading.
func pageMarkersAreOurs(root *xhtml.Node) bool {
	metas, ok := countPageMarkers(root)
	return ok && metas == templateOwnMetas()
}

// countPageMarkers counts the metas that are a charset or a hostile http-equiv,
// and reports false on a live resource-hint link or when the page does not hold
// exactly one element with each of the header and body classes.
func countPageMarkers(root *xhtml.Node) (metas int, ok bool) {
	ok = true
	headers, bodies := 0, 0
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch {
			case strings.EqualFold(n.Data, "meta"):
				hostile := metaIsCharset(n)
				for _, a := range n.Attr {
					if strings.EqualFold(a.Key, "http-equiv") && hostileEquiv(a.Val) {
						hostile = true
					}
				}
				if hostile {
					metas++
				}
			case strings.EqualFold(n.Data, "link") && liveResourceHint(n):
				ok = false
			}
			if classToken(n, "mailarchive-header") {
				headers++
			}
			if classToken(n, "mailarchive-body") {
				bodies++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return metas, ok && headers <= 1 && bodies <= 1
}

// firstElement returns the first element named name in document order.
func firstElement(n *xhtml.Node, name string) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == name {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if e := firstElement(c, name); e != nil {
			return e
		}
	}
	return nil
}

// classToken reports whether an element's class attribute holds the token cls.
func classToken(n *xhtml.Node, cls string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, t := range strings.Fields(a.Val) {
				if t == cls {
					return true
				}
			}
			return false
		}
	}
	return false
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
// and the mail has a noscript tag, the sanitize pass is retried ONCE with each
// <noscript>'s content neutralized IN PLACE (neutralizeNoscriptContents): the
// content is parsed with scripting off, its metas and links disarmed, and put
// back where the scripting-on parse had the noscript. The retry does NOT
// re-parse the whole document with scripting off, because that pops a head
// <noscript>'s flow content out into the body, and a word the live indexer
// skips (it sits in an explicit head) would then show up in the rebuilt index
// only (search parity, R8). A noscript's rewritten content is kept only where
// the renderer writes it as one closed noscript in both readings
// (closesItself: never a <plaintext> or a script left in an escape state that
// runs on past it, swallowing the rest of the page); otherwise its raw text
// stays. The retry is accepted only if it passes the same dual-mode re-check;
// otherwise the body falls back as before. (The re-check is still essential:
// an escaped <meta> inside a noscript decodes to a text child that Render
// writes back raw, so a live meta smuggled beside a real one is caught and
// falls back.)
//
// sanitizeHTML runs the passes without the page check; RenderWith runs them
// through sanitizeChecked, which also reads the page each pass makes.
func sanitizeHTML(doc string) (headExtra, bodyAttrs, body, fallback string) {
	return sanitizeChecked(doc, nil)
}

// sanitizeChecked is sanitizeHTML with a page check: pageCheck, when set,
// returns why the page a pass's pieces would make must not be written ("" when
// it may; RenderWith's is pageFallback on the assembled page), and a pass is
// accepted only with its page. So the in-place noscript retry also runs when
// the first pass's re-check passes but its page is unsafe-after-reparse: what a
// reader with JavaScript off reads out of a head <noscript>'s raw text, written
// back literally, can leave an element open around the tool's header (a benign
// `<noscript><div>enable JavaScript</noscript>`), which the rewritten content,
// written closed, does not. A retry that passes the re-check is judged by its
// own page — and reports that page's reason when it fails (a page too deep is
// too-deeply-nested, never unsafe; RV-9) — while a retry that fails the
// re-check leaves the first pass's reason.
func sanitizeChecked(doc string, pageCheck func(headExtra, bodyAttrs, body string) string) (headExtra, bodyAttrs, body, fallback string) {
	withPage := func(h, a, b, f string) (string, string, string, string) {
		if f == "" && pageCheck != nil {
			f = pageCheck(h, a, b)
		}
		return h, a, b, f
	}
	// Markup nested past flattenDepth loses its container tags (not its text)
	// first, as a browser flattens past its own cap, so deep mail renders
	// instead of failing the parser's 512-element cap (DIFF-FP-2).
	flat, ok := flattenDeepChecked(doc)
	if !ok {
		// A start tag the flattener cannot drop without changing what the mail
		// shows or hides (see flattener.refuses): show the source (R3-AC5).
		return "", "", "", FallbackTooDeep
	}
	headExtra, bodyAttrs, body, fallback = withPage(sanitizeParsed(flat, len(doc), false))
	if fallback == FallbackHostile && hasNoscriptTag(flat) {
		// Without a noscript tag the scripting-off parse is the same tree
		// (scriptingModes), so the retry could change nothing. Retry with the
		// noscript contents neutralized in place — NOT by re-parsing the whole
		// document with scripting off, which moves a head <noscript>'s flow
		// content into the body (a word the live index skips, because it is in
		// an explicit head, would then be in the rebuilt index only; R8). The
		// document keeps the structure the scripting-on parse gives it.
		if h, a, b, f := sanitizeParsed(flat, len(doc), true); f == "" {
			return withPage(h, a, b, f)
		}
	}
	return headExtra, bodyAttrs, body, fallback
}

// sanitizeParsed is one sanitizing pass over doc (already flattened). The main
// parse always runs with scripting on, like the browser that reads most
// archives; inputLen sizes the serialization cap. When neutralizeNoscript is
// set (the DIFF-FP-1 retry), each <noscript>'s content — opaque raw text to the
// scripting-on parse — is parsed with scripting off, neutralized and put back
// in place, so a benign meta/charset/link inside it is disarmed without moving
// the noscript's other content out of its head (search parity, R8).
func sanitizeParsed(doc string, inputLen int, neutralizeNoscript bool) (headExtra, bodyAttrs, body, fallback string) {
	root, err := xhtml.ParseWithOptions(strings.NewReader(doc), xhtml.ParseOptionEnableScripting(true))
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
	if neutralizeNoscript {
		// The rewritten noscripts are checked by writing them out; that draws on
		// a budget of its own the size of the serialization cap, so a noscript
		// whose rewrite inflates past it fails the retry as too-large unread.
		if err := neutralizeNoscriptContents(root, serializedCap(inputLen)); err != nil {
			return "", "", "", renderFailure(err)
		}
	}

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
	if bodyNode == nil {
		// The parser builds a <body> for every mail but a <frameset> document
		// (the frameset replaces it): there is no body to write, and an empty
		// page would lose the mail silently — its <noframes> text is the only
		// readable part (R1, R7; R3-AF3). Show the source instead.
		return "", "", "", FallbackUnparseable
	}
	for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
		if err := renderNode(bb, c); err != nil {
			return "", "", "", renderFailure(err)
		}
	}
	bodyAttrs = bodyAttributes(bodyNode)
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
//
// 16x alone scaled with the padding, though (R3-AC6): a small inflating mail plus
// a 10 MB harmless comment was allowed 160 MB, serialized to a 126 MB page and
// re-parsed in both readings (18.6 s, 3.2 GB, the archive lock held). The cap is
// therefore also at most serializedHeadroom beyond the input: ordinary output is
// within a few times its input, so only mail past some MB of input feels it, and
// the cost the re-checks can be made to spend is bounded by the headroom.
func serializedCap(n int) int { return max(min(16*n, n+serializedHeadroom), 1<<20) }

// serializedHeadroom is how far past its input the sanitized serialization may
// grow (see serializedCap). A variable only so a test can lower it (the real one
// is too big to inflate past in a unit test); production never changes it.
var serializedHeadroom = 32 << 20

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

// flattenDeepChecked is flattenDeep that also reports whether every dropped tag
// could be dropped without changing what the mail shows or hides: false when a
// start tag the flattener refuses to drop (flattener.refuses) comes past the
// cap, in which case the caller shows the mail's source.
func flattenDeepChecked(doc string) (string, bool) { return flattenChecked(doc, flattenDepth) }

func flattenAt(doc string, limit int) string {
	out, _ := flattenChecked(doc, limit)
	return out
}

func flattenChecked(doc string, limit int) (string, bool) {
	z := xhtml.NewTokenizer(strings.NewReader(doc))
	f := &flattener{limit: limit}
	var out *strings.Builder
	var last byte // the last byte written to out
	pos := 0      // bytes of doc covered by the tokens read so far (tokens are contiguous)
	for {
		if f.refused {
			return doc, false
		}
		top := f.top()
		z.AllowCDATA(top != nil && top.ns != "") // as the parser does: CDATA only in foreign content
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			break
		}
		raw := z.Raw()
		keep, name := true, ""
		var repl string // written in place of a dropped token
		switch tt {
		case xhtml.TextToken:
			if top != nil && top.ns != "" && len(f.drop) > 0 && bytes.HasPrefix(raw, []byte("<![CDATA[")) {
				// A CDATA section is text only in foreign content. Its SVG or
				// MathML root is dropped here, so written as it is the section would
				// be a bogus comment in HTML and its text would vanish from the page
				// (R3-AC5): write its text, escaped, as the foreign reading holds it.
				keep = false
				repl = html.EscapeString(strings.TrimSuffix(strings.TrimPrefix(string(raw), "<![CDATA["), "]]>"))
			}
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
		case !keep && repl != "":
			out.WriteString(repl)
			last = repl[len(repl)-1]
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
	if f.refused {
		return doc, false
	}
	if out == nil {
		return doc, true
	}
	out.WriteString(doc[pos:]) // bytes the tokenizer returned no token for (an unterminated tag at EOF)
	return out.String(), true
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
	refused  bool           // a start tag past the cap that cannot be dropped (refuses)
}

// refuses reports whether dropping el's start tag past the cap would change
// what the mail shows or hides, not merely how its content is grouped. The
// flattener keeps text and drops container tags, which is exact for a container
// that only groups. These are not that, and the dropped tag makes the page and
// the rebuilt index read the mail differently from the original (R3-AC5, san-Q2
// shape 2): a <template>, whose content is inert; an SVG <style> or <script>,
// which no reader renders or indexes (their text, once the tags are gone, is
// shown and indexed); and a <noscript>, raw text to the scripting-on reading and
// markup to the scripting-off one, whose end tag then ends differently than the
// mail's. Mail with one of these that deep is shown as source instead (the
// reason it had before flattening existed). An SVG or MathML root itself is
// dropped as before: what its scope changes for the leaves written in its place
// (a self-closing <textarea/>, a CDATA section) is handled where they are read.
func (el flatEl) refuses() bool {
	switch {
	case el.ns == "" && (el.a == atom.Template || el.a == atom.Noscript):
		return true
	case el.ns == "svg" && (el.a == atom.Style || el.a == atom.Script):
		return true
	}
	return false
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
		if el.refuses() {
			f.refused = true
		}
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
				// A leaf. In the dropped region its SVG or MathML root is gone,
				// and written as it is `<textarea/>` (or style, title, xmp, ...)
				// would be an HTML raw-text element that swallows the rest of
				// the page, in a mail where it was a closed foreign element
				// (R3-AC5): drop it, there is nothing in it to keep.
				return len(f.drop) == 0
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
		// path below, which would clear the drop stack. The part may belong to
		// a table opened (and dropped) in that region: credited to the kept
		// table instead, that table's </table> would close the kept one, and
		// the kept table's next <td> would be a stray tag the parser ignores,
		// gluing the words on either side of it.
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

// FlatBlockNames lists the names flatBlock leaves a word boundary for, sorted.
// It exists for one test: the set must be the search index's blockElements
// (listed by internal/index BlockElementNames), and neither package can see
// the other's, so an external test of this package, which imports both,
// compares the two lists (MA-477).
func FlatBlockNames() []string {
	var names []string
	for name, block := range flatBlock {
		if block {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

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
// it, or any live element wearing a reserved marker neutralize did not add (a
// forged tool header that stripReservedNamespace missed because it was raw text
// to the scripting-on parse, inside a noscript). Every meta and link element is
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
			if c.Type == xhtml.ElementNode && foreignReservedMarker(c) {
				// A mail element wearing the tool's private namespace (a
				// mailarchive-* class or a data-mailarchive-* attribute that is
				// not one neutralize adds): stripReservedNamespace never reached
				// it, so it was raw text to the scripting-on parse — inside a
				// <noscript>, where a reader with JavaScript off reads it as
				// markup and would see, e.g., a forged tool-styled header above
				// the real one. Fail closed so the in-place noscript retry strips
				// it (or the body falls back). See reader review issue 6.
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

// neutralizeNoscriptContents rewrites, in place, the content of every
// outermost HTML <noscript> on the scripting-on parse tree so a reader with
// JavaScript off reads it neutralized, WITHOUT the restructuring a full
// scripting-off re-parse of the whole document does — which pops a head
// <noscript>'s flow content out into the body, so a word the live indexer skips
// (it is in an explicit head) would appear in the rebuilt index only (R8). With
// scripting on an HTML <noscript> holds one raw-text node (its markup as text);
// that text is parsed as a body fragment with scripting off, neutralized
// (metas disarmed, charsets and the tool's reserved markers stripped,
// resource-hint links renamed) and put back as the noscript's element children.
// Serialized, those children are markup a JS-off browser reads as the
// neutralized elements and the scripting-on re-check reads back as inert raw
// text; an escaped `&lt;meta&gt;` decodes to a text child that Render writes
// literally, so a live meta smuggled beside a real one is caught by the
// dual-mode re-check and falls back.
//
// Each outermost noscript is rewritten ONCE: the walk never descends into the
// content it has just built. A <noscript> nested in it is markup already (the
// scripting-off fragment parse read it as an ordinary element), and its text is
// text; parsing that text again decoded its entities a second time and dropped
// the nested noscript's element children (an escaped refresh became a
// neutralized <meta> element, and its words left the rebuilt index). Once every
// outermost noscript is rewritten, a nested noscript's text is set to be
// written escaped (keepNestedNoscriptText). An SVG or MathML <noscript> is an
// ordinary foreign element whose children were markup to the main parse
// (neutralize reached them): it is not raw text and is left as it is.
//
// A rewrite is then kept only if the renderer writes it as ONE closed noscript
// in both readings of the page (closesItself); otherwise the noscript gets its
// raw text back, as the first pass had it. Writing the content out for that
// check draws on a budget of limit bytes; past it the error is errTooLarge.
func neutralizeNoscriptContents(root *xhtml.Node, limit int) error {
	type rewrite struct {
		ns  *xhtml.Node
		raw []*xhtml.Node // its raw-text children, put back if the rewrite is refused
	}
	var rewritten []rewrite
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && c.DataAtom == atom.Noscript && c.Namespace == "" {
				if raw, ok := neutralizeOneNoscript(c); ok {
					rewritten = append(rewritten, rewrite{c, raw})
				}
				continue
			}
			walk(c)
		}
	}
	walk(root)
	budget := limit
	for _, r := range rewritten {
		keepNestedNoscriptText(r.ns)
		closed, err := closesItself(r.ns, &budget)
		if err != nil {
			return err
		}
		if !closed {
			// Refused: the noscript keeps the raw text the scripting-on parse gave
			// it. The re-check then reads it as it read the first pass (falling
			// back if that is hostile), and the page check still refuses a page
			// whose header and body it would hide.
			for c := r.ns.FirstChild; c != nil; {
				next := c.NextSibling
				r.ns.RemoveChild(c)
				c = next
			}
			for _, c := range r.raw {
				r.ns.AppendChild(c)
			}
		}
	}
	return nil
}

// neutralizeOneNoscript neutralizes one <noscript>'s raw-text content in place
// (see neutralizeNoscriptContents) and reports whether it rewrote it, with the
// raw-text children it took out. A nested <noscript> inside is markup to the
// scripting-off fragment parse, so one pass reaches every level.
func neutralizeOneNoscript(ns *xhtml.Node) ([]*xhtml.Node, bool) {
	var raw strings.Builder
	for c := ns.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode {
			raw.WriteString(c.Data)
		}
	}
	if strings.TrimSpace(raw.String()) == "" {
		return nil, false
	}
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := xhtml.ParseFragmentWithOptions(strings.NewReader(raw.String()), ctx, xhtml.ParseOptionEnableScripting(false))
	if err != nil {
		return nil, false // leave as raw text; the dual-mode re-check falls back if hostile
	}
	var old []*xhtml.Node
	for c := ns.FirstChild; c != nil; {
		next := c.NextSibling
		ns.RemoveChild(c)
		old = append(old, c)
		c = next
	}
	for _, nd := range nodes {
		ns.AppendChild(nd)
	}
	neutralize(ns) // disarm the newly-appended children and their descendants
	return old, true
}

// noscriptSentinel follows a rewritten noscript in closesItself's
// scripting-off reading. Mail cannot carry the attribute: neutralize strips the
// data-mailarchive- namespace from the content first, and text that decodes to
// it is found by position (it would have to come out of the noscript, which is
// what the reading checks).
const noscriptSentinel = `<div data-mailarchive-sentinel=""></div>`

// closesItself reports whether a rewritten noscript, as the renderer writes it,
// re-parses to ONE closed noscript in both readings of the page: nothing after
// it is swallowed, and nothing in it gets out. Its content came from a
// scripting-off fragment parse that ended at the end of the noscript's raw
// text, and the renderer does not always write it back to read that way: it
// writes no end tag at all after a <plaintext> (render.go plaintextAbort, so
// the </noscript> is never written), it writes a script's and a noscript's
// text literally (a script left in an escape state by `<!--<script>` runs past
// its </script> and the </noscript>; text that decodes to `</noscript>`,
// `<plaintext>` or `<style>` ends the noscript early or opens raw text).
//
//   - With scripting on (a browser's default, and `reindex -rebuild`'s
//     reading), the content must be raw text running to the noscript's own
//     end tag, and only stray </noscript> end tags may follow it — the end tags
//     of noscripts nested at the end of the content, which the HTML insertion
//     modes ignore (no HTML noscript is ever open in that reading). Not where
//     the noscript's parent is an SVG or MathML integration point: there the
//     foreign-content rules read a stray end tag, and they pop up to a foreign
//     element of the same name (`<svg><noscript><foreignObject>` ...), moving
//     the content after it out of the integration point — an HTML <xmp>'s
//     text into an SVG <style> that no reader sees. The tokenizer reads a
//     noscript's content as raw text exactly as the scripting-on tree builder
//     has it read.
//   - With scripting off, the noscript in a <div> (the body div or mail element
//     it sits in) must close before a sentinel written after it: the div's
//     children are exactly the noscript and the sentinel, so a stray </div>
//     that would close the page's body div is caught too.
//
// The write draws on budget (errTooLarge past it).
func closesItself(ns *xhtml.Node, budget *int) (bool, error) {
	w := &capWriter{left: budget}
	if err := renderNode(w, ns); err != nil {
		return false, err
	}
	written := w.String()

	foreignParent := ns.Parent != nil && ns.Parent.Type == xhtml.ElementNode && ns.Parent.Namespace != ""
	z := xhtml.NewTokenizer(strings.NewReader(written))
	if tt := z.Next(); tt != xhtml.StartTagToken {
		return false, nil
	}
	if name, _ := z.TagName(); string(name) != "noscript" {
		return false, nil
	}
	ended := false
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			if z.Err() != io.EOF || !ended {
				return false, nil // no end tag: the raw text ran to the end
			}
			break
		}
		switch {
		case tt == xhtml.TextToken && !ended:
			// the raw text, before the end tag
		case tt == xhtml.EndTagToken:
			if name, _ := z.TagName(); string(name) != "noscript" || ended && foreignParent {
				return false, nil
			}
			ended = true
		default:
			return false, nil
		}
	}

	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := xhtml.ParseFragmentWithOptions(strings.NewReader("<div>"+written+noscriptSentinel+"</div>"), ctx, xhtml.ParseOptionEnableScripting(false))
	if err != nil || len(nodes) != 1 || nodes[0].Type != xhtml.ElementNode || nodes[0].DataAtom != atom.Div {
		return false, nil
	}
	first := nodes[0].FirstChild
	if first == nil || first.Type != xhtml.ElementNode || first.DataAtom != atom.Noscript || first.Namespace != "" {
		return false, nil
	}
	s := first.NextSibling
	if s == nil || s.NextSibling != nil || s.Type != xhtml.ElementNode || s.DataAtom != atom.Div || len(s.Attr) != 1 || s.Attr[0].Key != "data-mailarchive-sentinel" {
		return false, nil
	}
	return true, nil
}

// keepNestedNoscriptText writes the text of every HTML <noscript> nested in a
// rewritten noscript's content escaped, as the mail wrote it. To the
// scripting-off fragment parse that built the content, a nested noscript is an
// ordinary element and its text is text, its entities decoded once; but x/net
// writes a noscript's text children back literally (render.go
// childTextNodesAreLiteral always includes noscript), so a reader with
// JavaScript off would decode them a second time and read escaped markup as
// markup — `&lt;b&gt;` as a <b> element, an escaped refresh as a live <meta>.
// A RawNode holding the escaped text is written as it is, in that position and
// any other, so the nested noscript's text stays text. (The outer noscript's own
// text children are written literally still: MA-476 pins that an escaped meta
// there beside a real one falls back.)
func keepNestedNoscriptText(outer *xhtml.Node) {
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && c.DataAtom == atom.Noscript && c.Namespace == "" {
				for t := c.FirstChild; t != nil; t = t.NextSibling {
					if t.Type == xhtml.TextNode {
						t.Type, t.Data = xhtml.RawNode, xhtml.EscapeString(t.Data)
					}
				}
			}
			walk(c)
		}
	}
	walk(outer)
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
						// The caption and the marker class only where the image has
						// none: a second alt or class is dropped by the parser on
						// re-parse (the first attribute wins, so the browser used the
						// image's own anyway) and the serialization would no longer be
						// a fixed point, which the page check reads as hostile.
						if !hasAttrKey(c, "alt") {
							c.Attr = append(c.Attr, xhtml.Attribute{Key: "alt", Val: "[inline image not included in the archived message]"})
						}
						if !hasAttrKey(c, "class") {
							c.Attr = append(c.Attr, xhtml.Attribute{Key: "class", Val: "mailarchive-missing-image"})
						}
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

// foreignReservedMarker reports whether a live element carries a reserved
// marker — a class token beginning "mailarchive-" or an attribute keyed
// "data-mailarchive-" — that neutralize did NOT itself add. neutralize marks a
// disarmed meta (data-mailarchive-neutralized), a renamed link href
// (data-mailarchive-neutralized-href) and a dropped inline image
// (data-mailarchive-missing + class mailarchive-missing-image) on the parsed
// tree, and stripReservedNamespace clears every other reserved marker from
// mail elements there. So any other reserved marker on a live element in the
// serialized output means stripping never reached it — it was raw text to the
// scripting-on parse (inside a <noscript>), and a reader with JavaScript off
// reads it as markup. The re-check fails closed on it (reader review issue 6).
func foreignReservedMarker(n *xhtml.Node) bool {
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if strings.HasPrefix(key, "data-mailarchive-") {
			switch key {
			case "data-mailarchive-neutralized", "data-mailarchive-neutralized-href", "data-mailarchive-missing":
			default:
				return true
			}
		}
		if strings.EqualFold(a.Key, "class") {
			for _, t := range strings.Fields(a.Val) {
				if strings.HasPrefix(strings.ToLower(t), "mailarchive-") && !strings.EqualFold(t, "mailarchive-missing-image") {
					return true
				}
			}
		}
	}
	return false
}

// hasAttrKey reports whether n has an attribute named key (ASCII case-folded).
func hasAttrKey(n *xhtml.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
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

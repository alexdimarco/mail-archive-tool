package app

import (
	"bytes"
	"regexp"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// This file reads back the index fields the exporter wrote into a message's
// <stem>.html, for `reindex -rebuild` on an archive whose sibling .eml is not
// present (every PST/default archive; anything built without -raw). It is
// anchored to the FIRST `.mailarchive-header` element the renderer emits and to
// the FIRST `<dl>` inside it, so no mail body markup that appears later in the
// document can shadow a real header field (PC3, L1). It recovers what
// renderHeader (internal/export/html.go) already writes for the installed base;
// the renderer additionally tags those elements with `data-mailarchive-field`
// going forward, which this reader prefers when present. Whatever cannot be
// recovered is left empty and counted, never fatal.
//
// Two page layouts are on disk. v0.3.0 onward render the mail into the tool's
// own document: <body> holds the header div, then the body div, and every mail
// element has had the tool's reserved class tokens and data-mailarchive-*
// attributes stripped, so the first header div directly in <body> is the
// renderer's. v0.1.1–v0.2.3 stripped nothing, and for a mail that was a full
// HTML document kept the mail's own document with the header injected right
// after its first `<body ...>` tag; there the header is anchored by those bytes
// (legacyHeaderSpan), never by document order (RV-1).

// htmlHeaderLayout mirrors export.timeLayout — fmtTime's leading stamp, which
// carries the message's ORIGINAL offset. It is duplicated here (the export
// const is unexported) so the reader parses exactly what the renderer wrote.
const htmlHeaderLayout = "Mon, 02 Jan 2006 15:04:05 -0700"

// htmlHeaderLegacyLayout is the Date stamp v0.1.1–v0.2.3 wrote
// (d.Format("Mon, 02 Jan 2006 15:04:05 MST")): the zone's abbreviation, or a
// bare offset when the zone has no name (which htmlHeaderLayout already reads).
const htmlHeaderLegacyLayout = "Mon, 02 Jan 2006 15:04:05 MST"

// reHeaderUTC matches fmtTime's parenthetical UTC instant "(YYYY-MM-DD HH:MM
// UTC)", appended whenever the offset is not UTC. The design recovers the date
// from this parenthetical (using Received else Sent); the leading stamp is the
// fallback for a UTC time, which carries no parenthetical.
var reHeaderUTC = regexp.MustCompile(`\((\d{4}-\d{2}-\d{2} \d{2}:\d{2}) UTC\)`)

// reLegacyBodyOpen is v0.1.1–v0.2.3's reBodyOpen: the header went in right
// after the FIRST match in the mail's document (or the template's own <body>).
var reLegacyBodyOpen = regexp.MustCompile(`(?i)<body[^>]*>`)

// reLegacyHeader matches, anchored, what those releases wrote right after that
// match: "\n" and renderHeader's exact output, every value html-escaped (so no
// '<' inside), with no data-mailarchive-field markers and no nav.
var reLegacyHeader = regexp.MustCompile(`^\n(<div class="mailarchive-header"><div class="mailarchive-subject">[^<]*</div><dl>(?:<dt>[^<]*</dt><dd>[^<]*</dd>)*</dl></div>)`)

// readArchivedHTML parses an exported page's bytes and recovers a message's
// index fields (subject, sender, recipients, date, body text). It returns the
// recovered message and the number of CORE fields (subject, from, date) that
// could not be recovered — the honesty count the rebuild summary reports (PC5).
// Attachment names are NOT read here: the rebuild reads them from the sibling
// zip's central directory (PC3). A parse that finds no header leaves the three
// core fields empty and counts all three; the body is still recovered when a
// `.mailarchive-body` element is present.
func readArchivedHTML(data []byte) (*model.Message, int) {
	root, err := xhtml.Parse(bytes.NewReader(data))
	if err == nil {
		return readArchivedTree(data, root)
	}
	// The parser refuses trees nested past its cap (x/net: 512 open elements),
	// which a browser renders and an archived mail body can reach. The tool's
	// own head and header are shallow, so split the page at the renderer's
	// body div: read the header fields from the shallow part, and hand the body
	// bytes to the indexer as they are (its text extraction is a tokenizer pass
	// with no depth limit). A page v0.1.1–v0.2.3 wrote for a full-document mail
	// has no body div: split it right after its anchored header (RV-7).
	if root, body, ok := splitAtLegacyHeader(data); ok {
		m, unrecovered := readArchivedTree(data, root)
		m.HTMLBody = body
		return m, unrecovered
	}
	if root, body, ok := splitAtBodyDiv(data); ok {
		m, unrecovered := readArchivedTree(data, root)
		m.HTMLBody = body
		return m, unrecovered
	}
	// Unparseable bytes: nothing recovered. The record is still indexed with
	// empty fields (never dropped), and all three core fields are counted.
	return &model.Message{}, 3
}

// splitAtLegacyHeader splits a page too deep for the tree parser right after
// the header v0.1.1–v0.2.3 injected (legacyHeaderSpan), for the layout with no
// body div: the mail's own document, its content after the header. It returns
// the tree of the page up to the header's end and the bytes after it, and
// ok=true only when that header is the page's live one (anchorHeader, RV-1),
// so a page whose span is not live, or that is not theirs, is left to
// splitAtBodyDiv.
func splitAtLegacyHeader(data []byte) (root *xhtml.Node, body string, ok bool) {
	_, end, legacy := legacyHeaderSpan(data)
	if !legacy {
		return nil, "", false
	}
	root, err := parsePrefix(data[:end])
	if err != nil {
		return nil, "", false
	}
	bodyEl := findFirst(root, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "body" })
	if bodyEl == nil || anchorHeader(data, bodyEl) == nil {
		return nil, "", false
	}
	return root, string(data[end:]), true
}

// bodyDivOpen begins the renderer's body div start tag in every release
// (`<div class="mailarchive-body"` + the mail body's own attributes + `>`).
const bodyDivOpen = `<div class="mailarchive-body"`

// splitAtBodyDiv splits a page too deep for the tree parser at the renderer's
// body div. It returns the tree of the page up to the end of that div's start
// tag (the head and the header: shallow) and the bytes after the tag (the mail
// body, for the indexer's text pass, which has no depth limit).
//
// The split is chosen by the tree parser, never by a bare tokenizer: a
// tokenizer reads <style> as raw text even inside <svg>, where the tree does
// not, so mail head content could show it tags the tree reads as a comment,
// and choose the split (RV-5). Every occurrence of bodyDivOpen in the bytes is
// a candidate; candidate i is the split when data[:end of its tag] parses and,
// in that tree, the header (anchorHeader) is followed directly by a live body
// div: the candidate's own tag, which inside a comment or raw text is only
// text. Mail on a v0.3.0+ page cannot wear the class, so the renderer's div is
// the only live one and every candidate before it is text: "does not parse, or
// is the split" is false before the renderer's candidate and true from it on,
// and a binary search finds it in a logarithmic number of parses, however many
// decoys a hostile head carries. The result is checked, never assumed; with no
// split the page is unsplittable (ok=false).
func splitAtBodyDiv(data []byte) (root *xhtml.Node, body string, ok bool) {
	var ends []int // end offsets of the candidate start tags, ascending
	for off := 0; ; {
		i := bytes.Index(data[off:], []byte(bodyDivOpen))
		if i < 0 {
			break
		}
		start := off + i
		gt := bytes.IndexByte(data[start:], '>')
		if gt < 0 {
			break
		}
		if end := start + gt + 1; len(ends) == 0 || end > ends[len(ends)-1] {
			ends = append(ends, end)
		}
		off = start + len(bodyDivOpen)
	}
	probe := func(end int) (*xhtml.Node, bool) {
		root, err := parsePrefix(data[:end])
		if err != nil {
			return nil, false
		}
		return root, bodyDivFollowsHeader(data, root)
	}
	lo, hi := 0, len(ends)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if root, split := probe(ends[mid]); root == nil || split {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if lo == len(ends) {
		return nil, "", false
	}
	if root, split := probe(ends[lo]); split {
		return root, string(data[ends[lo]:]), true
	}
	return nil, "", false
}

// parsePrefix parses a page prefix for splitAtBodyDiv. It is a variable only so
// a test can count the parses a split costs (the cost guard); production always
// uses xhtml.Parse.
var parsePrefix = func(b []byte) (*xhtml.Node, error) { return xhtml.Parse(bytes.NewReader(b)) }

// bodyDivFollowsHeader reports whether, in a parsed page prefix, the page's
// header is followed (whitespace aside) by a live div.mailarchive-body.
//
// When the page carries old header bytes that are not live (anchorHeader
// returns nil: a current page whose head raw text mimics them, or an old page
// whose real header landed in its <title>; MA-187), the split is chosen after
// the first header div directly in <body> — the renderer's own on a current
// page — so the body words survive. Only the split is chosen here:
// readArchivedTree re-anchors on the prefix and counts the core fields, so
// that div is never read for them (RV-5's "vanishes from search" otherwise came
// back by this trigger: no candidate was ever accepted).
func bodyDivFollowsHeader(data []byte, root *xhtml.Node) bool {
	bodyEl := findFirst(root, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "body" })
	if bodyEl == nil {
		return false
	}
	header := anchorHeader(data, bodyEl)
	if header == nil {
		header = firstChildDiv(bodyEl, "mailarchive-header")
	}
	if header == nil {
		return false
	}
	next := nextElementSibling(header)
	return next != nil && next.Data == "div" && hasClass(next, "mailarchive-body")
}

func nextElementSibling(n *xhtml.Node) *xhtml.Node {
	for c := n.NextSibling; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode {
			return c
		}
	}
	return nil
}

// legacyHeaderSpan reports where a page written by v0.1.1–v0.2.3 carries the
// header that release wrote: the byte span [start, end) right after the first
// `<body ...>` match. Those releases put it there on every page, in exactly the
// shape reLegacyHeader matches, so ok=false proves the page is not theirs.
func legacyHeaderSpan(data []byte) (start, end int, ok bool) {
	loc := reLegacyBodyOpen.FindIndex(data)
	if loc == nil {
		return 0, 0, false
	}
	mm := reLegacyHeader.FindSubmatchIndex(data[loc[1]:])
	if mm == nil {
		return 0, 0, false
	}
	return loc[1] + mm[2], loc[1] + mm[3], true
}

// anchorHeader returns the renderer's own header element among bodyEl's
// children, or nil when it cannot be told apart.
//
// A page v0.3.0 or later wrote (no legacy span) keeps the first header div
// directly in <body>: every mail element was stripped of the reserved class.
//
// A page with a legacy span may be an old one, whose mail kept its classes: a
// forged header div in the mail's <head> becomes a child of <body> AHEAD of the
// real one (RV-1). Its header is the span's bytes, and it is trusted only where
// it is live — the same element, by its serialization, directly in <body>;
// earlier header divs are ignored. A span that is not live (an old mail whose
// first `<body` sat in its <title>, or a current mail whose head raw text
// mimics the old bytes) yields nil: the core fields are counted, never read
// from another div.
func anchorHeader(data []byte, bodyEl *xhtml.Node) *xhtml.Node {
	start, end, legacy := legacyHeaderSpan(data)
	if !legacy {
		return firstChildDiv(bodyEl, "mailarchive-header")
	}
	nodes, err := xhtml.ParseFragment(bytes.NewReader(data[start:end]), &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil || len(nodes) != 1 {
		return nil
	}
	want := renderNodeString(nodes[0])
	for c := bodyEl.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == "div" && hasClass(c, "mailarchive-header") && renderNodeString(c) == want {
			return c
		}
	}
	return nil
}

func renderNodeString(n *xhtml.Node) string {
	var b strings.Builder
	_ = xhtml.Render(&b, n)
	return b.String()
}

// readArchivedTree recovers the index fields from a parsed exported page (see
// readArchivedHTML). data is the page's bytes (all of them, also when root is
// the tree of only a prefix), which anchor the header on an old page.
func readArchivedTree(data []byte, root *xhtml.Node) (*model.Message, int) {
	m := &model.Message{}

	// Anchor to the renderer's OWN elements, not document-order-first. The
	// renderer emits exactly two <div> direct children of <body>: the
	// `.mailarchive-header` div then the `.mailarchive-body` div, and it writes
	// them itself (mail content lives nested inside the body div). Requiring a
	// <div> that is a DIRECT CHILD OF <body> — never descending into <head> or a
	// mail-supplied <template>/<style> — means a hostile message cannot inject an
	// element earlier in document order and have it read as the header
	// (AGG-1/INT-1). An old page, written before the source also stripped the
	// reserved namespace, CAN carry a live one ahead of the real header, so
	// there the header is anchored by bytes instead (anchorHeader, RV-1).
	bodyEl := findFirst(root, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "body" })
	if bodyEl == nil {
		return m, 3
	}
	header := anchorHeader(data, bodyEl)

	// The body text: the `.mailarchive-body` div right after the header, or,
	// with no header found, the FIRST such div directly in <body>, so search
	// still works on a torn page. A page v0.1.1–v0.2.3 wrote for a full-document
	// mail has no body div at all: the mail's content follows the header inside
	// its own <body>, so that content is the body (RV-7).
	var bodyDiv *xhtml.Node
	if header != nil {
		if next := nextElementSibling(header); next != nil && next.Data == "div" && hasClass(next, "mailarchive-body") {
			bodyDiv = next
		}
	} else {
		bodyDiv = firstChildDiv(bodyEl, "mailarchive-body")
	}
	switch {
	case bodyDiv != nil:
		m.HTMLBody = innerHTML(bodyDiv)
		// A body the renderer could not show as HTML is kept as escaped source in
		// a reserved-attribute <pre> (export.plainFallback). Its text IS the
		// mail's HTML, so index that — the mail's words, as the live export did —
		// not the markup as words.
		if src, ok := fallbackSource(bodyDiv); ok {
			m.HTMLBody = src
		}
	case header != nil:
		var b strings.Builder
		for c := header.NextSibling; c != nil; c = c.NextSibling {
			_ = xhtml.Render(&b, c)
		}
		m.HTMLBody = b.String()
	}

	if header == nil {
		return m, 3
	}

	unrecovered := 0

	// Subject: prefer the going-forward attribute, else the subject div class.
	subjEl := findFirst(header, func(n *xhtml.Node) bool { return attr(n, "data-mailarchive-field") == "subject" })
	if subjEl == nil {
		subjEl = findFirst(header, func(n *xhtml.Node) bool { return hasClass(n, "mailarchive-subject") })
	}
	if subjEl == nil {
		unrecovered++
	} else if s := strings.TrimSpace(textContent(subjEl)); s != "" && s != "(no subject)" {
		// "(no subject)" is displaySubject's placeholder for an empty subject:
		// recovering it as empty is faithful, not an unrecovered field.
		m.Subject = s
	}

	// Header fields from the FIRST <dl> inside the header (first-match): a
	// hostile body's later <dt>/<dd> or data-mailarchive-field cannot reach here.
	fields := readFirstDL(header)

	if from := fields["from"]; from != "" {
		m.SenderName, m.SenderEmail = splitSender(from)
	} else {
		unrecovered++
	}
	m.To = fields["to"]
	m.Cc = fields["cc"]

	// Date: Received, else a single Date row, else Sent (matching Date()'s
	// preference so the index date column is the same instant the export used).
	switch {
	case fields["received"] != "":
		if t, ok := parseHeaderTime(fields["received"]); ok {
			m.Received = t
		} else {
			unrecovered++
		}
	case fields["date"] != "":
		if t, ok := parseHeaderTime(fields["date"]); ok {
			m.Received = t
		} else {
			unrecovered++
		}
	case fields["sent"] != "":
		if t, ok := parseHeaderTime(fields["sent"]); ok {
			m.Sent = t
		} else {
			unrecovered++
		}
	default:
		unrecovered++
	}

	// Message state (PC17): the renderer's reversible "Status" line, read back
	// into the model. These fields are not indexed and not part of identity, so
	// recovering them changes no search result or dedup decision — it keeps a
	// rebuilt model faithful to the page it came from. Absent → left empty; not
	// counted as an unrecovered CORE field.
	if st := fields["status"]; st != "" {
		applyStatusLine(m, st)
	}

	// Categories (K1): recovered from their OWN dedicated field, span by span,
	// so the values round-trip losslessly and — living in their own field, never
	// the Status line — can NEVER be tokenised as a Status segment (QC3). Like
	// the state fields they are not indexed and not part of identity; recovering
	// them only keeps a rebuilt model faithful. Read from the SAME anchored
	// header the other fields use, so a hostile body cannot inject them.
	m.Categories = readCategoriesField(header)

	return m, unrecovered
}

// fallbackReasons are the values export.plainFallback writes on the <pre>.
var fallbackReasons = map[string]bool{
	export.FallbackTooDeep:     true,
	export.FallbackUnparseable: true,
	export.FallbackHostile:     true,
	"too-large":                true, // export.FallbackTooLarge, being added with the output-size cap (RV-8); a literal until it lands
}

// fallbackSource returns the mail's HTML source from a body div the renderer
// wrote for a body shown as source, and ok=false for any other body div. Mail
// on a v0.3.0+ page cannot carry data-mailarchive-fallback (the sanitizer
// strips the namespace), but pages v0.1.1–v0.2.3 wrote put a mail fragment raw
// into the body div, so a mail there can carry it on a decoy <pre> (RV-6).
// Only the renderer's exact layout counts: the <pre> is the ONLY non-whitespace
// child of the body div (no other element, comment or text), its class is
// "plain", and its value is a reason the renderer writes.
func fallbackSource(bodyDiv *xhtml.Node) (string, bool) {
	var pre *xhtml.Node
	for c := bodyDiv.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == xhtml.TextNode && strings.Trim(c.Data, " \t\n\f\r") == "":
		case c.Type == xhtml.ElementNode && c.Data == "pre" && pre == nil:
			pre = c
		default:
			return "", false
		}
	}
	if pre == nil || attr(pre, "class") != "plain" || !fallbackReasons[attr(pre, "data-mailarchive-fallback")] {
		return "", false
	}
	return textContent(pre), true
}

// readCategoriesField recovers the message's categories from the archived page:
// the text of each <span class="mailarchive-category"> inside the dedicated
// categories dd of the header's first <dl>. Each category rides in its own span,
// so the values round-trip with no separator ambiguity, and because they live
// in their own field they can never be read as a Status segment (QC3). Absent
// categories yield nil. It reads from the passed (already anchored) header node,
// so a mail-injected element elsewhere in the document cannot reach it.
func readCategoriesField(header *xhtml.Node) []string {
	dl := findFirst(header, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "dl" })
	if dl == nil {
		return nil
	}
	var dd *xhtml.Node
	for c := dl.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == "dd" && attr(c, "data-mailarchive-field") == "categories" {
			dd = c
			break // first categories dd wins, like readFirstDL
		}
	}
	if dd == nil {
		return nil
	}
	var cats []string
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && c.Data == "span" && hasClass(c, "mailarchive-category") {
				if s := strings.TrimSpace(textContent(c)); s != "" {
					cats = append(cats, s)
				}
				continue // one category per span; do not descend further
			}
			walk(c)
		}
	}
	walk(dd)
	return cats
}

// applyStatusLine reverses export.statusLine, recovering Unread/Importance/
// Sensitivity from the "Status" row the renderer wrote. The separator is the
// same middle dot (U+00B7) the renderer used; an unrecognised segment is
// ignored.
func applyStatusLine(m *model.Message, s string) {
	for _, part := range strings.Split(s, " · ") {
		part = strings.TrimSpace(part)
		switch {
		case part == "Unread":
			m.Unread = true
		case strings.HasPrefix(part, "Importance: "):
			m.Importance = strings.TrimSpace(strings.TrimPrefix(part, "Importance: "))
		case strings.HasPrefix(part, "Sensitivity: "):
			m.Sensitivity = strings.TrimSpace(strings.TrimPrefix(part, "Sensitivity: "))
		}
	}
}

// readFirstDL returns the field→value map recovered from the first <dl>
// descendant of header. A dd's field key is its `data-mailarchive-field`
// attribute when present, else its preceding dt's label lower-cased ("From" →
// "from"). Only the first value seen for a key is kept.
func readFirstDL(header *xhtml.Node) map[string]string {
	fields := map[string]string{}
	dl := findFirst(header, func(n *xhtml.Node) bool { return n.Type == xhtml.ElementNode && n.Data == "dl" })
	if dl == nil {
		return fields
	}
	var label string
	for c := dl.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != xhtml.ElementNode {
			continue
		}
		switch c.Data {
		case "dt":
			label = strings.ToLower(strings.TrimSpace(textContent(c)))
		case "dd":
			key := attr(c, "data-mailarchive-field")
			if key == "" {
				key = label
			}
			if key == "" {
				continue
			}
			if _, seen := fields[key]; !seen {
				fields[key] = strings.TrimSpace(textContent(c))
			}
		}
	}
	return fields
}

// splitSender reverses formatSender's "Name <email>" / "email" / "name" forms.
func splitSender(s string) (name, email string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if strings.HasSuffix(s, ">") {
		if i := strings.LastIndex(s, " <"); i >= 0 {
			return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+2 : len(s)-1])
		}
	}
	if strings.Contains(s, "@") && !strings.ContainsAny(s, " \t") {
		return "", s
	}
	return s, ""
}

// parseHeaderTime recovers a time from a header dd's text: the parenthetical
// UTC instant fmtTime appends for a non-UTC offset (minute precision), else the
// leading RFC-style stamp with its original offset (a UTC time, which has no
// parenthetical), else a v0.1–v0.2 stamp with a zone abbreviation. A dd whose
// text is none of these yields ok=false.
func parseHeaderTime(s string) (time.Time, bool) {
	if mm := reHeaderUTC.FindStringSubmatch(s); mm != nil {
		if t, err := time.Parse("2006-01-02 15:04", mm[1]); err == nil {
			return t.UTC(), true
		}
	}
	if t, err := time.Parse(htmlHeaderLayout, strings.TrimSpace(s)); err == nil {
		return t, true
	}
	if t, err := time.Parse(htmlHeaderLegacyLayout, strings.TrimSpace(s)); err == nil {
		// An abbreviation names an offset only where one is known: UTC and
		// GMT[±h] always, any other only when this machine's zone uses it then
		// (time.Parse resolves it to time.Local). An unknown one parses at
		// offset 0 in a made-up zone, a guess hours off, so it is counted
		// unrecovered instead of indexed.
		if name, _ := t.Zone(); t.Location() == time.UTC || t.Location() == time.Local || strings.HasPrefix(name, "GMT") {
			return t, true
		}
	}
	return time.Time{}, false
}

// --- small golang.org/x/net/html helpers (no external content is trusted;
// these only read structure the exporter itself wrote) ---

// findFirst returns the first node (pre-order) for which pred is true.
// firstChildDiv returns the first DIRECT element child of parent that is a
// <div> carrying the class token cls. It does not descend, so only the
// renderer's own header/body wrappers (the direct children of <body>) match —
// never a mail-supplied element nested inside the body or hidden in a
// <template>/<style>.
func firstChildDiv(parent *xhtml.Node, cls string) *xhtml.Node {
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.ElementNode && c.Data == "div" && hasClass(c, cls) {
			return c
		}
	}
	return nil
}

func findFirst(n *xhtml.Node, pred func(*xhtml.Node) bool) *xhtml.Node {
	if pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if got := findFirst(c, pred); got != nil {
			return got
		}
	}
	return nil
}

// attr returns the value of an element's attribute (empty when absent).
func attr(n *xhtml.Node, key string) string {
	if n.Type != xhtml.ElementNode {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// hasClass reports whether an element's class attribute contains a class token.
func hasClass(n *xhtml.Node, cls string) bool {
	if n.Type != xhtml.ElementNode {
		return false
	}
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == cls {
			return true
		}
	}
	return false
}

// textContent returns the concatenated text of a node's descendants.
func textContent(n *xhtml.Node) string {
	var b strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// innerHTML serializes a node's children (used to carry the body's markup into
// the index's text extractor, which strips tags itself).
func innerHTML(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		_ = xhtml.Render(&b, c)
	}
	return b.String()
}

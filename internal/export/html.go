package export

import (
	"bytes"
	"encoding/base64"
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
}

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
	body, isHTML := selectBody(m)
	consumed := map[int]bool{}
	body = embedInlineImages(body, m.Attachments, consumed)
	header := renderHeader(m, ctx, consumed)

	var headExtra, bodyAttrs, inner string
	var unresolved []string
	if isHTML {
		unresolved = unresolvedInlineRefs([]byte(body))
		headExtra, bodyAttrs, inner = sanitizeHTML(body)
	} else {
		inner = `<pre class="plain">` + html.EscapeString(body) + `</pre>`
	}
	out := fmt.Sprintf(docTemplate, html.EscapeString(displaySubject(m)), headExtra, header, bodyAttrs, inner)
	return RenderResult{HTML: []byte(out), Consumed: consumed, Unresolved: unresolved}, nil
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
//   - <img src="cid:…"> with no matching part → src removed, a caption added.
//
// Scripts, remote loads, <base> and forms need no rewriting: the policy meta
// that precedes everything makes them inert.
func sanitizeHTML(doc string) (headExtra, bodyAttrs, body string) {
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		// Unparseable bytes: show them as text rather than guess.
		return "", "", `<pre class="plain">` + html.EscapeString(doc) + `</pre>`
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

	var hb strings.Builder
	if headNode != nil {
		for c := headNode.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == xhtml.ElementNode && c.DataAtom == atom.Title {
				continue
			}
			xhtml.Render(&hb, c)
			hb.WriteByte('\n')
		}
	}
	var bb strings.Builder
	if bodyNode != nil {
		for c := bodyNode.FirstChild; c != nil; c = c.NextSibling {
			xhtml.Render(&bb, c)
		}
		bodyAttrs = bodyAttributes(bodyNode)
	}
	return hb.String(), bodyAttrs, bb.String()
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
					if strings.EqualFold(a.Key, "http-equiv") {
						switch strings.ToLower(strings.TrimSpace(a.Val)) {
						case "refresh", "content-security-policy", "set-cookie", "content-type":
							c.Attr[i].Key = "data-mailarchive-neutralized"
						}
					}
				}
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

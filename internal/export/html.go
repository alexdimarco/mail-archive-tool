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

	"mail-archive-tool/internal/model"
)

// ArchiveCSP is the Content-Security-Policy every exported document carries as
// a <meta> (browsers enforce it on file://, where no header exists) and that
// `serve` sends as a header for archived files: no scripts, no remote loads
// (tracking pixels, remote CSS/fonts/frames), no <base> rewriting, no form
// submission. Inline styles and data: images/fonts — what real mail needs to
// render — stay allowed. One constant, so the two surfaces cannot drift (R19).
const ArchiveCSP = "default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; base-uri 'none'; form-action 'none'"

var (
	reBodyOpen = regexp.MustCompile(`(?i)<body[^>]*>`)
	reHeadOpen = regexp.MustCompile(`(?i)<head[^>]*>`)
	reHTMLOpen = regexp.MustCompile(`(?i)<html[^>]*>`)
	reHasHTML  = regexp.MustCompile(`(?i)<html[\s>]`)
	reCharset  = regexp.MustCompile(`(?i)charset`)
	// Mail-supplied http-equiv metas that would make an archived page navigate
	// (refresh — CSP cannot restrain it) or carry a policy of the mail's own.
	reHostileMeta = regexp.MustCompile(`(?i)(<meta\b[^>]*\bhttp-equiv\s*=\s*["']?\s*)(refresh|content-security-policy)\b`)
)

// inertMetas are the first children of every exported document's <head>: the
// archive policy (enforced by browsers on file://) and a no-referrer rule.
// Placement is load-bearing — a meta CSP governs only what is parsed after it.
const inertMetas = `<meta http-equiv="Content-Security-Policy" content="` + ArchiveCSP + `">` + "\n" +
	`<meta name="referrer" content="no-referrer">`

// neutralizeMetas defangs mail-supplied refresh / policy metas by renaming
// their http-equiv value; the tag stays in the document (content preserved,
// nothing silently dropped) but no browser acts on it.
func neutralizeMetas(doc string) string {
	return reHostileMeta.ReplaceAllString(doc, "${1}x-mailarchive-neutralized-${2}")
}

// injectHead makes the inert metas (and a charset, when the document declares
// none) the first children of <head>, creating the <head> after <html> when the
// message's document has none.
func injectHead(doc string) string {
	metas := inertMetas
	if !reCharset.MatchString(doc) {
		metas += "\n<meta charset=\"utf-8\">"
	}
	if reHeadOpen.MatchString(doc) {
		return replaceFirst(doc, reHeadOpen, func(tag string) string { return tag + "\n" + metas })
	}
	if reHTMLOpen.MatchString(doc) {
		return replaceFirst(doc, reHTMLOpen, func(tag string) string { return tag + "\n<head>\n" + metas + "\n</head>" })
	}
	return "<head>\n" + metas + "\n</head>\n" + doc
}

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
</style>
</head>
<body>
%s
<div class="mailarchive-body">
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

// Render builds a self-contained HTML document for the message with no
// navigation context (see RenderWith).
func Render(m *model.Message) ([]byte, map[int]bool, error) { return RenderWith(m, RenderContext{}) }

// RenderWith builds a self-contained HTML document for the message. Inline
// images referenced via cid: are embedded as data: URIs; the set of attachment
// indices consumed that way is returned so the caller can exclude them from the
// attachment archive.
func RenderWith(m *model.Message, ctx RenderContext) ([]byte, map[int]bool, error) {
	body, isHTML := selectBody(m)
	consumed := map[int]bool{}
	body = embedInlineImages(body, m.Attachments, consumed)
	header := renderHeader(m, ctx, consumed)

	// When the message carries its own full HTML document, preserve it (its
	// <head> styles matter) and inject our metadata header into its <body>.
	if isHTML && reHasHTML.MatchString(body) && reBodyOpen.MatchString(body) {
		doc := injectHead(neutralizeMetas(body))
		doc = replaceFirst(doc, reBodyOpen, func(tag string) string { return tag + "\n" + header })
		return []byte(doc), consumed, nil
	}

	var inner string
	if isHTML {
		inner = neutralizeMetas(body) // HTML fragment (a meta refresh in a body is honoured by browsers)
	} else {
		inner = `<pre class="plain">` + html.EscapeString(body) + `</pre>`
	}
	out := fmt.Sprintf(docTemplate, html.EscapeString(displaySubject(m)), header, inner)
	return []byte(out), consumed, nil
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
	b.WriteString(`<div class="mailarchive-subject">` + html.EscapeString(displaySubject(m)) + `</div>`)
	b.WriteString(`<dl>`)
	row := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		b.WriteString(`<dt>` + html.EscapeString(label) + `</dt>`)
		b.WriteString(`<dd>` + html.EscapeString(value) + `</dd>`)
	}
	row("From", formatSender(m))
	row("Reply-To", m.ReplyTo)
	row("To", m.To)
	row("Cc", m.Cc)
	row("Bcc", m.Bcc)
	// Sent and Received are shown separately when both are known and differ;
	// otherwise the one known time is the Date.
	switch {
	case !m.Sent.IsZero() && !m.Received.IsZero() && !m.Sent.Equal(m.Received):
		row("Sent", m.Sent.Format(timeLayout))
		row("Received", m.Received.Format(timeLayout))
	default:
		if d := m.Date(); !d.IsZero() {
			row("Date", d.Format(timeLayout))
		}
	}
	row("Message-ID", m.InternetMessageID)
	row("In-Reply-To", m.InReplyTo)
	b.WriteString(`</dl>`)

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

// replaceFirst replaces only the first match of re in s using repl.
func replaceFirst(s string, re *regexp.Regexp, repl func(match string) string) string {
	loc := re.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + repl(s[loc[0]:loc[1]]) + s[loc[1]:]
}

func drain(a model.Attachment) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := a.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

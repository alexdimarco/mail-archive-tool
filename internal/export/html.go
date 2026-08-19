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

var (
	reBodyOpen = regexp.MustCompile(`(?i)<body[^>]*>`)
	reHeadOpen = regexp.MustCompile(`(?i)<head[^>]*>`)
	reHasHTML  = regexp.MustCompile(`(?i)<html[\s>]`)
	reCharset  = regexp.MustCompile(`(?i)charset`)
)

const docTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
body{font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;margin:0;padding:0;color:#1a1a1a;background:#fff}
.mailarchive-header{border-bottom:1px solid #ddd;padding:16px 20px;background:#f7f7f8;font-size:14px}
.mailarchive-header dl{display:grid;grid-template-columns:max-content 1fr;gap:2px 12px;margin:0}
.mailarchive-header dt{font-weight:600;color:#555}
.mailarchive-header dd{margin:0;word-break:break-word}
.mailarchive-subject{font-size:18px;font-weight:700;margin:0 0 10px}
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

// Render builds a self-contained HTML document for the message. Inline images
// referenced via cid: are embedded as data: URIs; the set of attachment
// indices consumed that way is returned so the caller can exclude them from the
// attachment archive.
func Render(m *model.Message) ([]byte, map[int]bool, error) {
	body, isHTML := selectBody(m)
	consumed := map[int]bool{}
	body = embedInlineImages(body, m.Attachments, consumed)
	header := renderHeader(m)

	// When the message carries its own full HTML document, preserve it (its
	// <head> styles matter) and inject our metadata header into its <body>.
	if isHTML && reHasHTML.MatchString(body) && reBodyOpen.MatchString(body) {
		doc := ensureCharset(body)
		doc = replaceFirst(doc, reBodyOpen, func(tag string) string { return tag + "\n" + header })
		return []byte(doc), consumed, nil
	}

	var inner string
	if isHTML {
		inner = body // HTML fragment
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

func renderHeader(m *model.Message) string {
	var b strings.Builder
	b.WriteString(`<div class="mailarchive-header">`)
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
	row("To", m.To)
	row("Cc", m.Cc)
	if d := m.Date(); !d.IsZero() {
		row("Date", d.Format("Mon, 02 Jan 2006 15:04:05 MST"))
	}
	b.WriteString(`</dl></div>`)
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

// ensureCharset injects a UTF-8 charset meta into the document head if none is
// declared, so browsers render the archived HTML with the right encoding.
func ensureCharset(doc string) string {
	if reCharset.MatchString(doc) {
		return doc
	}
	if reHeadOpen.MatchString(doc) {
		return replaceFirst(doc, reHeadOpen, func(tag string) string {
			return tag + "\n<meta charset=\"utf-8\">"
		})
	}
	return doc
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

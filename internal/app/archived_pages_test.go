package app

import (
	"fmt"
	"html"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// Archived pages exactly as RELEASED versions wrote them to disk. `reindex
// -rebuild` reads whatever an archive holds, written by any release, so its
// reader is tested against those bytes, not against today's renderer (which
// keeps changing). Each generator below is a verbatim port of the release's
// renderer for the inputs these fixtures use (no inline images, no
// attachments, no transport headers); the comments name the source.

// ---- v0.1.1 – v0.2.3 (internal/export/html.go is identical in every one of
// them; `git show v0.2.3:internal/export/html.go`). A message whose HTML body
// was a full document KEPT the mail's own document, with the tool's header
// regex-injected after the first `<body ...>` match (reBodyOpen); anything else
// went into the template below, the HTML fragment raw (no sanitizer, no
// reserved-namespace stripping — both arrived in v0.3.0). The header carries
// no data-mailarchive-field markers and stamps the date with the zone's
// abbreviation.

var (
	v023BodyOpen = regexp.MustCompile(`(?i)<body[^>]*>`)
	v023HeadOpen = regexp.MustCompile(`(?i)<head[^>]*>`)
	v023HasHTML  = regexp.MustCompile(`(?i)<html[\s>]`)
	v023Charset  = regexp.MustCompile(`(?i)charset`)
)

const v023DocTemplate = `<!DOCTYPE html>
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

// renderV023 is v0.2.3's Render for a message with no inline images.
func renderV023(m *model.Message) []byte {
	body, isHTML := m.HTMLBody, true
	if strings.TrimSpace(body) == "" {
		body, isHTML = m.PlainBody, false
	}
	header := v023Header(m)
	if isHTML && v023HasHTML.MatchString(body) && v023BodyOpen.MatchString(body) {
		doc := body
		if !v023Charset.MatchString(doc) && v023HeadOpen.MatchString(doc) {
			doc = v023ReplaceFirst(doc, v023HeadOpen, func(tag string) string { return tag + "\n<meta charset=\"utf-8\">" })
		}
		doc = v023ReplaceFirst(doc, v023BodyOpen, func(tag string) string { return tag + "\n" + header })
		return []byte(doc)
	}
	inner := body
	if !isHTML {
		inner = `<pre class="plain">` + html.EscapeString(body) + `</pre>`
	}
	return []byte(fmt.Sprintf(v023DocTemplate, html.EscapeString(fixtureSubject(m)), header, inner))
}

func v023Header(m *model.Message) string {
	var b strings.Builder
	b.WriteString(`<div class="mailarchive-header">`)
	b.WriteString(`<div class="mailarchive-subject">` + html.EscapeString(fixtureSubject(m)) + `</div>`)
	b.WriteString(`<dl>`)
	row := func(label, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		b.WriteString(`<dt>` + html.EscapeString(label) + `</dt>`)
		b.WriteString(`<dd>` + html.EscapeString(value) + `</dd>`)
	}
	row("From", fixtureSender(m))
	row("To", m.To)
	row("Cc", m.Cc)
	if d := m.Date(); !d.IsZero() {
		row("Date", d.Format("Mon, 02 Jan 2006 15:04:05 MST"))
	}
	b.WriteString(`</dl></div>`)
	return b.String()
}

func v023ReplaceFirst(s string, re *regexp.Regexp, repl func(string) string) string {
	loc := re.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + repl(s[loc[0]:loc[1]]) + s[loc[1]:]
}

// displaySubject / formatSender, unchanged from v0.1.1 through aa1c98d.
func fixtureSubject(m *model.Message) string {
	if s := strings.TrimSpace(m.Subject); s != "" {
		return s
	}
	return "(no subject)"
}

func fixtureSender(m *model.Message) string {
	name, email := strings.TrimSpace(m.SenderName), strings.TrimSpace(m.SenderEmail)
	switch {
	case name != "" && email != "" && !strings.EqualFold(name, email):
		return fmt.Sprintf("%s <%s>", name, email)
	case email != "":
		return email
	default:
		return name
	}
}

// ---- end to end: an archive whose pages are exactly the given bytes.

// diskPage is one archived page as some release wrote it, plus the message
// whose manifest record points at it.
type diskPage struct {
	m    *model.Message
	page []byte
}

// buildOnDiskArchive lays down a real archive (manifest, folder pages, one
// record per page) by exporting a shallow placeholder for each message with
// today's exporter — only so the record and its path exist — and then writes
// each page's on-disk bytes over the placeholder page, keeps no .eml (so the
// rebuild must re-derive the record from the page) and deletes search.db. The
// rebuild then reads exactly the bytes the release left behind.
func buildOnDiskArchive(t *testing.T, out string, pages []diskPage) {
	t.Helper()
	msgs := make([]rbMsg, len(pages))
	for i, p := range pages {
		ph := *p.m
		ph.HTMLBody, ph.PlainBody, ph.Raw = "<p>placeholder</p>", "", nil
		msgs[i] = rbMsg{[]string{"Inbox"}, &ph}
	}
	rels := buildRebuildArchive(t, out, msgs)
	for _, p := range pages {
		rel, ok := rels[p.m.Subject]
		if !ok {
			t.Fatalf("no page exported for %q (subjects must be distinct)", p.m.Subject)
		}
		full := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.WriteFile(full, p.page, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(strings.TrimSuffix(full, ".html") + ".eml"); err == nil {
			t.Fatalf("%s kept a .eml; the rebuild would not read the page", rel)
		}
	}
	deleteSearchDB(t, out)
}

// rebuildOnDisk runs the real `reindex -rebuild` over out and checks that every
// record was re-derived from its page.
func rebuildOnDisk(t *testing.T, out string, want int) RebuildReport {
	t.Helper()
	rep, err := Rebuild(out, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if rep.Rebuilt != want || rep.FromHTML != want {
		t.Fatalf("rebuild report = %+v, want %d records all re-derived from their pages", rep, want)
	}
	return rep
}

// searchTotal runs q through the search verb's own grammar (index.ParseQuery:
// from:, folder:, after: ...) against the archive's index.
func searchTotal(t *testing.T, out, q string) int {
	t.Helper()
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	_, total, err := ix.Search(index.ParseQuery(q, index.Query{}))
	if err != nil {
		t.Fatal(err)
	}
	return total
}

package index

import (
	"strings"

	"golang.org/x/net/html"

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

// blockElements introduce a word boundary so adjacent text doesn't run together.
var blockElements = map[string]bool{
	"p": true, "br": true, "div": true, "li": true, "tr": true, "td": true,
	"th": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "ul": true, "ol": true, "table": true, "blockquote": true,
	"section": true, "article": true, "header": true, "footer": true,
}

// htmlToText strips tags and returns collapsed visible text, skipping the
// contents of <script>, <style> and <head>.
func htmlToText(h string) string {
	z := html.NewTokenizer(strings.NewReader(h))
	var b strings.Builder
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return normalizeWS(b.String())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			n := string(name)
			if n == "script" || n == "style" || n == "head" {
				skip++
			}
			if blockElements[n] {
				b.WriteByte(' ')
			}
		case html.EndTagToken:
			name, _ := z.TagName()
			n := string(name)
			if (n == "script" || n == "style" || n == "head") && skip > 0 {
				skip--
			}
			if blockElements[n] {
				b.WriteByte(' ')
			}
		case html.TextToken:
			if skip == 0 {
				b.Write(z.Text())
			}
		}
	}
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

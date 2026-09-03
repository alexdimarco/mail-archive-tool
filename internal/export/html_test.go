package export

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
)

// covers: MA-17
func TestRenderPlainBody(t *testing.T) {
	m := &model.Message{
		Subject:     "Hi <there>",
		SenderName:  "Alice",
		SenderEmail: "alice@example.com",
		To:          "bob@example.com",
		Received:    time.Date(2026, 7, 15, 10, 32, 0, 0, time.UTC),
		PlainBody:   "line1\n<script>evil</script>",
	}
	out, consumed, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if len(consumed) != 0 {
		t.Errorf("expected no inline attachments, got %v", consumed)
	}
	if !strings.Contains(s, "<meta charset=\"utf-8\">") {
		t.Error("missing charset meta")
	}
	// Subject and body must be HTML-escaped.
	if !strings.Contains(s, "Hi &lt;there&gt;") {
		t.Error("subject not escaped")
	}
	if strings.Contains(s, "<script>evil</script>") {
		t.Error("plain body was not escaped")
	}
	if !strings.Contains(s, "alice@example.com") || !strings.Contains(s, "bob@example.com") {
		t.Error("header missing sender/recipient")
	}
}

// covers: MA-18
func TestRenderHTMLDocumentInjectsHeader(t *testing.T) {
	m := &model.Message{
		Subject:  "Report",
		HTMLBody: "<html><head><style>p{color:red}</style></head><body><p>Hello</p></body></html>",
	}
	out, _, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	// Original document styles preserved.
	if !strings.Contains(s, "p{color:red}") {
		t.Error("original <style> lost")
	}
	// Our metadata header injected inside the body.
	if !strings.Contains(s, "mailarchive-header") {
		t.Error("metadata header not injected")
	}
	bodyIdx := strings.Index(s, "<body>")
	headerIdx := strings.Index(s, "mailarchive-header")
	if bodyIdx < 0 || headerIdx < bodyIdx {
		t.Error("header should appear after <body>")
	}
}

// covers: MA-19
func TestRenderEmbedsInlineImage(t *testing.T) {
	payload := []byte("PNGDATA")
	m := &model.Message{
		Subject:  "Inline",
		HTMLBody: `<html><body><img src="cid:img1@host"></body></html>`,
		Attachments: []model.Attachment{
			{
				Filename:  "logo.png",
				MimeType:  "image/png",
				ContentID: "<img1@host>",
				WriteTo:   func(w io.Writer) (int64, error) { n, err := w.Write(payload); return int64(n), err },
			},
		},
	}
	out, consumed, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "data:image/png;base64,") {
		t.Error("inline image not embedded as data URI")
	}
	if strings.Contains(s, "cid:img1@host") {
		t.Error("cid reference not replaced")
	}
	if !consumed[0] {
		t.Error("attachment 0 should be marked consumed inline")
	}
	if hasArchivable(m.Attachments, consumed) {
		t.Error("inline-only attachment should not be archivable")
	}
}

// covers: MA-20
func TestWriteZipSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	zipPath := dir + "/a.zip"
	atts := []model.Attachment{
		{Filename: "empty.txt", WriteTo: func(w io.Writer) (int64, error) { return 0, nil }},
		{Filename: "doc.txt", WriteTo: func(w io.Writer) (int64, error) {
			return io.Copy(w, bytes.NewReader([]byte("content")))
		}},
	}
	zr, err := WriteZip(zipPath, atts, map[int]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if zr.Written != 1 {
		t.Errorf("expected 1 archived attachment, got %d", zr.Written)
	}
	if len(zr.Empty) != 1 || zr.Empty[0] != "empty.txt" {
		t.Errorf("expected empty=[empty.txt], got %v", zr.Empty)
	}
}

// covers: MA-80, R19, S21
// An exported document is inert when opened from disk (file://), where no HTTP
// header can protect it: on BOTH render paths (a message carrying its own
// <html> document, and a fragment/plain body placed in our template) the
// document's <head> starts with the archive Content-Security-Policy meta — the
// same policy `serve` sends — and a no-referrer meta, placed BEFORE any
// mail-supplied <base>/<link>/<style>; navigation-triggering and policy-relaxing
// http-equiv metas from the mail (refresh, content-security-policy) are
// neutralized; a document with <html> but no <head> gets one.
func TestRenderIsInertOffline(t *testing.T) {
	cspMeta := `<meta http-equiv="Content-Security-Policy" content="` + ArchiveCSP + `">`

	// 1. Full document with hostile head + body.
	full := &model.Message{
		Subject: "Your account needs attention",
		HTMLBody: `<html><head>` +
			`<base href="http://tracker.evil.example/">` +
			`<meta http-equiv="Refresh" content="0;url=http://tracker.evil.example/go">` +
			`<meta HTTP-EQUIV='content-security-policy' content="default-src *">` +
			`<link rel="stylesheet" href="http://tracker.evil.example/x.css">` +
			`</head><body><img src="http://tracker.evil.example/pixel.png"><script>alert(1)</script></body></html>`,
	}
	out, _, err := Render(full)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	cspAt := strings.Index(s, cspMeta)
	if cspAt < 0 {
		t.Fatalf("full document lacks the archive CSP meta:\n%s", s)
	}
	headAt := strings.Index(strings.ToLower(s), "<head>")
	baseAt := strings.Index(s, "<base ")
	if headAt < 0 || cspAt < headAt || (baseAt >= 0 && cspAt > baseAt) {
		t.Errorf("CSP meta must be the first child of <head>, before mail-supplied <base>: head=%d csp=%d base=%d", headAt, cspAt, baseAt)
	}
	if !strings.Contains(s, `<meta name="referrer" content="no-referrer">`) {
		t.Error("missing no-referrer meta")
	}
	low := strings.ToLower(s)
	if strings.Contains(low, `http-equiv="refresh"`) || strings.Contains(low, `http-equiv='refresh'`) || strings.Contains(low, `http-equiv=refresh`) {
		t.Error("mail-supplied meta refresh survived")
	}
	if strings.Count(low, `http-equiv="content-security-policy"`)+strings.Count(low, `http-equiv='content-security-policy'`) != 1 {
		t.Errorf("exactly one live Content-Security-Policy meta (ours) expected:\n%s", s)
	}
	if !strings.Contains(s, "tracker.evil.example/pixel.png") {
		t.Error("body content must be preserved verbatim (the CSP, not stripping, is the control)")
	}

	// 2. Fragment body in our template: the meta refresh in the body is neutralized
	//    and the template head carries the policy.
	frag := &model.Message{
		Subject:  "Fragment",
		HTMLBody: `<p>hi</p><meta http-equiv=refresh content="5;url=http://tracker.evil.example/f">`,
	}
	out, _, err = Render(frag)
	if err != nil {
		t.Fatal(err)
	}
	s = string(out)
	if !strings.Contains(s, cspMeta) {
		t.Error("template document lacks the archive CSP meta")
	}
	if strings.Contains(strings.ToLower(s), "http-equiv=refresh") {
		t.Error("meta refresh inside a fragment body survived")
	}

	// 3. A document with <html> but no <head> gets one, carrying the policy.
	noHead := &model.Message{Subject: "NoHead", HTMLBody: `<html><body><p>x</p></body></html>`}
	out, _, err = Render(noHead)
	if err != nil {
		t.Fatal(err)
	}
	s = string(out)
	if !strings.Contains(s, cspMeta) || strings.Index(s, cspMeta) > strings.Index(s, "<body") {
		t.Errorf("head-less document must gain a <head> with the policy before <body>:\n%s", s)
	}

	// 4. Plain-text messages render through the template too.
	plain := &model.Message{Subject: "Plain", PlainBody: "hello"}
	out, _, err = Render(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), cspMeta) {
		t.Error("plain-body document lacks the archive CSP meta")
	}
}

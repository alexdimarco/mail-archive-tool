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
	n, empty, err := WriteZip(zipPath, atts, map[int]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 archived attachment, got %d", n)
	}
	if len(empty) != 1 || empty[0] != "empty.txt" {
		t.Errorf("expected empty=[empty.txt], got %v", empty)
	}
}

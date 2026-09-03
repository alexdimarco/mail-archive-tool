package export

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-90, R7, S27
// A message page is legible on its own, without the tool: it links back to its
// folder page and the archive root, lists its archived attachments with a link
// to the sibling zip (inline-embedded images are not listed), and shows the
// Message-ID, Sent and Received (when both are known and differ), Reply-To and
// Bcc, with times in their ORIGINAL UTC offset — never silently converted.
func TestMessagePageIsSelfDescribing(t *testing.T) {
	pst := time.FixedZone("PST", -8*3600)
	m := &model.Message{
		Subject:           "Board pack",
		SenderName:        "Alice",
		SenderEmail:       "alice@example.com",
		To:                "bob@example.com",
		Bcc:               "auditor@example.com",
		ReplyTo:           "board@example.com",
		InternetMessageID: "pack-42@example.com",
		Sent:              time.Date(2024, 1, 1, 9, 30, 0, 0, pst),
		Received:          time.Date(2024, 1, 1, 9, 45, 0, 0, pst),
		HTMLBody:          `<p>See attached <img src="cid:logo@x"></p>`,
		Attachments: []model.Attachment{
			{Filename: "logo.png", MimeType: "image/png", ContentID: "<logo@x>", WriteTo: func(w io.Writer) (int64, error) { n, _ := w.Write([]byte("PNG")); return int64(n), nil }},
			{Filename: "pack.pdf", WriteTo: func(w io.Writer) (int64, error) { n, _ := w.Write([]byte("PDF")); return int64(n), nil }},
			{Filename: "budget.xlsx", WriteTo: func(w io.Writer) (int64, error) { n, _ := w.Write([]byte("XLS")); return int64(n), nil }},
		},
	}
	ctx := RenderContext{RootRel: "../../", FolderIndexRel: "index.html", ZipName: "x-attachments.zip"}
	out, consumed, err := RenderWith(m, ctx)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !consumed[0] {
		t.Fatal("inline image should have been embedded")
	}
	for _, want := range []string{
		`href="../../index.html"`, `href="index.html"`, // breadcrumbs
		`href="x-attachments.zip"`, "pack.pdf", "budget.xlsx", // attachments block
		"<dt>Message-ID</dt>", "pack-42@example.com",
		"<dt>Sent</dt>", "<dt>Received</dt>",
		"<dt>Reply-To</dt>", "board@example.com",
		"<dt>Bcc</dt>", "auditor@example.com",
		"09:30:00 -0800", "09:45:00 -0800", // original offset kept
	} {
		if !strings.Contains(s, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	attBlock := s[strings.Index(s, "mailarchive-attachments"):]
	if strings.Contains(attBlock[:strings.Index(attBlock, "</div>")], "logo.png") {
		t.Error("inline-embedded image listed as an attachment")
	}
	if strings.Contains(s, "17:30:00") {
		t.Error("time was converted away from its original offset")
	}

	// Without context (a bare render) there is no navigation and no zip link;
	// with only Sent known, a single Date row is shown.
	bare := &model.Message{Subject: "x", Sent: time.Date(2024, 1, 1, 9, 30, 0, 0, time.UTC), PlainBody: "hi"}
	out, _, err = Render(bare)
	if err != nil {
		t.Fatal(err)
	}
	s = string(out)
	if strings.Contains(s, "index.html") || strings.Contains(s, "attachments.zip") {
		t.Error("bare render carries navigation it cannot know")
	}
	if !strings.Contains(s, "<dt>Date</dt>") || strings.Contains(s, "<dt>Sent</dt>") {
		t.Error("a single known time must be one Date row")
	}
}

// covers: MA-96, R1, S27
// With KeepRaw, a message that carries its original RFC 822 bytes is exported
// alongside as <stem>.eml, byte-identical, and the page links to it; a message
// without raw bytes (a PST item) gets no .eml and no link.
func TestKeepRawWritesOriginalMessage(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	e := incExporter(out, manifest)
	e.KeepRaw = true

	raw := []byte("From: a@example.com\r\nSubject: Raw\r\nMessage-ID: <raw@x>\r\n\r\nbody\r\n")
	withRaw := &model.Message{Subject: "Raw", Received: testDate, InternetMessageID: "<raw@x>", PlainBody: "body", Raw: raw}
	if _, err := e.Export("store", []string{"Inbox"}, withRaw); err != nil {
		t.Fatal(err)
	}
	rec, _ := manifest.Get(state.Key("Inbox", withRaw.Identity()))
	htmlPath := filepath.Join(out, filepath.FromSlash(rec.Path))
	emlPath := strings.TrimSuffix(htmlPath, ".html") + ".eml"
	got, err := os.ReadFile(emlPath)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("raw message not preserved byte-identically: err=%v", err)
	}
	page, _ := os.ReadFile(htmlPath)
	if !strings.Contains(string(page), filepath.Base(emlPath)) {
		t.Error("page does not link its .eml")
	}

	noRaw := &model.Message{Subject: "PST", Received: testDate, InternetMessageID: "<pst@x>", PlainBody: "body"}
	if _, err := e.Export("store", []string{"Inbox"}, noRaw); err != nil {
		t.Fatal(err)
	}
	if n := countSuffix(t, out, ".eml"); n != 1 {
		t.Errorf("eml files = %d, want 1", n)
	}

	// KeepRaw off: nothing written even when raw bytes exist.
	e2 := incExporter(t.TempDir(), mustManifest(t))
	if _, err := e2.Export("store", []string{"Inbox"}, withRaw); err != nil {
		t.Fatal(err)
	}
	if n := countSuffix(t, e2.OutDir, ".eml"); n != 0 {
		t.Errorf("KeepRaw off wrote %d eml files", n)
	}
}

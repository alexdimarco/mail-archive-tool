package export

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
)

// The extraction workspace's slice E5 (design docs/design-extraction-workspace.md
// §4.11): RenderContext gains AttachDirName, and the header says "the attachments are
// inside the .eml file" only in the export's own renders.

// e5Attachment is a message part with fixed bytes.
func e5Attachment(name, mime, cid, data string) model.Attachment {
	return model.Attachment{Filename: name, MimeType: mime, ContentID: cid,
		WriteTo: func(w io.Writer) (int64, error) { n, err := io.WriteString(w, data); return int64(n), err }}
}

// e5Messages are the messages the archive's renders are pinned on: no attachment, two
// listed attachments, and one whose only attachment is an inline image the body embeds.
func e5Messages() map[string]*model.Message {
	at := time.Date(2026, 3, 28, 9, 0, 0, 0, time.UTC)
	base := func(subject, html string, atts ...model.Attachment) *model.Message {
		return &model.Message{Subject: subject, SenderName: "A", SenderEmail: "a@example.com", To: "b@example.com",
			Received: at, Sent: at, InternetMessageID: "m@x", HTMLBody: html, Attachments: atts}
	}
	return map[string]*model.Message{
		"none":   base("no attachment", "<p>hello</p>"),
		"listed": base("two attachments", "<p>see attached</p>", e5Attachment("a.pdf", "application/pdf", "", "AAAA"), e5Attachment("b.png", "image/png", "", "BBBB")),
		"inline": base("inline only", `<p>logo <img src="cid:logo"></p>`, e5Attachment("logo.png", "image/png", "logo", "PNGDATA")),
	}
}

// e5ArchiveContexts are the contexts the archive exporter builds: the navigation is always
// set, the original (KeepRaw) and the zip come and go.
func e5ArchiveContexts() map[string]RenderContext {
	nav := RenderContext{RootRel: "../../", FolderIndexRel: "index.html", Store: "store", Folder: "Inbox"}
	raw, zip, both := nav, nav, nav
	raw.RawName = "stem.eml"
	zip.ZipName = "stem-attachments.zip"
	both.RawName, both.ZipName = "stem.eml", "stem-attachments.zip"
	return map[string]RenderContext{"no raw, no zip": nav, "raw, no zip": raw, "zip, no raw": zip, "raw and zip": both}
}

const e5Sentence = "attachments are inside the .eml file"

// covers: MA-525, R7, R19, R29
// Archive renders are byte-identical after RenderContext.AttachDirName is added: for every
// context the exporter builds (KeepRaw on and off, with and without a zip) and every kind
// of message, the page's SHA-256 is the one the renderer gave before the field existed, and
// none carries the sentence "attachments are inside the .eml file". The sentence is the
// export's own: a render with the original beside it (RawName), no zip, no attachments
// directory and attachments to list says it, and no other context does.
func TestArchiveRendersAreUnchangedByAttachDirName(t *testing.T) {
	msgs, ctxs := e5Messages(), e5ArchiveContexts()
	for mname, m := range msgs {
		for cname, ctx := range ctxs {
			rr, err := RenderWith(m, ctx)
			if err != nil {
				t.Fatalf("%s / %s: %v", mname, cname, err)
			}
			sum := sha256.Sum256(rr.HTML)
			got := hex.EncodeToString(sum[:])
			if want := e5ArchiveGolden[mname+"|"+cname]; got != want {
				t.Errorf("%s / %s: the archive's page changed: sha256 %s, want %s", mname, cname, got, want)
			}
			if strings.Contains(string(rr.HTML), e5Sentence) {
				t.Errorf("%s / %s: an archive render says %q", mname, cname, e5Sentence)
			}
		}
	}

	// the export's own render: the original beside the page, no zip, no attachments directory
	export := RenderContext{RawName: "stem.eml"}
	says := func(name string, m *model.Message, ctx RenderContext) bool {
		t.Helper()
		rr, err := RenderWith(m, ctx)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return strings.Contains(string(rr.HTML), e5Sentence)
	}
	if !says("export, listed attachments", msgs["listed"], export) {
		t.Errorf("an export render with attachments to list does not say %q", e5Sentence)
	}
	if says("export, none", msgs["none"], export) || says("export, inline only", msgs["inline"], export) {
		t.Errorf("an export render with no attachment listed says %q", e5Sentence)
	}
	withZip := export
	withZip.ZipName = "stem-attachments.zip"
	if says("export with a zip", msgs["listed"], withZip) {
		t.Errorf("a render with a zip beside it says %q: the attachments are in the zip", e5Sentence)
	}
	withDir := export
	withDir.AttachDirName = "stem.attachments"
	rr, err := RenderWith(msgs["listed"], withDir)
	if err != nil {
		t.Fatal(err)
	}
	page := string(rr.HTML)
	if strings.Contains(page, e5Sentence) || !strings.Contains(page, `<a href="stem.attachments/">`) {
		t.Errorf("a render with extracted attachments must link the directory by relative name and not say %q:\n%s", e5Sentence, page)
	}
	if says("no original", msgs["listed"], RenderContext{}) {
		t.Errorf("a render with no original beside it says %q", e5Sentence)
	}
}

// e5ArchiveGolden is the SHA-256 of each archive render, taken from the renderer as it was
// before RenderContext.AttachDirName existed.
var e5ArchiveGolden = map[string]string{
	"inline|no raw, no zip": "d4f0f42326a53648bab6421b0134bc4e5f933b959a926c56cea0d470ddeda3d6",
	"inline|raw and zip":    "58ff77c412815879e319337bc90b31329d41c0994a154fce1456b7e2533c8399",
	"inline|raw, no zip":    "58ff77c412815879e319337bc90b31329d41c0994a154fce1456b7e2533c8399",
	"inline|zip, no raw":    "d4f0f42326a53648bab6421b0134bc4e5f933b959a926c56cea0d470ddeda3d6",
	"listed|no raw, no zip": "63d6b0bfc312738fd49738c0c6e2d553003899008eb221b22471d4d089b8a255",
	"listed|raw and zip":    "aae3d41e95cff10a8b34f1741ab839c368121842ac563a05a7a2c2857f969fc7",
	"listed|raw, no zip":    "91bcde2d4b54b4201bc4766d70c51909c9bc60a8ef9b72c88b4a63dbb7e699b8",
	"listed|zip, no raw":    "7a0cf13353e0b07543595938025fdcae24281e434a1031ef910df56a26b0211d",
	"none|no raw, no zip":   "12051642a11e4dc8a3cfa25863962247c1cae33f3d92a83e91a3c568e084b7e5",
	"none|raw and zip":      "fbb329d81e54ee66d4b69e941732cd6c82b804986c247ac3bf3e99abd97f7639",
	"none|raw, no zip":      "fbb329d81e54ee66d4b69e941732cd6c82b804986c247ac3bf3e99abd97f7639",
	"none|zip, no raw":      "12051642a11e4dc8a3cfa25863962247c1cae33f3d92a83e91a3c568e084b7e5",
}

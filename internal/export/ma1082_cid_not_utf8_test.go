package export

import (
	"bytes"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-1082, R10
// An inline image whose Content-ID is not valid UTF-8 (an old Korean mailer wrote its
// file name into the id in EUC-KR, as in the owner's 2013 Gmail mail) is embedded like any
// other: the page holds the image, no cid: reference to it is left, and the size
// prediction counts it. Before the fix the id was compiled into a regular expression,
// which refuses invalid UTF-8, so the message panicked and every backup failed on it.
func TestAnInlineImageWhoseIdIsNotUTF8IsEmbedded(t *testing.T) {
	image := bytes.Repeat([]byte{0xAB}, 300)
	cid := "\xb0\xa2\xc0\xef 2.jpg@41353.921109838.46" // the owner's report, bytes as found
	atts := []model.Attachment{
		{Filename: "x.jpg", MimeType: "image/jpeg", ContentID: "<" + cid + ">", WriteTo: bytesTo(image)},
	}
	body := `<p>hi</p><img src="cid:` + cid + `"><img src="CID:` + cid + `">`
	m := &model.Message{HTMLBody: body, Attachments: atts}

	consumed := map[int]bool{}
	got := embedInlineImages(body, atts, consumed)
	if strings.Contains(strings.ToLower(got), "cid:") {
		t.Errorf("a cid: reference is left in the page: %q", got)
	}
	if strings.Count(got, "data:image/jpeg;base64,") != 2 {
		t.Errorf("want both references embedded as data URIs, got %q", got)
	}
	if !consumed[0] {
		t.Errorf("the image is not recorded as embedded")
	}
	if p := PredictedRenderSize(m); p <= int64(len(body)) {
		t.Errorf("the size prediction %d does not count the embedded image (body %d)", p, len(body))
	}
	if _, err := RenderWith(m, RenderContext{}); err != nil {
		t.Fatalf("rendering the message failed: %v", err)
	}
}

// covers: MA-1082, R10
// The positive twin: a valid id keeps today's matching, any case and as a prefix, so the
// fix changes nothing for ordinary mail.
func TestAnInlineImageWithAValidIdIsStillEmbeddedInAnyCase(t *testing.T) {
	image := bytes.Repeat([]byte{0xCD}, 100)
	atts := []model.Attachment{
		{Filename: "a.png", MimeType: "image/png", ContentID: "<Img.1@Host>", WriteTo: bytesTo(image)},
	}
	body := `<img src="cid:Img.1@Host"><img src="CID:img.1@host">`
	got := embedInlineImages(body, atts, map[int]bool{})
	if strings.Contains(strings.ToLower(got), "cid:") || strings.Count(got, "data:image/png;base64,") != 2 {
		t.Errorf("a valid id in either case is not embedded at both references: %q", got)
	}
}

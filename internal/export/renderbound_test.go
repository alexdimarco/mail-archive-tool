package export

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
)

// covers: MA-509, R29, R10
// PredictedRenderSize is what the page will hold before it is rendered: the body, plus the
// data URI of an inline image once for every cid: reference to it (the renderer writes the
// whole URI at each one), so the size follows the references and not the message's size
// cap. An image no reference names, one with no id, one that cannot be read and one that is
// empty add nothing, as the renderer skips them; the references are read as the renderer
// reads them (any case, the id as a prefix), and its answer is not smaller than what the
// renderer made of the same message.
func TestPredictedRenderSizeFollowsTheReferences(t *testing.T) {
	image := bytes.Repeat([]byte{0xAB}, 3000)
	uri := int64(len("data:image/png;base64,") + base64.StdEncoding.EncodedLen(len(image)))
	atts := []model.Attachment{
		{Filename: "a.png", MimeType: "image/png", ContentID: "<img1>", WriteTo: bytesTo(image)},
		{Filename: "unnamed.png", MimeType: "image/png", WriteTo: bytesTo(image)},                      // no id: never embedded
		{Filename: "b.png", MimeType: "image/png", ContentID: "unreferenced", WriteTo: bytesTo(image)}, // no reference names it
		{Filename: "empty.png", MimeType: "image/png", ContentID: "empty", WriteTo: bytesTo(nil)},      // empty: skipped
		{Filename: "bad.png", MimeType: "image/png", ContentID: "bad", WriteTo: failingTo()},           // unreadable: skipped
		{Filename: "typed.png", ContentID: "typed", WriteTo: bytesTo(image)},                           // type from the extension
	}
	body := "<p>x</p>" + strings.Repeat(`<img src="cid:img1">`, 10) + `<img src="CID:img1"><img src="cid:empty"><img src="cid:bad"><img src="cid:typed">`
	m := &model.Message{HTMLBody: body, Attachments: atts}
	want := int64(len(body)) + 11*uri + uri // eleven references to img1 in either case, one to typed
	if got := PredictedRenderSize(m); got != want {
		t.Errorf("the prediction is %d, want %d (body %d + 12 data URIs of %d)", got, want, len(body), uri)
	}
	rr, err := RenderWith(m, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got := PredictedRenderSize(m); got < int64(len(rr.HTML))-8192 {
		t.Errorf("the prediction %d is far below the page the renderer made, %d bytes", got, len(rr.HTML))
	}
	if got := PredictedRenderSize(&model.Message{PlainBody: "plain body", Attachments: atts}); got != int64(len("plain body")) {
		t.Errorf("a body with no reference predicts %d, want its own length", got)
	}
}

// bytesTo is a part's WriteTo that writes b.
func bytesTo(b []byte) func(io.Writer) (int64, error) {
	return func(w io.Writer) (int64, error) { n, err := w.Write(b); return int64(n), err }
}

// failingTo is a part's WriteTo that cannot read its part.
func failingTo() func(io.Writer) (int64, error) {
	return func(io.Writer) (int64, error) { return 0, errors.New("the part cannot be read") }
}

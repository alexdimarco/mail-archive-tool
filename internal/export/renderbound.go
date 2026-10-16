package export

import (
	"encoding/base64"
	"io"
	"mime"
	"path/filepath"
	"regexp"
	"strings"

	"mail-archive-tool/internal/model"
)

// PredictedRenderSize is the size, in bytes, the page RenderWith would write for m is
// bounded by, before anything is rendered: the selected body's length, plus, for each
// attachment that a cid: reference in that body names, its data URI (its base64 length
// and the "data:<type>;base64," prefix) once for every reference. embedInlineImages
// writes the image's whole data URI at every occurrence of its cid: reference, so K
// references to one N-byte image make about K × 4N/3 bytes, which the size cap on the
// message (it bounds what is downloaded, not what the renderer makes of it) does not
// bound; the sanitizer then parses, serializes and re-parses that text. The extraction
// workspace asks for this before it renders a readable copy, and makes none above its
// bound (design §4.11, RR2b-C2).
//
// It reads the references the way embedInlineImages does (the same case-insensitive
// "cid:" + the id, matched as a prefix), so what it counts is what would be replaced, and
// measures an attachment by streaming it into a counter, not into memory.
func PredictedRenderSize(m *model.Message) int64 {
	body, _ := selectBody(m)
	size := int64(len(body))
	if !strings.Contains(strings.ToLower(body), "cid:") {
		return size
	}
	for i := range m.Attachments {
		a := m.Attachments[i]
		cid := strings.Trim(a.ContentID, "<>")
		if cid == "" || a.WriteTo == nil {
			continue
		}
		refs := int64(len(regexp.MustCompile(`(?i)cid:`+regexp.QuoteMeta(cid)).FindAllStringIndex(body, -1)))
		if refs == 0 {
			continue
		}
		var n countWriter
		if _, err := a.WriteTo(&n); err != nil || n == 0 {
			continue // embedInlineImages skips a part it cannot read, or an empty one
		}
		mt := a.MimeType
		if mt == "" {
			mt = mime.TypeByExtension(filepath.Ext(a.Filename))
		}
		if mt == "" {
			mt = "application/octet-stream"
		}
		size += refs * int64(len("data:"+mt+";base64,")+base64.StdEncoding.EncodedLen(int(n)))
	}
	return size
}

// countWriter counts the bytes written to it.
type countWriter int64

func (c *countWriter) Write(p []byte) (int, error) {
	*c += countWriter(len(p))
	return len(p), nil
}

var _ io.Writer = (*countWriter)(nil)

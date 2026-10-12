package source

import (
	"bytes"
	"io"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-message/charset"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// Text decoding of design-mime-fidelity §4.4 (P7, C1-8): what a body text part becomes
// before it is shown and indexed. It never fails and it never registers a charset or
// touches go-message's table (H3, MA-331): a label the shared table resolves is decoded
// with it, exactly as v1 converted it, and any other label, an unlabelled part or one
// labelled utf-8 or us-ascii that is not, is "valid UTF-8 as is, else Windows-1252"
// (util.DecodeLegacy, the PST reader's rule). Where a guess shaped the text the result
// says so in a ParseNote.

// Note codes of slice S2, each introduced under state.ParseSchemeFidelity (§4.10).
const (
	noteCharsetFallback     = "charset-fallback"      // a label the table does not know
	noteLegacy8bit          = "legacy-8bit"           // unlabelled / utf-8 / us-ascii text that is not UTF-8
	noteLenientCTE          = "lenient-cte"           // a transfer encoding that needed normalizing
	noteLenientParams       = "lenient-params"        // strict Content-Type / Disposition parsing failed
	noteContinuedAfterError = "continued-after-error" // v1 would have stopped at this part
	noteTextAttachmentBytes = "text-attachment-bytes" // a text attachment kept its source bytes
	noteHeaderFallback      = "header-fallback"       // v1 would have used fallbackParse
	noteEncodedWordFallback = "encoded-word-fallback" // an encoded word in a charset the table does not know
	noteFidelityRegression  = "fidelity-regression"   // the superset check failed (MC4)
)

func newNote(code, detail string) model.ParseNote {
	return model.ParseNote{Code: code, Since: state.ParseSchemeFidelity, Detail: detail}
}

// normalizeCharset is a charset label as a table is asked for it: lower-cased, trimmed,
// unquoted and without a trailing RFC 822 comment.
func normalizeCharset(label string) string {
	l := strings.TrimSpace(label)
	if i := strings.IndexByte(l, '('); i >= 0 {
		l = strings.TrimSpace(l[:i])
	}
	l = strings.Trim(l, `"' `)
	return strings.ToLower(l)
}

// decodeText turns the transfer-decoded bytes of a body text part into valid UTF-8.
// label is the part's declared charset, "" when it declared none. The notes say how.
func decodeText(label string, b []byte) (string, []model.ParseNote) {
	l := normalizeCharset(label)
	switch l {
	case "", "utf-8", "us-ascii", "ascii":
		if utf8.Valid(b) {
			return string(b), nil
		}
		return util.DecodeLegacy(b), []model.ParseNote{newNote(noteLegacy8bit,
			"the text is not valid UTF-8, so it is shown as Windows-1252")}
	}
	if r, err := charset.Reader(l, bytes.NewReader(b)); err == nil {
		if out, rerr := io.ReadAll(r); rerr == nil && utf8.Valid(out) {
			return string(out), nil
		}
	}
	// A label this build does not know. v1 gave up on such a part (and every part after
	// it); the bytes are shown as UTF-8 if they are one, else as Windows-1252.
	detail := "the charset " + quoteLabel(l) + " is not known to this version, so the text is shown as "
	if utf8.Valid(b) {
		detail += "UTF-8"
	} else {
		detail += "Windows-1252"
	}
	if l == "utf-7" {
		detail = "the charset utf-7 is not decoded by this version: the text is shown as stored"
	}
	return util.DecodeLegacy(b), []model.ParseNote{newNote(noteCharsetFallback, detail)}
}

// quoteLabel makes a label safe to put in a note: it comes from the mail.
func quoteLabel(l string) string {
	l = util.StripControl(l)
	if r := []rune(l); len(r) > 40 {
		l = string(r[:40]) + "…"
	}
	return `"` + l + `"`
}

// decodeHeaderWords RFC 2047-decodes an untrusted header value, with a charset reader
// that never fails: an encoded word in a charset the table does not know is read as
// decodeText reads an unknown label. It is used only after go-message's own decoder
// failed, so a value it decodes is still decoded exactly as v1 decodes it.
func decodeHeaderWords(s string) (string, []model.ParseNote) {
	var notes []model.ParseNote
	dec := mime.WordDecoder{CharsetReader: func(label string, input io.Reader) (io.Reader, error) {
		raw, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		text, n := decodeText(label, raw)
		notes = append(notes, n...)
		return strings.NewReader(text), nil
	}}
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s, nil
	}
	if len(notes) > 0 {
		return out, []model.ParseNote{newNote(noteEncodedWordFallback, "an encoded word names a charset this version does not know, so it is shown as stored")}
	}
	return out, nil
}

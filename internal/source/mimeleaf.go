package source

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/emersion/go-message/textproto"

	v1charset "mail-archive-tool/internal/source/parsev1/_dep/charset"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// leafPart is one non-multipart part as the walker holds it: the headers' reading, the
// raw (still transfer-encoded) bytes, the decoded ones and how the decoding went.
type leafPart struct {
	n       int // the 1-based ordinal of the leaf, for part-N names
	info    partInfo
	cteRaw  string // the Content-Transfer-Encoding header as sent
	raw     []byte
	data    []byte
	st      transfer
	charset string // the declared charset of a text part, "" if it declares none
	hasCS   bool   // a charset parameter was declared (even an empty one)
}

// leaf reads one non-multipart part: it is decoded, classified as v1 classifies it (an
// inline text/html or text/plain part is a body, every other part an attachment), and
// becomes a body segment or an attachment.
func (w *walker) leaf(h textproto.Header, p partInfo, raw []byte) {
	w.leafN++
	l := leafPart{n: w.leafN, info: p, raw: raw}
	w.site = fmt.Sprintf("part %d", l.n)
	if partDecodeHook != nil {
		partDecodeHook(l.n)
	}

	l.cteRaw = h.Get("Content-Transfer-Encoding")
	norm, changed := normalizeCTE(l.cteRaw)
	l.data, l.st = decodeTransfer(norm, raw)
	if l.st == transferOK && w.cut {
		l.st = transferCorrupt // the data ended inside this part: what arrived is kept as a cut part
	}
	if changed && l.st == transferOK {
		w.note(noteLenientCTE, fmt.Sprintf("the transfer encoding %s was read as %q", quoteLabel(l.cteRaw), norm))
	}
	if v, ok := p.params["charset"]; ok && strings.HasPrefix(p.mt, "text/") {
		l.charset, l.hasCS = v, true
	}

	// Where v1 would have stopped: at a part whose charset or transfer encoding its table
	// does not know. The walker goes on, and the note says this is a part v1 lost.
	if v1CTEUnknown(l.cteRaw) {
		w.note(noteContinuedAfterError, fmt.Sprintf("the transfer encoding %s stopped the earlier parser, which lost this part and the parts after it", quoteLabel(l.cteRaw)))
	}
	if l.hasCS && v1CharsetUnknown(l.charset) {
		w.note(noteContinuedAfterError, fmt.Sprintf("the charset %s stopped the earlier parser, which lost this part and the parts after it", quoteLabel(l.charset)))
	}

	inline := p.disp == "inline" || (p.disp != "attachment" && strings.HasPrefix(p.v1Type, "text/"))
	switch {
	case inline && strings.HasPrefix(p.v1Type, "text/html"):
		w.bodyText(false, l)
	case inline && strings.HasPrefix(p.v1Type, "text/plain"):
		w.bodyText(true, l)
	case inline:
		// An inline non-text part (an image a body refers to by cid) is named as v1
		// names it: by its Content-ID, else "inline".
		name := p.cid
		if name == "" {
			name = "inline"
		}
		w.attach(name, l)
		w.textAttachmentNote(l)
	default:
		w.attach(p.filename, l)
		w.textAttachmentNote(l)
	}
}

// textAttachmentNote records that a text part kept as an attachment is in the bytes it was
// sent in where v1 converted it to UTF-8, which is every declared charset but the two v1 read
// as they came (MS2-AI3: an inline text/csv and an "ascii" charset were left out).
func (w *walker) textAttachmentNote(l leafPart) {
	if l.hasCS && v1Converts(l.charset) {
		w.note(noteTextAttachmentBytes, "a text attachment is kept in the bytes it was sent in; this version no longer converts it to UTF-8")
	}
}

// bodyText decodes a body text part into the body field it fills: the first of each kind,
// as v1 kept them (assembly of several is slice S3's). A part whose transfer encoding is
// not known is also kept as received; one cut short is used as far as it goes.
func (w *walker) bodyText(plain bool, l leafPart) {
	switch l.st {
	case transferUnknown:
		w.undecoded("", l)
		if w.plain.Len() == 0 {
			text, notes := decodeText(l.charset, l.raw)
			w.addNotes(notes)
			w.plain.WriteString(text) // the still-encoded text, shown as it came
		}
		return
	case transferCorrupt:
		w.gap(state.GapKindCorruptPart, fmt.Sprintf(state.CorruptBodyDetailPrefix+"%d is cut short or damaged after %d bytes: only the readable start is shown", l.n, len(l.data)))
	}
	if (plain && w.plain.Len() != 0) || (!plain && w.msg.HTMLBody != "") {
		return
	}
	text, notes := decodeText(l.charset, l.data)
	w.addNotes(notes)
	if plain {
		w.plain.WriteString(text)
	} else {
		w.msg.HTMLBody = text
	}
}

// attach adds an attachment: its bytes are the transfer-decoded bytes whatever its type
// (a text attachment included), an unknown encoding keeps them as received under
// <name>.undecoded, and one cut short keeps what decoded under <name>.partial (P4).
func (w *walker) attach(name string, l leafPart) {
	switch l.st {
	case transferUnknown:
		w.undecoded(name, l)
		return
	case transferCorrupt:
		detail := fmt.Sprintf("the data is cut short or damaged after %d bytes: only the readable start is kept", len(l.data))
		if l.hasCS {
			// What v1 converted from the declared charset, the check converts these bytes from
			// too, or it cannot find them and discards the walker's reading (MS2-AO5).
			w.meta.declared[len(w.msg.Attachments)] = l.charset
		}
		w.msg.Attachments = append(w.msg.Attachments, model.Attachment{
			Filename: partName(name, l.n) + ".partial", MimeType: l.info.mt, ContentID: l.info.cid,
			WriteTo: bytesWriterTo(l.data), GapKind: state.GapKindCorruptPart, GapDetail: detail,
		})
		w.note(state.GapKindCorruptPart, "an attachment is cut short or damaged")
		return
	}
	if l.hasCS {
		w.meta.declared[len(w.msg.Attachments)] = l.charset
	}
	w.msg.Attachments = append(w.msg.Attachments, model.Attachment{
		Filename: name, MimeType: l.info.mt, ContentID: l.info.cid, WriteTo: bytesWriterTo(l.data),
	})
}

// undecoded keeps a part whose transfer encoding this version does not decode, as
// received, as <name or part-N>.undecoded, with a terminal undecoded-part gap.
func (w *walker) undecoded(name string, l leafPart) {
	detail := fmt.Sprintf("the transfer encoding %s is not supported by this version, so the part is kept as received", quoteLabel(l.cteRaw))
	w.msg.Attachments = append(w.msg.Attachments, model.Attachment{
		Filename: partName(name, l.n) + ".undecoded", MimeType: l.info.mt, ContentID: l.info.cid,
		WriteTo: bytesWriterTo(l.raw), GapKind: state.GapKindUndecodedPart, GapDetail: detail,
	})
	w.note(state.GapKindUndecodedPart, "a part uses a transfer encoding this version does not decode")
}

// partName is the name an unnamed part is given in a .partial or .undecoded entry.
//
// The sender's name loses its bidirectional controls: this version makes the .undecoded and
// .partial suffix, and an override in the name would reverse it out of sight (friction F9).
func partName(name string, n int) string {
	if name = util.StripBidi(name); name != "" {
		return name
	}
	return fmt.Sprintf("part-%d", n)
}

// v1Converts says v1 converted a text part declared in this charset to UTF-8: it passed the
// bytes through only for a label that is exactly "utf-8" or "us-ascii" once lower-cased (and
// for none), and ran any other label, "ascii" and "utf-8 " included, through its table.
func v1Converts(label string) bool {
	switch strings.ToLower(label) {
	case "", "utf-8", "us-ascii":
		return false
	}
	return true
}

// v1CTEUnknown says v1's transfer-encoding reader did not know the encoding: it lower-
// cases the header value and knows exactly these.
func v1CTEUnknown(enc string) bool {
	switch strings.ToLower(enc) {
	case "quoted-printable", "base64", "7bit", "8bit", "binary", "":
		return false
	}
	return true
}

// v1CharsetUnknown says v1's charset table did not know the label of a text part, so v1
// stopped there. It asks the vendored v1 table, never the shared one.
func v1CharsetUnknown(label string) bool {
	l := strings.ToLower(label)
	if l == "utf-8" || l == "us-ascii" {
		return false
	}
	_, err := v1charset.Reader(l, bytes.NewReader(nil))
	return err != nil
}

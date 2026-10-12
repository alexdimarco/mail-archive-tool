package source

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	netmail "net/mail"
	"strings"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// The fidelity walker of design-mime-fidelity §4.3 (P2, P4, P5, P13): the parser whose
// result is the message a user sees and the index holds once slice S2 is switched on
// (fidelityContent, mbox.go). It is built on go-message's textproto (ReadHeader,
// NewMultipartReader), not on mail.Reader, so no part is ever returned nil and no
// charset is applied implicitly:
//
//   - a decode problem in one part (an unknown charset, an unknown or corrupt transfer
//     encoding, a panic in a decoder) is that part's: the part is kept, as received where
//     it cannot be decoded, and a parse gap says so; later parts are read as usual (P2);
//   - an attachment's bytes are its transfer-decoded bytes, never converted to UTF-8, so
//     the zip digest vouches for the bytes the store holds (P4); only a body text part is
//     decoded to valid UTF-8 (text.go);
//   - a Content-Type or Content-Disposition the strict parser rejects is read by the
//     lenient tokenizer, so a boundary or a filename survives a stray parameter (P5);
//   - structure is bounded: multipart depth, parts, header size, lenient parameters (P13).
//
// Slice S3 owns what is left of F25 and the names (assembly of several text parts,
// classification, final zip names): the walker classifies a part as v1 does and keeps
// v1's first-of-each body rule, so what a part is called and where it lands is what v1
// said; what changes is that it is decoded, kept and recorded.

const (
	maxMultipartDepth = 32      // nested multipart containers read; a deeper one is kept unread
	maxParts          = 10_000  // parts (leaves and containers) read from one message
	maxHeaderBytes    = 1 << 20 // the top-level header and every part header (MC22)
)

var errHeaderTooBig = errors.New("message: header exceeds maximum size")

// partDecodeHook, when set, is called with the 1-based ordinal of each leaf part just
// before its transfer decoding runs. It is nil in production: a test panics in it to
// prove that a panic in one part's decoder costs that part only (MA-349).
var partDecodeHook func(n int)

// walkMeta is what the superset check (holdsV1) needs to know about a parse beyond the
// message: the charset a text attachment declared, by attachment index, which the
// attachment's bytes were not converted from.
type walkMeta struct {
	declared map[int]string
}

type walker struct {
	data      []byte
	msg       *model.Message
	meta      walkMeta
	notes     []model.ParseNote
	seenNotes map[string]bool
	gaps      []model.ParseGap
	parts     int    // parts read so far, leaves and containers (the maxParts bound)
	leafN     int    // the 1-based ordinal of the last leaf: the N of "part-N" names
	site      string // where the walker is, for the panic detail of a fallback
}

// parseFidelity parses data with the fidelity walker.
func parseFidelity(data []byte) *model.Message {
	m, _ := newWalker(data).run()
	return m
}

func newWalker(data []byte) *walker {
	return &walker{
		data:      data,
		msg:       &model.Message{Raw: data},
		meta:      walkMeta{declared: map[int]string{}},
		seenNotes: map[string]bool{},
		site:      "the message headers",
	}
}

// run walks the message. It does not recover a panic of its own: the routing does, and
// reads w.site to name where it happened.
func (w *walker) run() (*model.Message, walkMeta) {
	hdr, body, err := readHeaderCapped(w.data)
	if err != nil {
		w.headerFallback()
	} else {
		w.envelope(hdr)
		w.entity(hdr, body, 1)
	}
	w.msg.ParseNotes, w.msg.ParseGaps = w.notes, w.gaps
	return w.msg, w.meta
}

// capReader reads at most n bytes and then fails with errHeaderTooBig, so a header with
// no end cannot be read without bound.
type capReader struct {
	r *bytes.Reader
	n int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.n <= 0 {
		return 0, errHeaderTooBig
	}
	if int64(len(p)) > c.n {
		p = p[:c.n]
	}
	n, err := c.r.Read(p)
	c.n -= int64(n)
	return n, err
}

// readHeaderCapped reads the header of a message and returns it with the rest of the
// data, which is the (still encoded) body. A header over maxHeaderBytes, or one the
// parser rejects, is an error: the walker then reads the message as plain mail headers.
func readHeaderCapped(data []byte) (textproto.Header, []byte, error) {
	cr := &capReader{r: bytes.NewReader(data), n: maxHeaderBytes}
	br := bufio.NewReader(cr)
	h, err := textproto.ReadHeader(br)
	if err != nil && !(errors.Is(err, io.EOF) && h.Len() > 0) {
		return h, nil, err
	}
	consumed := len(data) - cr.r.Len() - br.Buffered()
	return h, data[consumed:], nil
}

// headerSize is the size of a parsed header as it was read.
func headerSize(h textproto.Header) int {
	n := 0
	for f := h.Fields(); f.Next(); {
		if raw, err := f.Raw(); err == nil {
			n += len(raw)
		}
	}
	return n
}

func (w *walker) note(code, detail string) {
	if w.seenNotes[code+"\x00"+detail] {
		return
	}
	w.seenNotes[code+"\x00"+detail] = true
	w.notes = append(w.notes, newNote(code, detail))
}

func (w *walker) addNotes(ns []model.ParseNote) {
	for _, n := range ns {
		w.note(n.Code, n.Detail)
	}
}

// gap records a message-level parse gap (and its note: a gap is a fidelity event too).
func (w *walker) gap(kind, detail string) {
	w.gaps = append(w.gaps, model.ParseGap{Kind: kind, Detail: detail})
	w.note(kind, detail)
}

// envelope reads the fields of the header block that are not content, exactly as v1 reads
// them (headers are slice S3's: header-8bit and the address rules). The one difference is
// a Subject whose encoded word names a charset the table does not know: v1 keeps the raw
// encoded word, the walker decodes it as decodeText reads an unknown charset.
func (w *walker) envelope(hdr textproto.Header) {
	msg := w.msg
	h := mail.Header{Header: message.Header{Header: hdr}}
	subject, err := h.Subject()
	if err != nil {
		var notes []model.ParseNote
		subject, notes = decodeHeaderWords(h.Get("Subject"))
		w.addNotes(notes)
	}
	msg.Subject = subject
	if id, err := h.MessageID(); err == nil {
		msg.InternetMessageID = id
	}
	if d, err := h.Date(); err == nil && !d.IsZero() {
		msg.Received = d
		msg.IdentityDate = d
	}
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		msg.SenderName = from[0].Name
		msg.SenderEmail = from[0].Address
	}
	msg.To = addressList(&h, "To")
	msg.Cc = addressList(&h, "Cc")
	msg.Bcc = addressList(&h, "Bcc")
	msg.ReplyTo = addressList(&h, "Reply-To")
	msg.InReplyTo = strings.Trim(h.Get("In-Reply-To"), "<> ")
	msg.References = strings.TrimSpace(h.Get("References"))

	msg.Importance = importanceFromHeaders(h.Get("Importance"), h.Get("X-Priority"))
	msg.Sensitivity = sensitivityFromHeader(h.Get("Sensitivity"))
	if unread, ok := mozillaReadFlag(h.Get("X-Mozilla-Status")); ok {
		msg.Unread = unread
	}
	msg.Categories = mozillaCategories(h.Get("X-Mozilla-Keys"))
}

// headerFallback is the walker's reading of a message whose header block the MIME parser
// cannot read: the headers as net/mail reads them and the whole body as text, valid UTF-8.
// If net/mail cannot read it either, the input is the body of an "(unparseable message)".
func (w *walker) headerFallback() {
	w.note(noteHeaderFallback, "the message headers could not be read as MIME, so they are read as plain mail headers and the body is shown as text")
	m, err := netmail.ReadMessage(bytes.NewReader(w.data))
	if err != nil {
		text, notes := decodeText("", w.data)
		w.addNotes(notes)
		*w.msg = model.Message{Subject: "(unparseable message)", PlainBody: text, Raw: w.data}
		return
	}
	body, _ := io.ReadAll(m.Body)
	text, notes := decodeText("", body)
	w.addNotes(notes)
	msg := w.msg
	msg.Subject = m.Header.Get("Subject")
	msg.InternetMessageID = strings.Trim(m.Header.Get("Message-Id"), "<>")
	msg.To, msg.Cc, msg.Bcc = m.Header.Get("To"), m.Header.Get("Cc"), m.Header.Get("Bcc")
	msg.ReplyTo = m.Header.Get("Reply-To")
	msg.InReplyTo = strings.Trim(m.Header.Get("In-Reply-To"), "<> ")
	msg.References = strings.TrimSpace(m.Header.Get("References"))
	msg.PlainBody = text
	if addrs, err := m.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		msg.SenderName = addrs[0].Name
		msg.SenderEmail = addrs[0].Address
	}
	if d, err := m.Header.Date(); err == nil {
		msg.Received = d
		msg.IdentityDate = d
	}
	msg.Importance = importanceFromHeaders(m.Header.Get("Importance"), m.Header.Get("X-Priority"))
	msg.Sensitivity = sensitivityFromHeader(m.Header.Get("Sensitivity"))
	if unread, ok := mozillaReadFlag(m.Header.Get("X-Mozilla-Status")); ok {
		msg.Unread = unread
	}
	msg.Categories = mozillaCategories(m.Header.Get("X-Mozilla-Keys"))
}

// partInfo is what the walker needs to know about one part's headers.
type partInfo struct {
	mt       string            // media type, recovered by the lenient tokenizer when strict parsing failed
	params   map[string]string // Content-Type parameters
	v1Type   string            // the type v1's prefix tests saw: the parsed type, or the raw header when parsing failed
	disp     string            // v1's reading of the disposition: parsed, or the raw header when parsing failed
	filename string            // the disposition's filename, else the Content-Type's name
	cid      string
}

// describe reads a part's Content-Type, Content-Disposition and Content-ID. The strict
// parser (go-message's, so parameters are RFC 2047-decoded as v1 decodes them) is tried
// first; the lenient tokenizer runs only after it fails, and says so in a note (P5).
func (w *walker) describe(h textproto.Header) partInfo {
	mh := message.Header{Header: h}
	var p partInfo

	rawCT := h.Get("Content-Type")
	switch t, params, err := mh.ContentType(); {
	case rawCT == "":
		p.mt, p.v1Type = "text/plain", "text/plain"
	case err == nil:
		p.mt, p.params, p.v1Type = t, params, t
	default:
		p.mt, p.params = lenientMediaType(rawCT)
		p.v1Type = rawCT
		if p.mt == "" {
			p.mt = "application/octet-stream"
		}
		w.note(noteLenientParams, "the Content-Type of a part is malformed; its parameters were read leniently")
		for k, v := range p.params {
			p.params[k], _ = decodeHeaderWords(v)
		}
	}

	var dparams map[string]string
	rawD := h.Get("Content-Disposition")
	switch d, dp, err := mh.ContentDisposition(); {
	case rawD == "":
	case err == nil:
		p.disp, dparams = d, dp
	default:
		p.disp = rawD
		_, dparams = lenientMediaType(rawD)
		w.note(noteLenientParams, "the Content-Disposition of a part is malformed; its parameters were read leniently")
		for k, v := range dparams {
			dparams[k], _ = decodeHeaderWords(v)
		}
	}
	if f, ok := dparams["filename"]; ok {
		p.filename = f
	} else {
		p.filename = p.params["name"]
	}
	p.cid = strings.Trim(h.Get("Content-Id"), "<>")
	return p
}

// entity reads one part: a multipart is a container, anything else a leaf.
func (w *walker) entity(h textproto.Header, raw []byte, depth int) {
	p := w.describe(h)
	if strings.HasPrefix(p.mt, "multipart/") {
		w.container(p, raw, depth)
		return
	}
	w.leafSafe(h, p, raw)
}

// container reads the parts of a multipart. A multipart framing error stops this
// container only (the multipart reader is never called on it again) and the walk goes on
// with its parent. A multipart whose boundary is missing or wrong keeps its body as text.
func (w *walker) container(p partInfo, raw []byte, depth int) {
	if depth > maxMultipartDepth {
		w.limit(raw, fmt.Sprintf("multipart parts are nested more than %d levels deep, so the rest is kept unread", maxMultipartDepth))
		return
	}
	boundary := p.params["boundary"]
	if boundary == "" {
		w.unsplit(raw, "the multipart has no boundary parameter")
		return
	}
	// The multipart reader parses a part's header with no cap of its own, so the parts whose
	// header is over the cap are found first (MC22) and never handed to it: such a part is
	// kept unread and the reader starts again at the next delimiter. cur is what the reader
	// runs over, base the number of delimiters before it, read the parts it has given.
	sc := scanParts(raw, boundary)
	cur, base, read := raw, 0, 0
	mr := textproto.NewMultipartReader(bytes.NewReader(cur), boundary)
	for {
		if w.parts >= maxParts {
			if rest, ok := restFromPart(cur, boundary, read); ok {
				w.limit(rest, fmt.Sprintf("the message has more than %d parts, so the rest is kept unread", maxParts))
			}
			return
		}
		if len(sc.over) > 0 && sc.over[0].idx == base+read {
			s := sc.over[0]
			sc.over = sc.over[1:]
			w.parts++
			w.site = fmt.Sprintf("part %d", w.parts)
			w.leafN++
			w.blob(fmt.Sprintf("part-%d", w.leafN), raw[s.bodyStart:s.bodyEnd:s.bodyEnd], state.GapKindStructureLimit,
				"the part's header is larger than 1 MiB, so the part is kept unread")
			if s.next >= len(raw) {
				return
			}
			cur, base, read = raw[s.next:], s.idx+1, 0
			mr = textproto.NewMultipartReader(bytes.NewReader(cur), boundary)
			continue
		}
		part, err := mr.NextPart()
		if err == io.EOF {
			return
		}
		if err != nil {
			w.framing(err, base+read, cur)
			return
		}
		read++
		w.partOf(part, depth, raw, sc.end(raw, base+read-1))
	}
}

// partOf reads one part of a container. parent is the container's body and end where this
// part's body ends in it: the part's raw is kept as a window onto parent, not as a copy, so
// nesting does not hold one copy of the message per level (MS2-AO2).
func (w *walker) partOf(part *textproto.Part, depth int, parent []byte, end int) {
	w.parts++ // the root is not counted: maxParts bounds the parts of a message, not the message
	w.site = fmt.Sprintf("part %d", w.parts)
	body, _ := io.ReadAll(part) // the raw, still transfer-encoded body; a read error ends it where it ended
	raw := windowOf(parent, end, body)
	if headerSize(part.Header) > maxHeaderBytes {
		w.leafN++
		w.blob(fmt.Sprintf("part-%d", w.leafN), raw, state.GapKindStructureLimit,
			"the part's header is larger than 1 MiB, so the part is kept unread")
		return
	}
	w.entity(part.Header, raw, depth+1)
}

// windowOf is body as a slice of parent ending at end, when parent holds exactly it there;
// otherwise (the scan and the multipart reader disagree about where the part ends) the copy
// is returned as it is, so a window is never anything but the bytes the reader gave.
func windowOf(parent []byte, end int, body []byte) []byte {
	n := len(body)
	if n == 0 || end < n || end > len(parent) || !bytes.Equal(parent[end-n:end], body) {
		return body
	}
	return parent[end-n : end : end]
}

// headSpan is a part whose header is over the cap: its delimiter's ordinal in the
// container, the span of its body and where the next delimiter starts.
type headSpan struct{ idx, bodyStart, bodyEnd, next int }

// partScan is what one pass over a container's body learns before the multipart reader
// reads it: where its delimiter lines start (the first few, one per part the walker can
// read) and which parts have a header over the cap.
type partScan struct {
	starts []int
	over   []headSpan
}

// end is the offset in body at which the part of ordinal j ends: just before the line
// break that precedes the next delimiter line, or the end of the data for the last part.
func (s partScan) end(body []byte, j int) int {
	if j+1 >= len(s.starts) {
		return len(body)
	}
	e := s.starts[j+1]
	switch {
	case e >= 2 && body[e-2] == '\r' && body[e-1] == '\n':
		return e - 2
	case e >= 1 && body[e-1] == '\n':
		return e - 1
	}
	return e
}

// scanParts reads a container's body line by line once. Delimiter lines are the multipart
// reader's own: "--boundary" and nothing but white space after it, and the closing line
// "--boundary--". A part's header runs from its delimiter line to the first blank line.
func scanParts(body []byte, boundary string) partScan {
	dash := []byte("--" + boundary)
	var sc partScan
	idx := -1
	var hdrBytes, bodyStart int
	open := false // a part's header is being measured or its body skipped
	finish := func(next int) {
		if !open || hdrBytes <= maxHeaderBytes {
			return
		}
		if bodyStart < 0 {
			bodyStart = next // the header ran into the next delimiter: no body
		}
		end := next
		switch {
		case end-bodyStart >= 2 && body[end-2] == '\r' && body[end-1] == '\n':
			end -= 2
		case end-bodyStart >= 1 && body[end-1] == '\n':
			end--
		}
		sc.over = append(sc.over, headSpan{idx: idx, bodyStart: bodyStart, bodyEnd: max(end, bodyStart), next: next})
	}
	for i := 0; i < len(body); {
		lineEnd := len(body)
		if j := bytes.IndexByte(body[i:], '\n'); j >= 0 {
			lineEnd = i + j + 1
		}
		line := body[i:lineEnd]
		if bytes.HasPrefix(line, dash) {
			rest := bytes.TrimRight(line[len(dash):], " \t\r\n")
			if len(rest) == 0 || string(rest) == "--" {
				finish(i)
				if len(sc.starts) <= maxParts+1 {
					sc.starts = append(sc.starts, i)
				}
				open = len(rest) == 0
				if open {
					idx++
					hdrBytes, bodyStart = 0, -1
				}
				i = lineEnd
				continue
			}
		}
		if open && bodyStart < 0 {
			if len(bytes.TrimRight(line, "\r\n")) == 0 {
				bodyStart = lineEnd
			} else {
				hdrBytes += len(line)
			}
		}
		i = lineEnd
	}
	finish(len(body))
	return sc
}

// framing handles an error the multipart reader gave instead of the next part. With no
// part read, the body could not be split at all (its boundary is not in it); a missing
// closing boundary at the end of the data is the common benign case; anything else is a
// multipart that ended early.
func (w *walker) framing(err error, read int, raw []byte) {
	if read == 0 {
		if len(bytes.TrimSpace(raw)) > 0 {
			w.unsplit(raw, "the parts could not be separated at the boundary")
		}
		return
	}
	if strings.HasSuffix(err.Error(), "EOF") {
		return
	}
	w.gap(state.GapKindCorruptPart, fmt.Sprintf("the multipart ended early, after part %d: the rest of it could not be read", read))
}

// unsplit keeps the body of a multipart whose parts cannot be separated as text.
func (w *walker) unsplit(raw []byte, why string) {
	text, notes := decodeText("", raw)
	w.addNotes(notes)
	if w.msg.PlainBody == "" {
		w.msg.PlainBody = text
	} else {
		w.msg.PlainBody += "\n\n" + text
	}
	w.gap(state.GapKindUnsplitMultipart, why+", so its text is shown as one body")
}

// limit keeps the unread rest of a container as one still-encoded blob.
func (w *walker) limit(rest []byte, why string) {
	w.blob("remaining-parts", rest, state.GapKindStructureLimit, why)
}

// blob adds raw bytes to the attachments as <name>.undecoded, with a gap of kind.
func (w *walker) blob(name string, raw []byte, kind, why string) {
	w.msg.Attachments = append(w.msg.Attachments, model.Attachment{
		Filename: name + ".undecoded", MimeType: "application/octet-stream",
		WriteTo: bytesWriterTo(raw), GapKind: kind, GapDetail: why,
	})
	w.note(kind, why)
}

// restFromPart returns the bytes of a container's body from the start of its n-th
// (0-based) delimiter line on: what a limit leaves unread.
func restFromPart(body []byte, boundary string, n int) ([]byte, bool) {
	dash := []byte("--" + boundary)
	seen := 0
	for i := 0; i < len(body); {
		end := len(body)
		if j := bytes.IndexByte(body[i:], '\n'); j >= 0 {
			end = i + j + 1
		}
		if line := body[i:end]; bytes.HasPrefix(line, dash) && len(bytes.TrimRight(line[len(dash):], " \t\r\n")) == 0 {
			if seen == n {
				return body[i:], true
			}
			seen++
		}
		i = end
	}
	return nil, false
}

// leafSafe is leaf under its own recover: a panic in a decoder costs this part only, and
// the part is kept as received with a corrupt-part gap.
func (w *walker) leafSafe(h textproto.Header, p partInfo, raw []byte) {
	defer func() {
		if r := recover(); r != nil {
			w.blob(fmt.Sprintf("part-%d", w.leafN), raw, state.GapKindCorruptPart,
				"this version's decoder failed on the part, so it is kept as received")
		}
	}()
	w.leaf(h, p, raw)
}

// transfer says how a part's transfer decoding went.
type transfer int

const (
	transferOK      transfer = iota
	transferUnknown          // an encoding this version does not decode: the bytes are as received
	transferCorrupt          // a decode error part way: the bytes are the decoded prefix
)

// normalizeCTE reads a Content-Transfer-Encoding header leniently: comments, quotes and
// case are dropped, and "7-bit" and "8-bit" are the encodings they mean. changed says it
// was not a clean token already.
func normalizeCTE(v string) (norm string, changed bool) {
	// One pass: each "(" runs to the next ")" and an unclosed one runs to the end. The
	// comments are dropped as the string is built, so a header made of a million of them
	// costs a million steps, not a million copies of the rest of the string (MS2-AO1).
	var b strings.Builder
	for s := v; ; {
		open := strings.IndexByte(s, '(')
		if open < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:open])
		closeAt := strings.IndexByte(s[open:], ')')
		if closeAt < 0 {
			break
		}
		s = s[open+closeAt+1:]
	}
	norm = strings.ToLower(strings.Trim(strings.TrimSpace(b.String()), `"'`))
	norm = strings.TrimSpace(norm)
	switch norm {
	case "7-bit":
		norm = "7bit"
	case "8-bit":
		norm = "8bit"
	}
	return norm, norm != strings.ToLower(strings.TrimSpace(v))
}

// decodeTransfer undoes a part's transfer encoding with go-message's own decoder (the one
// v1 used, so the bytes are identical for every encoding v1 understood), through a
// synthetic header that names no charset: no charset is ever applied here. An encoding it
// does not know leaves the bytes as received; an error part way leaves the decoded prefix.
func decodeTransfer(norm string, raw []byte) ([]byte, transfer) {
	var hdr message.Header
	hdr.Set("Content-Type", "application/octet-stream")
	hdr.Set("Content-Transfer-Encoding", norm)
	e, err := message.New(hdr, bytes.NewReader(raw))
	if err != nil {
		return raw, transferUnknown
	}
	out, err := io.ReadAll(e.Body)
	if err != nil {
		return out, transferCorrupt
	}
	return out, transferOK
}

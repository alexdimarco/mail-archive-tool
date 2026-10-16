// Package source opens Outlook data files (.pst/.ost) and yields normalized
// messages, hiding the go-pst API behind a small callback-based walk.
package source

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	charsets "github.com/emersion/go-message/charset"
	pst "github.com/mooijtech/go-pst/v6/pkg"
	"github.com/mooijtech/go-pst/v6/pkg/properties"
	"golang.org/x/text/encoding"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// MAPI property IDs read directly from the property context. go-pst's generated
// struct getters only decode Unicode/integer/time/boolean properties and skip
// binary ones, so binary-stored text (notably PidTagHtml in modern Outlook)
// must be read this way to avoid silently losing it.
const (
	pidTagDisplayName = 12289 // 0x3001, message-store display name
	pidTagRecordKey   = 4089  // 0x0FF9, PidTagRecordKey: the message store's unique id (FC8)
	pidTagBody        = 4096  // 0x1000, PidTagBody (plain text)
	pidTagHtml        = 4115  // 0x1013, PidTagHtml
	pidTagDisplayBcc  = 3586  // 0x0E02, PidTagDisplayBcc
	pidTagReplyRecips = 80    // 0x0050, PidTagReplyRecipientNames
	pidTagInReplyTo   = 4162  // 0x1042, PidTagInReplyToId
	pidTagReferences  = 4153  // 0x1039, PidTagInternetReferences
	// pidTagTransportMessageHeaders is Outlook's decoded copy of the item's
	// internet header block (Received chain, Return-Path, Authentication-
	// Results, List-Id). PST items carry no raw bytes, so this is the only way
	// to recover them; it is often absent for items that never crossed the
	// internet.
	pidTagTransportMessageHeaders = 125 // 0x007D, PidTagTransportMessageHeaders

	// Message-state properties (integers), read for the page's "Status" row.
	// They are captured but excluded from the message's identity (see
	// model.Message).
	pidTagImportance   = 23   // 0x0017, PidTagImportance (0 low, 1 normal, 2 high)
	pidTagSensitivity  = 54   // 0x0036, PidTagSensitivity (0 none, 1 personal, 2 private, 3 confidential)
	pidTagMessageFlags = 3591 // 0x0E07, PidTagMessageFlags (bit 0x1 = mfRead)
)

// registerCharsets wires go-message's charset catalogue into go-pst once, so
// legacy (non-Unicode) String8 properties decode correctly.
var registerCharsets = sync.OnceFunc(func() {
	pst.ExtendCharsets(func(name string, enc encoding.Encoding) {
		charsets.RegisterEncoding(name, enc)
	})
})

// syntheticFolders are container folders Outlook creates that carry no useful
// name; their segment is dropped from the mirrored path.
var syntheticFolders = map[string]bool{
	"ROOT_FOLDER":              true,
	"Root":                     true,
	"Root - Mailbox":           true,
	"Root Container":           true,
	"Top of Personal Folders":  true,
	"Top of Outlook data file": true,
	"Top of Information Store": true,
	"IPM_SUBTREE":              true,
}

// Reader is an open Outlook data file. It implements Source and FaultWalker.
type Reader struct {
	file   *pst.File
	closer io.Closer
	store  string

	// uid is the message store's PidTagRecordKey (0x0FF9) as hex, the identity a
	// pst-nid: locator carries so a recovery never matches across two stores
	// (FC8); "" when the store has none that can be read, which makes every
	// zero-content failure a per-folder aggregate.
	uid string

	// ops is what the walk asks of go-pst: production wraps the file, a test
	// substitutes the tables and nodes a byte patch cannot model precisely.
	ops pstOps

	// catPropID is the property id the store's name-to-id map assigns to the
	// named "Keywords" property (PidNameKeywords in PS_PUBLIC_STRINGS — the
	// message categories), resolved once at open; hasCatProp is false when the
	// store defines no such named property (then no message carries categories).
	catPropID  uint16
	hasCatProp bool
}

// openPST opens the .pst/.ost file at path.
func openPST(path string) (r *Reader, err error) {
	registerCharsets()

	// The operator's own spelling of the input may be a link (the file it names is what is
	// read); the open itself neither follows one nor waits on a pipe, and the descriptor
	// must be a regular file: a FIFO named like a data file used to hold the run, and the
	// archive lock, until a writer appeared (FC2, S2B-AO4).
	real := path
	if p, err := filepath.EvalSymlinks(path); err == nil {
		real = p
	}
	f, err := util.OpenRegularFile(real)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	r, err = openPSTReader(f, f, path)
	if err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// openPSTReader parses the data file read through ra; closer is closed when the
// Reader is, and path is what the file is reported as.
func openPSTReader(ra io.ReaderAt, closer io.Closer, path string) (r *Reader, err error) {
	// go-pst indexes into decrypted/decompressed buffers without bounds checks in
	// places, so a corrupt or truncated .pst/.ost can panic during parsing. Turn
	// that into a clean error, so one bad file (e.g. an orphaned Outlook .ost stub
	// left by a removed account) is skipped rather than crashing the whole run —
	// or, in the no-console GUI, exiting silently (R10).
	defer func() {
		if rec := recover(); rec != nil {
			r = nil
			err = fmt.Errorf("parse %s: corrupt or unsupported Outlook data file (go-pst: %v)", path, rec)
		}
	}()

	pstFile, err := pst.New(ra)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	r = &Reader{
		file:   pstFile,
		closer: closer,
		store:  pstStoreName(pstFile, path),
		uid:    readStoreUID(pstFile),
		ops:    fileOps(pstFile),
	}
	// Resolve the named "Keywords" property (message categories) once per file
	// from go-pst's flat string→id map. StringToID is a flat map, so a (rare)
	// different property set that also names a property "Keywords" could shadow
	// this one — an acknowledged limit (docs/scenario-catalog.md). Absent → no
	// categories are read for any message in this store.
	if pstFile.NameToIDMap != nil {
		if id, ok := pstFile.NameToIDMap.StringToID["Keywords"]; ok {
			r.catPropID = uint16(id)
			r.hasCatProp = true
		}
	}
	return r, nil
}

// StoreName returns the display name of the mail store.
func (r *Reader) StoreName() string { return r.store }

// Close releases the underlying file.
func (r *Reader) Close() error {
	r.file.Cleanup()
	return r.closer.Close()
}

// Walk traverses every folder depth-first, invoking handler for each mail item.
// Non-mail items (appointments, contacts, tasks) are skipped in this version.
// A caller that wants to know about the items and folders it could not read, as
// they happen, uses WalkWithFaults; Walk collects them and returns a
// *FaultsError, so a caller that ignores them still sees a non-nil error (FC29).
func (r *Reader) Walk(handler MessageHandler) error {
	return r.WalkWithFaults(handler, WalkOptions{})
}

// WalkWithFaults is Walk that delivers each fault through opts.Faults as it
// happens (the loops are in pstwalk.go).
func (r *Reader) WalkWithFaults(handler MessageHandler, opts WalkOptions) error {
	w := r.newWalk(handler, opts)
	return w.run()
}

// convertMessage maps a go-pst message into a model.Message, or returns nil for
// non-mail items. catID/hasCat carry the store's resolved "Keywords" property
// id so per-message category reads need no re-resolution; locator, when the store
// has a stable identity, is the item's pst-nid: locator (set on every message,
// never part of an identity, FC7).
//
// Each step runs under its own recover (§4.4), so the smallest thing is lost:
//   - the ENVELOPE (subject, sender, recipients, ids, dates, state, categories) is
//     what the item IS: a failure there returns an error, and the caller records a
//     zero-content gap and yields no message — nothing reliable was read (FC7);
//   - the BODY failing keeps the envelope and adds the unreadable:body label;
//   - the ATTACHMENT TABLE failing (an error or a panic) keeps the envelope and
//     body, sets no attachments and adds unreadable:attachments; it never fails the
//     message.
func convertMessage(m *pst.Message, catID uint16, hasCat bool, locator string) (*model.Message, error) {
	mp, ok := m.Properties.(*properties.Message)
	if !ok {
		return nil, nil
	}
	msg, err := readEnvelope(m, mp, catID, hasCat)
	if err != nil {
		return nil, err
	}
	msg.Locator = locator
	var why []string
	if detail, failed := guard(func() { readBody(m, mp, msg) }); failed {
		msg.ReadGaps = append(msg.ReadGaps, state.LabelUnreadableBody)
		why = append(why, "body: "+detail)
	}
	if detail, failed := guard(func() { msg.Attachments = readAttachments(m) }); failed {
		msg.Attachments = nil
		msg.ReadGaps = append(msg.ReadGaps, state.LabelUnreadableAttachments)
		why = append(why, "attachments: "+detail)
	}
	if len(why) > 0 {
		msg.ReadError = boundDetail(strings.Join(why, "; "))
	}
	return msg, nil
}

// guard runs fn and reports a failure as text: a panic's value, or the error fn
// recorded through failWith. It is how a step of convertMessage loses only itself.
func guard(fn func()) (detail string, failed bool) {
	defer func() {
		if rec := recover(); rec != nil {
			detail, failed = fmt.Sprint(rec), true
		}
	}()
	fn()
	return "", false
}

// errStepFailed is what a step panics with when go-pst answers it with an error
// instead of a panic, so guard sees one kind of failure.
type errStepFailed struct{ err error }

func (e errStepFailed) String() string { return e.err.Error() }

// readEnvelope reads the fields that make the message itself (a variable only so a
// test can make this step fail).
var readEnvelope = func(m *pst.Message, mp *properties.Message, catID uint16, hasCat bool) (msg *model.Message, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			msg, err = nil, fmt.Errorf("the message header could not be read (go-pst: %v)", rec)
		}
	}()
	msg = &model.Message{
		Subject:           mp.GetSubject(),
		SenderName:        mp.GetSenderName(),
		SenderEmail:       mp.GetSenderEmailAddress(),
		To:                mp.GetDisplayTo(),
		Cc:                mp.GetDisplayCc(),
		Bcc:               readTextProperty(m, pidTagDisplayBcc),
		ReplyTo:           readTextProperty(m, pidTagReplyRecips),
		InReplyTo:         strings.Trim(readTextProperty(m, pidTagInReplyTo), "<> "),
		References:        strings.TrimSpace(readTextProperty(m, pidTagReferences)),
		InternetMessageID: mp.GetInternetMessageId(),
		TransportHeaders:  readTextProperty(m, pidTagTransportMessageHeaders),
	}
	if t := mp.GetMessageDeliveryTime(); t != 0 {
		msg.Received = time.Unix(0, t).UTC()
	}
	if t := mp.GetClientSubmitTime(); t != 0 {
		msg.Sent = time.Unix(0, t).UTC()
	}

	// Message state (read/importance/sensitivity). Read straight from the
	// property context so an ABSENT importance stays "" (unremarkable) rather
	// than being conflated with a present 0 (low). Excluded from identity.
	if v, ok := readIntProperty(m, pidTagImportance); ok {
		msg.Importance = importanceString(v)
	}
	if v, ok := readIntProperty(m, pidTagSensitivity); ok {
		msg.Sensitivity = sensitivityString(v)
	}
	if v, ok := readIntProperty(m, pidTagMessageFlags); ok {
		msg.Unread = unreadFromMessageFlags(v)
	}

	// Categories (the named "Keywords" property), read under readCategories'
	// OWN localized recover so a corrupt/hostile Keywords node costs only the
	// categories, never the whole message. Excluded from identity.
	if hasCat {
		msg.Categories = readCategories(m, catID)
	}
	return msg, nil
}

// readBody reads the body into msg: the HTML and plain text, and the RTF when
// there is nothing better. A panic stops it where it is (what was read stays) and
// guard turns it into unreadable:body; a go-pst ERROR for an absent RTF is no
// failure, as it never was (a message with no body is the commonest thing).
// A variable only so a test can make this step panic.
var readBody = func(m *pst.Message, mp *properties.Message, msg *model.Message) {
	// Read the HTML body straight from the property context: modern Outlook
	// stores PidTagHtml as binary, which go-pst's GetBodyHtml() does not decode.
	msg.HTMLBody = readTextProperty(m, pidTagHtml)
	msg.PlainBody = mp.GetBody()
	if strings.TrimSpace(msg.PlainBody) == "" {
		msg.PlainBody = readTextProperty(m, pidTagBody)
	}
	// Only pay the cost of decompressing RTF when there is no better body.
	if strings.TrimSpace(msg.HTMLBody) == "" && strings.TrimSpace(msg.PlainBody) == "" {
		if rtf, err := readRTF(m); err == nil {
			msg.RTFBody = rtf
		}
	}
}

// readRTF and readAttachments are the two reads a test makes panic or fail
// (variables for that reason only).
var (
	readRTF         = func(m *pst.Message) (string, error) { return m.GetBodyRTF() }
	readAttachments = func(m *pst.Message) []model.Attachment {
		atts, err := collectAttachments(m)
		if err != nil {
			panic(errStepFailed{err})
		}
		return atts
	}
)

// readTextProperty reads a text-valued property directly from the message's
// property context, handling both Unicode (PtypString, UTF-16LE) and binary
// (PtypBinary) storage. It returns "" when the property is absent or unreadable.
func readTextProperty(m *pst.Message, propertyID uint16) string {
	r, err := m.PropertyContext.GetPropertyReader(propertyID, m.LocalDescriptors)
	if err != nil {
		return ""
	}

	// Unicode strings decode correctly through the library helper.
	if r.Property.Type == pst.PropertyTypeString {
		if s, err := r.GetString(); err == nil {
			return s
		}
	}

	size := r.Size()
	if size <= 0 {
		return ""
	}
	buf := make([]byte, size)
	if _, err := r.ReadAt(buf, 0); err != nil {
		return ""
	}
	return decodeBytes(buf)
}

// readIntProperty reads a 32-bit integer property directly from the message's
// property context. The bool is false when the property is absent or is not an
// integer, letting the caller distinguish "unset" from a real zero value.
func readIntProperty(m *pst.Message, propertyID uint16) (int32, bool) {
	r, err := m.PropertyContext.GetPropertyReader(propertyID, m.LocalDescriptors)
	if err != nil {
		return 0, false
	}
	v, err := r.GetInteger32()
	if err != nil {
		return 0, false
	}
	return v, true
}

// importanceString maps PidTagImportance (0 low / 1 normal / 2 high) to the
// model's convention; normal and any unexpected value are the empty
// (unremarkable) state.
func importanceString(v int32) string {
	switch v {
	case 0:
		return "low"
	case 2:
		return "high"
	default:
		return ""
	}
}

// sensitivityString maps PidTagSensitivity (0 none / 1 personal / 2 private /
// 3 confidential) to the model's convention; none and any unexpected value are
// the empty state.
func sensitivityString(v int32) string {
	switch v {
	case 1:
		return "personal"
	case 2:
		return "private"
	case 3:
		return "confidential"
	default:
		return ""
	}
}

// unreadFromMessageFlags reports the read state from PidTagMessageFlags: bit
// 0x1 (mfRead) is set once the item has been read, so its ABSENCE means unread.
func unreadFromMessageFlags(flags int32) bool { return flags&0x1 == 0 }

// readCategories reads the store's named "Keywords" property (message
// categories) at the resolved property id. It carries its OWN defer/recover
// (returning nil), so a corrupt or hostile Keywords node costs ONLY the
// categories — the whole-message stub never swallows a categorized message
// (QC5, R10). Both a single-valued unicode (PT_UNICODE) and a multi-valued
// unicode (PT_MV_UNICODE = 4127) categories property are handled; every value
// is control-stripped and trimmed and empties dropped. An absent property, a
// foreign type, or an unreadable blob yields no categories and never fails the
// message (R1).
func readCategories(m *pst.Message, propID uint16) (cats []string) {
	defer func() {
		if recover() != nil {
			cats = nil
		}
	}()
	r, err := m.PropertyContext.GetPropertyReader(propID, m.LocalDescriptors)
	if err != nil {
		return nil
	}
	switch r.Property.Type {
	case pst.PropertyTypeString:
		// Bound the single-value read like the MV branch: categories are small.
		if r.Size() > 1<<20 {
			return nil
		}
		s, err := r.GetString()
		if err != nil {
			return nil
		}
		if s = cleanCategory(s); s != "" {
			return []string{s}
		}
		return nil
	case pst.PropertyTypeMultipleString:
		size := r.Size()
		if size <= 0 {
			return nil
		}
		// Bound the blob read BEFORE allocating: the declared size is untrusted
		// and categories are small. parseMVUnicode bounds the parse itself.
		const maxBlob = 1 << 20 // 1 MiB
		if size > maxBlob {
			size = maxBlob
		}
		buf := make([]byte, size)
		n, _ := r.ReadAt(buf, 0)
		return parseMVUnicode(buf[:n])
	}
	return nil
}

// parseMVUnicode parses a PT_MV_UNICODE (PropertyTypeMultipleString = 4127)
// property blob into its string values. The blob is a 4-byte little-endian
// value count, then that many 4-byte little-endian byte offsets (each a value's
// start, measured from the blob's start), then the values as UTF-16LE bytes;
// value i runs to the next offset, and the last value to the blob's end. The
// blob is UNTRUSTED (it rides in from the source file), so every quantity is
// bounded BEFORE it is used to allocate or index (QC2):
//
//   - the declared count is clamped to what the offset table could physically
//     hold, (len(blob)-4)/4, AND to a small absolute ceiling, BEFORE anything
//     is allocated — a hostile 0xFFFFFFFF can never size a slice or a loop;
//   - no slice is pre-sized from the untrusted count; offsets are appended as
//     they validate;
//   - offset arithmetic is 64-bit;
//   - every offset must fall within [headerEnd, len(blob)] and not move
//     backwards; the FIRST violation stops the parse.
//
// A truncated or hostile blob thus yields a bounded, safe subset of in-range
// values — never an over-allocation, an out-of-range index, or a panic. Each
// value is control-stripped and trimmed; empties are dropped.
func parseMVUnicode(blob []byte) []string {
	const maxValues = 4096
	blen := int64(len(blob))
	if blen < 4 {
		return nil
	}
	count := uint64(binary.LittleEndian.Uint32(blob[:4]))
	if maxByLen := uint64(blen-4) / 4; count > maxByLen {
		count = maxByLen // cannot exceed what the offset table could hold
	}
	if count > maxValues {
		count = maxValues // absolute ceiling
	}
	if count == 0 {
		return nil
	}
	headerEnd := 4 + int64(count)*4

	// Read the offset table, validating each entry before trusting it. The
	// offsets slice is NOT pre-sized from the count: entries are appended as
	// they pass, so a mid-table violation just yields a shorter, still-safe
	// table.
	var offsets []int64
	prev := headerEnd
	for i := int64(0); i < int64(count); i++ {
		off := int64(binary.LittleEndian.Uint32(blob[4+i*4 : 8+i*4]))
		if off < prev || off > blen {
			break // first violation: stop, keep what validated
		}
		offsets = append(offsets, off)
		prev = off
	}

	var out []string
	for i := 0; i < len(offsets); i++ {
		start := offsets[i]
		end := blen
		if i+1 < len(offsets) {
			end = offsets[i+1]
		}
		// headerEnd ≤ start ≤ end ≤ blen, all validated above: the slice is
		// always in range.
		if v := cleanCategory(decodeUTF16LE(blob[start:end])); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// decodeUTF16LE decodes UTF-16LE bytes into a Go string; an odd trailing byte
// (a truncated value) is ignored.
func decodeUTF16LE(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// cleanCategory trims and control-strips one untrusted category value.
func cleanCategory(s string) string {
	return strings.TrimSpace(util.StripControl(s))
}

// decodeBytes turns raw binary text bytes into a Go string. Modern Outlook
// stores PidTagHtml as UTF-8 bytes; legacy messages use a single-byte codepage,
// for which Windows-1252 is the safe western default. The rule is util.DecodeLegacy's,
// which the raw-MIME body decoder shares (design-mime-fidelity MC35).
func decodeBytes(buf []byte) string { return util.DecodeLegacy(buf) }

func collectAttachments(m *pst.Message) ([]model.Attachment, error) {
	it, err := m.GetAttachmentIterator()
	if errors.Is(err, pst.ErrAttachmentsNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []model.Attachment
	idx := 0
	for it.Next() {
		a := it.Value() // fresh *Attachment per iteration, safe to capture

		name := a.GetAttachLongFilename()
		if name == "" {
			name = a.GetAttachFilename()
		}
		if name == "" {
			if ext := a.GetAttachExtension(); ext != "" {
				name = fmt.Sprintf("attachment-%d%s", idx, ext)
			}
		}

		out = append(out, model.Attachment{
			Filename:  name,
			MimeType:  a.GetAttachMimeTag(),
			ContentID: a.GetAttachContentId(),
			WriteTo: func(w io.Writer) (n int64, err error) {
				// The stream is lazy, so it runs long after convertMessage's guard has
				// returned: a panic in it (a property that lies about its type leaves
				// go-pst's reader nil, S2A-AC7) is this attachment's error, like a torn
				// stream, never the message's.
				defer func() {
					if rec := recover(); rec != nil {
						n, err = 0, fmt.Errorf("go-pst panicked reading the attachment: %v", rec)
					}
				}()
				return a.WriteTo(w)
			},
		})
		idx++
	}
	if it.Err() != nil {
		return nil, it.Err()
	}
	return out, nil
}

// readStoreUID reads the message store's PidTagRecordKey (0x0FF9) as hex: the
// store's identity, which a pst-nid: locator carries so a recovery can never match
// an item of another store (FC8). It is "" when the store has none that can be
// read — absent, unreadable, over-long or all zero — and then every zero-content
// failure is counted in one per-folder aggregate instead of by NID.
func readStoreUID(f *pst.File) (uid string) {
	defer func() {
		if recover() != nil {
			uid = ""
		}
	}()
	pc, err := f.GetMessageStore()
	if err != nil {
		return ""
	}
	r, err := pc.GetPropertyReader(pidTagRecordKey, nil)
	if err != nil {
		return ""
	}
	size := r.Size()
	if size <= 0 || size > 64 {
		return ""
	}
	buf := make([]byte, size)
	if n, err := r.ReadAt(buf, 0); err != nil || n != len(buf) {
		return ""
	}
	if allZero(buf) {
		return ""
	}
	return hex.EncodeToString(buf)
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

// pstStoreName derives a human-readable label for the data file, preferring the
// message-store display name and falling back to the file's base name.
func pstStoreName(f *pst.File, path string) string {
	if pc, err := f.GetMessageStore(); err == nil {
		if reader, err := pc.GetPropertyReader(pidTagDisplayName, nil); err == nil {
			if name, err := reader.GetString(); err == nil && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
		}
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

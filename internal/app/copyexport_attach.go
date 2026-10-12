package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// Extract attachments (design §4.12, slice E6): with the option on, each message that has
// an original also gets its attachments as loose decoded files under <stem>.attachments/,
// beside the original. They are made from the original's own bytes read back from the
// destination (the same read-back and hash check as the readable copy), written through the
// contained writer, hashed as written, and labelled derived: the .eml stays the
// authoritative copy. A part that cannot be extracted is a note on a message that is still
// exported, never silent (R1); where the archive kept no original, the attachments zip is
// copied byte-exact (E4) and its entries are extracted under bounds too.

const (
	// attachDirSuffix follows the message's stem in the name of its attachments directory.
	attachDirSuffix = ".attachments"

	// attachNotePrefix starts every note this slice adds to an entry, so the README can count
	// the messages that have one.
	attachNotePrefix = "attachments: "

	// copyAttachNotesMax bounds the attachment notes one entry carries: a hostile message can
	// have thousands of parts, and a note is a line of the run record. The rest are counted
	// in one more note.
	copyAttachNotesMax = 20

	// attachAuthorityEML and attachAuthorityZip say what holds a part that was not extracted.
	attachAuthorityEML = "the .eml beside it holds the message as it was sent"
	attachAuthorityZip = "the zip beside it holds it as the archive kept it"
)

// The bounds on extracting a zip's entries (design §4.12): at most 1,000 entries, 1 GiB
// uncompressed for one entry, 4 GiB for one message, and a compression ratio of at most 200:1.
// An entry over a bound is skipped with a note; a zip is untrusted input (a bomb is a few
// kilobytes), so none of them is ever expanded to find out.
const (
	copyZipEntries    = 1000
	copyZipEntryBytes = 1 << 30
	copyZipTotalBytes = 4 << 30
	copyZipRatio      = 200
)

// attachBounds are the bounds a run extracts a zip under; a zero field is the design's.
type attachBounds struct {
	entries    int
	entryBytes int64
	totalBytes int64
	ratio      int64
}

// zipBounds are the bounds in force: the design's, unless a test lowered one (zipLimit).
func (e *CopyExportEnv) zipBounds() attachBounds {
	b := e.zipLimit
	if b.entries <= 0 {
		b.entries = copyZipEntries
	}
	if b.entryBytes <= 0 {
		b.entryBytes = copyZipEntryBytes
	}
	if b.totalBytes <= 0 {
		b.totalBytes = copyZipTotalBytes
	}
	if b.ratio <= 0 {
		b.ratio = copyZipRatio
	}
	return b
}

var (
	// errAttachEmpty is a part with no content.
	errAttachEmpty = errors.New("the attachment has no content")
	// errAttachNoSpace is a derived file that does not fit in what is left on the destination.
	errAttachNoSpace = errors.New("no space left for it")
)

// attachPartError is a part whose own stream failed: it could not be read from the message
// (or panicked while it was).
type attachPartError struct{ why string }

func (e *attachPartError) Error() string { return e.why }

// attachNotes collects an entry's attachment notes, bounded.
type attachNotes struct {
	list  []string
	extra int
}

func (n *attachNotes) add(s string) {
	if len(n.list) < copyAttachNotesMax {
		n.list = append(n.list, attachNotePrefix+s)
		return
	}
	n.extra++
}

// done is the notes, with one more that counts those past the bound.
func (n *attachNotes) done() []string {
	if n.extra > 0 {
		return append(slices.Clone(n.list), attachNotePrefix+fmt.Sprintf("%s more attachments have a note of their own that is not listed here; the original beside them holds every part", commas(n.extra)))
	}
	return n.list
}

// attachWriter writes one message's extracted files into <dir>/<stem>.attachments/.
type attachWriter struct {
	r      *CopyRun
	ctx    context.Context
	adir   string // the attachments directory, relative to the export root, forward slashes
	used   map[string]struct{}
	line   *ceMsgLine
	notes  attachNotes
	placed int
	// written is every byte the writer has put into a temp file for this message, whether the part
	// was kept or removed again after it failed: the per-message bound on a zip's entries counts it.
	written int64
	// expanded is the bytes a zip's entries expanded to when they were read through before being
	// written (expand), kept or not: with written, what the per-message bound counts.
	expanded int64
	holds    string // what holds a part that was not extracted
	from     string // where the parts are read from: "the message", or "the zip"
}

func newAttachWriter(r *CopyRun, ctx context.Context, dir, stem string, line *ceMsgLine, holds string) *attachWriter {
	return &attachWriter{r: r, ctx: ctx, adir: ceJoin(dir, stem+attachDirSuffix), used: map[string]struct{}{}, line: line, holds: holds, from: "the message"}
}

// attachSpaceWriter refuses what no longer fits in the space the destination has left.
type attachSpaceWriter struct {
	w    io.Writer
	left int64
}

func (s *attachSpaceWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > s.left {
		return 0, errAttachNoSpace
	}
	n, err := s.w.Write(p)
	s.left -= int64(n)
	return n, err
}

// classifyStream sorts what a part's stream returned: nil, no space, a failed write of the
// destination, or (anything else) the part's own failure to be read, with its reason made
// plain and bounded.
func classifyStream(err error) error {
	var we *ceWriteError
	var pe *attachPartError
	switch {
	case err == nil, errors.Is(err, errAttachNoSpace), errors.As(err, &we), errors.As(err, &pe):
		return err
	}
	why := plainText(err.Error())
	if len(why) > 200 {
		why = strings.ToValidUTF8(why[:200], "")
	}
	return &attachPartError{why}
}

// guardStream makes a part's stream a call that cannot panic the run: a panic is the part's
// failure.
func guardStream(fn func(io.Writer) (int64, error)) func(io.Writer) (int64, error) {
	return func(w io.Writer) (n int64, err error) {
		defer func() {
			if p := recover(); p != nil {
				n, err = 0, &attachPartError{panicReason(p)}
			}
		}()
		return fn(w)
	}
}

// put streams one part into a new file of the attachments directory and gives it its name:
// a temp file made through the contained writer, hashed as written, fsynced and renamed with
// no replace. The destination's free space is read first and the stream may not pass it
// (derived files are checked against what is left, design §4.13). The file is recorded in
// the entry's line as it is placed, so what the record says is what the disk holds.
func (w *attachWriter) put(filename string, index int, stream func(io.Writer) (int64, error)) (ceFileRec, error) {
	r := w.r
	budget, err := r.budget()
	if err != nil {
		return ceFileRec{}, &ceWriteError{err}
	}
	if budget <= 0 {
		return ceFileRec{}, errAttachNoSpace
	}
	if err := r.ensureDir(w.adir); err != nil {
		return ceFileRec{}, &ceWriteError{err}
	}
	f, tmp, err := r.fs.CreateTemp(w.adir)
	if err != nil {
		return ceFileRec{}, &ceWriteError{err}
	}
	hw := &ceHashWriter{w: f, h: sha256.New(), watch: r.watch, total: &r.bytes}
	_, serr := stream(&attachSpaceWriter{w: hw, left: budget})
	w.written += hw.n // charged whether the part is kept or not: what was written is what the bound limits (E6-AO1)
	serr = classifyStream(serr)
	if serr == nil {
		if serr = f.Sync(); serr != nil {
			serr = &ceWriteError{serr}
		}
	}
	if cerr := f.Close(); serr == nil && cerr != nil {
		serr = &ceWriteError{cerr}
	}
	if serr == nil && hw.n == 0 {
		serr = errAttachEmpty
	}
	if serr != nil {
		r.fs.Remove(ceJoin(w.adir, tmp))
		return ceFileRec{}, serr
	}
	name := ceUniqueName(w.used, util.ExportFilename(filename, index))
	base, ext := splitExt(name)
	placed, perr := r.placeFinalAs(w.ctx, w.adir, tmp, base, ext)
	if perr != nil {
		r.fs.Remove(ceJoin(w.adir, tmp))
		return ceFileRec{}, &ceWriteError{perr}
	}
	rec := ceFileRec{Path: ceJoin(w.adir, placed), Role: roleDerivedAttach, SHA256: hex.EncodeToString(hw.h.Sum(nil)), Size: hw.n}
	w.line.Files = append(w.line.Files, rec)
	w.placed++
	r.derivedReserve += derivedFileReserve(rec.Path)
	return rec, nil
}

// derivedFileReserve is what the finalization files need for one more recorded file: its
// manifest entry, its line in SHA256SUMS.txt and its share of the run record, sized as the
// plan sizes a message's (RR2-C5), so a destination that is almost full still has room for
// the files that make the export checkable.
func derivedFileReserve(rel string) int64 {
	return int64(240+6*len(rel)) + int64(64+2+len(rel)+1)
}

// problem makes the note for a part that was not extracted.
func (w *attachWriter) problem(label string, err error) {
	var we *ceWriteError
	var pe *attachPartError
	switch {
	case errors.Is(err, errAttachEmpty):
		w.notes.add(fmt.Sprintf("%q has no content (0 bytes), so no file was written for it; %s", label, w.holds))
	case errors.Is(err, errAttachNoSpace):
		w.notes.add(fmt.Sprintf("%q was not extracted: no space was left for it on the destination; free some space there and export this message again", label))
	case errors.As(err, &we):
		w.notes.add(fmt.Sprintf("%q was not extracted: it could not be written (%s); check the destination and export this message again", label, leaveName(we.err)))
	case errors.As(err, &pe):
		w.notes.add(fmt.Sprintf("%q was not extracted: it could not be read from %s (%s); %s", label, w.from, pe.why, w.holds))
	default:
		w.notes.add(fmt.Sprintf("%q was not extracted: %s; %s", label, plainText(err.Error()), w.holds))
	}
}

// gapNote says what a part the reader kept with a parse gap is. Its file is extracted under
// the name the walker gave it (.undecoded or .partial), as what it is, and is never
// presented as the complete decoded attachment.
func (w *attachWriter) gapNote(name, kind string) {
	switch kind {
	case state.GapKindUndecodedPart:
		w.notes.add(fmt.Sprintf("%q is the part exactly as received, not a decoded attachment: it uses a transfer encoding this version does not decode; %s", name, w.holds))
	case state.GapKindCorruptPart:
		w.notes.add(fmt.Sprintf("%q holds only the readable start of a part that was cut short or damaged, so it is not the complete attachment; %s", name, w.holds))
	case state.GapKindStructureLimit:
		w.notes.add(fmt.Sprintf("%q is part of the message kept exactly as received because this version stopped reading its structure there, so it is not a decoded attachment; %s", name, w.holds))
	default:
		w.notes.add(fmt.Sprintf("%q was kept with a reading problem (%s), so it is not necessarily the complete attachment; %s", name, plainText(kind), w.holds))
	}
}

// namelessExt maps the media types a nameless attachment is most often to the extension its
// file takes (design §4.9). The table is fixed, so a name never depends on the computer's
// own list of types, and it leaves out the types a browser would run (HTML, SVG).
var namelessExt = map[string]string{
	"application/pdf": ".pdf", "application/zip": ".zip", "application/gzip": ".gz", "application/json": ".json",
	"application/xml": ".xml", "text/xml": ".xml", "application/rtf": ".rtf", "text/rtf": ".rtf",
	"application/msword": ".doc", "application/vnd.ms-excel": ".xls", "application/vnd.ms-powerpoint": ".ppt",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   ".docx",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         ".xlsx",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": ".pptx",
	"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/bmp": ".bmp", "image/tiff": ".tif",
	"text/plain": ".txt", "text/csv": ".csv", "text/calendar": ".ics", "text/vcard": ".vcf", "text/x-vcard": ".vcf",
	"message/rfc822": ".eml", "audio/mpeg": ".mp3", "video/mp4": ".mp4",
}

// partFilename is the name a part is written under before sanitizing: its own, or, for a
// nameless part, attachment-<its place><the extension of its media type>.
func partFilename(a model.Attachment, index int) string {
	if strings.TrimSpace(a.Filename) != "" {
		return a.Filename
	}
	mt, _, _ := strings.Cut(strings.ToLower(a.MimeType), ";")
	return fmt.Sprintf("attachment-%d%s", index, namelessExt[strings.TrimSpace(mt)])
}

// parts extracts the parsed message's attachments, inline images included, in order.
func (w *attachWriter) parts(atts []model.Attachment) {
	for i, a := range atts {
		name := partFilename(a, i)
		label := plainText(util.ExportFilename(name, i))
		if a.WriteTo == nil {
			w.notes.add(fmt.Sprintf("%q was not extracted: nothing of it could be kept; %s", label, w.holds))
			continue
		}
		rec, err := w.put(name, i, guardStream(a.WriteTo))
		if err != nil {
			w.problem(label, err)
			continue
		}
		if a.GapKind != "" {
			w.gapNote(plainText(path.Base(rec.Path)), a.GapKind)
		}
	}
}

// finish adds the notes to the entry and, when no file was placed, takes the directory
// back, so an entry never has an empty one.
func (w *attachWriter) finish() {
	if w.placed == 0 {
		w.r.dropDir(w.adir)
	}
	w.line.Notes = append(w.line.Notes, w.notes.done()...)
}

// dropDir removes a directory the run made and nothing was placed in.
func (r *CopyRun) dropDir(rel string) {
	if !r.made[rel] {
		return
	}
	if err := r.fs.Remove(rel); err != nil {
		return // something is in it: the presence check and the sums will say
	}
	delete(r.made, rel)
	r.dirs = slices.DeleteFunc(r.dirs, func(d string) bool { return d == rel })
}

// bytesBound words a bound for a note: "1 GiB", "100 KiB".
func bytesBound(n int64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return fmt.Sprintf("%d GiB", n>>30)
	case n >= 1<<20 && n%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", n>>20)
	case n >= 1<<10 && n%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}

// zipBase is a zip entry's name flattened to its base name: whatever stands before the last
// slash or backslash is dropped, so "../x" and "a\b" are "x" and "b" and no entry can name a
// place outside the attachments directory (zip-slip). What is left goes through the same
// naming as any attachment.
func zipBase(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		return name[i+1:]
	}
	return name
}

// errZipSize is an entry whose data does not match the size its header states: it runs past
// it (a header that lies low, which is how a bomb hides its size), or stops short of it.
var errZipSize = &attachPartError{"its data does not match the size its header states"}

// zipReadError is a failure to read one entry as words: the library's own (ErrFormat is what
// it returns when the data runs past the declared size) said plainly.
func zipReadError(err error) error {
	switch {
	case errors.Is(err, zip.ErrFormat), errors.Is(err, io.ErrUnexpectedEOF):
		return errZipSize
	case errors.Is(err, zip.ErrChecksum):
		return &attachPartError{"its data does not match its checksum, so it is damaged"}
	}
	return err
}

// zipSource is the ReaderAt a zip is read through. It counts the bytes read from the zip's data,
// which is what an entry really occupies: the compressed size in an entry's header is the
// sender's claim, and nothing in the zip reader checks it against the stream (E6-AO2).
type zipSource struct {
	r    *bytes.Reader
	read int64 // bytes read since the last reset
}

func (s *zipSource) ReadAt(p []byte, off int64) (int, error) {
	n, err := s.r.ReadAt(p, off)
	s.read += int64(n)
	return n, err
}

// openZip opens the zip's central directory under a panic guard.
func openZip(data []byte) (zr *zip.Reader, src *zipSource, err error) {
	defer func() {
		if p := recover(); p != nil {
			zr, src, err = nil, nil, errors.New(panicReason(p))
		}
	}()
	src = &zipSource{r: bytes.NewReader(data)}
	zr, err = zip.NewReader(src, int64(len(data)))
	return zr, src, err
}

// breach says why an entry may not be extracted, judged on what its header declares and what
// has been extracted so far, before a byte of it is read: "" when it may. A zip header can lie
// about its sizes, so the stream is held to the size it declares too (zipStream), and what
// the declared sizes allow is never more than the bounds.
func (bd attachBounds) breach(f *zip.File, total int64) string {
	declared := f.UncompressedSize64
	switch {
	case f.Flags&0x1 != 0:
		return "it is encrypted, which this version cannot read"
	case declared > uint64(bd.entryBytes):
		return fmt.Sprintf("it is larger than %s uncompressed (the most this version extracts from one entry)", bytesBound(bd.entryBytes))
	case declared > 0 && (declared-1)/uint64(bd.ratio) >= f.CompressedSize64: // declared > ratio*packed, without the product, which wraps for a claim near 2^64 (E6-AI2)
		return fmt.Sprintf("it would expand more than %d times (%s packed, %s unpacked), which this version takes for a zip bomb and does not expand",
			bd.ratio, humanBytes(int64(min(f.CompressedSize64, 1<<62))), humanBytes(int64(min(declared, 1<<62))))
	case total+int64(declared) > bd.totalBytes:
		return fmt.Sprintf("extracting it would take this message's extracted files past %s (the most this version extracts for one message)", bytesBound(bd.totalBytes))
	}
	return ""
}

// zipStream is the stream of one entry: its decompressed data, which may not pass the size
// its header declares (a header that lies low is a bomb the declared bounds never saw) and
// whose checksum the reader checks at the end.
func zipStream(f *zip.File) func(io.Writer) (int64, error) {
	return func(dst io.Writer) (int64, error) {
		rc, err := f.Open()
		if err != nil {
			return 0, zipReadError(err)
		}
		defer rc.Close()
		limit := int64(f.UncompressedSize64)
		n, err := io.Copy(dst, io.LimitReader(rc, limit+1))
		if err != nil {
			return n, zipReadError(err)
		}
		if n > limit { // the library stops an entry at its declared size too; this does not rest on that
			return n, errZipSize
		}
		return n, nil
	}
}

// zipEntries extracts the entries of an archived attachments zip under the bounds. Directory
// entries are not attachments and are left out; every other entry is a file named by its base
// name, or a note.
func (w *attachWriter) zipEntries(data []byte, bd attachBounds) {
	zr, src, err := openZip(data)
	if err != nil {
		w.notes.add(fmt.Sprintf("the zip could not be opened (%s), so its entries were not extracted; %s", plainText(err.Error()), w.holds))
		return
	}
	files := zr.File
	if len(files) > bd.entries {
		w.notes.add(fmt.Sprintf("the zip holds %s entries and this version extracts the first %s: the others were not extracted; %s", commas(len(files)), commas(bd.entries), w.holds))
		files = files[:bd.entries]
	}
	for i, f := range files {
		if f.FileInfo().IsDir() || strings.HasSuffix(f.Name, "/") {
			continue
		}
		name := zipBase(f.Name)
		label := plainText(util.ExportFilename(name, i))
		if why := bd.breach(f, max(w.written, w.expanded)); why != "" {
			w.notes.add(fmt.Sprintf("%q (an entry of the zip) was not extracted: %s; %s", label, why, w.holds))
			continue
		}
		// Nothing of an entry is written before it is known to be what it says: its data is read
		// through to its end once to nowhere, which finds a damaged or mis-sized entry and a
		// bomb whose header lied about its packed size, and costs time, not disk (E6-AO2).
		n, why, err := w.expand(f, src, bd)
		w.expanded += n
		if why != "" {
			w.notes.add(fmt.Sprintf("%q (an entry of the zip) was not extracted: %s; %s", label, why, w.holds))
			continue
		}
		if err != nil {
			w.problem(label, err)
			continue
		}
		if _, err := w.put(name, i, guardStream(zipStream(f))); err != nil {
			w.problem(label, err)
		}
	}
}

// expand reads one entry's data through to its end and throws it away: how many bytes it
// expands to, and why it may not be extracted when it is a bomb whatever its header claims
// (what it really packs into, which src counted, against the ratio), or the failure of its own
// data. Its bytes count toward the message's total whether or not the entry is then kept.
func (w *attachWriter) expand(f *zip.File, src *zipSource, bd attachBounds) (n int64, why string, err error) {
	src.read = 0
	n, err = guardStream(zipStream(f))(io.Discard)
	if err = classifyStream(err); err != nil {
		return n, "", err
	}
	if n > bd.ratio*src.read {
		return n, fmt.Sprintf("its data expands more than %d times (%s packed, %s unpacked) whatever its header says, which this version takes for a zip bomb and does not extract",
			bd.ratio, humanBytes(src.read), humanBytes(n)), nil
	}
	return n, "", nil
}

// addZipEntries extracts the entries of the archived attachments zip just placed as zrec into
// dir/<stem>.attachments/, for an archive record the archive kept no original of. The zip is
// read back from the destination under the hash check, as an original is, and the entries are
// read from those bytes. It returns errOriginalChanged when the zip is not what was written.
func (r *CopyRun) addZipEntries(ctx context.Context, n int, dir, stem string, zrec ceFileRec, line *ceMsgLine) error {
	r.env.step(fmt.Sprintf("W5zip:%d", n))
	data, err := r.readBack(zrec)
	if err != nil {
		if errors.Is(err, errOriginalChanged) {
			return err
		}
		line.Notes = append(line.Notes, attachNotePrefix+"the zip's entries were not extracted: the zip could not be read back ("+leaveName(err)+"); "+attachAuthorityZip)
		return nil
	}
	w := newAttachWriter(r, ctx, dir, stem, line, attachAuthorityZip)
	w.from = "the zip"
	w.zipEntries(data, r.env.zipBounds())
	w.finish()
	r.env.step(fmt.Sprintf("W5att:%d", n))
	return nil
}

// addAttachments extracts the attachments of the parsed original (or says why it could not)
// into dir/<stem>.attachments/ and adds them and their notes to the message's line. It
// returns the directory's name when at least one file was placed.
func (r *CopyRun) addAttachments(ctx context.Context, n int, dir, stem string, msg *model.Message, why string, line *ceMsgLine) string {
	w := newAttachWriter(r, ctx, dir, stem, line, attachAuthorityEML)
	if why != "" {
		w.notes.add(fmt.Sprintf("the message could not be read, so its attachments were not extracted (%s); %s", why, attachAuthorityEML))
	} else {
		w.parts(msg.Attachments)
	}
	w.finish()
	r.env.step(fmt.Sprintf("W5att:%d", n))
	if w.placed == 0 {
		return ""
	}
	return stem + attachDirSuffix
}

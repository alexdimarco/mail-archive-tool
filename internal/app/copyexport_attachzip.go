package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// Extract attachments in zip mode (owner ruling 2026-10-12; catalog rows MA-533..MA-535): with
// Extract attachments on and CopyOptions.AttachmentsAsZip set, a message's attachments are ONE
// file beside its original, <stem>.attachments.zip, instead of a folder of loose files. Nothing
// is expanded, so no directory holds more than the message's own files (XC12), there is no loose
// file with a hostile name or a long path, and nothing of the zip-bomb bounds of loose mode
// applies. The entries are the parts the parser yields, under the names loose mode gives them
// (the export-only pass, FoldKey-unique, flattened), made from the original's own bytes read back
// from the destination under the hash check, as loose mode's are.

const (
	// attachZipSuffix follows the message's stem in the name of its attachments zip. It is the
	// longest suffix a message of this mode has (the plan counts it, MA-535).
	attachZipSuffix = ".attachments.zip"

	// attachZipEntryOverhead is what an entry adds to the zip beyond its data, bounded: a local
	// header and a record in the directory with a name of up to 255 bytes twice over, a Zip64
	// extension and an extended timestamp, and a data descriptor.
	attachZipEntryOverhead = 1024
	// attachZipSlack is what the end of the zip (its directory's end record, Zip64's) and the
	// writer's own buffering are given beyond the entries.
	attachZipSlack = 4096
)

// isDerivedAttach reports whether a file role is an extracted attachment: a loose file or the zip.
func isDerivedAttach(role string) bool {
	return role == roleDerivedAttach || role == roleDerivedAttachZip
}

// attachZipEpoch is the entry time of a message with no usable date: the earliest a zip can say.
var attachZipEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// attachZipStamp is the time every entry of one message's zip carries: the message's own date, or
// the epoch when it has none a zip can hold, so the same message always gives the same zip.
func attachZipStamp(received time.Time) time.Time {
	// The range is the UTC time's, which is what the entry holds (a date in 1980 in the message's own
	// zone can be 1979 in UTC, which has no DOS date), and ends where the extended timestamp's 32 bits
	// do (2106-02-07), past which the time wraps to 1970 (E6Z-AO6).
	t := received.UTC()
	if received.IsZero() || t.Before(attachZipEpoch) || !t.Before(attachZipLast) {
		return attachZipEpoch
	}
	return t.Truncate(2 * time.Second)
}

// attachZipLast is the first day no entry time can be: the 32-bit extended timestamp ends on
// 2106-02-07 06:28:15 UTC.
var attachZipLast = time.Date(2106, 2, 7, 0, 0, 0, 0, time.UTC)

// attachZipHeader is the header of one entry: deflated, dated, and made on a Unix host with a
// plain 0644 file mode. Go's own default is host 0 (MS-DOS) and no mode, and macOS's unzip reads
// the name of such an entry as CP437 whatever its UTF-8 flag says, makes it under a mangled name
// and then stops at the first one ("write error (disk full?)", exit 50), so a sender's first
// accented attachment would cost the owner every later one (E6Z-AO1). A zero mode would be
// extracted unreadable, and the fixed mode keeps the zip the same bytes every time.
func attachZipHeader(name string, stamp time.Time) *zip.FileHeader {
	h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: stamp}
	h.SetMode(0o644)
	return h
}

// attachZipFile is the zip while it is being made: a temp file made through the contained
// writer, the hash of every byte as written, and the zip writer over them (through a writer
// that refuses what the space left cannot hold).
type attachZipFile struct {
	f   ceFile
	tmp string
	hw  *ceHashWriter
	zw  *zip.Writer
}

// zipParts writes the parsed message's attachments as the entries of one zip and returns the
// record of the zip placed, or false when no zip was made (no attachment could be kept, the run
// was stopped, or the zip could not be written: each is a note, never silent, R1).
func (w *attachWriter) zipParts(dir, stem string, stamp time.Time, atts []model.Attachment) (ceFileRec, bool) {
	r := w.r
	rel := ceJoin(dir, stem+attachZipSuffix)
	var zf *attachZipFile
	// A zip is whole or it is not there, so the notes made for its parts while it was being made
	// (a kept part's "exactly as received", an empty part's) go with it when it is discarded: they
	// would name entries of a file that does not exist (E6Z-AO2, E6Z-AC2).
	noteMark, noteExtra := len(w.notes.list), w.notes.extra
	abort := func() {
		if zf != nil {
			zf.f.Close()
			r.fs.Remove(ceJoin(dir, zf.tmp))
			zf = nil
		}
		w.notes.list, w.notes.extra = w.notes.list[:noteMark], noteExtra
	}
	// failed ends the zip: nothing of it stays, one note says why and where every part is.
	failed := func(err error) (ceFileRec, bool) {
		abort()
		w.zipFailure(err)
		return ceFileRec{}, false
	}
	// stopped ends the zip too: a stop loses every attachment of the message, not the ones after it.
	stopped := func() (ceFileRec, bool) {
		abort()
		w.zipStopNote(len(atts))
		return ceFileRec{}, false
	}
	if len(atts) == 0 {
		return ceFileRec{}, false
	}
	if w.stopped() {
		return stopped()
	}
	// The message's line in the run record lists the zip, at about 230 bytes: nothing is placed
	// that the record could not then list (as for a loose file, E6-AO5).
	if sfree, serr := r.env.freeOnState(); serr == nil {
		need := attachRecordBytes(rel) + util.ExportSegmentMaxBytes + 1 + int64(len(r.plan.Messages)-int(r.done.Load()))*copyRecordLineBound + 4096
		if int64(sfree) < need {
			return failed(errAttachNoState)
		}
	}
	budget, err := r.derivedBudget()
	if err != nil {
		return failed(&ceWriteError{err})
	}
	if budget <= 0 {
		return failed(errAttachNoSpace)
	}
	if err := r.ensureDir(dir); err != nil {
		return failed(&ceWriteError{err})
	}
	placedParts := 0
	// used is the most the zip can hold so far: what a part would take stored, and what an entry and
	// its record in the directory add. The space left is judged against it, never against what
	// deflate happened to write so far, so a part is refused or kept as loose mode would, and the
	// zip is never cut by the space it was promised (the writer below still refuses what passes it).
	var used int64
	for i, a := range atts {
		if w.stopped() {
			return stopped()
		}
		name := partFilename(a, i)
		label := plainText(util.ExportFilename(name, i))
		if a.WriteTo == nil {
			w.notes.add(fmt.Sprintf("%q was not extracted: nothing of it could be kept; %s", label, w.holds))
			continue
		}
		// The part is read whole before it is written, as the archive writer reads it, so an empty
		// or failed part leaves no entry in the zip (an entry cannot be taken back).
		var buf bytes.Buffer
		// The part is admitted if its own cost fits: its bytes, deflate's overhead on them (len/512, as
		// used counts it) and its entry and record in the directory, on top of what is used and the
		// end of the zip. Judged on its bytes alone, a part that passed could still overflow the
		// zip's budget and cost every earlier entry (E6Z-AC1).
		room := (budget - used - attachZipSlack - attachZipEntryOverhead) * 512 / 513
		_, serr := guardStream(a.WriteTo)(&attachSpaceWriter{w: &buf, left: max(room, 0), stop: w.stopped, tick: r.watch.bump})
		serr = classifyStream(serr)
		if serr != nil && w.stopped() {
			serr = errAttachStopped
		}
		if errors.Is(serr, errAttachStopped) {
			return stopped()
		}
		if serr == nil && buf.Len() == 0 {
			serr = errAttachEmpty
		}
		if serr != nil {
			w.problem(label, serr)
			continue
		}
		if zf == nil {
			f, tmp, cerr := r.fs.CreateTemp(dir)
			if cerr != nil {
				return failed(&ceWriteError{cerr})
			}
			hw := &ceHashWriter{w: f, h: sha256.New(), watch: r.watch, total: &r.bytes}
			left := &attachSpaceWriter{w: hw, left: budget, stop: w.stopped}
			zf = &attachZipFile{f: f, tmp: tmp, hw: hw, zw: zip.NewWriter(left)}
		}
		entry := ceUniqueNameFrom(w.used, w.next, util.ExportFilename(name, i))
		ew, cerr := zf.zw.CreateHeader(attachZipHeader(entry, stamp))
		if cerr == nil {
			_, cerr = ew.Write(buf.Bytes())
		}
		if cerr != nil {
			if w.stopped() {
				return stopped()
			}
			return failed(classifyZipWrite(cerr))
		}
		atts[i].ZipName = entry // the readable page names the part as the zip does (E6Z-AI1)
		placedParts++
		used += int64(buf.Len()) + int64(buf.Len())/512 + attachZipEntryOverhead
		if a.GapKind != "" {
			w.gapNote(plainText(entry), a.GapKind)
		}
	}
	if zf == nil { // nothing could be kept: no zip, and the notes say why for each part
		return ceFileRec{}, false
	}
	if cerr := zf.zw.Close(); cerr != nil {
		if w.stopped() {
			return stopped()
		}
		return failed(classifyZipWrite(cerr))
	}
	serr := zf.f.Sync()
	if cerr := zf.f.Close(); serr == nil {
		serr = cerr
	}
	if serr != nil {
		r.fs.Remove(ceJoin(dir, zf.tmp))
		return failed(&ceWriteError{serr})
	}
	// A flush that was stuck (a hard mount) and returned after Cancel or Abandon: the zip is whole
	// but the caller has let go of the folder, and an abandoned run makes no further write (E6-AO3,
	// E6Z-AC3). The check cannot close the rename's own window, only the one the flush opens.
	if w.stopped() {
		return stopped()
	}
	placed, perr := r.placeFinalAs(w.ctx, dir, zf.tmp, stem, attachZipSuffix)
	if perr != nil {
		r.fs.Remove(ceJoin(dir, zf.tmp))
		return failed(&ceWriteError{perr})
	}
	rec := ceFileRec{Path: ceJoin(dir, placed), Role: roleDerivedAttachZip, SHA256: hex.EncodeToString(zf.hw.h.Sum(nil)), Size: zf.hw.n}
	w.line.Files = append(w.line.Files, rec)
	w.lineBytes += attachRecordBytes(rec.Path)
	w.placed = placedParts
	r.derivedReserve += derivedFileReserve(rec.Path)
	return rec, true
}

// classifyZipWrite sorts what writing the zip returned: no space, a failed write of the
// destination, or (anything else, which the zip writer does not return for a plain write) a
// failure to write.
func classifyZipWrite(err error) error {
	var we *ceWriteError
	if errors.Is(err, errAttachNoSpace) || errors.As(err, &we) {
		return err
	}
	return &ceWriteError{err}
}

// zipStopNote is the note for a zip the run was stopped in the making of: it is discarded whole
// (the temp is removed), so every one of the message's n attachments is not extracted.
func (w *attachWriter) zipStopNote(n int) {
	what := "its attachment was"
	if n != 1 {
		what = fmt.Sprintf("all %s attachments were", commas(n))
	}
	w.notes.add(fmt.Sprintf("the export was stopped while this message's attachments were being extracted, so no attachments zip was written and %s not extracted; %s; export this message again", what, w.holds))
}

// zipFailure is the note for a zip that could not be written at all: every attachment is still in
// the original.
func (w *attachWriter) zipFailure(err error) {
	var we *ceWriteError
	switch {
	case errors.Is(err, errAttachNoSpace):
		w.notes.add("the attachments zip was not written: no space was left for it on the destination; free some space there and export this message again; " + w.holds)
	case errors.Is(err, errAttachNoState):
		w.notes.add("the attachments zip was not written: there was no room left on this computer's disk for MailArchive's record of this export, which lists every file it makes; free some space there and export this message again; " + w.holds)
	case errors.As(err, &we):
		w.notes.add(fmt.Sprintf("the attachments zip was not written: it could not be written (%s); check the destination and export this message again; %s", leaveName(we.err), w.holds))
	default:
		w.notes.add(fmt.Sprintf("the attachments zip was not written: %s; %s", plainText(err.Error()), w.holds))
	}
}

// addAttachmentsZip makes the attachments zip of the parsed original (or says why it could not)
// beside the original and adds it and its notes to the message's line. It returns the zip's name
// when one was placed.
func (r *CopyRun) addAttachmentsZip(ctx context.Context, n int, dir, stem string, m *CopyMessage, msg *model.Message, why string, line *ceMsgLine) string {
	w := newAttachWriter(r, ctx, dir, stem, line, attachAuthorityEML)
	name := ""
	if why != "" {
		w.notes.add(fmt.Sprintf("the message could not be read, so its attachments were not extracted (%s); %s", why, attachAuthorityEML))
	} else if rec, ok := w.zipParts(dir, stem, attachZipStamp(m.Received), msg.Attachments); ok {
		name = path.Base(rec.Path) // the stem's own name, or a variant of it when another file stood there
	}
	line.Notes = append(line.Notes, w.notes.done()...)
	r.env.step(fmt.Sprintf("W5att:%d", n))
	return name
}

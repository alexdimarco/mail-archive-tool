package app

import (
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
	holds  string // what holds a part that was not extracted
}

func newAttachWriter(r *CopyRun, ctx context.Context, dir, stem string, line *ceMsgLine, holds string) *attachWriter {
	return &attachWriter{r: r, ctx: ctx, adir: ceJoin(dir, stem+attachDirSuffix), used: map[string]struct{}{}, line: line, holds: holds}
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
		w.notes.add(fmt.Sprintf("%q was not extracted: it could not be read from the message (%s); %s", label, pe.why, w.holds))
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

// parts extracts the parsed message's attachments, inline images included, in order.
func (w *attachWriter) parts(atts []model.Attachment) {
	for i, a := range atts {
		label := plainText(util.ExportFilename(a.Filename, i))
		if a.WriteTo == nil {
			w.notes.add(fmt.Sprintf("%q was not extracted: nothing of it could be kept; %s", label, w.holds))
			continue
		}
		rec, err := w.put(a.Filename, i, guardStream(a.WriteTo))
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

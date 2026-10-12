package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"runtime"
	"slices"
	"strings"
)

// A source whose message is files, not one stream (design §4.6, slice E4): the archive
// has, for a message, its original .eml, or else its archived page and its attachments
// zip, each already on disk and each held to the fixity the plan recorded. The engine
// writes every file of a message into temp files first and gives them their final names
// only when all of them were copied and checked, so a message with one bad file leaves
// none of its files behind and is never emitted mixed.

// CopyFilesSource is a CopySource that yields a message as files.
type CopyFilesSource interface {
	CopySource
	// Files opens m's files, after the source's gate. An error is a per-message failure
	// or, when the source itself is gone, an error Gone tells from one.
	Files(ctx context.Context, m *CopyMessage) (*CopyFiles, error)
	// Gone reports whether the source itself, not one of its files, is gone: the archive's
	// folder or manifest. Asked after a missing-file or read error, so that a vanished
	// archive stops the run once (R-o2) and does not list thousands of false failures.
	Gone() (why string, gone bool)
	// Changed reports whether the source's own record for m no longer says what the plan
	// recorded (a concurrent capture rewrote it, or it was removed).
	Changed(m *CopyMessage) bool
	// Capturing reports whether something is writing to the source right now (a capture holds
	// the archive's lock): the source's record of a message is then behind its files, and a
	// file that does not match it, or is missing, may be a message being rewritten and not one
	// that is damaged or removed.
	Capturing() bool
}

// CopyFiles are the files of one message, ready to copy.
type CopyFiles struct {
	Status  string   // statusExported, or statusWithoutOrig when the source kept no original
	Notes   []string // the entry's notes
	Parts   []CopyFilePart
	release func()
}

// CopyFilePart is one file of a message.
type CopyFilePart struct {
	Role   string // roleOriginal, roleArchivedPage, roleArchivedZip or roleQuarantinedPage
	Suffix string // appended to the message's stem: ".eml", ".html", ".archived-page.txt", "-attachments.zip"
	Size   int64  // the declared size
	// Copy streams the part into w, refusing one above maxBytes (zero or less: no limit)
	// and one whose bytes do not match the fixity the plan recorded.
	Copy func(w io.Writer, maxBytes int64) (int64, error)
}

func (f *CopyFiles) close() {
	if f != nil && f.release != nil {
		f.release()
	}
}

// copyFileCount is how many files a message puts in its directory: the namer counts
// files, not messages, against the 1,000 a directory holds (XC12), and the preview and
// the run must count the same.
func copyFileCount(m *CopyMessage) int {
	if m.NoOrig != "" && m.Zip {
		return 2
	}
	return 1
}

// copyLongestSuffix is the longest name a message's files take after the stem: the path
// budget is read against it.
func copyLongestSuffix(m *CopyMessage) string {
	switch {
	case m.NoOrig == "":
		return ".eml"
	case m.Zip:
		return "-attachments.zip"
	case m.Quarantine:
		return archivedPageTxtSuffix
	}
	return ".html"
}

// copyLongestSuffixFor is copyLongestSuffix with a readable copy beside an original: its
// .html is a byte longer than the .eml it sits by.
func copyLongestSuffixFor(m *CopyMessage, opts CopyOptions) string {
	if opts.ReadableHTML && m.NoOrig == "" {
		return ".html"
	}
	return copyLongestSuffix(m)
}

// copyFirstSuffix is the first file's name after the stem.
func copyFirstSuffix(m *CopyMessage) string {
	switch {
	case m.NoOrig == "":
		return ".eml"
	case m.Quarantine:
		return archivedPageTxtSuffix
	}
	return ".html"
}

// exportFiles copies one message of a source that yields files. It returns the progress
// line to record (nil when the message was not started), a run-fatal stop, or cancelled.
func (r *CopyRun) exportFiles(walk context.Context, n int, m *CopyMessage, src CopyFilesSource) (line *ceMsgLine, stop *CopyStop, cancelled bool) {
	e := r.env
	failure := func(class, reason string) *ceMsgLine {
		return &ceMsgLine{T: "msg", N: n, Status: statusFailed, Class: class, Reason: reason}
	}
	if walk.Err() != nil {
		return nil, nil, true
	}
	if s := r.watch.stoppedFatal(); s != nil {
		return failure(classRetryable, stopLineReason(s)), r.complete(s, int(r.done.Load()), s.Message), false
	}
	budget, berr := r.budget()
	if berr != nil || budget <= 0 || r.exceedsBudget(m, budget) {
		// Before the message's first byte: the disk stopped the run, not the message, so it
		// stays first in not_attempted (RR2b-C6).
		why := "no space left"
		if berr != nil {
			why = leaveName(berr)
		}
		return nil, r.complete(&CopyStop{RID: "R-g", Class: stopClassDestination}, int(r.done.Load()), why), false
	}

	mctx, mcancel := context.WithCancelCause(walk)
	r.watch.setMessage(mcancel)
	defer func() {
		mcancel(nil)
		r.watch.setMessage(nil)
	}()

	files, ferr := src.Files(mctx, m)
	defer files.close()
	var placed []string
	if ferr == nil {
		line, placed, ferr = r.writeFiles(mctx, n, m, files, min(e.cap(), budget))
	}
	if ferr == nil {
		r.watch.bump()
		r.consecWrite, r.consecServer = 0, 0
		e.step(fmt.Sprintf("W5:%d", n))
		return line, nil, false
	}
	for _, p := range placed { // nothing of a failed message stays in the export
		r.fs.Remove(p)
	}

	v := r.classifyFiles(ferr, mctx, walk, src, m)
	switch v.kind {
	case verdictCancelled:
		return nil, nil, true
	case verdictFatal:
		s := r.complete(v.stop, int(r.done.Load()), v.stop.Message)
		return failure(classRetryable, stopLineReason(s)), s, false
	}
	if _, isWrite := asWriteErr(ferr); isWrite {
		if s := r.writeFailed(ferr, ""); s != nil {
			return failure(classRetryable, v.reason), s, false
		}
	} else {
		r.consecWrite = 0
	}
	return failure(v.class, v.reason), nil, false
}

// writeFiles writes every part of m into temp files, then gives them their names, and
// returns the progress line. On an error it returns the files already given names (the
// caller removes them) after removing every temp itself.
func (r *CopyRun) writeFiles(ctx context.Context, n int, m *CopyMessage, files *CopyFiles, maxBytes int64) (*ceMsgLine, []string, error) {
	e := r.env
	pl := r.namer.place(m, copyFileCountFor(m, r.plan.Options))
	if err := r.ensureDir(pl.Dir); err != nil {
		return nil, nil, &ceWriteError{err}
	}
	type temp struct {
		name string
		part CopyFilePart
		hw   *ceHashWriter
		got  int64
	}
	var temps []*temp
	var placed []string
	cleanup := func() {
		for _, t := range temps {
			r.fs.Remove(ceJoin(pl.Dir, t.name)) // a placed temp is already gone: the removal is a no-op
		}
	}
	remaining := maxBytes
	for _, p := range files.Parts {
		f, name, err := r.fs.CreateTemp(pl.Dir)
		if err != nil {
			cleanup()
			return nil, nil, &ceWriteError{err}
		}
		t := &temp{name: name, part: p, hw: &ceHashWriter{w: f, h: sha256.New(), watch: r.watch, total: &r.bytes}}
		temps = append(temps, t)
		t.got, err = p.Copy(t.hw, remaining)
		if err == nil {
			e.step(fmt.Sprintf("W5pre:%d", n))
			if serr := f.Sync(); serr != nil {
				err = &ceWriteError{serr}
			}
		}
		if cerr := f.Close(); err == nil && cerr != nil {
			err = &ceWriteError{cerr}
		}
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		remaining -= t.got
	}
	e.step(fmt.Sprintf("W5tmp:%d", n))
	var recs []ceFileRec
	for _, t := range temps {
		name, err := r.placeFinalAs(ctx, pl.Dir, t.name, pl.Stem, t.part.Suffix)
		if err != nil {
			cleanup()
			return nil, placed, &ceWriteError{err}
		}
		placed = append(placed, ceJoin(pl.Dir, name))
		recs = append(recs, ceFileRec{Path: pl.Dir + "/" + name, Role: t.part.Role, SHA256: hex.EncodeToString(t.hw.h.Sum(nil)), Size: t.got})
	}
	line := &ceMsgLine{T: "msg", N: n, Status: files.Status, Files: recs, Notes: slices.Clone(files.Notes)}
	if (r.plan.Options.ReadableHTML || r.plan.Options.Attachments) && m.NoOrig == "" && len(recs) > 0 && recs[0].Role == roleOriginal {
		// the derived files of the original just placed (slices E5, E6): an original that is not
		// what was written fails the message, and the caller removes every file it placed
		if err := r.addDerived(ctx, n, m, pl.Dir, path.Base(recs[0].Path), recs[0], line); err != nil {
			return nil, placed, err
		}
	}
	if r.plan.Options.Attachments && !r.plan.Options.AttachmentsAsZip && m.NoOrig != "" { // the archive kept no original: the zip's entries are extracted (slice E6); in zip mode the copied zip is the attachments zip (MA-534)
		for _, rec := range recs {
			if rec.Role == roleArchivedZip {
				if err := r.addZipEntries(ctx, n, pl.Dir, pl.Stem, rec, line); err != nil {
					return nil, placed, err
				}
			}
		}
	}
	return line, nil, nil
}

const (
	// checkIntegrityButton is the label of Desktop's Maintenance control that checks the archive's files
	// against their checksums (the command-line tool calls it verify): the owner's word, not the verb.
	checkIntegrityButton = "Maintenance → Check integrity"
	// plainFolderRemedy is what to do about a link, or a file with several hard links, in the archive.
	plainFolderRemedy = "copy the archive into a plain folder that holds real files, not links, and export from that folder instead"
)

// damagedRemedy is the next step for a file that does not match its checksum, or a record that does not
// hold together: the owner's own archive can be checked and restored from a backup; another archive folder
// is read-only and cannot be checked from here (friction F17).
func damagedRemedy(other bool) string {
	if other {
		return "this folder is read-only, so ask whoever gave you the archive for a fresh copy of the file, then export again"
	}
	return "restore the file from a backup of the archive, or run " + checkIntegrityButton + " to see what else is affected, then export again"
}

const (
	// reasonArchiveRemoved is why a message whose file is not in the archive is listed as failed for good.
	reasonArchiveRemoved = "removed from the archive after you selected it"
	// reasonArchiveWentAway replaces it for a message that failed in the moments before the run saw the
	// archive itself was gone (a share failing file by file, a folder deleted under the run): the file
	// may well not have been removed (friction F6).
	reasonArchiveWentAway = "its file could not be found because the archive went away while this export was running; a retry looks again once the archive is back"
)

// classifyFiles reads one failed message of a source that yields files (design §4.6,
// §4.13). A file that is not there, or cannot be read, makes the source probe itself: if
// the archive's folder or manifest is gone the run stops run-fatal with R-o2 and does not
// list thousands of false "damaged" messages.
func (r *CopyRun) classifyFiles(err error, mctx, walk context.Context, src CopyFilesSource, m *CopyMessage) verdict {
	if walk.Err() != nil {
		return verdict{kind: verdictCancelled}
	}
	var cause *ceBreakerCause
	if errors.As(context.Cause(mctx), &cause) {
		return cause.verdict
	}
	if errors.Is(err, errOriginalChanged) { // the read-back for the readable copy (slice E5)
		return verdict{kind: verdictList, class: classRetryable, reason: reasonOriginalChanged}
	}
	var we *ceWriteError
	if errors.As(err, &we) {
		if destinationFatal(runtime.GOOS, we.err) {
			return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-g", Class: stopClassDestination, Message: we.err.Error()}}
		}
		return verdict{kind: verdictList, class: classRetryable, reason: "a file could not be written to the destination (" + leaveName(we.err) + ")"}
	}
	var af *ArchiveFileError
	if !errors.As(err, &af) {
		return verdict{kind: verdictList, class: classRetryable, reason: "the archive's files could not be read (" + readName(err) + ")"}
	}
	other := r.plan != nil && r.plan.Source.Other // another archive folder is read-only: nothing here can check or repair it
	switch af.Kind {
	case AFMissing, AFRead:
		if why, gone := src.Gone(); gone {
			return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-o2", Class: stopClassArchive, Message: why}}
		}
		if af.Kind == AFMissing {
			if src.Capturing() { // a re-capture removes the zip before it writes the page again (E4-AI2)
				return verdict{kind: verdictList, class: classRetryable, reason: "changed in the archive during export (a capture is rewriting this message); preview again for the new copy"}
			}
			if other {
				return verdict{kind: verdictList, class: classTerminal, reason: reasonArchiveRemoved + " — if that was not on purpose, ask whoever gave you the archive for a complete copy"}
			}
			return verdict{kind: verdictList, class: classTerminal, reason: reasonArchiveRemoved + " — if that was not on purpose, restore the file from a backup of the archive, then export again"}
		}
		return verdict{kind: verdictList, class: classRetryable, reason: "the archive's file could not be read (" + readName(af.Err) + ")"}
	case AFSymlink:
		return verdict{kind: verdictList, class: classTerminal, reason: "a link stands in the archive where this file should be, so it was not followed — " + plainFolderRemedy}
	case AFIrregular:
		return verdict{kind: verdictList, class: classTerminal, reason: "the archive's file is not a regular file — " + plainFolderRemedy}
	case AFLinks:
		return verdict{kind: verdictList, class: classTerminal, reason: "the archive's file has more than one hard link, so MailArchive can't be sure it is the archive's own — " + plainFolderRemedy}
	case AFPath:
		return verdict{kind: verdictList, class: classTerminal, reason: "the archive's record names a path that is not inside the archive — the record is damaged: " + damagedRemedy(other)}
	case AFChanged:
		// The manifest is saved at a capture's checkpoints, after the files are written, so while a
		// capture runs a rewritten file is ahead of the checksum the manifest still has (E4-AI2).
		if src.Changed(m) || src.Capturing() {
			return verdict{kind: verdictList, class: classRetryable, reason: "changed in the archive during export (a capture rewrote this message); preview again for the new copy"}
		}
		return verdict{kind: verdictList, class: classTerminal, reason: "the archive's file doesn't match the checksum the archive recorded for it (modified or damaged): " + damagedRemedy(other)}
	case AFTooLarge:
		if af.Size > r.env.cap() || af.Limit >= r.env.cap() {
			return verdict{kind: verdictList, class: classTerminal, reason: fmt.Sprintf("larger than this version of MailArchive exports (%d MiB)", r.env.cap()>>20)}
		}
		return verdict{kind: verdictFatal, stop: &CopyStop{RID: "R-g", Class: stopClassDestination, Message: "no space left"}}
	}
	return verdict{kind: verdictList, class: classRetryable, reason: strings.TrimSpace("the archive's file could not be copied")}
}

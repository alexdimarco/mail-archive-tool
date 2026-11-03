package archdelete

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/util"
)

// The delete marker's write and its removal (docs/design-archive-delete.md §15.2, as the owner's
// ruling Q1 of 2 November 2026 places it, "When files start going"; §15.10 slice 3; MA-1046,
// MA-1047). The delete writes .mailarchive-deleting.json just before step 6's first removal —
// after steps 2 to 5, still under the archive lock, through step 6's own root on the folder the
// plan names — and removes it right after the anchor, as the last of the archive's files. A
// delete stopped before step 6 leaves none: the archive is whole, and this computer's log still
// refuses here (G2).

// markerCreate creates the marker's name in root, owner-only: O_EXCL, so it never follows or
// reuses a name already there, a link included (§15.2 point 2). A test seam for a name that
// cannot be created (a full drive, a folder that may not be written).
var markerCreate = func(root *os.Root) (*os.File, error) {
	return root.OpenFile(delmark.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

// markerWrite writes the marker's bytes into f and flushes them: a test seam for bytes that
// cannot be written once the name is there.
var markerWrite = func(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// createMarkerIn creates the marker in root and writes data into it in place, not as a temp
// file renamed over the name (§15.2 point 3): a nearly full drive where the name can be made but
// no bytes written still gets a marker (presence is the signal; an empty file is a marker), and a
// crash leaves no temp file. err is only for a name that could not be created; bytes that could
// not be written or flushed leave the file there and the delete goes on (wrote is false).
func createMarkerIn(root *os.Root, data []byte) (wrote bool, err error) {
	f, err := markerCreate(root)
	if err != nil {
		return false, err
	}
	werr := markerWrite(f, data)
	cerr := f.Close()
	return werr == nil && cerr == nil, nil
}

// markFolder is §15.2's write, just before step 6's first removal (Q1). present: a marker is in
// the folder for step 6 to remove last; stopped: the delete stopped here, before any removal, in
// words that list the steps done — after steps 2 to 5 it is "not fully deleted", never "nothing
// changed" (§15.2's Q1 note).
//   - None there: created with this delete's first run and its start, unless the anchor is gone
//     (the no-anchor re-run never creates one: DM-9); then the `marked` line (DM-27).
//   - A marker there (an earlier run's, this computer's or another's): kept as it is — never
//     rewritten or filled in (DM-26: opening an existing name to write would follow a planted
//     hard link) — with the `marked` line when it is this delete's own.
//   - Anything else at the name (a link, a folder, a device): the delete stops; nothing is
//     followed or opened.
func (e *run) markFolder() (present, stopped bool) {
	p := e.plan
	fi, err := e.root.Lstat(delmark.Name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if p.Rerun == RerunNoAnchor {
			return false, false
		}
		first, began := e.firstRun()
		if _, cerr := createMarkerIn(e.root, delmark.Content(first, began)); cerr != nil {
			e.stop(e.markerNotWritten(cerr))
			return false, true
		}
		util.SyncDir(p.Folder) // best effort: the name survives a power cut
		e.logMarked(first)
		return true, false
	case err != nil:
		e.stop(e.markerNotWritten(err))
		return false, true
	}
	l := &lister{root: e.root, folder: p.Folder, goos: runtime.GOOS}
	if link, lerr := l.linkLike(delmark.Name, fi); lerr != nil || link || !l.fileLike(fi) {
		e.stop(fmt.Sprintf("This archive's folder holds %s, which MailArchive did not write (it is a link, a folder or a device), so none of the archive's files were deleted. Steps done: %s. Remove it by hand, then click Delete again.",
			delmark.Name, e.stepsDone()))
		return false, true
	}
	if first, _ := e.firstRun(); delmark.ReadIn(e.root, p.Folder).Run == first {
		e.logMarked(first)
	}
	return true, false
}

// firstRun is the run id and start of the first run of the delete this run belongs to, as the
// marker records them (§15.1): the delete it finishes (D1), else this run.
func (e *run) firstRun() (string, time.Time) {
	if f := e.plan.Finishes; f != nil && f.FirstRun != "" {
		if f.FirstStarted.IsZero() {
			return f.FirstRun, e.started
		}
		return f.FirstRun, f.FirstStarted
	}
	return e.id, e.started
}

// logMarked appends DM-27's durable `marked` line: this run, and the first run the folder's marker
// names. One that cannot be written is said, as step 7's folder line is, and the delete goes on:
// without it a later plan reads the log as it did before the marker (§15.4's DM-27 note).
func (e *run) logMarked(first string) {
	p := e.plan
	if err := appendLog(e.log, logLine{At: e.stamp(), Event: evMarked, Run: e.id, Folder: p.Folder, Row: e.row, FirstRun: first}); err != nil {
		e.notes = append(e.notes, fmt.Sprintf("This step could not be added to deletions.log (%s).", cleanErr(err)))
	}
}

// markerNotWritten is the stop for a marker name that could not be created (§15.2's Q1 note, with
// DM-7's full-drive words; lead's words, owner-delegated 2026-11-02): this person began the delete,
// so "again" is right.
func (e *run) markerNotWritten(err error) string {
	if isDiskFull(err) {
		return fmt.Sprintf("The drive is full, so none of the archive's files were deleted. Steps done: %s. Free a little space, then click Delete again.", e.stepsDone())
	}
	return fmt.Sprintf("MailArchive could not write a note into the archive's folder that a delete is under way (%s), so none of the archive's files were deleted. Steps done: %s. Check that the folder can be written, then click Delete again.",
		cleanErr(err), e.stepsDone())
}

// stepsDone are the steps done, as "Steps done" words them.
func (e *run) stepsDone() string {
	if len(e.done) == 0 {
		return "none"
	}
	return strings.Join(e.done, ", ")
}

// removeMarker is step 6's last removal (§15.2): right after the anchor, under the lock, by its
// exact name through step 6's root, a regular file only (never a link, never RemoveAll), retried
// briefly as step 7 retries the lock file (Windows' indexer or antivirus). nil when it is gone.
func (e *run) removeMarker() error {
	l := &lister{root: e.root, folder: e.plan.Folder, goos: runtime.GOOS}
	var err error
	for i := 0; i < 10; i++ {
		fi, lerr := e.root.Lstat(delmark.Name)
		if errors.Is(lerr, fs.ErrNotExist) {
			return nil
		}
		if lerr != nil {
			return lerr
		}
		if link, kerr := l.linkLike(delmark.Name, fi); kerr != nil || link || !l.fileLike(fi) {
			return fmt.Errorf("%s %w", delmark.Name, errLinkNow)
		}
		err = removeFileFn(e.root, delmark.Name)
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		time.Sleep(retryPause)
	}
	return err
}

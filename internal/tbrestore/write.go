package tbrestore

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"strings"
	"syscall"
	"time"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/interchange"
)

// WriteInput is one restore's write (design §5.4 as folded by foldcheck, foldcheck2 and
// foldcheck3). The caller holds the archive lock, the MailArchive restore lock and
// Thunderbird's profile lock, has re-checked the plan (§5.3) and settled every earlier
// restore into the profile (Settle) before it calls Write.
type WriteInput struct {
	ID         string
	ProfileDir string
	Local      LocalFolders
	Archive    string
	Encrypted  bool
	Layout     *Layout
	// Top is the free parent name the plan chose ("Restored — X" or "… (n)").
	Top string
	// Messages emits, in order, the original bytes of each message of a folder (an index
	// into Layout.Folders). A message the archive cannot give faithfully is the caller's
	// to skip and count; emit's error (a cancel, a write failure) is returned as is.
	Messages func(folder int, emit func(raw []byte) error) error
	// Progress is told the count of messages written so far (optional).
	Progress func(done int)
	// Checking is told once when the post-write check begins, so a pause while every
	// message is read back can be explained (optional; R6-10).
	Checking func()
	// MustBePrivate: every folder and file the restore makes must be shown readable by
	// this account alone (FS.Private), or the write stops with the one cleanup (an
	// encrypted archive whose plan did not ask a second time about other readers; R6-01).
	// Otherwise each is made owner-only where it can be and a failure is passed over.
	MustBePrivate bool
	Log           *Log
}

// Outcome is how a write ended.
type Outcome string

const (
	OutDone      Outcome = "done"      // visible and checked; AfterErr may name a late error
	OutStopped   Outcome = "stopped"   // Cancel or Shut down
	OutFailed    Outcome = "failed"    // an error while writing, or the parent file not made
	OutCheckFail Outcome = "checkfail" // the post-write check found a file missing or changed
	OutTaken     Outcome = "taken"     // the parent file's name was taken at the commit
	OutCommitErr Outcome = "commiterr" // the parent file made, but not known to be saved
)

// WriteResult is what a write did, with the words to show.
type WriteResult struct {
	Outcome Outcome
	Top     string
	// Restored is how many messages were written and checked; Written the count per
	// folder (an index into Layout.Folders).
	Restored int
	Written  []int
	// Clean is the cleanup's result when the write did not finish.
	Clean *CleanResult
	// Err is why the write did not finish; AfterErr an error after the folder became
	// visible and complete ([5], [6], or the parent's fsync with its re-check passing),
	// named in the result but never a reason to remove anything.
	Err      error
	AfterErr error
	Words    string
}

// errStopped is a write ended by its context (Cancel, Shut down).
var errStopped = errors.New("stopped")

// msgRecord is what the post-write check compares: each message's SHA-256 and length
// as handed to the writer, and the line ending the writer added to a final line that
// had none.
type msgRecord struct {
	sum   [32]byte
	n     int
	added int
}

// Write runs the sequence (numbers in brackets are RT-4's fsync points):
//  1. [1] the "started" line;
//  2. Mkdir the container exclusively (a taken name: a fsynced "name" line, then the
//     next " (n)");
//  3. the marker, its line, fsynced;
//  4. the made-list, fsynced, and the container fsynced;
//  5. per folder, parents first: each entry appended to the made-list and fsynced
//     BEFORE it is created exclusively; the messages written straight into the file
//     under its final name; [2] the file fsynced;
//  6. [3] every folder made fsynced;
//  7. the post-write check: every file re-read by its final name and re-parsed;
//  8. the "checked" line;
//  9. [4] the commit: the empty parent file, exclusively, fsynced;
//  10. [5] Local Folders fsynced, [6] "ended".
//
// Anything that stops it before step 9 goes to the one cleanup. Nothing after the
// parent file's create removes anything.
func Write(ctx context.Context, f FS, in WriteInput) *WriteResult {
	w := &writer{ctx: ctx, f: f, in: in, res: &WriteResult{Top: in.Top, Written: make([]int, len(in.Layout.Folders))}}
	return w.run()
}

type writer struct {
	ctx      context.Context
	f        FS
	in       WriteInput
	res      *WriteResult
	list     wfile
	dirs     []string
	records  [][]msgRecord // per folder
	sizes    []int64       // per folder: bytes written
	done     int
	started  bool
	madeTree bool // the container exists (made by this restore)
	// notMine are the container-relative names whose exclusive create failed because
	// something already stood there: never this restore's, so its cleanup never
	// removes them (ADV-03).
	notMine map[string]bool
}

// created notes a create of the container-relative name rel that failed because the
// name was already there, and returns err.
func (w *writer) created(rel string, err error) error {
	if err != nil && errors.Is(err, fs.ErrExist) {
		if w.notMine == nil {
			w.notMine = map[string]bool{}
		}
		w.notMine[rel] = true
	}
	return err
}

func (w *writer) logLine(ev string, set func(*LogLine)) error {
	ln := LogLine{Event: ev, ID: w.in.ID}
	if set != nil {
		set(&ln)
	}
	return w.in.Log.Append(ln)
}

func (w *writer) run() *WriteResult {
	in := w.in
	restored := in.Layout.Restored()
	msgs, bytesN := 0, int64(0)
	for _, i := range restored {
		msgs += in.Layout.Folders[i].Messages
		bytesN += in.Layout.Folders[i].Bytes
	}
	id := in.Local.Identity
	if err := w.logLine(EvStarted, func(l *LogLine) {
		l.Profile, l.ProfileDir = ProfileKeyOf(in.ProfileDir), in.ProfileDir
		l.Local, l.LocalID = in.Local.Dir, &id
		l.Archive, l.Name, l.Encrypted = in.Archive, in.Top, in.Encrypted
		l.Messages, l.Folders, l.Bytes = intp(msgs), intp(len(restored)), int64p(bytesN)
	}); err != nil {
		return w.fail(OutFailed, err) // nothing was created
	}
	w.started = true
	// 2. the container, exclusively. Settled 2026-11-07 [R6-16]: the parent file's name
	// is looked at too, before the Mkdir and right after it, so a name another program
	// took since the re-check moves the restore to the next " (n)" before anything is
	// written, never to W-TAKEN after the whole write. (A name taken after this is still
	// met at the commit, W-TAKEN.)
	top := in.Top
	for {
		taken := entryKind(w.f, top) != ""
		var err error
		if !taken {
			if err = w.f.Mkdir(top + ".sbd"); err == nil {
				if entryKind(w.f, top) == "" {
					break
				}
				// The parent name appeared between the look and the Mkdir: the empty
				// container just made is this restore's, removed as seen, and the next
				// name tried.
				if rerr := w.removeEmptyContainer(top + ".sbd"); rerr != nil {
					w.res.Top, w.madeTree = top, true
					return w.stop(rerr)
				}
				taken = true
			}
		}
		if !taken && !errors.Is(err, fs.ErrExist) {
			return w.fail(OutFailed, err)
		}
		next, ok := nextTop(in.Layout.Top, top)
		if !ok {
			return w.fail(OutFailed, refuse("W-NAMES", fill(wNames, "NAME", strings.TrimPrefix(in.Layout.Top, TopPrefix)), nil))
		}
		for entryKind(w.f, next) != "" {
			if next, ok = nextTop(in.Layout.Top, next); !ok {
				return w.fail(OutFailed, refuse("W-NAMES", fill(wNames, "NAME", strings.TrimPrefix(in.Layout.Top, TopPrefix)), nil))
			}
		}
		if err := w.logLine(EvName, func(l *LogLine) { l.Name = next }); err != nil {
			return w.fail(OutFailed, err)
		}
		top = next
	}
	w.res.Top, w.madeTree = top, true
	container := top + ".sbd"
	if err := w.private(container, true); err != nil {
		return w.stop(err)
	}
	// 3. the marker.
	if err := w.writeSmall(container, MarkerName, markerLine(in.ID, ProfileKeyOf(in.ProfileDir), time.Now())); err != nil {
		return w.stop(err)
	}
	// 4. the made-list, on disk before any entry is made.
	list, err := w.f.Create(container + "/" + MadeListName)
	if err != nil {
		return w.stop(w.created(MadeListName, err))
	}
	w.list = list
	defer func() {
		if w.list != nil {
			w.list.Close()
		}
	}()
	if err := w.private(container+"/"+MadeListName, false); err != nil {
		return w.stop(err)
	}
	if err := list.Sync(); err != nil {
		return w.stop(err)
	}
	w.dirs = append(w.dirs, container)
	if err := w.f.SyncDir(container); err != nil {
		return w.stop(err)
	}
	// 5. the folders.
	w.records = make([][]msgRecord, len(in.Layout.Folders))
	w.sizes = make([]int64, len(in.Layout.Folders))
	for _, i := range restored {
		if err := w.ctx.Err(); err != nil {
			return w.stop(errStopped)
		}
		fo := in.Layout.Folders[i]
		if fo.HasChildren {
			if err := w.made("d", fo.Dir()); err != nil {
				return w.stop(err)
			}
			if err := w.f.Mkdir(container + "/" + fo.Dir()); err != nil {
				return w.stop(w.created(fo.Dir(), err))
			}
			w.dirs = append(w.dirs, container+"/"+fo.Dir())
			if err := w.private(container+"/"+fo.Dir(), true); err != nil {
				return w.stop(err)
			}
		}
		if err := w.writeFolder(container, i, fo); err != nil {
			return w.stop(err)
		}
	}
	if err := w.list.Close(); err != nil {
		w.list = nil
		return w.stop(err)
	}
	w.list = nil
	// 6. [3] every folder made.
	for _, d := range w.dirs {
		if err := w.f.SyncDir(d); err != nil {
			return w.stop(err)
		}
	}
	// 7. the post-write check. Settled 2026-11-07 [R6-10]: Cancel, Ctrl-C and Shut down
	// are heard here too — between files and between messages of the check, and once more
	// before the commit — and are a stop with the one cleanup, as before step 8; Checking
	// is told when the check begins, so the pause is explained.
	if w.in.Checking != nil {
		w.in.Checking()
	}
	if file, folder, err := w.check(container, restored); err != nil {
		if errors.Is(err, errStopped) {
			return w.stop(errStopped)
		}
		return w.checkFailed(file, folder, err)
	}
	if w.ctx.Err() != nil {
		return w.stop(errStopped)
	}
	// 8. "checked".
	if err := w.logLine(EvChecked, nil); err != nil {
		return w.stop(err)
	}
	return w.commit(top)
}

// nextTop is the next " (n)" of the layout's top name after cur; ok false past 99.
func nextTop(base, cur string) (string, bool) {
	for n := 1; n <= 99; n++ {
		if withNumber(base, n) == cur {
			if n == 99 {
				return "", false
			}
			return withNumber(base, n+1), true
		}
	}
	return "", false
}

// removeEmptyContainer removes the empty container this restore just made, as the
// cleanup removes: only the very folder seen, never through a link (R6-16).
func (w *writer) removeEmptyContainer(name string) error {
	fi, err := w.f.Lstat(name)
	if err != nil {
		return err
	}
	return w.f.RemoveSeen(name, map[string]fs.FileInfo{norm.NFC.String(name): fi})
}

// errNotPrivateHere wraps an entry MustBePrivate needed owner-only and could not have.
type errNotPrivateHere struct{ err error }

func (e *errNotPrivateHere) Error() string { return "not readable only by you: " + e.err.Error() }
func (e *errNotPrivateHere) Unwrap() error { return e.err }

// private makes one entry the restore created owner-only (FS.Private); a failure stops
// the write only when MustBePrivate (R6-01).
func (w *writer) private(name string, dir bool) error {
	if err := w.f.Private(name, dir); err != nil && w.in.MustBePrivate {
		return &errNotPrivateHere{err: err}
	}
	return nil
}

// made appends one made-list line and fsyncs it, before the entry is created (M1).
func (w *writer) made(kind, path string) error {
	if _, err := w.list.Write(madeLine(kind, path)); err != nil {
		return err
	}
	return w.list.Sync()
}

func (w *writer) writeSmall(container, rel string, data []byte) error {
	f, err := w.f.Create(container + "/" + rel)
	if err != nil {
		return w.created(rel, err)
	}
	if err := w.private(container+"/"+rel, false); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// countWriter counts what passes through it.
type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// writeFolder makes the folder's file (listed first) and writes its messages into it.
func (w *writer) writeFolder(container string, i int, fo Folder) error {
	name := container + "/" + fo.File()
	if err := w.made("f", fo.File()); err != nil {
		return err
	}
	file, err := w.f.Create(name)
	if err != nil {
		return w.created(fo.File(), err)
	}
	if err := w.private(name, false); err != nil {
		file.Close()
		return err
	}
	bw := bufio.NewWriterSize(file, 256<<10)
	cw := &countWriter{w: bw}
	emitErr := error(nil)
	if fo.Messages > 0 && w.in.Messages != nil {
		emitErr = w.in.Messages(i, func(raw []byte) error {
			if w.ctx.Err() != nil {
				return errStopped
			}
			added, err := interchange.WriteMessageMatched(cw, raw)
			if err != nil {
				return err
			}
			w.records[i] = append(w.records[i], msgRecord{sum: sha256.Sum256(raw), n: len(raw), added: added})
			w.done++
			if w.in.Progress != nil {
				w.in.Progress(w.done)
			}
			return nil
		})
	}
	if err := bw.Flush(); err != nil && emitErr == nil {
		emitErr = err
	}
	if emitErr == nil {
		emitErr = file.Sync() // [2]
	}
	if cerr := file.Close(); cerr != nil && emitErr == nil {
		emitErr = cerr
	}
	w.sizes[i] = cw.n
	w.res.Written[i] = len(w.records[i])
	return emitErr
}

// check is step 7: each file, reopened by its final name through the root (a sharing
// violation retried, RT-24), is the size written and re-parses to exactly the messages
// recorded, in order, each the original bytes (the added line ending taken off).
func (w *writer) check(container string, restored []int) (file, folder string, err error) {
	for _, i := range restored {
		if w.ctx.Err() != nil {
			return "", "", errStopped
		}
		fo := w.in.Layout.Folders[i]
		name := container + "/" + fo.File()
		var k int
		err := withRetry(func() error {
			r, err := w.f.Open(name)
			if err != nil {
				return err
			}
			defer r.Close()
			fi, err := w.f.Lstat(name)
			if err != nil {
				return err
			}
			if fi.Size() != w.sizes[i] {
				return fmt.Errorf("%s is %d bytes, %d were written", name, fi.Size(), w.sizes[i])
			}
			k, err = checkMessages(w.ctx, r, w.records[i])
			return err
		})
		if errors.Is(err, errStopped) {
			return "", "", errStopped
		}
		if err != nil {
			return name, strings.Join(fo.Names, "/"), err
		}
		w.res.Restored += k
	}
	return "", "", nil
}

// checkMessages re-parses one mbox (streamed) and compares each message, in order, with
// what was written: its length and SHA-256, the line ending the writer added to a final
// line that had none taken off (and checked to be that line ending). It returns how
// many messages matched.
func checkMessages(ctx context.Context, r io.Reader, want []msgRecord) (int, error) {
	k := 0
	var h hash.Hash
	var n int
	var tail []byte // the last two bytes, held back: they may be an added line ending
	err := interchange.ScanMboxrd(bufio.NewReaderSize(r, 256<<10), interchange.MboxrdHandler{
		Start: func() error {
			if ctx.Err() != nil {
				return errStopped
			}
			if k >= len(want) {
				return errors.New("a message more than was written")
			}
			h, n, tail = sha256.New(), 0, tail[:0]
			return nil
		},
		Data: func(p []byte) error {
			all := append(tail, p...)
			if len(all) > 2 {
				h.Write(all[:len(all)-2])
				n += len(all) - 2
				all = all[len(all)-2:]
			}
			tail = append(tail[:0], all...)
			return nil
		},
		End: func() error {
			m := want[k]
			last := tail
			if m.added > 0 {
				if len(last) < m.added || (m.added == 1 && last[len(last)-1] != '\n') || (m.added == 2 && !bytes.Equal(last[len(last)-2:], []byte("\r\n"))) {
					return fmt.Errorf("message %d does not end as written", k+1)
				}
				last = last[:len(last)-m.added]
			}
			h.Write(last)
			n += len(last)
			var sum [32]byte
			copy(sum[:], h.Sum(nil))
			if n != m.n || sum != m.sum {
				return fmt.Errorf("message %d is not the original", k+1)
			}
			k++
			return nil
		},
	})
	if err == nil && k != len(want) {
		err = fmt.Errorf("%d messages, %d were written", k, len(want))
	}
	return k, err
}

// commit is steps 9 and 10.
func (w *writer) commit(top string) *WriteResult {
	in := w.in
	pf, err := w.f.Create(top)
	if err != nil {
		// Settled 2026-11-07 [R7-01 independent F6]: a create the root could not bind
		// (W-CHANGED: a folder on the way changed) is said as that, never as W-TAKEN,
		// though something may stand under the name.
		if RefusalCode(err) != "" {
			return w.stop(err)
		}
		if kind := entryKind(w.f, top); kind != "" || errors.Is(err, fs.ErrExist) {
			// The name was taken while the restore ran: "taken" first, then the one
			// cleanup, which removes nothing (step 0b), then "abandoned" (foldcheck2 P1).
			if lerr := w.logLine(EvTaken, nil); lerr != nil {
				w.res.Outcome, w.res.Err = OutTaken, err
				w.res.Words = fill(wTaken, "TOP", top, "SHOWS", showsWord(kind, "shows"))
				return w.res
			}
			c := cleanupContainer(w.f, top, in.ID, true, w.notMine)
			w.res.Clean = &c
			_ = w.logLine(EvAbandoned, func(l *LogLine) { l.Result, l.Detail = string(OutTaken), w.why(err) })
			w.res.Outcome, w.res.Err = OutTaken, err
			kind = entryKind(w.f, top)
			if kind == "" {
				w.res.Words = fill(wTakenGone, "TOP", top, "PATH", in.Local.Dir)
			} else {
				w.res.Words = fill(wTaken, "TOP", top, "SHOWS", showsWord(kind, "shows"))
			}
			return w.res
		}
		return w.stop(err)
	}
	// Settled 2026-11-07 [R7-01 independent F4]: the parent file is read back by
	// FS.Private like every other entry. Where that fails and MustBePrivate, the write
	// stops as for any other entry: the parent file, made exclusively by this restore an
	// instant ago with Thunderbird held out, is removed (only the very file its own handle
	// names), then the one cleanup. If it cannot be shown to be that file, it stays, and the
	// cleanup, finding it, removes nothing.
	if perr := w.private(top, false); perr != nil {
		w.unmakeParent(pf, top)
		return w.stop(perr)
	}
	syncErr := pf.Sync()
	pf.Close()
	if syncErr != nil {
		// N-c: one re-check. A regular, empty parent file is the commit (here, right
		// after the create, nothing else can have written into it: Thunderbird is held
		// out); anything else is abandoned, nothing removed.
		fi, lerr := w.f.Lstat(top)
		if lerr != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
			_ = w.logLine(EvAbandoned, func(l *LogLine) { l.Result, l.Detail = string(OutCommitErr), w.why(syncErr) })
			w.res.Outcome, w.res.Err = OutCommitErr, syncErr
			w.res.Words = fill(wCommitErr, "TOP", top, "PATH", in.Local.Dir, "WHY", w.why(syncErr))
			return w.res
		}
		w.res.AfterErr = syncErr
	}
	// 10. Nothing from here removes anything.
	if err := w.f.SyncDir("."); err != nil && w.res.AfterErr == nil {
		w.res.AfterErr = err
	}
	if err := w.logLine(EvEnded, func(l *LogLine) {
		l.Result, l.Restored, l.Name = string(OutDone), intp(w.res.Restored), top
	}); err != nil && w.res.AfterErr == nil {
		w.res.AfterErr = err
	}
	w.res.Outcome = OutDone
	w.res.Words = doneWords(w.res.Restored, top)
	if w.res.AfterErr != nil {
		w.res.Words += " (MailArchive met an error after the folder was complete: " + w.why(w.res.AfterErr) + ".)"
	}
	return w.res
}

// unmakeParent closes the parent file just made and removes it, only as the very file its
// open handle names (RemoveSeen, which walks by handle and follows no link); a file that
// cannot be named so is left (R7-01 independent F4).
func (w *writer) unmakeParent(pf wfile, top string) {
	st, ok := pf.(interface{ Stat() (fs.FileInfo, error) })
	var fi fs.FileInfo
	var err error
	if ok {
		fi, err = st.Stat()
	}
	pf.Close()
	if !ok || err != nil || !fi.Mode().IsRegular() {
		return
	}
	_ = w.f.RemoveSeen(top, map[string]fs.FileInfo{norm.NFC.String(top): fi})
}

// doneWords are W-DONE for n messages restored as top: the count grouped ("15,000") and
// "1 message" for one (friction F13, settled 2026-11-07: the plan and the progress group
// digits, so the result does too).
func doneWords(n int, top string) string {
	return fill(wDone, "N", Count(n, "message"), "TOP", top)
}

// Count is n with thousands separators and its noun, singular for one ("1 message",
// "15,000 messages").
func Count(n int, noun string) string {
	s := itoa(n)
	if n >= 0 {
		for i := len(s) - 3; i > 0; i -= 3 {
			s = s[:i] + "," + s[i:]
		}
	}
	if n == 1 {
		return s + " " + noun
	}
	return s + " " + noun + "s"
}

func showsWord(kind, shows string) string {
	if kind == "file" {
		return shows
	}
	return "may show"
}

// fail is a write that created nothing (no container): no cleanup.
func (w *writer) fail(o Outcome, err error) *WriteResult {
	w.res.Outcome, w.res.Err = o, err
	if w.started {
		_ = w.logLine(EvEnded, func(l *LogLine) { l.Result, l.Detail = string(o), w.why(err) })
	}
	if c := RefusalCode(err); c != "" {
		w.res.Words = err.Error()
	} else {
		w.res.Words = fill(wFailedHead, "WHY", w.why(err))
	}
	return w.res
}

// stop is a write that made the container and did not finish (Cancel, Shut down, an
// error): the one cleanup, then the words for what it did.
func (w *writer) stop(err error) *WriteResult {
	o := OutFailed
	if errors.Is(err, errStopped) || w.ctx.Err() != nil {
		o = OutStopped
	}
	head := fill(wFailedHead, "WHY", w.why(err))
	var np *errNotPrivateHere
	switch {
	case o == OutStopped:
		head = wStoppedHead
	case errors.As(err, &np):
		head = fill(wNotPrivateHead, "PATH", w.in.Local.Dir)
	case RefusalCode(err) != "":
		// A create's own plain words (W-NOTPRIVATE, W-CHANGED; R7-01 independent F1, F6),
		// which name Local Folders and the drive, never a restored folder.
		head = err.Error()
	}
	return w.cleanAfter(o, err, head, removedTail)
}

// checkFailed is W-CHECKFAIL: nothing visible, the one cleanup.
func (w *writer) checkFailed(file, folder string, err error) *WriteResult {
	head := fill(wCheckFail, "FILE", file, "FOLDER", folder)
	if w.in.Encrypted {
		head = wCheckFailEnc
	}
	return w.cleanAfter(OutCheckFail, err, head, wTreeRemoved)
}

// cleanAfter runs the one cleanup for a write that did not finish and logs and words
// what it did: everything removed → "ended" with head and removedTail; nothing removed
// → "abandoned" with that state's words; blocked → no line.
func (w *writer) cleanAfter(o Outcome, err error, head, tail string) *WriteResult {
	if w.list != nil {
		w.list.Close()
		w.list = nil
	}
	w.res.Outcome, w.res.Err = o, err
	top := w.res.Top
	c := cleanupContainer(w.f, top, w.in.ID, false, w.notMine)
	w.res.Clean = &c
	detail := w.why(err)
	switch c.Kind {
	case CleanRemoved:
		_ = w.logLine(EvEnded, func(l *LogLine) { l.Result, l.Detail = string(o), detail })
		w.res.Words = head + tail
	case CleanParent:
		_ = w.logLine(EvAbandoned, func(l *LogLine) { l.Result, l.Detail = string(o), detail })
		w.res.Words = fill(wNameAppeared, "TOP", top)
		if RefusalCode(err) == "W-CHANGED" {
			// R7-01 independent F6: what changed first, then what the person finds.
			w.res.Words = head + " " + w.res.Words
		}
	case CleanBlocked:
		w.res.Words = strings.TrimSuffix(head, " The part already written was removed.") + " " + w.blockedWords(c) + wTriesAgain
	default:
		_ = w.logLine(EvAbandoned, func(l *LogLine) { l.Result, l.Detail = string(o), detail })
		w.res.Words = head + " " + leftoverWords(c, top, w.in.Local.Dir, w.in.Encrypted)
	}
	return w.res
}

func (w *writer) blockedWords(c CleanResult) string {
	if w.in.Encrypted {
		return fill(wBlockedEnc, "TOP", w.res.Top)
	}
	return fill(wBlocked, "FILE", c.Blocked, "TOP", w.res.Top)
}

// leftoverWords are the words of a cleanup that left the container whole (or part of
// it): W-LEFT's two forms and the made-list's.
func leftoverWords(c CleanResult, top, local string, encrypted bool) string {
	switch c.Kind {
	case CleanLeft:
		if encrypted {
			return strings.Replace(fill(wLeftEnc, "N", itoa(c.ForeignN), "TOP", top, "PATH", local), " 1 entries", " 1 entry", 1)
		}
		return strings.Replace(fill(wLeft, "N", itoa(c.ForeignN), "FIRST NAMES", strings.Join(c.Foreign, ", "), "TOP", top, "PATH", local), " 1 entries", " 1 entry", 1)
	case CleanLeftPart:
		if encrypted {
			return fill(wLeftPartEnc, "TOP", top, "PATH", local)
		}
		return fill(wLeftPart, "FIRST NAMES", strings.Join(c.Foreign, ", "), "TOP", top, "PATH", local)
	case CleanListBad:
		return fill(wListBad, "TOP", top, "PATH", local)
	}
	return ""
}

// why is an error as the writer words it and logs it. For an encrypted archive only its
// kind is said, never its text, which can name a restored folder's file: restores.log,
// the progress and the result carry no folder names for an encrypted archive (design
// §6.2, MA-1128; slice R6).
func (w *writer) why(err error) string {
	if err == nil || !w.in.Encrypted {
		return why(err)
	}
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return "the disk is full"
	case errors.Is(err, errStopped):
		return "stopped"
	}
	return "a file error, not named for an encrypted archive, since it could name a folder"
}

// why is an error in words: a full disk said so.
func why(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, syscall.ENOSPC) {
		return "the disk is full"
	}
	return err.Error()
}

// The heads and tail of the Stopped and failure words (§7.5): the tail only when the
// cleanup removed what was written (foldcheck2: otherwise the row that applies replaces
// it).
const (
	wStoppedHead = "The restore was stopped, so nothing was added to Thunderbird."
	wFailedHead  = "The restore could not finish (WHY), so nothing was added to Thunderbird."
	// wNotPrivateHead (R6-01; the lead's words, owner-delegated): an encrypted archive's
	// restore that could not make its files readable only by this account.
	wNotPrivateHead = "MailArchive could not make the restored files readable only by your account in PATH, so nothing was added to Thunderbird: other accounts on this computer might have read the decrypted mail. Choose a profile whose Local Folders is on this computer's own drive."
	removedTail     = " The part already written was removed. That cannot undo a copy already uploaded by a sync service, kept in a backup or snapshot, or left on the disk."
)

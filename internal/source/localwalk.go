package source

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	gombox "github.com/emersion/go-mbox"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// localWalk is one walk of one local store (mbox, maildir, Evolution). It reads under
// R25 (localstore.go) and costs a fault exactly the item or folder it hit: what it
// cannot read it delivers as a Fault, as it happens, and goes on with the next entry
// (design-fault-isolation §4.5, F29). Only the store's own root failing to list is a
// store-level error.
type localWalk struct {
	handler MessageHandler
	opts    WalkOptions
	sink    faultSink
	follow  bool          // the input's directory-link opt-in
	visited []fs.FileInfo // under the opt-in: every directory the walk has entered
	depth   int           // how many directories deep the walk is (every descent goes through into)
}

func newLocalWalk(handler MessageHandler, opts WalkOptions, follow bool) *localWalk {
	return &localWalk{handler: handler, opts: opts, sink: faultSink{cb: opts.Faults}, follow: follow}
}

func (w *localWalk) fault(f Fault) { w.sink.emit(f) }

func (w *localWalk) abort() error {
	if w.opts.Abort != nil {
		return w.opts.Abort()
	}
	return nil
}

func (w *localWalk) noteFolder(folder []string) {
	if w.opts.Folder != nil {
		w.opts.Folder(folder)
	}
}

// seen reports whether the walk has entered this directory already, and remembers it
// when not. It is the (dev, inode) set of the opt-in: without the opt-in no link is
// followed, so no directory can be reached twice and nothing is remembered.
func (w *localWalk) seen(fi fs.FileInfo) bool {
	for _, v := range w.visited {
		if os.SameFile(v, fi) {
			return true
		}
	}
	w.visited = append(w.visited, fi)
	return false
}

// withFolder is folder with one more sanitized segment, in a slice of its own.
func withFolder(folder []string, name string) []string {
	return appendSeg(folder, util.SanitizeSegment(name))
}

// refuse records an entry the walk will not read: a special file (info: nothing is
// missing), a link out of the store or a directory link (terminal: it might hold
// mail). folder is where the gap belongs: for an item the folder it sat in, for a
// directory link the folder the link would have been.
func (w *localWalk) refuse(sc *scope, rel string, folder []string, v verdict) {
	reason, class, scopeOf := refusal(v.kind)
	f := Fault{Reason: reason, Scope: scopeOf, Class: class, Folder: folder, Detail: v.detail}
	if scopeOf == scopeFolder {
		f.Locator = "folder:" + sc.at(rel)
	} else {
		f.Locator = "file:" + sc.at(rel)
	}
	w.fault(f)
}

// folderFault records a folder the walk could not read.
func (w *localWalk) folderFault(sc *scope, rel string, folder []string, reason string, err error, detail string) {
	class := classTerm
	if reason == "folder-unreadable" && errnoRetryable(err) {
		class = state.ClassFillable
	}
	if err != nil {
		detail += err.Error()
	}
	w.fault(Fault{Reason: reason, Scope: scopeFolder, Class: class, Folder: folder, Locator: "folder:" + sc.at(rel), Detail: detail})
}

// errnoRetryable says a read error is one the operator fixes outside the source
// (permissions, the disk): fillable, as a retry after the fix reads it. A structural
// error is terminal (§4.1).
func errnoRetryable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, fs.ErrPermission) || state.GapErrnoClass(err.Error()) != ""
}

// into reads the directory a verdict names, if it may be read. A plain directory is
// read where it is; a directory link is refused (a terminal symlinked-folder-skipped
// gap whose remedy names the opt-in) unless the input has the opt-in, and then it is
// read as a scope of its own, once (a directory the walk has entered is not entered
// again, so a link that loops or repeats a folder reads it once). Anything else at a
// folder's place is refused.
func (w *localWalk) into(sc *scope, rel string, v verdict, folder []string, read func(*scope, string) error) error {
	switch v.kind {
	case kindDir:
		if w.tooDeep(sc, rel, folder) {
			return nil
		}
		if w.follow {
			if fi, err := sc.lstat(rel); err == nil && w.seen(fi) {
				w.alreadyRead(sc, rel, folder)
				return nil
			}
		}
		return w.deeper(func() error { return read(sc, rel) })
	case kindDirLink:
		if !w.follow {
			w.refuse(sc, rel, folder, v)
			return nil
		}
		if w.tooDeep(sc, rel, folder) {
			return nil
		}
		return w.followInto(sc, rel, folder, read)
	}
	w.refuse(sc, rel, folder, v)
	return nil
}

// tooDeep says the walk is at the depth cap and records the directory it will not
// enter: a terminal gap, since what is below it might hold mail (S2B-AO1). Every
// descent goes through into, so the cap bounds the recursion of every local reader;
// without it a chain of thousands of empty directories (cheap to make) made a run's
// cost grow faster than its depth, as every level lists its whole relative path from
// the root.
func (w *localWalk) tooDeep(sc *scope, rel string, folder []string) bool {
	if w.depth < maxFolderDepth {
		return false
	}
	w.fault(Fault{Reason: "folder-too-deep", Scope: scopeFolder, Class: classTerm, Folder: folder, Locator: "folder:" + sc.at(rel),
		Detail: fmt.Sprintf("the folder is nested more than %d levels deep; it and the folders below it were not read", maxFolderDepth)})
	return true
}

// deeper runs read one directory level down.
func (w *localWalk) deeper(read func() error) error {
	w.depth++
	defer func() { w.depth-- }()
	return read()
}

// alreadyRead records that a directory the opt-in would have read again was read
// already: nothing is missing, so it is info.
func (w *localWalk) alreadyRead(sc *scope, rel string, folder []string) {
	w.fault(Fault{Reason: "folder-cycle", Scope: scopeFolder, Class: state.ClassInfo, Folder: folder, Locator: "folder:" + sc.at(rel),
		Detail: "this folder was read already, through another path, and is not read again"})
}

// followInto reads a directory link's target as a scope of its own.
func (w *localWalk) followInto(sc *scope, rel string, folder []string, read func(*scope, string) error) error {
	real, err := filepath.EvalSymlinks(sc.display(rel))
	var fi fs.FileInfo
	if err == nil {
		fi, err = os.Stat(real)
	}
	if err != nil || !fi.IsDir() {
		detail := "the link is not a folder"
		if err != nil {
			detail = "the link could not be followed: " + err.Error()
		}
		w.refuse(sc, rel, folder, verdict{kind: kindDirLink, detail: detail})
		return nil
	}
	if w.seen(fi) {
		w.alreadyRead(sc, rel, folder)
		return nil
	}
	root, err := os.OpenRoot(real)
	if err != nil {
		w.refuse(sc, rel, folder, verdict{kind: kindDirLink, detail: "the link could not be followed: " + err.Error()})
		return nil
	}
	sub := &scope{root: root, path: real, prefix: sc.at(rel)}
	defer sub.close()
	return w.deeper(func() error { return read(sub, ".") })
}

// isMaildirIn reports whether the directory at rel has a cur or new entry that is a
// directory or a link. The entries are lstat'ed, never followed: a linked cur still
// makes the folder a maildir, so it is judged and recorded when read, never silently
// dropped (§4.5, "detected but linked").
func isMaildirIn(sc *scope, rel string) bool {
	for _, sub := range []string{"cur", "new"} {
		if fi, err := sc.lstat(relJoin(rel, sub)); err == nil && maildirEntryMode(fi.Mode()) {
			return true
		}
	}
	return false
}

// maildirEntryMode says an lstat mode is one a maildir's cur or new may have and be
// detected by: a directory, a symlink, or (Windows) a junction or mount point, which Go
// reports as irregular. A linked one is judged and recorded when it is read; it must not
// make its folder silently no maildir (§4.5).
func maildirEntryMode(m fs.FileMode) bool {
	return m.IsDir() || m&(fs.ModeSymlink|fs.ModeIrregular) != 0
}

// maildir reads every message file of a maildir folder's new/ and cur/ directories
// (shared by the Thunderbird/mbox reader and the Evolution reader: both store
// folders as maildirs). new/ and cur/ are folder positions: a link there is a
// directory link.
func (w *localWalk) maildir(sc *scope, rel string, folder []string) error {
	w.noteFolder(folder)
	for _, sub := range []string{"new", "cur"} {
		srel := relJoin(rel, sub)
		fi, err := sc.lstat(srel)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				w.folderFault(sc, srel, folder, "folder-unreadable", err, "the "+sub+" folder could not be read: ")
			}
			continue // an absent new/ or cur/: nothing to read
		}
		v := w.judge(sc, srel, fi.Mode(), posDir)
		if err := w.into(sc, srel, v, folder, func(s *scope, r string) error { return w.maildirDir(s, r, folder) }); err != nil {
			return err
		}
	}
	return nil
}

// maildirDir reads every message file under a maildir new/ or cur/ directory.
// Standard maildirs store messages as flat files; Evolution sub-buckets them into
// two-hex-character shard directories (e.g. cur/04/<msg>), so a plain directory is
// descended into, not skipped.
func (w *localWalk) maildirDir(sc *scope, rel string, folder []string) error {
	entries, err := sc.readDir(rel)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			w.folderFault(sc, rel, folder, "folder-unreadable", err, "the folder's mail directory could not be listed: ")
		}
		return nil
	}
	for _, fe := range entries {
		if err := w.abort(); err != nil {
			return err
		}
		erel := relJoin(rel, fe.Name())
		v := w.judge(sc, erel, fe.Type(), posEither)
		switch v.kind {
		case kindDir, kindDirLink:
			if err := w.into(sc, erel, v, folder, func(s *scope, r string) error { return w.maildirDir(s, r, folder) }); err != nil {
				return err
			}
		case kindFile, kindFileLink, kindPlaceholder:
			if err := w.maildirMessage(sc, v.rel, fe.Name(), folder); err != nil {
				return err
			}
		default:
			w.refuse(sc, erel, folder, v)
		}
	}
	return nil
}

// maildirUnique is the part of a maildir file name that stays when a mail client
// renames the file to change its flags: the name up to the first ':' (or the '!'
// that stands for it where ':' is not a legal file name character).
func maildirUnique(name string) string {
	if i := strings.IndexAny(name, ":!"); i >= 0 {
		return name[:i]
	}
	return name
}

// readFileIn reads one message file through the root: the descriptor is checked to be
// a regular file, and a read is not trusted to end (the data is whatever arrives).
func readFileIn(sc *scope, rel string) ([]byte, error) {
	f, err := util.OpenRegularFileIn(sc.root, filepath.FromSlash(rel))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(wrapMaildirFile(sc.at(rel), f))
}

// maildirMessage reads and delivers one message file. A file that cannot be read costs
// that file: the gap is fillable when the operator fixes it outside the source (its
// permissions, the disk) and the walk goes on; no record is minted for it (FC7).
func (w *localWalk) maildirMessage(sc *scope, rel, name string, folder []string) error {
	key := strings.Join(folder, "/")
	// The ledger strips control characters from a locator and cuts it at a bound, so the
	// locator is built in the ledger's own spelling, with a digest where it had to be
	// changed: two names that read alike once stripped have an entry each, and the key
	// a recovery builds is the key the gap was stored under (S2B-AO2).
	locator := state.BoundLocator("maildir:" + key + "/" + maildirUnique(name))
	data, err := readFileIn(sc, rel)
	if errors.Is(err, fs.ErrNotExist) {
		// A live mail client renames a file to change its flags between the listing and
		// this read: look ONCE for the same unique part in the same directory. A file that
		// moved from new/ to cur/ is found later, when cur/ is read.
		if nrel, nname, ok := w.relocate(sc, rel, name); ok {
			rel, name = nrel, nname
			data, err = readFileIn(sc, rel)
		}
	}
	if err != nil {
		var nr *util.NotRegularError
		switch {
		case errors.Is(err, fs.ErrNotExist):
			w.fault(Fault{Reason: "vanished-during-read", Scope: scopeItem, Class: state.ClassFillable, Folder: folder, Locator: locator,
				Detail: "the file was renamed or deleted while the run was reading it"})
		case errors.As(err, &nr):
			w.refuse(sc, rel, folder, verdict{kind: kindSpecial, detail: err.Error()})
		default:
			w.fault(Fault{Reason: "file-unreadable", Scope: scopeItem, Class: state.ClassFillable, Folder: folder, Locator: locator, Detail: err.Error()})
		}
		return nil
	}
	m := parseRaw(data)
	if m == nil {
		return nil
	}
	m.ReadLocator = locator // not m.Locator: see model.Message.ReadLocator
	// Read state for a maildir message is the "S" (Seen) info flag in its filename —
	// UNLESS the message also carries an X-Mozilla-Status header (a Thunderbird
	// artifact parseMessage already honoured, which wins per the design). Threaded here
	// because only the maildir layout puts the read flag in the path.
	if !headerBlockHas(data, "X-Mozilla-Status") {
		m.Unread = !maildirSeen(name)
	}
	return w.handler(folder, m)
}

// relocate looks once in rel's directory for the file whose unique part is name's:
// the same message under the flags a client has just given it.
func (w *localWalk) relocate(sc *scope, rel, name string) (nrel, nname string, ok bool) {
	dir := path.Dir(rel)
	entries, err := sc.readDir(dir)
	if err != nil {
		return "", "", false
	}
	want := maildirUnique(name)
	for _, e := range entries {
		if e.Type().IsRegular() && maildirUnique(e.Name()) == want {
			return relJoin(dir, e.Name()), e.Name(), true
		}
	}
	return "", "", false
}

// mboxFile reads every message of one mbox file. A file that cannot be opened is
// folder-unreadable; a stream error mid-file is folder-partially-read with the count
// read, and the next folder goes on: the tool does not resynchronise a broken stream
// (N2).
func (w *localWalk) mboxFile(sc *scope, rel string, folder []string) error {
	w.noteFolder(folder)
	f, err := util.OpenRegularFileIn(sc.root, filepath.FromSlash(rel))
	if err != nil {
		var nr *util.NotRegularError
		if errors.As(err, &nr) {
			w.refuse(sc, rel, folder, verdict{kind: kindSpecial, detail: err.Error()})
			return nil
		}
		w.folderFault(sc, rel, folder, "folder-unreadable", err, "the mbox file could not be opened: ")
		return nil
	}
	defer f.Close()

	mr := gombox.NewReader(skipBOM(wrapMboxStream(sc.at(rel), f)))
	n := 0
	partial := func(err error) {
		w.folderFault(sc, rel, folder, "folder-partially-read", err, fmt.Sprintf("%d message(s) were read before the stream failed: ", n))
	}
	for {
		if err := w.abort(); err != nil {
			return err
		}
		msgReader, err := mr.NextMessage()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			partial(err)
			return nil
		}
		data, err := io.ReadAll(msgReader)
		if err != nil {
			partial(err)
			return nil
		}
		n++
		if m := parseRaw(data); m != nil {
			if err := w.handler(folder, m); err != nil {
				return err
			}
		}
	}
}

// mboxDir recurses a mail directory. Each folder is either an mbox file or a maildir
// directory; its subfolders live in a sibling "<name>.sbd" directory (Thunderbird's
// layout). top is the store's root, whose own listing failing is a store-level error;
// any other folder failing to list costs that folder.
func (w *localWalk) mboxDir(sc *scope, rel string, prefix []string, top bool) error {
	entries, err := sc.readDir(rel)
	if err != nil {
		if top {
			return fmt.Errorf("read mail dir %s: %w", sc.display(rel), err)
		}
		w.folderFault(sc, rel, prefix, "folder-unreadable", err, "the folder could not be listed: ")
		return nil
	}
	// A folder's subfolders live in "<name>.sbd", whatever the folder's own entry turns
	// out to be: a parent with no messages of its own is an EMPTY file (or no file at
	// all) beside its container, and a mailbox the sniff does not take is no reason to
	// drop the folders below it (S2B-AO6). sub reads the container once, and the entries
	// whose container no entry reached are read after the loop.
	sbdDone := map[string]bool{}
	sub := func(name string, folderPath []string) error {
		sbdDone[name+".sbd"] = true
		return w.sbd(sc, rel, name, folderPath)
	}
	for _, e := range entries {
		if err := w.abort(); err != nil {
			return err
		}
		name := e.Name()
		erel := relJoin(rel, name)
		if strings.HasSuffix(name, ".sbd") {
			continue // subfolder container, reached through its parent folder
		}
		v := w.judge(sc, erel, e.Type(), posEither)
		folderPath := withFolder(prefix, name)
		switch v.kind {
		case kindDir, kindDirLink:
			// A folder of the store is a maildir. A directory link is recorded where a
			// folder would have been; with the opt-in it is read as the folder it names.
			if v.kind == kindDir && !isMaildirIn(sc, erel) {
				if err := sub(name, folderPath); err != nil {
					return err
				}
				continue // a plain directory that is no folder
			}
			if v.kind == kindDirLink && !isMailboxCandidate(name) {
				continue // a hidden or index entry, not a folder
			}
			if err := w.into(sc, erel, v, folderPath, func(s *scope, r string) error {
				if !isMaildirIn(s, r) {
					return nil
				}
				if err := w.maildir(s, r, folderPath); err != nil {
					return err
				}
				return sub(name, folderPath)
			}); err != nil {
				return err
			}
		case kindFile, kindFileLink, kindPlaceholder:
			if !isMailboxCandidate(name) {
				continue
			}
			if ok, err := looksLikeMboxIn(sc, v.rel); err != nil {
				w.folderFault(sc, erel, folderPath, "folder-unreadable", err, "the file could not be opened: ")
			} else if ok {
				if err := w.mboxFile(sc, v.rel, folderPath); err != nil {
					return err
				}
			}
			if err := sub(name, folderPath); err != nil {
				return err
			}
		default:
			if isMailboxCandidate(name) {
				w.refuse(sc, erel, prefix, v)
			}
		}
	}
	// A container whose folder has no entry of its own (or none that was a folder).
	for _, e := range entries {
		name := e.Name()
		base := strings.TrimSuffix(name, ".sbd")
		if base == "" || base == name || sbdDone[name] {
			continue
		}
		if err := w.abort(); err != nil {
			return err
		}
		if err := sub(base, withFolder(prefix, base)); err != nil {
			return err
		}
	}
	return nil
}

// sbd recurses into a "<name>.sbd" subfolder container if it exists. The container is
// a folder position: a link there is a directory link, recorded and not followed.
func (w *localWalk) sbd(sc *scope, dir, name string, folderPath []string) error {
	srel := relJoin(dir, name+".sbd")
	fi, err := sc.lstat(srel)
	if err != nil {
		return nil // no container
	}
	v := w.judge(sc, srel, fi.Mode(), posDir)
	if v.kind == kindFile || v.kind == kindFileLink {
		return nil // a file of that name is no container
	}
	return w.into(sc, srel, v, folderPath, func(s *scope, r string) error { return w.mboxDir(s, r, folderPath, false) })
}

// looksLikeMboxIn reports whether the file at rel begins with an mbox "From "
// separator, opened through the root like every other read. A file that cannot be
// opened is no answer: it is returned as an error, because a mailbox the run cannot
// open must be recorded, never read as "not a mailbox" (a pipe swapped in after the
// entry was judged is the one open failure that is a plain no).
func looksLikeMboxIn(sc *scope, rel string) (bool, error) {
	f, err := util.OpenRegularFileIn(sc.root, filepath.FromSlash(rel))
	if err != nil {
		var nr *util.NotRegularError
		if errors.As(err, &nr) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	return sniffMbox(f), nil
}

// utf8BOM is the byte order mark some editors and exporters put before a text file.
const utf8BOM = "\xef\xbb\xbf"

// sniffMbox reports whether r begins like an mbox: an optional UTF-8 byte order mark
// and any blank lines, then the "From " separator. A mailbox with a mark or a leading
// blank line is a mailbox (the reader skips the blank lines and mboxFile the mark).
func sniffMbox(r io.Reader) bool {
	buf := make([]byte, 4096)
	n, _ := io.ReadFull(r, buf)
	b := strings.TrimPrefix(string(buf[:n]), utf8BOM)
	b = strings.TrimLeft(b, "\r\n")
	return strings.HasPrefix(b, "From ")
}

// skipBOM is r without a leading UTF-8 byte order mark: the mbox reader takes the mark
// for text before the first separator and refuses the file.
func skipBOM(r io.Reader) io.Reader {
	head := make([]byte, len(utf8BOM))
	n, err := io.ReadFull(r, head)
	if err == nil && string(head) == utf8BOM {
		return r
	}
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return io.MultiReader(bytes.NewReader(head[:n]), &failingRead{err: err})
	}
	return io.MultiReader(bytes.NewReader(head[:n]), r)
}

// wrapMboxStream wraps the stream an mbox file is read through. It is the identity in
// production: the seam a test fails a read through, mid-file, the way a dying disk does
// (rel is the file's place in the store).
var wrapMboxStream = func(rel string, r io.Reader) io.Reader { return r }

// wrapMaildirFile is wrapMboxStream's twin for a maildir message file: the identity in
// production, the seam a test fails a file's read through with an I/O error (MA-321's EIO
// case: no file on a healthy disk fails its read that way).
var wrapMaildirFile = func(rel string, r io.Reader) io.Reader { return r }

// InjectMaildirReadFaults makes the read of every maildir message file whose place in
// the store (a slash path, "Inbox/cur/b:2,S") fault returns an error, fail the read with
// that error until restore is called. It is for tests; nothing in production calls it.
func InjectMaildirReadFaults(fault func(rel string) error) (restore func()) {
	old := wrapMaildirFile
	wrapMaildirFile = func(rel string, r io.Reader) io.Reader {
		if err := fault(rel); err != nil {
			return &failingRead{err: err}
		}
		return r
	}
	return func() { wrapMaildirFile = old }
}

// failingRead is a stream whose first read fails.
type failingRead struct{ err error }

func (f *failingRead) Read([]byte) (int, error) { return 0, f.err }

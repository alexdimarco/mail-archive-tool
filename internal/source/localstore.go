package source

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// Local stores (an mbox or Thunderbird store, a maildir, an Evolution store) are read
// under R25, source-read containment (design-fault-isolation §4.5, FC1 to FC3): the
// store is the operator's input resolved ONCE and opened as an os.Root, so no path a
// reader builds can leave it; every entry is judged by its own type (the lstat type a
// directory listing gives) before anything is opened; a link is judged lexically, by
// its Readlink text, before anything it names is touched; and every open goes through
// util.OpenRegularFileIn, which checks the descriptor too. What is refused is recorded
// (a Fault), never only skipped.

// OpenOptions are what a caller says about an input when it opens it.
type OpenOptions struct {
	// FollowLinks is the input's directory-link opt-in (owner ruling OR6): this
	// input's directory links are followed, under a visited set of the directories the
	// walk has read, so a link that loops or repeats a folder reads it once. A file
	// link out of the store and a special file stay refused. Without it no directory
	// link is ever followed.
	FollowLinks bool
}

// entryKind is what a directory entry is to a local reader.
type entryKind int

const (
	kindFile        entryKind = iota // a regular file: read it
	kindDir                          // a plain directory: descend
	kindFileLink                     // a link, in the store, to a regular file there: read it
	kindDirLink                      // a directory link (a symlink; on Windows a junction or mount point)
	kindOutside                      // a link out of the store, or to anything but a file or folder: refused
	kindSpecial                      // a pipe, socket or device: never opened
	kindPlaceholder                  // a Windows reparse file that is not a link (a cloud placeholder): read it
)

// classifyMode is FC1's classifier: what a directory entry of this mode is on goos.
// readlinkOK says Readlink answered for an entry the OS reports as irregular, which is
// how a Windows junction or mount point (a directory link) is told from a cloud or
// HSM placeholder (OneDrive Files-On-Demand, Dropbox online-only, Azure File Sync).
// A symlink is judged by judgeLink, which needs its text; here it is kindOutside. It
// is a function of goos and mode alone so a table test pins it on any OS.
func classifyMode(goos string, mode fs.FileMode, readlinkOK bool) entryKind {
	switch {
	case mode&fs.ModeSymlink != 0:
		return kindOutside
	case mode&(fs.ModeNamedPipe|fs.ModeSocket|fs.ModeDevice|fs.ModeCharDevice) != 0:
		return kindSpecial
	case mode&fs.ModeIrregular != 0:
		switch {
		case goos != "windows":
			return kindOutside // nothing on Unix is known to hold mail and be irregular
		case readlinkOK:
			return kindDirLink
		default:
			return kindPlaceholder
		}
	case mode.IsDir():
		return kindDir
	case mode.IsRegular():
		return kindFile
	}
	return kindOutside
}

// refusal is the ledger entry of a refused entry (§4.1): the reason and class, and the
// scope it is recorded at. Only a proven non-mail special file is info (FC1); anything
// else that might hold mail is terminal.
func refusal(kind entryKind) (reason, class, scope string) {
	switch kind {
	case kindSpecial:
		return "not-a-message-file", state.ClassInfo, scopeItem
	case kindDirLink:
		return "symlinked-folder-skipped", classTerm, scopeFolder
	}
	return "symlink-outside-store", classTerm, scopeItem
}

// position says what a place in a store expects of an entry, which is how a link
// whose target must not be touched is still told apart: a link at the place of a
// folder (cur, new, a .sbd container, subfolders) is a directory link, one among a
// folder's files is a file link.
type position int

const (
	posEither position = iota
	posDir
)

// lexicalLink judges a link by its text alone (FC3): target is what Readlink said,
// rel the link's own place in the root (slash path). It returns the in-root path the
// link names, relative to the root ("." is the root itself), or false for a target
// that is absolute, drive-qualified, a UNC path, or leaves the root after a lexical
// clean. Nothing the target names is touched.
func lexicalLink(rel, target string) (string, bool) {
	t := strings.ReplaceAll(target, `\`, "/")
	if t == "" || strings.HasPrefix(t, "/") || filepath.IsAbs(target) || filepath.VolumeName(target) != "" || (len(t) >= 2 && t[1] == ':') {
		return "", false
	}
	joined := path.Clean(path.Join(path.Dir(rel), t))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}

// isMaildir reports whether dir is a maildir folder (has a "cur" or "new" subdir). It
// is DETECTION, for discovery and for the choice of reader: it follows a link, so a
// linked maildir is still seen. Reading never follows one: the walk judges and records
// it (§4.5).
func isMaildir(dir string) bool {
	for _, sub := range []string{"cur", "new"} {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// scope is a store root, resolved once and opened as an os.Root. A followed directory
// link opens a scope of its own, whose root is the link's resolved target; prefix is
// where that scope sits in the store, so every locator reads from the input's root.
type scope struct {
	root   *os.Root
	path   string // the resolved root directory
	prefix string // slash path of this root within the store; "" for the store's own root
}

// openScope resolves dir (a symlink in the operator's own spelling is the operator's
// to give) and opens it.
func openScope(dir string) (*scope, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(real)
	if err != nil {
		return nil, err
	}
	return &scope{root: root, path: real}, nil
}

func (s *scope) close() { s.root.Close() }

// at is the store-relative slash path of rel in this scope: what a locator names.
func (s *scope) at(rel string) string {
	if rel == "." || rel == "" {
		return s.prefix
	}
	if s.prefix == "" {
		return rel
	}
	return s.prefix + "/" + rel
}

func (s *scope) lstat(rel string) (fs.FileInfo, error) { return s.root.Lstat(filepath.FromSlash(rel)) }

func (s *scope) readDir(rel string) ([]fs.DirEntry, error) {
	return util.ReadDirIn(s.root, filepath.FromSlash(rel))
}

// display is a path for an error message: the real place on disk.
func (s *scope) display(rel string) string {
	if rel == "." || rel == "" {
		return s.path
	}
	return filepath.Join(s.path, filepath.FromSlash(rel))
}

// relJoin joins a slash path under a directory of the scope ("." is the root).
func relJoin(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// verdict is the judgment of one entry: its kind, the in-root path to read it at (the
// target, for a link to a file) and the words of why it was refused.
type verdict struct {
	kind   entryKind
	rel    string
	detail string
}

// judge decides what an entry is, before anything is opened (FC1, FC3). mode is its
// own type (a listing's DirEntry.Type, or an Lstat's Mode), never its target's.
func (w *localWalk) judge(sc *scope, rel string, mode fs.FileMode, pos position) verdict {
	if mode&fs.ModeSymlink != 0 {
		return w.judgeLink(sc, rel, pos)
	}
	readlinkOK := false
	if mode&fs.ModeIrregular != 0 {
		_, err := sc.root.Readlink(filepath.FromSlash(rel))
		readlinkOK = err == nil
	}
	v := verdict{kind: classifyMode(runtime.GOOS, mode, readlinkOK), rel: rel}
	switch v.kind {
	case kindSpecial:
		v.detail = "a " + strings.TrimPrefix(util.ModeKind(mode), "a ") + " is not a mail file and was not opened"
	case kindOutside:
		v.detail = "the entry is not a regular file or a folder (" + util.ModeKind(mode) + ")"
	case kindDirLink:
		v.detail = "a junction or mount point is a directory link"
	}
	return v
}

// judgeLink reads a link's text and judges it lexically (FC3). Only a relative target
// that stays in the root is looked at, and only by lstat; a target that does not is
// refused without being touched, as a directory link where the place is a folder's.
// With the input's opt-in a directory link is followed wherever it points, so such a
// target is looked at (stat only) to see that it is a directory: a file link out of
// the store is never followed.
func (w *localWalk) judgeLink(sc *scope, rel string, pos position) verdict {
	target, err := sc.root.Readlink(filepath.FromSlash(rel))
	if err != nil {
		return verdict{kind: refusedLinkKind(pos), rel: rel, detail: "the link could not be read: " + err.Error()}
	}
	in, ok := lexicalLink(rel, target)
	if !ok {
		if w.follow {
			if fi, err := os.Stat(sc.display(rel)); err == nil && fi.IsDir() {
				return verdict{kind: kindDirLink, rel: rel, detail: "a link to " + target}
			}
			pos = posEither // not a folder: a link to a file out of the store, refused even with the opt-in
		}
		return verdict{kind: refusedLinkKind(pos), rel: rel, detail: "the link points outside the store (to " + target + ") and was not followed"}
	}
	fi, err := sc.root.Lstat(filepath.FromSlash(in))
	switch {
	case err != nil:
		return verdict{kind: kindOutside, rel: rel, detail: "the link's target could not be read: " + err.Error()}
	case fi.Mode().IsRegular():
		return verdict{kind: kindFileLink, rel: in}
	case fi.IsDir():
		return verdict{kind: kindDirLink, rel: rel, detail: "a link to " + target}
	}
	return verdict{kind: kindOutside, rel: rel, detail: "the link's target is not a regular file or a folder (" + util.ModeKind(fi.Mode()) + ")"}
}

// refusedLinkKind is what a link whose target cannot be told is, by the place it sits
// in: a folder's place means a directory link, anywhere else a file link out of the
// store.
func refusedLinkKind(pos position) entryKind {
	if pos == posDir {
		return kindDirLink
	}
	return kindOutside
}

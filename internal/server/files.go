package server

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/util"
)

// allowedFiles is the http.FileSystem /files/ serves: the archive's exported
// content and nothing else (design-local-confidentiality P9). That is the root
// index.html and README.txt, and below them a folder (served only through its
// own index.html, never listed), a page (*.html), a preserved original (*.eml)
// or an attachment zip (*-attachments.zip). Every other name is 404: a dotfile,
// search.db and its siblings, a log, a report, a token or secret, the
// _outlook-pst staging folder. So is a link at any component (a symlink, or on
// Windows a junction or mount point), while a cloud placeholder is served as the
// file or folder it represents [CC13]. Each request opens the archive as an
// os.Root, so no open escapes it, and the file opened must be the very entry the
// checks saw: a link swapped in after the check is never followed (N10).
type allowedFiles struct {
	dir string
}

func (a allowedFiles) Open(name string) (http.File, error) {
	segs, ok := allowedPath(name)
	if !ok {
		return nil, fs.ErrNotExist
	}
	root, err := os.OpenRoot(a.dir)
	if err != nil {
		return nil, fs.ErrNotExist
	}
	defer root.Close() // a file opened from the root stays open on its own
	return openChecked(root, segs)
}

// allowedPath splits a /files/ request path into its segments and applies the
// rules a name alone decides: no segment may start with a dot, hold a
// backslash or colon (Windows stream and separator syntax), or end in a dot or
// space (names Windows would quietly rewrite); the top-level _outlook-pst
// staging folder is never served. The archive root itself is an empty slice.
func allowedPath(name string) ([]string, bool) {
	p := path.Clean("/" + name)
	if p == "/" {
		return nil, true
	}
	segs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, s := range segs {
		if s == "" || strings.HasPrefix(s, ".") || strings.ContainsAny(s, `\:`) ||
			strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") {
			return nil, false
		}
	}
	if strings.EqualFold(segs[0], "_outlook-pst") {
		return nil, false
	}
	return segs, true
}

// fileAllowed reports whether a file named name may be served at depth (1 is the
// archive root): only index.html and README.txt there; below it a page, an .eml
// or an attachment zip.
func fileAllowed(depth int, name string) bool {
	if depth == 1 {
		return name == "index.html" || name == "README.txt"
	}
	return strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".eml") || strings.HasSuffix(name, "-attachments.zip")
}

// pinFileTypes fixes the Content-Type of an archived .eml and an attachment zip
// before http.FileServer picks one: the type otherwise comes from the host's mime
// database, or from sniffing the first bytes, and a raw message that begins with
// HTML must never be served as a page (AO3). A page keeps text/html.
func pinFileTypes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".eml"):
			w.Header().Set("Content-Type", "message/rfc822")
		case strings.HasSuffix(r.URL.Path, "-attachments.zip"):
			w.Header().Set("Content-Type", "application/zip")
		}
		next.ServeHTTP(w, r)
	})
}

type entryKind int

const (
	entryRefused entryKind = iota
	entryFile
	entryDir
)

// classify judges one directory entry by the link rule the no-follow designs
// share (fault-isolation FC1) [CC13]: a symlink, or a name surrogate that Go
// reports as ModeIrregular (a Windows junction or mount point), is a link and is
// refused; a FIFO, socket or device is refused; a folder — a cloud placeholder
// folder, ModeIrregular with ModeDir, included — is a folder; a regular file, or
// any other ModeIrregular entry (a cloud placeholder file), is a file.
func classify(mode fs.FileMode, surrogate bool) entryKind {
	switch {
	case mode&fs.ModeSymlink != 0:
		return entryRefused
	case mode&fs.ModeIrregular != 0 && surrogate:
		return entryRefused
	case mode&(fs.ModeNamedPipe|fs.ModeSocket|fs.ModeDevice|fs.ModeCharDevice) != 0:
		return entryRefused
	case mode.IsDir():
		return entryDir
	case mode.IsRegular() || mode&fs.ModeIrregular != 0:
		return entryFile
	}
	return entryRefused
}

// probeEntry Lstats name in root and reports what classify needs: its mode, and
// for a ModeIrregular entry whether it is a name surrogate (os.Readlink succeeds
// on a junction or mount point and fails on a cloud placeholder). A variable so
// a test can present a placeholder or a junction on any OS (MA-374).
var probeEntry = func(root *os.Root, name string) (fi fs.FileInfo, mode fs.FileMode, surrogate bool, err error) {
	fi, err = root.Lstat(name)
	if err != nil {
		return nil, 0, false, err
	}
	mode = fi.Mode()
	if mode&fs.ModeIrregular != 0 {
		_, rerr := root.Readlink(name)
		surrogate = rerr == nil
	}
	return fi, mode, surrogate, nil
}

// afterCheck runs between the checks and the open: the fault-injection point
// where a test swaps a link in after the check (MA-374).
var afterCheck = func(rel string) {}

// checkEntry walks segs in root, Lstat-ing each prefix: every component on the
// way must be a real folder, and the last a folder or a file the name rules
// allow. It returns the Lstat info of every prefix of segs (empty for the root)
// and the last entry's kind.
func checkEntry(root *os.Root, segs []string) ([]fs.FileInfo, entryKind, bool) {
	var infos []fs.FileInfo
	kind := entryDir
	for i := range segs {
		info, mode, surrogate, err := probeEntry(root, filepath.Join(segs[:i+1]...))
		if err != nil {
			return nil, entryRefused, false
		}
		k := classify(mode, surrogate)
		if k == entryRefused || (i < len(segs)-1 && k != entryDir) {
			return nil, entryRefused, false
		}
		// Pinned: each is compared with what is opened later (P3-8 Windows lab, B1). A
		// pin that does not take is refused: its identity would be read at the
		// comparison, from whatever is there then (WINPATH-1).
		pinned, ok := util.PinIdentity(info)
		if !ok {
			return nil, entryRefused, false
		}
		infos, kind = append(infos, pinned), k
	}
	if kind == entryFile && !fileAllowed(len(segs), segs[len(segs)-1]) {
		return nil, entryRefused, false
	}
	return infos, kind, true
}

// sameDir reports whether the directory root is bound to is the one info (an
// Lstat) described.
func sameDir(root *os.Root, info fs.FileInfo) bool {
	d, err := root.Open(".")
	if err != nil {
		return false
	}
	defer d.Close()
	got, err := d.Stat()
	return err == nil && os.SameFile(info, got)
}

// openChecked opens segs in root once checkEntry allows it. A folder must hold a
// servable index.html, its only way in. The open walks the path one folder at a
// time, each bound with OpenRoot and compared with the entry its Lstat saw, so a
// link swapped in at ANY component after that component's check is caught: the
// opened path is the path the name rules judged, not merely a file the last Lstat
// agrees with (os.Root follows a link that stays inside the archive, which would
// otherwise reach search.db or a staging page). The file itself must be the entry
// the check saw (same file, same kind), and is opened without waiting for a
// writer, so a FIFO swapped in cannot block the handler.
func openChecked(root *os.Root, segs []string) (http.File, error) {
	infos, kind, ok := checkEntry(root, segs)
	if !ok {
		return nil, fs.ErrNotExist
	}
	if kind == entryDir {
		if _, ik, ok := checkEntry(root, append(append([]string(nil), segs...), "index.html")); !ok || ik != entryFile {
			return nil, fs.ErrNotExist
		}
	}
	rel := "."
	var seen fs.FileInfo
	if len(segs) > 0 {
		rel = filepath.Join(segs...)
		seen = infos[len(infos)-1]
	}
	afterCheck(rel)
	cur := root
	release := func() {
		if cur != root {
			cur.Close()
		}
	}
	name := "."
	if len(segs) > 0 {
		for i := 0; i < len(segs)-1; i++ {
			next, err := cur.OpenRoot(segs[i])
			release()
			if err != nil {
				return nil, fs.ErrNotExist
			}
			cur = next
			if !sameDir(cur, infos[i]) {
				release()
				return nil, fs.ErrNotExist
			}
		}
		name = segs[len(segs)-1]
	}
	f, err := cur.OpenFile(name, openFlags, 0)
	release()
	if err != nil {
		return nil, fs.ErrNotExist
	}
	got, err := f.Stat()
	if err != nil || got.IsDir() != (kind == entryDir) || (seen != nil && !os.SameFile(seen, got)) {
		f.Close()
		return nil, fs.ErrNotExist
	}
	if kind == entryDir {
		return unlistedDir{f}, nil
	}
	return f, nil
}

// unlistedDir is a folder opened for http.FileServer: it can be stat'ed, so the
// server finds and serves its index.html, but it can never be listed.
type unlistedDir struct{ *os.File }

func (unlistedDir) Readdir(int) ([]fs.FileInfo, error) { return nil, fs.ErrPermission }
func (unlistedDir) ReadDir(int) ([]fs.DirEntry, error) { return nil, fs.ErrPermission }

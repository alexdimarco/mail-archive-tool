package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strings"
	"syscall"
)

// The contained writer (design P6, §4.9). Every write of an export goes through an
// *os.Root opened on the export directory the run just created, so no name, symlink
// or ".." can put a byte outside it. Files are created exclusively (O_EXCL), at
// 0600 in directories made at 0700, whatever the umask, and never chmod-ed
// afterwards (so FAT, exFAT and SMB mounts that reject a chmod still take every
// write); a file reaches its final name only by a no-replace rename made relative
// to its containing directory's handle (RR2-C10), never by a path-based rename.

// ceFile is the part of an open file the engine writes through.
type ceFile interface {
	io.Writer
	Sync() error
	Close() error
}

// ceFS is the contained writer. rootFS is the one production implementation; a test
// wraps it to inject a full disk, a swapped parent or a kill.
type ceFS interface {
	// Path is the export root's absolute path.
	Path() string
	// Mkdir creates one directory, exclusively, at 0700. An existing name is an
	// error that satisfies errors.Is(err, fs.ErrExist), never swallowed (XC13).
	Mkdir(rel string) error
	// CreateTemp creates ".mailexport-<16 hex>.tmp" exclusively at 0600 in dir.
	CreateTemp(dir string) (f ceFile, name string, err error)
	// Place gives dir/tmp the name dir/final without replacing anything: the rename
	// is relative to dir's handle, opened through the root. An existing final is
	// an error that satisfies errors.Is(err, fs.ErrExist).
	Place(dir, tmp, final string) error
	// Replace atomically renames dir/tmp over dir/final: the run's own marker and
	// the files a finishing run rewrites in its own confirmed root, nothing else.
	Replace(dir, tmp, final string) error
	Remove(rel string) error
	Lstat(rel string) (fs.FileInfo, error)
	Open(rel string) (*os.File, error)
	// SyncDir flushes a directory's entries (a no-op where the OS has no such call).
	SyncDir(rel string) error
	// Probe reports whether the root itself is still there (Stat(".")).
	Probe() error
	// Identity reads the root directory's identity tuple.
	Identity() (ceIdentity, error)
	Close() error
}

// ceJoin joins a directory and a name in the forward-slash form the root takes;
// the root itself is "" or ".".
func ceJoin(dir, name string) string {
	if dir == "" || dir == "." {
		return name
	}
	return path.Join(dir, name)
}

// rootFS is ceFS over an *os.Root.
type rootFS struct {
	root     *os.Root
	abs      string
	randHex  func(n int) string
	syncFile func(*os.File) error
}

func (r *rootFS) Path() string { return r.abs }

func (r *rootFS) Mkdir(rel string) error { return r.root.Mkdir(rel, 0o700) }

func (r *rootFS) CreateTemp(dir string) (ceFile, string, error) {
	for range 5 {
		name := copyTempPrefix + r.randHex(16) + copyTempSuffix
		f, err := r.root.OpenFile(ceJoin(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return &rootFile{File: f, sync: r.syncFile}, name, nil
	}
	return nil, "", fmt.Errorf("couldn't make a temp file name in %s", dir)
}

// rootFile is an *os.File whose Sync goes through the environment's sync seam.
type rootFile struct {
	*os.File
	sync func(*os.File) error
}

func (f *rootFile) Sync() error {
	if f.sync != nil {
		return f.sync(f.File)
	}
	return f.File.Sync()
}

func (r *rootFS) Place(dir, tmp, final string) error {
	d, err := r.dirHandle(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return renameNoReplace(d, tmp, final)
}

func (r *rootFS) Replace(dir, tmp, final string) error {
	return r.root.Rename(ceJoin(dir, tmp), ceJoin(dir, final))
}

// dirHandle opens dir through the root: a directory that was swapped for a symlink
// that leaves the root is refused here, not followed.
func (r *rootFS) dirHandle(dir string) (*os.Root, error) {
	if dir == "" || dir == "." {
		return r.root.OpenRoot(".")
	}
	return r.root.OpenRoot(dir)
}

func (r *rootFS) Remove(rel string) error { return r.root.Remove(rel) }

func (r *rootFS) Lstat(rel string) (fs.FileInfo, error) { return r.root.Lstat(rel) }

func (r *rootFS) Open(rel string) (*os.File, error) { return r.root.Open(rel) }

func (r *rootFS) SyncDir(rel string) error {
	if runtime.GOOS == "windows" {
		return nil // a directory fsync is a no-op there; the claim rests on NTFS's journal (§5.1)
	}
	if rel == "" {
		rel = "."
	}
	d, err := r.root.Open(rel)
	if err != nil {
		return err
	}
	defer d.Close()
	if r.syncFile != nil {
		return r.syncFile(d)
	}
	return d.Sync()
}

func (r *rootFS) Probe() error {
	_, err := r.root.Stat(".")
	return err
}

func (r *rootFS) Identity() (ceIdentity, error) {
	f, err := r.root.Open(".")
	if err != nil {
		return ceIdentity{}, err
	}
	defer f.Close()
	return readIdentity(f)
}

func (r *rootFS) Close() error { return r.root.Close() }

// openExportRoot opens the export directory at abs as a contained root.
func (e *CopyExportEnv) openExportRoot(abs string) (ceFS, error) {
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return e.wrapFS(&rootFS{root: root, abs: abs, randHex: e.rand, syncFile: e.syncFile}), nil
}

func (e *CopyExportEnv) wrapFS(f ceFS) ceFS {
	if e.fsWrap != nil {
		return e.fsWrap(f)
	}
	return f
}

// isExist reports whether err is the "name already exists" refusal of an exclusive
// create or a no-replace rename.
func isExist(err error) bool { return errors.Is(err, fs.ErrExist) }

// writeTempFile writes data to a temp file in dir, fsyncs it and gives it final:
// without replacing anything when replace is false, over its own earlier copy
// when it is true (finishing a run in its own confirmed root, §4.13).
func writeTempFile(f ceFS, dir, final string, data []byte, replace bool) error {
	tmp, name, err := f.CreateTemp(dir)
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		f.Remove(ceJoin(dir, name))
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		f.Remove(ceJoin(dir, name))
		return err
	}
	if err := tmp.Close(); err != nil {
		f.Remove(ceJoin(dir, name))
		return err
	}
	if replace {
		err = f.Replace(dir, name, final)
	} else {
		err = f.Place(dir, name, final)
	}
	if err != nil {
		f.Remove(ceJoin(dir, name))
		return err
	}
	return nil
}

// leaveName turns a path error into the name that failed, for a refusal or a
// failure line: the error's own text can hold the absolute destination path, which
// a listed reason never repeats.
func leaveName(err error) string {
	// What the owner reads is what happened, not the system call that met it ("openat",
	// "mkdirat"): the folder is not there, is write-protected, or the disk is full. The
	// system's own words stay in brackets after, where they say more (friction F8).
	sys := strings.TrimSpace(err.Error())
	var pe *fs.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		sys = pe.Err.Error()
	case errors.As(err, &le):
		sys = le.Err.Error()
	}
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return "the disk is full: " + sys
	case errors.Is(err, fs.ErrNotExist):
		return "the folder isn't there any more"
	case errors.Is(err, fs.ErrPermission):
		return "the folder is write-protected, or this account isn't allowed to write to it: " + sys
	}
	return sys
}

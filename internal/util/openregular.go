package util

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"sort"
)

// NotRegularError is what OpenRegularFile returns for a path that opened but whose
// descriptor is not a regular file: a pipe, a socket, a device, a directory. Nothing
// was read from it.
type NotRegularError struct {
	Path string
	Mode fs.FileMode
}

func (e *NotRegularError) Error() string {
	return fmt.Sprintf("%s is not a regular file (%s)", e.Path, ModeKind(e.Mode))
}

// ModeKind names what a file mode is, for an error message.
func ModeKind(m fs.FileMode) string {
	switch {
	case m&fs.ModeNamedPipe != 0:
		return "a pipe"
	case m&fs.ModeSocket != 0:
		return "a socket"
	case m&fs.ModeCharDevice != 0, m&fs.ModeDevice != 0:
		return "a device"
	case m&fs.ModeSymlink != 0:
		return "a link"
	case m.IsDir():
		return "a directory"
	case m&fs.ModeIrregular != 0:
		return "an irregular file"
	}
	return "not a regular file"
}

// OpenRegularFile opens path for reading and returns it only if the DESCRIPTOR is a
// regular file. It is the one way every local reader and every discovery step opens a
// file of a store it does not trust (R25, FC2): a sibling of ReadSecureFile without
// its permission check. On Unix it opens without following a link and without
// blocking (secureOpenFlags), so a pipe swapped in after a path was judged neither
// blocks the run nor is read; the descriptor is then checked, because the path's
// type at judgment time says nothing about what was opened. On Windows the open
// itself refuses nothing (there is no O_NOFOLLOW), so containment there rests on the
// judgment made before the open; a Windows reparse file that is not a link (a cloud
// placeholder, which hydrates on demand) is accepted, a directory is not.
func OpenRegularFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|secureOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	return checkRegular(f, path)
}

// OpenRegularFileIn is OpenRegularFile for the file name inside root, an os.Root
// opened on the store's resolved root: the open never resolves outside root, and a
// swap of an intermediate path component cannot move it (FC3).
func OpenRegularFileIn(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|rootOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	return checkRegular(f, root.Name()+string(os.PathSeparator)+name)
}

// ReadDirIn lists the directory name inside root, sorted by name. Like the file open
// it does not block (a pipe where a directory was judged is refused, not waited on)
// and never leaves root.
func ReadDirIn(root *os.Root, name string) ([]fs.DirEntry, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|rootOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errNotDir}
	}
	ents, err := f.ReadDir(-1)
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	return ents, err
}

var errNotDir = errors.New("not a directory")

func checkRegular(f *os.File, path string) (*os.File, error) {
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !DescriptorReadable(runtime.GOOS, fi.Mode()) {
		f.Close()
		return nil, &NotRegularError{Path: path, Mode: fi.Mode()}
	}
	return f, nil
}

// DescriptorReadable says whether an opened descriptor of this mode may be read as a
// mail file on goos: a regular file anywhere, and on Windows also an irregular file
// that is not a directory (a cloud or HSM placeholder, which Go reports as
// ModeIrregular). It is a function of goos so a table test can pin the Windows rule
// on any OS (FC1).
func DescriptorReadable(goos string, mode fs.FileMode) bool {
	if mode.IsRegular() {
		return true
	}
	return goos == "windows" && mode&fs.ModeIrregular != 0 && !mode.IsDir()
}

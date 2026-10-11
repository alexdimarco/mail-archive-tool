package app

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
)

// Reading files of an export folder another party can write. A named pipe left where
// the app expects a file blocks open(2) until a writer appears, so a planted pipe at a
// listed message file, at SHA256SUMS.txt, at README.txt or at the marker hung the
// handler (and, in the seal, held the ledger mutex) for ever (EW-AI2, EW-AI3). Every
// read of such a file goes through openRegularIn: the open does not block (O_NONBLOCK
// where the OS has it), the opened handle is checked to be a regular file, and anything
// else is refused as "not a regular file" before a byte is read.

// errNotRegular is the refusal of a read of a special file: a pipe, a device, a socket
// or a directory where a file was expected.
var errNotRegular = errors.New("not a regular file")

// openRegularIn opens rel in root for reading, only if it is a regular file.
func openRegularIn(root *os.Root, rel string) (*os.File, error) {
	f, err := root.OpenFile(rel, os.O_RDONLY|openNoBlock, 0)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, &fs.PathError{Op: "open", Path: rel, Err: errNotRegular}
	}
	return f, nil
}

// nonceFromReader reads the run id from the first line of a marker or README
// ("Run: <id>"); "" when the line is not one.
func nonceFromReader(r io.Reader) string {
	buf := make([]byte, 256)
	n, _ := r.Read(buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	id, ok := strings.CutPrefix(strings.TrimSpace(line), copyRunLinePrefix)
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

// seenFromRoot is what a probe sees of an export folder through a root already opened
// on it: its identity as the handle reads it, and the marker's and README's run id.
// Judging the handle that will be written through, not the path it was opened by, is
// what closes the gap between a probe and the open (EW-AI1).
func seenFromRoot(root *os.Root) ceFolderSeen {
	seen := ceFolderSeen{Exists: true, IsDir: true}
	if f, err := root.Open("."); err == nil {
		seen.Cur, _ = readIdentity(f)
		f.Close()
	}
	if _, err := root.Lstat(copyMarkerName); err == nil {
		seen.HasMarker = true
		seen.Nonce = nonceFrom(root, copyMarkerName)
	}
	if _, err := root.Lstat(copyReadmeName); err == nil {
		seen.HasReadme = true
		if seen.Nonce == "" {
			seen.Nonce = nonceFrom(root, copyReadmeName)
		}
	}
	return seen
}

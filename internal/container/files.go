package container

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// tempPattern names every in-flight write of this package: the `.mailarchive-*.tmp`
// shape the temp sweep reaps after a crash.
const tempPattern = ".mailarchive-*.tmp"

// ErrStore refuses a path an attacker could have planted where a record file or
// folder belongs: a link, a special file, a file where a folder belongs.
var ErrStore = errors.New("container: the encrypted record's files are damaged")

// errTooLarge is a record file over the bound its reader allows.
var errTooLarge = errors.New("container: a record file is over its size bound")

// realDir refuses a path that exists but is not a real directory; absent is fine.
func realDir(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return false, fmt.Errorf("%w: %s is %s, not a folder", ErrStore, path, util.ModeKind(fi.Mode()))
	}
	return true, nil
}

// ensureDir makes dir (one level) if absent, refusing a planted non-directory, and
// fsyncs its parent so the new entry survives power loss before a pointer names a
// file inside it.
func ensureDir(dir string) error {
	exists, err := realDir(dir)
	if err != nil || exists {
		return err
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	if _, err := realDir(dir); err != nil {
		return err
	}
	util.SyncDir(filepath.Dir(dir))
	return nil
}

// writeFile places data at root/rel atomically and durably (temp, fsync, rename
// with the Windows wait, folder fsync best effort), refusing linked folders on the
// way, and sets its mtime.
func writeFile(root, rel string, data []byte, mtime time.Time) error {
	cur := root
	if dir := path.Dir(rel); dir != "." {
		for _, part := range strings.Split(dir, "/") {
			cur = filepath.Join(cur, part)
			if err := ensureDir(cur); err != nil {
				return err
			}
		}
	}
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if err := util.WriteFileAtomic0600Temp(dst, data, tempPattern); err != nil {
		return err
	}
	if !mtime.IsZero() {
		_ = os.Chtimes(dst, mtime, mtime)
	}
	return nil
}

// readFile reads root/rel judged without following a link: a regular file (or a
// Windows cloud placeholder), its folders real folders, at most max bytes.
func readFile(root, rel string, max int64) ([]byte, os.FileInfo, error) {
	cur := root
	if dir := path.Dir(rel); dir != "." {
		for _, part := range strings.Split(dir, "/") {
			cur = filepath.Join(cur, part)
			if _, err := realDir(cur); err != nil {
				return nil, nil, err
			}
		}
	}
	file := filepath.Join(root, filepath.FromSlash(rel))
	fi, err := os.Lstat(file)
	if err != nil {
		return nil, nil, err
	}
	if fi.Mode()&fs.ModeSymlink != 0 || !util.DescriptorReadable(runtime.GOOS, fi.Mode()) {
		return nil, nil, fmt.Errorf("%w: %s is %s, not a record file", ErrStore, rel, util.ModeKind(fi.Mode()))
	}
	f, err := util.OpenRegularFile(file)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > max {
		return nil, nil, fmt.Errorf("%w: %w: %s is over its %d-byte bound", ErrStore, errTooLarge, rel, max)
	}
	return data, fi, nil
}

// readChunk reads the chunk e names and checks its size and ciphertext digest
// before anything decrypts it.
func readChunk(root string, e Entry) ([]byte, os.FileInfo, error) {
	data, fi, err := readFile(root, e.Name.Path(), int64(e.Size))
	if err != nil {
		return nil, fi, err
	}
	if len(data) != int(e.Size) || sha256.Sum256(data) != e.SHA256 {
		return nil, fi, fmt.Errorf("%w: %s is not the chunk its pointer names (size or digest differs)", ErrStore, e.Name.Path())
	}
	return data, fi, nil
}

// exists reports whether root/rel exists in any form (Lstat).
func exists(root, rel string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

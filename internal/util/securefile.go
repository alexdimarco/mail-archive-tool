package util

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// WriteFileAtomic0600 writes data to path atomically and 0600: it creates the
// parent dir (0700), writes a temp file in the same dir, fsyncs it, chmods it
// 0600, and renames it over path. A crash never leaves a torn file — the rename
// is atomic, so path is either the old bytes or the whole new bytes. Used for
// secret-bearing and config files (the token cache has its own copy for the
// {upn,token} shape).
func WriteFileAtomic0600(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed away
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename into place %s: %w", path, err)
	}
	return nil
}

// ReadSecureFile reads a small secret-bearing file with the hardened discipline a
// client secret and an OAuth token cache both need, factored into one place so the
// two callers cannot drift (design-graph-delegated D-C4):
//
//   - a regular file ONLY — a symlink, FIFO or device is refused (a FIFO would hang
//     an unattended job forever), judged both by Lstat and on the descriptor actually
//     read (no check/use gap);
//   - opened without following links and without blocking (secureOpenFlags);
//   - on Unix, not readable by group or others (mode 0o077 clear) else refused naming
//     `chmod 600`;
//   - at most maxBytes.
//
// kind names the file in error messages (e.g. "client secret file", "token cache
// file"). The bytes are returned verbatim (untrimmed); the caller trims/parses and
// decides what "empty" means. A missing file yields an error that wraps os.ErrNotExist
// so a caller can branch (the token-cache path signs in interactively instead of
// failing). maxBytes must be > 0.
func ReadSecureFile(path, kind string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("%s %s: internal: non-positive size bound", kind, path)
	}
	// Lstat first (a legible refusal for a symlink), then open ONCE without
	// following links and without blocking, and judge the descriptor we actually
	// read from.
	if fi, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s %s does not exist — create it readable only by you (chmod 600): %w", kind, path, os.ErrNotExist)
		}
		return nil, fmt.Errorf("%s %s: %w", kind, path, err)
	} else if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %s must be a regular file (not a symlink, pipe or device)", kind, path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|secureOpenFlags, 0)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", kind, path, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", kind, path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s %s must be a regular file (not a symlink, pipe or device)", kind, path)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s %s is readable by other users (mode %04o): run `chmod 600 %s`", kind, path, fi.Mode().Perm(), path)
	}
	if fi.Size() > maxBytes {
		return nil, fmt.Errorf("%s %s is %d bytes; larger than the %d-byte limit — is this the right file?", kind, path, fi.Size(), maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", kind, path, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%s %s is larger than the %d-byte limit — is this the right file?", kind, path, maxBytes)
	}
	return data, nil
}

package util

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// createTemp and syncDirFn are the temp-create and directory-sync steps of the
// atomic writer, vars so a test can observe the temp name and the sync.
var (
	createTemp = os.CreateTemp
	syncDirFn  = syncDir
)

// WriteFileAtomic0600 writes data to path atomically and 0600: it creates the
// parent dir (0700), writes a temp file in the same dir, fsyncs it, chmods it
// 0600, renames it over path, and fsyncs the dir so the rename itself survives
// power loss. A crash never leaves a torn file — the rename is atomic, so path
// is either the old bytes or the whole new bytes. Used for secret-bearing and
// config files (the token cache has its own copy for the {upn,token} shape).
func WriteFileAtomic0600(path string, data []byte) error {
	return WriteFileAtomic0600Temp(path, data, ".tmp-*")
}

// WriteFileAtomic0600Temp is WriteFileAtomic0600 with the temp file named by
// pattern (os.CreateTemp syntax), so a writer inside an archive can use the
// `.mailarchive-*.tmp` name that export.SweepOrphans reaps after a crash.
func WriteFileAtomic0600Temp(path string, data []byte, pattern string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dir %s: %w", dir, err)
	}
	tmp, err := createTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed away
	inFlight.Store(tmpName, struct{}{})
	defer inFlight.Delete(tmpName)
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
	BeforeAtomicRename(tmpName)
	if err := ReplaceRetrying(tmpName, path, renameFile, renameInUse, renameSleep); err != nil {
		return fmt.Errorf("rename into place %s: %w", path, err)
	}
	syncDirFn(dir)
	return nil
}

// inFlight are the temp files WriteFileAtomic0600Temp has made and not yet renamed or removed,
// in this process.
var inFlight sync.Map

// BeforeAtomicRename runs between an atomic write's temp file being complete and its rename: a test
// seam (a stop that lands in that moment), nothing in a real run.
var BeforeAtomicRename = func(tmp string) {}

// RemoveInFlightTemps removes, best effort, every temp file an atomic write of this process has
// made and not yet renamed: for a process about to exit at once on a signal, whose deferred
// removals will not run, so no ".tmp-…" file is left beside a settings file (the 1.5.3 re-walk's
// N19: Ctrl-C just after a delete's step 9 left one in the settings folder). A write it races
// either renames first, and the removal finds nothing, or fails its rename; the file it was
// replacing is untouched either way.
func RemoveInFlightTemps() {
	inFlight.Range(func(k, _ any) bool {
		_ = os.Remove(k.(string))
		return true
	})
}

// SyncDir fsyncs a directory (best effort) so a completed rename or removal in
// it survives power loss: syncDir for callers outside this package.
func SyncDir(dir string) { syncDir(dir) }

// syncDir fsyncs a directory so a completed rename survives power loss on
// filesystems that honour it. Errors are ignored: some filesystems (and
// Windows) do not support fsync on a directory handle, and the namespace
// guarantee does not depend on it (mirrors export's syncDir).
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	d.Close()
}

// ErrReadableByOthers is wrapped by ReadSecureFile's refusal of a file other users
// can read, so a caller can name that case and its remedy.
var ErrReadableByOthers = errors.New("readable by other users")

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
		return nil, fmt.Errorf("%s %s is readable by other users (mode %04o): run `chmod 600 %s`: %w", kind, path, fi.Mode().Perm(), path, ErrReadableByOthers)
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

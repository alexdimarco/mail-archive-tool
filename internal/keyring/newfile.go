package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/winsec"
)

// restrictToOwner makes a new file readable only by its owner; a seam, so a test can
// make it fail as a FAT or exFAT drive does on Windows.
var restrictToOwner = winsec.RestrictToOwner

// The files a recovery key or a recovery file is saved to (§5.2, §9.4), for the
// command line and the Desktop alike (the P3-3 plan, 3a). label names where the path
// came from (a flag, or "the file chosen"), what says what the file holds.

// CheckNewFileOut refuses a new file inside the archive folder out or any encrypted
// archive, or over a file that exists, before anything is asked. It returns the
// absolute path.
func CheckNewFileOut(label, what, path, out string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if util.Within(util.ResolveExisting(out), util.ResolveExisting(abs)) {
		return "", fmt.Errorf("%s %s is inside the archive folder %s; save %s outside it", label, abs, out, what)
	}
	if dir, why, _ := encmark.AtOrAbove(filepath.Dir(abs)); dir != "" {
		return "", fmt.Errorf("%s %s is inside the encrypted archive %s (%s); save %s outside every archive", label, abs, dir, why, what)
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", fmt.Errorf("%s %s already exists; choose a new file", label, abs)
	}
	return abs, nil
}

// WriteNewFile makes a NEW file owner-only and only then writes data, flushed and
// its folder synced (the P3-1a pass, SEC-09). If it cannot be restricted, it is
// removed and nothing is saved: a recovery key or file is never left where others
// may read it.
func WriteNewFile(label, what, path, out string, data []byte) (string, error) {
	abs, err := CheckNewFileOut(label, what, path, out)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%s %s already exists; choose a new file", label, abs)
		}
		return "", fmt.Errorf("%s %s: %w", label, abs, err)
	}
	if err := restrictToOwner(abs); err != nil {
		f.Close()
		os.Remove(abs)
		remedy := "choose a folder of your own"
		if strings.HasPrefix(label, "-") {
			// At the command line leaving the flag out always works: the key is still
			// shown on screen (the P3-3 pass, REG-03).
			remedy += ", or leave out " + label
		}
		return "", fmt.Errorf("%s %s could not be made readable only by you (%v); nothing was saved — %s", label, abs, err, remedy)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(abs)
		return "", fmt.Errorf("%s %s could not be written: %w", label, abs, werr)
	}
	util.SyncDir(filepath.Dir(abs))
	return abs, nil
}

// VoidFile overwrites and removes a saved recovery key whose key never took effect,
// so it is not later mistaken for the archive's recovery key (the P3-1a pass,
// SEC-01, CON-03). An error means it is still there.
func VoidFile(path string) error {
	if path == "" {
		return nil
	}
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		if fi, serr := f.Stat(); serr == nil {
			f.Write(make([]byte, fi.Size()))
			f.Sync()
		}
		f.Close()
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

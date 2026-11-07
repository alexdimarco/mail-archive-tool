package util

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// AppendLogLine appends one line to the journal at path and flushes it to disk before
// returning: the append discipline MailArchive's journals share, lifted out of the
// deletion log (archdelete's appendLog) so the restore record (restores.log,
// docs/design-restore-thunderbird.md §7.4 foldcheck note) writes exactly as it does.
//   - the folder is made 0700 if absent; the file is owner-only (0600; an existing
//     one is made so), never opened through a link, never a file other than a
//     regular one (judged by Lstat and on the opened descriptor);
//   - admit, when set, is asked with the file's size and the line's length (its
//     newline included) before anything is written, and its error refuses the append
//     (a journal's cap);
//   - a line torn by a power cut has no line break, so this line starts on a line of
//     its own and is not joined to the fragment;
//   - the line is written, fsynced, and for a new file the folder is fsynced too, so
//     its name survives power loss.
//
// line is the line without its newline; it must hold none.
func AppendLogLine(path string, line []byte, admit func(size, n int64) error) error {
	data := append(append([]byte(nil), line...), '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_, statErr := os.Lstat(path)
	if statErr == nil {
		if fi, _ := os.Lstat(path); !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|appendNoFollow, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return fmt.Errorf("%s is not a regular file", path)
	}
	if admit != nil {
		if err := admit(fi.Size(), int64(len(data))); err != nil {
			f.Close()
			return err
		}
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return err
		}
	}
	if n := fi.Size(); n > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, n-1); err != nil {
			f.Close()
			return err
		}
		if last[0] != '\n' {
			data = append([]byte{'\n'}, data...)
		}
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if statErr != nil {
		SyncDir(dir)
	}
	return nil
}

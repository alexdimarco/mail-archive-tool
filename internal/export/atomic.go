package export

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// tempPrefix names the in-flight temp files the exporter writes beside their
// final destination. Anything with this prefix that outlives its run is garbage
// (a crash mid-write) and is swept by SweepOrphans.
const tempPrefix = ".mailarchive-"

// createTemp opens a uniquely named temp file in dir. Unique names (not a
// deterministic <base>.tmp) mean two concurrent runs can never trample each
// other's in-flight file (R5).
func createTemp(dir string) (*os.File, error) {
	return os.CreateTemp(dir, tempPrefix+"*.tmp")
}

// commitTemp fsyncs the finished temp file, closes it, renames it over dst, and
// fsyncs the directory (best effort) so the rename itself is durable. On any
// error the temp is removed and dst is untouched — a previously good dst
// survives, and no partial file ever carries the final name.
func commitTemp(tmp *os.File, dst string) error {
	name := tmp.Name()
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		os.Remove(name)
		return err
	}
	syncDir(filepath.Dir(dst))
	return nil
}

// WriteFileAtomic writes data to dst via a temp sibling + rename (see
// commitTemp): used for the report and any other archive-level file.
func WriteFileAtomic(dst string, data []byte) error { return writeFileAtomic(dst, data) }

// writeFileAtomic writes data to dst via a temp sibling + rename (see commitTemp).
func writeFileAtomic(dst string, data []byte) error {
	tmp, err := createTemp(filepath.Dir(dst))
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	return commitTemp(tmp, dst)
}

// syncDir fsyncs a directory so a completed rename survives power loss on
// filesystems that honour it. Errors are ignored: some filesystems (and
// Windows) do not support fsync on a directory handle, and the namespace
// guarantee does not depend on it.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	d.Close()
}

// SweepOrphans removes what a crashed run can leave behind under out: temp files
// (tempPrefix…tmp) whose modification time is before `before` (the current run's
// start, so an overlapping run's live temps are never touched), and attachment
// zips whose sibling .html does not exist (a crash between the zip rename and
// the html rename, or a message re-exported under a new name). It returns the
// number of files removed. Nothing else is ever deleted.
func SweepOrphans(out string, before time.Time, logger *log.Logger) int {
	removed := 0
	filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		switch {
		case strings.HasPrefix(name, tempPrefix) && strings.HasSuffix(name, ".tmp"):
			info, statErr := d.Info()
			if statErr != nil || !info.ModTime().Before(before) {
				return nil
			}
		case strings.HasSuffix(name, zipSuffix):
			html := strings.TrimSuffix(path, zipSuffix) + ".html"
			if _, statErr := os.Stat(html); statErr == nil {
				return nil
			}
		default:
			return nil
		}
		if rmErr := os.Remove(path); rmErr == nil {
			removed++
			if logger != nil {
				logger.Printf("swept orphan: %s", path)
			}
		}
		return nil
	})
	return removed
}

// zipSuffix is the attachment archive's name suffix, shared with baseName users.
const zipSuffix = "-attachments.zip"

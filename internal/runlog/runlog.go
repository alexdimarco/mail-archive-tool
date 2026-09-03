// Package runlog opens the operator log a scheduled (or any -log) run writes
// to, with a size cap: an unattended job that runs for years must not grow a
// file without bound on the archive volume (design-schedule-v2 S9).
package runlog

import (
	"fmt"
	"os"
	"time"
)

// MaxSize is the size past which the log is rotated on open (one generation,
// <path>.1, is kept). A var so tests can shrink it.
var MaxSize int64 = 8 << 20

// Open opens path for appending, first rotating it to path+".1" (replacing any
// previous .1) when it already exceeds MaxSize, and writes a run header line.
// The caller closes the file.
func Open(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > MaxSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, fmt.Errorf("rotate log %s: %w", path, err)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	fmt.Fprintf(f, "==== run started %s (pid %d)\n", time.Now().Format(time.RFC3339), os.Getpid())
	return f, nil
}

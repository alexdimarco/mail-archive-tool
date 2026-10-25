package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// An encrypted archive's run keeps its plaintext working copies — the -copy-first
// snapshot of a mailbox file, the PSTs the Outlook app exports — OUTSIDE the archive,
// in MailArchive's own per-user scratch folder (job.ScratchDir, owner ruling
// P2a-C12): inside the archive they would be the mail itself in cleartext. A
// plaintext run keeps today's places, inside the archive.

// scratchRoot is MailArchive's scratch folder; a var so a test points it elsewhere.
var scratchRoot = job.ScratchDir

// archiveScratch is the scratch folder of the archive at out: the first 128 bits, in
// hex, of SHA-256 over its canonical source id. Stable across runs (an Outlook PST's
// path is its store's identity), one per archive location (two copies of one archive
// never share one), computable before the lock and without the key, and naming no path.
func archiveScratch(out string) (string, error) {
	root, err := scratchRoot()
	if err != nil {
		return "", fmt.Errorf("MailArchive's working folder: %w", err)
	}
	sum := sha256.Sum256([]byte(state.PathSourceID(out)))
	return filepath.Join(root, hex.EncodeToString(sum[:16])), nil
}

// snapshotDir is where a run's -copy-first snapshots go: the archive itself for a
// plaintext run (MA-100, the drive the operator chose and sized), the archive's
// scratch folder for an encrypted one. A run without -copy-first takes none.
func snapshotDir(opts Options) (string, error) {
	if opts.Key == nil || !opts.CopyFirst {
		return opts.Out, nil
	}
	d, err := archiveScratch(opts.Out)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "snap"), nil
}

// OutlookPSTDir is where the Outlook app's PSTs for the archive at out are exported:
// <out>/_outlook-pst for a plaintext archive (key nil; cleanup nil — the caller's
// reclaim keeps them on failure, MA-110/MA-127), and for an encrypted one the
// archive's scratch folder's _outlook-pst, emptied of any leftover now and removed by
// cleanup on every exit, failure included. The literal _outlook-pst segment is kept:
// it marks the PSTs as scratch exports (state.SourceKind).
func OutlookPSTDir(out string, key *vault.Key) (dir string, cleanup func(), err error) {
	if key == nil {
		return filepath.Join(out, "_outlook-pst"), nil, nil
	}
	d, err := archiveScratch(out)
	if err != nil {
		return "", nil, err
	}
	dir = filepath.Join(d, "_outlook-pst")
	if err := os.RemoveAll(dir); err != nil {
		return "", nil, fmt.Errorf("clear MailArchive's working folder %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create MailArchive's working folder %s: %w", dir, err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// ErrScratchSpace refuses an encrypted -copy-first run whose scratch folder's drive
// cannot hold the largest snapshot it would take.
var ErrScratchSpace = errors.New("not enough free space for MailArchive's working copies")

// scratchFree is the free space on the drive holding dir; a var so a test can fake a
// full drive.
var scratchFree = util.FreeBytes

// scratchMargin is kept free beyond the largest snapshot.
const scratchMargin = 64 << 20

// checkScratchSpace refuses, before anything is written, an encrypted -copy-first
// run whose largest regular-file input would not fit on the scratch folder's drive
// (snapshots are taken one at a time and removed after each source). It measures
// the nearest existing folder above the scratch folder: creating it would be a write.
func checkScratchSpace(dir string, inputs []string) error {
	var need int64
	for _, p := range inputs {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Size() > need {
			need = fi.Size()
		}
	}
	if need == 0 {
		return nil
	}
	need += scratchMargin
	free, short := scratchSpaceShort(dir, need)
	if !short {
		return nil
	}
	return withKind(state.FailScratch, fmt.Errorf("%w: a -copy-first copy needs about %d MB on the drive that holds %s, which has %d MB free — free space on that drive, or run without -copy-first",
		ErrScratchSpace, need>>20, dir, free>>20))
}

// scratchSpaceShort reports whether the drive that will hold dir has less than need
// bytes free, and how much it has. It measures the nearest existing folder above dir:
// creating dir would be a write. Not short when nothing can be measured — the write
// itself then reports the drive.
func scratchSpaceShort(dir string, need int64) (free uint64, short bool) {
	probe := dir
	for {
		if fi, err := os.Stat(probe); err == nil && fi.IsDir() {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return 0, false
		}
		probe = parent
	}
	free, err := scratchFree(probe)
	if err != nil || free >= uint64(need) {
		return free, false
	}
	return free, true
}

// scratchFatalFor words a run-fatal failure of the scratch folder's drive (full,
// read-only, gone) while it took a snapshot: the archive itself is not the problem.
func scratchFatalFor(dir string, err error) *RunFatalError {
	return &RunFatalError{Err: err, scratch: true, Msg: cleanText(fmt.Sprintf(
		"MailArchive's working folder %s could not hold a -copy-first copy (%v): free space on the drive that holds it or make it writable, or run without -copy-first, then re-run. %s",
		dir, err, keptNote))}
}

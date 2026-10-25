package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/util"
)

// RunHistoryName is the archive-local rolling record of recent backup runs — a bounded
// companion to the single-slot last-run record (LastRunName, which each run overwrites),
// so the dashboard and `status` can show the last several runs, not only the latest
// (MA-701). It lives beside the archive, 0600, and travels with it.
const RunHistoryName = ".mailarchive-runhistory.json"

// archiveTempPattern names the temp file of a record written inside an archive: the
// name the crash sweep (export.SweepTemps) reaps, so an interrupted write never
// leaves a copy behind for good (MA-743).
const archiveTempPattern = ".mailarchive-*.tmp"

// RunHistoryMax is how many recent runs the history keeps; older entries are dropped, so
// the file stays small and bounded.
const RunHistoryMax = 50

// RunHistoryEntry is one completed run's compact outcome, enough for a history line
// without re-reading the whole archive.
type RunHistoryEntry struct {
	When       time.Time `json:"when"`
	Status     string    `json:"status"` // RunOK | RunFailed | RunCancelled
	Exported   int       `json:"exported,omitempty"`
	Filled     int       `json:"filled,omitempty"`
	DurationMs int64     `json:"durationMs,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	Error      string    `json:"error,omitempty"`
	// ErrorKind is an encrypted archive's failure kind (FailureText says it), in
	// place of Error; absent on a plaintext entry.
	ErrorKind string `json:"errorKind,omitempty"`
}

// ReadRunHistory returns the recorded runs oldest-first, or nil for an archive with no
// history yet. A present-but-unparseable file returns an error (the caller decides
// whether to surface or ignore it).
func ReadRunHistory(out string) ([]RunHistoryEntry, error) {
	data, err := os.ReadFile(filepath.Join(out, RunHistoryName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var h []RunHistoryEntry
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return h, nil
}

// AppendRunHistory adds one completed run to the archive's history, keeping only the most
// recent RunHistoryMax entries and writing the file atomically at 0600. A history file
// that is missing or unreadable starts fresh — a lost history must never fail a run.
// Callers write under the archive lock (as recordRun does), so there is no concurrent
// writer to race the read-modify-write.
func AppendRunHistory(out string, e RunHistoryEntry) error {
	h, _ := ReadRunHistory(out) // best effort: a damaged or older-format file is replaced, not an error
	h = append(h, e)
	if len(h) > RunHistoryMax {
		h = h[len(h)-RunHistoryMax:]
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(filepath.Join(out, RunHistoryName), data, archiveTempPattern)
}

// RunHistoryEntryFrom builds a history entry from a finalized last-run record, so the
// history and the last-run slot always agree on how a run ended.
func RunHistoryEntryFrom(lr LastRun) RunHistoryEntry {
	// An encrypted archive's entry is its allow-list: when, the outcome and the
	// failure kind — no counts, duration, mode or error text (P2a-C16).
	if lr.Encrypted {
		return RunHistoryEntry{When: lr.Finished, Status: lr.Status, ErrorKind: lr.ErrorKind}
	}
	e := RunHistoryEntry{When: lr.Finished, Status: lr.Status, Exported: lr.Exported, Filled: lr.Filled, Mode: lr.Mode, Error: lr.Error}
	if !lr.Started.IsZero() && !lr.Finished.IsZero() && lr.Finished.After(lr.Started) {
		e.DurationMs = lr.Finished.Sub(lr.Started).Milliseconds()
	}
	return e
}

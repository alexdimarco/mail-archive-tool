package state

import (
	"encoding/json"
	"errors"
	"io/fs"
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

// maxRunHistorySize bounds the history a reader takes in: RunHistoryMax entries of a
// few hundred bytes each, under the same bound as one last-run record (whose error an
// entry carries), so no history a run writes nears it.
const maxRunHistorySize = maxLastRunSize

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
	// Encrypted marks an encrypted archive's entry; absent on a plaintext one.
	Encrypted bool `json:"encrypted,omitempty"`
	// Presync is the name of the run's Thunderbird sync outcome (design-presync-attended
	// §4.3, slice AS7): a plaintext entry only, shown only through presync.TagPhrase's
	// closed map. Absent when the run had no sync, on an older entry, and always on an
	// encrypted archive's (minimal never copies it, §4.4).
	Presync string `json:"presync,omitempty"`
}

// minimal is e as an encrypted archive keeps it: when, the outcome and the kind.
func (e RunHistoryEntry) minimal() RunHistoryEntry {
	return RunHistoryEntry{When: e.When, Status: e.Status, ErrorKind: e.ErrorKind, Encrypted: true}
}

// ReadRunHistory returns the recorded runs oldest-first, or nil for an archive with no
// history yet. A present-but-unparseable file returns an error (the caller decides
// whether to surface or ignore it).
func ReadRunHistory(out string) ([]RunHistoryEntry, error) {
	// Read as a planted file may be, as the last-run record is: never through a link,
	// never waiting on a pipe, never whole past its bound — a run appends to it while
	// it holds the archive lock, and the Desktop reads it on every page (gate-2
	// adversarial pass 1, as7-1).
	data, err := util.ReadRegularFile(filepath.Join(out, RunHistoryName), maxRunHistorySize)
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
	// Anyone who can write the archive folder can write this file: its strings lose
	// their control characters here, so none can forge a surface's output
	// (design-presync-attended §4.3, C-37).
	for i := range h {
		e := &h[i]
		e.Status, e.Mode, e.Error = util.StripControl(e.Status), util.StripControl(e.Mode), util.StripControl(e.Error)
		e.ErrorKind, e.Presync = util.StripControl(e.ErrorKind), util.StripControl(e.Presync)
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
	if e.Encrypted {
		// An encrypted archive keeps no older entry's text, counts or mode either (an
		// entry written before it was encrypted, F19).
		for i := range h {
			h[i] = h[i].minimal()
		}
	}
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
		return RunHistoryEntry{When: lr.Finished, Status: lr.Status, ErrorKind: lr.ErrorKind}.minimal()
	}
	e := RunHistoryEntry{When: lr.Finished, Status: lr.Status, Exported: lr.Exported, Filled: lr.Filled, Mode: lr.Mode, Error: lr.Error}
	if lr.Presync != nil {
		e.Presync = lr.Presync.Name // the sync's outcome name (design-presync-attended §4.3)
	}
	if !lr.Started.IsZero() && !lr.Finished.IsZero() && lr.Finished.After(lr.Started) {
		e.DurationMs = lr.Finished.Sub(lr.Started).Milliseconds()
	}
	return e
}

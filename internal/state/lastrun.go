package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// LastRunName is the archive-local record of the most recent run (R18).
const LastRunName = ".mailarchive-lastrun.json"

// LastRun statuses.
const (
	RunRunning   = "running"
	RunOK        = "ok"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

// LastRun is written as "running" the moment a run holds the archive lock —
// before any early return — and finalized when it ends, so `status` can tell
// a run that never finished (power loss, kill) from one that succeeded.
type LastRun struct {
	Version  int       `json:"version"`
	Status   string    `json:"status"` // running | ok | failed | cancelled
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitempty"`
	PID      int       `json:"pid"`
	Exe      string    `json:"exe,omitempty"`
	Mode     string    `json:"mode,omitempty"`
	// Job is the run's canonical export flags (e.g. -out … -auto, or -out …
	// -input …), recorded so `status` can shape a "keep it current" remedy from
	// the job that actually made this archive instead of hard-coding -auto.
	// Absent on older records and on runs with no determinable local job (a
	// Graph run); status then falls back to a generic phrase.
	Job         []string `json:"job,omitempty"`
	Exported    int      `json:"exported"`
	Filled      int      `json:"filled"`
	Fillable    int      `json:"fillable"`
	Terminal    int      `json:"terminal"`
	Unknown     int      `json:"unknown"`
	IndexErrors int      `json:"index_errors"`
	Error       string   `json:"error,omitempty"`
}

// LastRunState says what ReadLastRun found.
type LastRunState int

const (
	LastRunAbsent LastRunState = iota
	LastRunUnreadable
	LastRunPresent
)

// WriteLastRun writes the record atomically into out.
func WriteLastRun(out string, r LastRun) error {
	r.Version = 1
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(out, LastRunName)
	tmp, err := os.CreateTemp(out, ".mailarchive-lastrun-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ReadLastRun reads the record, distinguishing absent from unreadable (a torn
// or hand-edited file) so a status surface can fail closed on each.
func ReadLastRun(out string) (LastRun, LastRunState, error) {
	var r LastRun
	data, err := os.ReadFile(filepath.Join(out, LastRunName))
	if errors.Is(err, fs.ErrNotExist) {
		return r, LastRunAbsent, nil
	}
	if err != nil {
		return r, LastRunUnreadable, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, LastRunUnreadable, err
	}
	return r, LastRunPresent, nil
}

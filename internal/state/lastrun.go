package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// LastRunName is the archive-local record of the most recent run (R18).
const LastRunName = ".mailarchive-lastrun.json"

// AttentionName is a plain-text sidecar written at the archive root when a run
// finalizes failed, and removed when one finalizes ok: a desktop user must not
// have to open a JSON file (or run a command) to learn last night's backup did
// not work.
const AttentionName = "BACKUP-NEEDS-ATTENTION.txt"

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
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	updateAttentionSidecar(out, r)
	return nil
}

// updateAttentionSidecar writes (on a failed run) or removes (on an ok run) the
// human-facing BACKUP-NEEDS-ATTENTION.txt at the archive root. A running or
// cancelled record leaves it as-is — a failure stays flagged until the next
// successful run clears it. Best effort: the archive is intact without it.
func updateAttentionSidecar(out string, r LastRun) {
	path := filepath.Join(out, AttentionName)
	switch r.Status {
	case RunFailed:
		reason := r.Error
		if reason == "" {
			reason = "the run failed (no error text was recorded)"
		}
		body := fmt.Sprintf("This email backup FAILED and needs your attention.\n\n"+
			"Archive: %s\n"+
			"When:    %s\n"+
			"Reason:  %s\n\n"+
			"What to do: run\n"+
			"  mailarchive status -out %q\n"+
			"for the full posture and the remedy. This file is removed automatically\n"+
			"after the next run that succeeds.\n",
			out, r.Finished.UTC().Format(time.RFC3339), reason, out)
		_ = writeSidecarAtomic(path, []byte(body))
	case RunOK:
		_ = os.Remove(path)
	}
}

// writeSidecarAtomic writes data to path via a temp file and rename, so a torn
// write never leaves a half-written attention notice.
func writeSidecarAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".mailarchive-attention-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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

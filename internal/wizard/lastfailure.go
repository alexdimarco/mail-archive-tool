package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/job"
)

// lastFailureName is the config-dir breadcrumb a headless run leaves when it
// cannot even start — most importantly when the job's -out is absent, so there
// is no archive directory to write a last-run record into. internal/wizard
// writes it here; from SE2 internal/health is its one reader (§4.6.6), so the
// launch view no longer reads it. Without the breadcrumb such a failure is
// invisible except for a transient desktop toast and a buried .jobfail.log
// (friction #20).
const lastFailureName = "last-failure.json"

// lastFailure is one recorded headless start-failure.
type lastFailure struct {
	Version int       `json:"version"`
	Name    string    `json:"name"`
	When    time.Time `json:"when"`
	Reason  string    `json:"reason"`
}

// failureDir locates the directory the breadcrumb lives in (beside the job
// files). A package var so a test can redirect it off the real user config dir.
var failureDir = job.Dir

// writeLastFailure records f into dir atomically, owner-readable only. The
// version-1 format and the file's place are the contract health's reader keys
// on (P2); control characters in the recorded strings are stripped by the
// reader, not here.
func writeLastFailure(dir string, f lastFailure) error {
	f.Version = 1
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".last-failure-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, lastFailureName))
}

// recordHeadlessFailure writes the breadcrumb for a headless start-failure,
// best effort: a run that cannot even start must not be derailed by a failed
// breadcrumb write. Called from jobFail — the sink for a run that fails before
// it can write an archive-local last-run record.
func recordHeadlessFailure(name string, reason error) {
	dir, err := failureDir()
	if err != nil {
		return
	}
	_ = writeLastFailure(dir, lastFailure{Name: name, When: time.Now().UTC(), Reason: reason.Error()})
}

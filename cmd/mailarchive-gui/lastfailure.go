package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/job"
)

// lastFailureName is the config-dir breadcrumb a headless run leaves when it
// cannot even start — most importantly when the job's -out is absent, so there
// is no archive directory to write a last-run record into. The GUI's launch
// health view reads it; without it such a failure is invisible except for a
// transient desktop toast and a buried .jobfail.log (friction #20).
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

// writeLastFailure records f into dir atomically, owner-readable only.
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

// readLastFailure loads the breadcrumb from dir. The file is written only by
// this program, but the config dir is writable by any process running as the
// user and the record's strings are echoed into a dialog, so control characters
// are stripped on read (as ReadDescriptor does for the untrusted descriptor).
// ok is false when there is no readable record.
func readLastFailure(dir string) (rec lastFailure, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, lastFailureName))
	if err != nil {
		return lastFailure{}, false
	}
	var f lastFailure
	if err := json.Unmarshal(data, &f); err != nil {
		return lastFailure{}, false
	}
	f.Name = stripControl(f.Name)
	f.Reason = stripControl(f.Reason)
	return f, true
}

// stripControl removes control characters from an untrusted string echoed into
// a terminal or dialog.
func stripControl(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
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

// lastFailureLine is the one-line health-view message for a recorded start
// failure, naming the reason and when it happened (local time, for the person
// reading the dialog).
func lastFailureLine(f lastFailure) string {
	return fmt.Sprintf("The last scheduled run could not start: %s (%s)", f.Reason, f.When.Local().Format("2006-01-02 15:04"))
}

// showLastFailure decides whether the launch health view should surface a
// recorded headless start-failure. It does so only when there is a record (a
// non-zero time), and either the archive itself is unreachable — so its own
// last-run record cannot speak to whether the backup is healthy — or the
// failure is newer than the archive's last recorded run, since a later
// successful run supersedes an older breadcrumb.
func showLastFailure(f lastFailure, ok bool, lastRun time.Time, archiveReachable bool) bool {
	if !ok || f.When.IsZero() {
		return false
	}
	if !archiveReachable {
		return true
	}
	return f.When.After(lastRun)
}

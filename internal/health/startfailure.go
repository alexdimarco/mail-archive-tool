package health

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// startFailureName is the config-dir breadcrumb a headless run-job leaves when
// it cannot even start — most importantly when the job's -out is absent, so
// there is no archive directory to write a last-run record into. internal/wizard
// writes it (jobFail -> recordHeadlessFailure); from SE2 health is its one reader
// (§4.6.6, SC4). The name, its place (job.Dir()), its version-1 format and its
// control-stripping on read are the contract between writer and reader (P2) and
// are unchanged from the SE1 wizard-local reader.
const startFailureName = "last-failure.json"

// startFailure is one recorded headless start-failure, read from the breadcrumb.
type startFailure struct {
	Version int       `json:"version"`
	Name    string    `json:"name"`
	When    time.Time `json:"when"`
	Reason  string    `json:"reason"`
}

// failureDir locates the directory the breadcrumb lives in (beside the job
// files). A package var so a test can redirect it off the real user config dir.
var failureDir = job.Dir

// readStartFailure loads the breadcrumb from dir. The file is written only by
// this program, but the config dir is writable by any process running as the
// user and the reason is echoed into a dialog, so control characters are
// stripped on read. ok is false when there is no readable record.
func readStartFailure(dir string) (rec startFailure, ok bool) {
	data, err := os.ReadFile(filepath.Join(dir, startFailureName))
	if err != nil {
		return startFailure{}, false
	}
	var f startFailure
	if err := json.Unmarshal(data, &f); err != nil {
		return startFailure{}, false
	}
	f.Name = util.StripControl(f.Name)
	f.Reason = util.StripControl(f.Reason)
	return f, true
}

// startFailureLine is the one-line message for a recorded start-failure, naming
// the reason and when it happened (local time, for the person reading it).
func startFailureLine(f startFailure) string {
	return fmt.Sprintf("the last scheduled run could not start: %s (%s)", f.Reason, f.When.Local().Format("2006-01-02 15:04"))
}

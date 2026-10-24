package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// lastRunJobArgs securely reads the archive's last-run canonical job flags for the
// INSTALL path (not display). The record lives in the archive directory and is
// UNTRUSTED (writable by anyone who can write there — design TP-D correction 2), so
// it is read with util.ReadSecureFile — refusing a symlink, a non-regular file or an
// oversized one — rather than state.ReadLastRun's plain os.ReadFile, and only the Job
// field is taken. Absent, unreadable, odd or empty → no args.
func lastRunJobArgs(out string) ([]string, bool) {
	data, err := util.ReadSecureFile(filepath.Join(out, state.LastRunName), "last-run record", 64*1024)
	if err != nil {
		return nil, false
	}
	var lr struct {
		Job []string `json:"job"`
	}
	if json.Unmarshal(data, &lr) != nil || len(lr.Job) == 0 {
		return nil, false
	}
	return lr.Job, true
}

// reconstructLocalJob builds the schedulable job for a LOCAL archive at out from its
// own last-run record, for the Desktop's local-schedule create (design TP-D
// corrections 1-3): the record is reconstructed through the strict whitelist mapper
// (job.FromCanonicalArgs), -out is PINNED to this archive (never the record's value),
// and the job is refused unless every reconstructed -input still EXISTS and none lives
// under <out>/_outlook-pst (an Outlook-app archive's deleted scratch PSTs, which must
// not be scheduled). ok=false → the Desktop shows no create control and points at the
// wizard. It sets no pre-sync; the caller adds the toggle.
func reconstructLocalJob(out string) (job.Job, bool) {
	args, ok := lastRunJobArgs(out)
	if !ok {
		return job.Job{}, false
	}
	j, ok := job.FromCanonicalArgs(args)
	if !ok {
		return job.Job{}, false
	}
	j.Out = out // correction 2b: pin to the archive shown, never the record's -out
	scratch := filepath.Join(out, "_outlook-pst")
	for _, in := range j.Inputs {
		if underPath(scratch, in) {
			return job.Job{}, false // an Outlook-app archive's scratch PSTs (correction 3)
		}
		if _, err := os.Stat(in); err != nil {
			return job.Job{}, false // a vanished input — never schedule a broken job
		}
	}
	return j, true
}

// underPath reports whether p is dir itself or a path inside it (clean, separator-
// aware, so "/a/bc" is not treated as under "/a/b").
func underPath(dir, p string) bool {
	dir, p = filepath.Clean(dir), filepath.Clean(p)
	return p == dir || strings.HasPrefix(p, dir+string(os.PathSeparator))
}

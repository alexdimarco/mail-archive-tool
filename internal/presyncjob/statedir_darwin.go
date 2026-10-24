//go:build darwin

package presyncjob

import (
	"path/filepath"

	jobfile "mail-archive-tool/internal/job"
)

// StateDir is the per-user "presync" directory on macOS (design §4.6): the
// write-ahead record and the session lock live here, never in the archive. It is
// the job files' pinned per-user location (jobfile.Dir, MA-621) + /presync, so
// the -thunderbird-sync CLI flag and the GUI job share one state location.
func StateDir() (string, error) {
	dir, err := jobfile.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presync"), nil
}

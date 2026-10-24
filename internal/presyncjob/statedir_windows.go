//go:build windows

package presyncjob

import (
	"path/filepath"

	"mail-archive-tool/internal/instance"
)

// StateDir is the per-user, NON-ROAMING "presync" directory on Windows (design
// §4.6, PC-W5): instance.Dir (the pinned local app-data location, MA-621) +
// /presync, so a live pid in the write-ahead record never roams to another
// machine the way jobfile.Dir (roaming on Windows) would allow. The
// -thunderbird-sync CLI flag and the GUI job share it.
func StateDir() (string, error) {
	dir, err := instance.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "presync"), nil
}

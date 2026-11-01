package archdelete

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// The per-archive files beside the Desktop's settings that a scheduled backup's
// alerts keep (backupalert.StreakSuffix, backupalert.SyncAlertSuffix; a test keeps
// them in step: backupalert imports the engine, which this package must not).
const (
	alertStreakSuffix = ".alert-streak"
	syncAlertSuffix   = ".sync-alert"
	lastFailureName   = "last-failure.json"
)

// alertKey is backupalert.KeyFor: the alert files' key for a run backing up into
// out, schedule.DefaultNameFor(out), "" for no folder.
func alertKey(out string) string {
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return schedule.DefaultNameFor(out)
}

// removeNameFiles is §5.3 step 3 [C6] (and §5.2's): for each discovered entry
// name, its job file (schedule.RemoveJobFile), <name>.jobfail.log and
// <name>.older.jobfail.log; the alert streak and sync-alert files of each spelling
// of the folder (backupalert.KeyFor); last-failure.json only when the name it
// records is one of them. A file already gone counts as removed. On a failure it
// returns the file it could not remove.
func removeNameFiles(names, spellings []string) (string, error) {
	dir, err := job.Dir()
	if err != nil {
		return "MailArchive's settings folder", err
	}
	isName := map[string]bool{}
	for _, n := range names {
		if _, err := schedule.SanitizeName(n); err != nil || n != filepath.Base(n) {
			continue // never a path: a name is a file name in the settings folder
		}
		isName[n] = true
		p := filepath.Join(dir, n+".json")
		if err := schedule.RemoveJobFile(p); err != nil {
			return p, err
		}
		for _, f := range []string{n + ".jobfail.log", n + ".older.jobfail.log"} {
			if err := removeIfThere(filepath.Join(dir, f)); err != nil {
				return filepath.Join(dir, f), err
			}
		}
	}
	seen := map[string]bool{}
	for _, s := range spellings {
		k := alertKey(s)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		for _, suffix := range []string{alertStreakSuffix, syncAlertSuffix} {
			if err := removeIfThere(filepath.Join(dir, k+suffix)); err != nil {
				return filepath.Join(dir, k+suffix), err
			}
		}
	}
	lf := filepath.Join(dir, lastFailureName)
	if data, err := util.ReadSecureFile(lf, "last start failure", 64*1024); err == nil {
		var f struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(data, &f) == nil && isName[f.Name] {
			if err := removeIfThere(lf); err != nil {
				return lf, err
			}
		}
	}
	return "", nil
}

// removeIfThere removes path; one already gone is no error.
func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

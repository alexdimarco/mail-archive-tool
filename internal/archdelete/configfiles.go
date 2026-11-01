package archdelete

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/archindex"
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
// of the folder (backupalert.KeyFor), in MailArchive's settings folder and in the
// Desktop's settings folder settingsDir when it is another (a Desktop started with
// -config keeps them beside that file, backupalert.StreakPath; del-s1b-A3-04);
// last-failure.json only when the name it records is one of them. A file already
// gone counts as removed. On a failure it returns the file it could not remove.
func removeNameFiles(names, spellings []string, settingsDir string) (string, error) {
	dir, err := job.Dir()
	if err != nil {
		return "MailArchive's settings folder", err
	}
	alertDirs := []string{dir}
	if settingsDir != "" && filepath.Clean(settingsDir) != filepath.Clean(dir) {
		alertDirs = append(alertDirs, settingsDir)
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
		for _, d := range alertDirs {
			for _, suffix := range []string{alertStreakSuffix, syncAlertSuffix} {
				if err := removeIfThere(filepath.Join(d, k+suffix)); err != nil {
					return filepath.Join(d, k+suffix), err
				}
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

// ErrRowNotListed is a ForgetRow that found no row for the folder in Your archives:
// nothing was removed, and the result must not say it was (del-s1b-A3-06).
var ErrRowNotListed = errors.New("this folder is not listed in Your archives")

// forgetRowIn removes the folder's row from the list of archives in settingsDir ("":
// MailArchive's settings folder): every entry naming the same folder — the same
// spelling, the same folder by identity when both can be read, or else the same
// spelling where goos's file system ignores case — so a row spelled otherwise than
// the caller's is still removed (archindex.Forget alone compares exact text). When no
// entry names it, nothing is written and ErrRowNotListed says so.
func forgetRowIn(settingsDir, row, goos string) error {
	dir := settingsDir
	if dir == "" {
		d, err := job.Dir()
		if err != nil {
			return err
		}
		dir = d
	}
	path := filepath.Join(dir, archindex.Name)
	idx, err := archindex.ReadFile(path)
	if err != nil {
		return err
	}
	n := len(idx.Entries)
	idx = archindex.Forget(idx, row)
	kept := idx.Entries[:0]
	for _, en := range idx.Entries {
		if !sameFolder(en.Path, row, goos) {
			kept = append(kept, en)
		}
	}
	idx.Entries = kept
	if len(idx.Entries) == n {
		return ErrRowNotListed
	}
	return archindex.WriteFile(path, idx)
}

// removeIfThere removes path; one already gone is no error.
func removeIfThere(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

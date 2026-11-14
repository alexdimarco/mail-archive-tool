package gmailanswer

// The wait for another computer's tidy-up (the B3 UX walk's U2, the lead's call): a scheduled
// backup refused because the archive is at version 8 (another computer, with a newer MailArchive,
// started removing its extra Gmail copies) is a skip, not a failed run. This computer keeps when
// its scheduled backups began to wait, beside the job files (<key>.tidy-wait, never in the archive,
// which the other computer is changing), so `status` can say since when, and so the backup alert's
// own failure streak sends one alert once the wait has lasted 3 days (Due). Any backup that gets
// past the manifest removes it.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// TidyWaitSuffix ends the wait's file: <key>.tidy-wait, beside the job files. Not .json, so the
// job folder's scan never reads one as a job; uninstall, Delete and Remove from list remove it.
const TidyWaitSuffix = ".tidy-wait"

// TidyWaitAlertAfter is how long the wait lasts before its one alert: the 3 days the other
// "this has gone on" alerts use.
const TidyWaitAlertAfter = 3 * 24 * time.Hour

// TidyWait is the wait this computer keeps for one archive.
type TidyWait struct {
	Version int       `json:"version"`
	Since   time.Time `json:"since"`
}

// Due says the wait has lasted long enough at now for its alert: from then on each waiting
// scheduled backup reports to the backup alert's failure streak, which sends its one email and
// one Desktop notice per streak (and the recovery once a backup goes through).
func (w TidyWait) Due(now time.Time) bool { return now.Sub(w.Since) >= TidyWaitAlertAfter }

// TidyWaitPath is the wait's file for the archive at out.
func TidyWaitPath(out string) (string, error) {
	if strings.TrimSpace(out) == "" {
		return "", errors.New("no archive folder")
	}
	d, err := job.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, schedule.DefaultNameFor(folderKey(out, runtime.GOOS))+TidyWaitSuffix), nil
}

// ReadTidyWait is the wait kept for out; ok false when none is (absent, unreadable, damaged, or
// a time no clock could have written: after now plus a day).
func ReadTidyWait(out string) (TidyWait, bool) {
	p, err := TidyWaitPath(out)
	if err != nil {
		return TidyWait{}, false
	}
	data, err := util.ReadSecureFile(p, "tidy-up wait", maxBytes)
	if err != nil {
		return TidyWait{}, false
	}
	var w TidyWait
	if json.Unmarshal(data, &w) != nil || w.Since.IsZero() || w.Since.After(time.Now().Add(24*time.Hour)) {
		return TidyWait{}, false
	}
	return w, true
}

// NoteTidyWait records that a scheduled backup of out waited at now: the wait's start is kept
// from the first such backup. It returns the wait as kept.
func NoteTidyWait(out string, now time.Time) (TidyWait, error) {
	if w, ok := ReadTidyWait(out); ok {
		return w, nil
	}
	w := TidyWait{Version: 1, Since: now.UTC()}
	return w, writeTidyWait(out, w)
}

// ClearTidyWait removes the wait: a backup got past the manifest.
func ClearTidyWait(out string) error {
	p, err := TidyWaitPath(out)
	if err != nil {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func writeTidyWait(out string, w TidyWait) error {
	p, err := TidyWaitPath(out)
	if err != nil {
		return err
	}
	w.Version = 1
	data, err := json.Marshal(w)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(p, data, ".tidy-wait-*.tmp")
}

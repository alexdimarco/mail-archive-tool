package gmailanswer

// The wait for another computer's tidy-up (the B3 UX walk's U2, the lead's call): a scheduled
// backup refused because the archive is at version 8 (another computer, with a newer MailArchive,
// started removing its extra Gmail copies) is a skip, not a failed run. This computer keeps when
// its scheduled backups of that archive began to wait, beside the job files (<key>.tidy-wait,
// never in the archive, which the other computer is changing), so `status` can say since when, and
// so the backup alert's own failure streak sends one alert once the wait has lasted 3 days (Due).
//
// The wait is kept for the archive, by the id its last-run record carries (the skip puts that
// record back as it was), not for the folder (the B3 adversarial pass, round 2's G3): with two
// drives used in turn at one path, the second drive's backups end the second drive's wait only,
// and the first drive's wait keeps its start. The file is still one per folder, so Delete, Remove
// from list and uninstall remove every wait kept for it. An archive whose record carries no id (one
// an older MailArchive wrote) is kept under no id. Any backup of the archive that gets past the
// manifest ends its wait, whatever started it (G4): the engine calls ClearTidyWaitFor.

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
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// TidyWaitSuffix ends the wait's file: <key>.tidy-wait, beside the job files. Not .json, so the
// job folder's scan never reads one as a job; uninstall, Delete and Remove from list remove it.
const TidyWaitSuffix = ".tidy-wait"

// TidyWaitAlertAfter is how long the wait lasts before its one alert: the 3 days the other
// "this has gone on" alerts use.
const TidyWaitAlertAfter = 3 * 24 * time.Hour

// tidyWaitVersion is the wait file's format: the waits by archive id.
const tidyWaitVersion = 2

// maxTidyWaits bounds the archives one folder's file keeps a wait for (two drives of a rotation
// are two; a file planted with more is read as none).
const maxTidyWaits = 64

// TidyWait is the wait this computer keeps for one archive.
type TidyWait struct {
	// Archive is the archive's id ("" for an archive whose record carries none).
	Archive string
	Since   time.Time
}

// tidyWaitFile is the file's content: when each archive's wait began.
type tidyWaitFile struct {
	Version int                  `json:"version"`
	Waits   map[string]time.Time `json:"waits"`
}

// Due says the wait has lasted long enough at now for its alert: from then on each waiting
// scheduled backup reports to the backup alert's failure streak, which sends its one email and
// one Desktop notice per streak (and the recovery once a backup goes through).
func (w TidyWait) Due(now time.Time) bool { return now.Sub(w.Since) >= TidyWaitAlertAfter }

// TidyWaitPath is the wait's file for the archive folder out.
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

// readTidyWaits is every wait kept for the folder out; none when the file is absent, unreadable,
// damaged or of another format. A time no clock could have written (after now plus a day) is left
// out.
func readTidyWaits(out string) map[string]time.Time {
	p, err := TidyWaitPath(out)
	if err != nil {
		return nil
	}
	data, err := util.ReadSecureFile(p, "tidy-up wait", maxBytes)
	if err != nil {
		return nil
	}
	var f tidyWaitFile
	if json.Unmarshal(data, &f) != nil || f.Version != tidyWaitVersion || len(f.Waits) > maxTidyWaits {
		return nil
	}
	waits := map[string]time.Time{}
	for id, since := range f.Waits {
		if (id == "" || state.ValidArchiveID(id)) && !since.IsZero() && !since.After(time.Now().Add(24*time.Hour)) {
			waits[id] = since
		}
	}
	return waits
}

// writeTidyWaits keeps waits for out; none left removes the file.
func writeTidyWaits(out string, waits map[string]time.Time) error {
	p, err := TidyWaitPath(out)
	if err != nil {
		return err
	}
	if len(waits) == 0 {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	data, err := json.Marshal(tidyWaitFile{Version: tidyWaitVersion, Waits: waits})
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(p, data, ".tidy-wait-*.tmp")
}

// ReadTidyWaitFor is the wait kept for the archive id at out; ok false when none is.
func ReadTidyWaitFor(out, archive string) (TidyWait, bool) {
	since, ok := readTidyWaits(out)[archive]
	if !ok {
		return TidyWait{}, false
	}
	return TidyWait{Archive: archive, Since: since}, true
}

// ReadTidyWait is the wait kept for the archive the folder out holds now (its record's id).
func ReadTidyWait(out string) (TidyWait, bool) { return ReadTidyWaitFor(out, state.ArchiveID(out)) }

// NoteTidyWaitFor records that a scheduled backup of the archive id at out waited at now: the
// wait's start is kept from the first such backup. It returns the wait as kept.
func NoteTidyWaitFor(out, archive string, now time.Time) (TidyWait, error) {
	waits := readTidyWaits(out)
	if since, ok := waits[archive]; ok {
		return TidyWait{Archive: archive, Since: since}, nil
	}
	if waits == nil {
		waits = map[string]time.Time{}
	}
	if len(waits) >= maxTidyWaits {
		return TidyWait{}, errors.New("too many archives wait for this folder")
	}
	w := TidyWait{Archive: archive, Since: now.UTC()}
	waits[archive] = w.Since
	return w, writeTidyWaits(out, waits)
}

// NoteTidyWait is NoteTidyWaitFor for the archive the folder out holds now: a skip has put its
// record back as it was, so the id is the archive's own.
func NoteTidyWait(out string, now time.Time) (TidyWait, error) {
	return NoteTidyWaitFor(out, state.ArchiveID(out), now)
}

// ClearTidyWaitFor ends the wait of the archive id at out: a backup of it got past the manifest.
// The waits of other archives kept for the folder stay. Nothing is written when it had no wait.
func ClearTidyWaitFor(out, archive string) error {
	waits := readTidyWaits(out)
	if _, ok := waits[archive]; !ok {
		return nil
	}
	delete(waits, archive)
	return writeTidyWaits(out, waits)
}

// ClearTidyWait is ClearTidyWaitFor for the archive the folder out holds now.
func ClearTidyWait(out string) error { return ClearTidyWaitFor(out, state.ArchiveID(out)) }

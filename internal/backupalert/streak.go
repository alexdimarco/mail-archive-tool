package backupalert

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// The alert streak (design-desktop-settings-card 3.7, R3C-10, MA-870): with an hourly backup,
// one email per failed run is 24 a day. So a scheduled run's outcome is kept in a small file in
// the owner's config folder, one per archive, and a failure email goes once per failing streak,
// plus one recovery email when backups work again. The file is updated on EVERY scheduled run's
// outcome, before the alert options are asked and whether alerts are on or off, so a success
// ends the streak even with success emails off (the default).

// ResultRecovered is the recovery email's result, re-exported for callers.
const ResultRecovered = alert.ResultRecovered

// StreakSuffix ends an alert streak file's name: <key>.alert-streak, beside the Desktop
// settings file. Not .json, so the job folder's scan never reads one as a job; uninstall
// removes them by this suffix.
const StreakSuffix = ".alert-streak"

const maxStreakBytes = 4 * 1024

// streakRecord is the file's content.
type streakRecord struct {
	Version int       `json:"version"`
	Failing bool      `json:"failing"`
	Since   time.Time `json:"since,omitempty"` // when the failure that was told happened
}

// KeyFor is the alert streak's key for a scheduled run that backs up into the archive folder
// out: schedule.DefaultNameFor(out), the name Install and Remove give a Desktop- or wizard-made
// schedule, so the run, Install and Remove compute the same key (conditional on the same
// spelling of the folder). "" for no folder: such a run keeps no streak, and each failure sends.
func KeyFor(out string) string {
	if strings.TrimSpace(out) == "" {
		return ""
	}
	return schedule.DefaultNameFor(out)
}

// KeyForJobFile is the key for a run whose job file can't be read, so it has no folder to key
// by (rev4.1): the job file's own name without .json (job.PathFor). For a Desktop- or
// wizard-made job that name is DefaultNameFor(out), the same key as the run's other exits. A
// name that is not a valid schedule name keys nothing.
func KeyForJobFile(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".json")
	if n, err := schedule.SanitizeName(name); err == nil {
		return n
	}
	return ""
}

// StreakPath is where the streak for key is kept: beside the settings file at settingsPath.
// "" when either is unknown.
func StreakPath(settingsPath, key string) string {
	if strings.TrimSpace(settingsPath) == "" || key == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(settingsPath), key+StreakSuffix)
}

// readStreak reads the streak at path: absent is "not failing" with no error; a file that is
// there and can't be read (not ours, a link, too big, damaged) is an error, and the caller
// fails loud.
func readStreak(path string) (streakRecord, error) {
	data, err := util.ReadSecureFile(path, "backup alert record", maxStreakBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return streakRecord{}, nil
		}
		return streakRecord{}, err
	}
	var r streakRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return streakRecord{}, errors.New("backup alert record " + path + " is damaged")
	}
	return r, nil
}

// clearStreakAt removes the streak file at path; an absent one is already clear.
func clearStreakAt(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Outcome is what Report did, for the caller's Desktop notice, which follows the streak too
// (R3C-12): one per failing streak, plus a recovery notice.
type Outcome struct {
	Notify    bool // this failure starts a failing streak (or retries one whose email was not delivered)
	Recovered bool // the first success after a failing streak
}

// Report records one scheduled run's outcome (ResultFailed or ResultOK) in the alert streak for
// key, kept beside the settings file at settingsPath, and sends what is due:
//
//   - a failure sends only when the streak says "not failing"; "failing" is written only after
//     the email was delivered, or at once when no email is due (alerts off); a send error is
//     logged and the next failure tries again;
//   - the first success after a failing streak sends one recovery email, under the failure
//     option, then clears the streak; a success email, if wanted, goes otherwise;
//   - a streak file that can't be read fails loud: the failure sends, and the file is
//     rewritten; when it can't be written, each failure sends and the log says why.
//
// Best effort throughout: nothing here fails the backup.
func Report(settingsPath string, store graphconfig.SecretStore, key, out, result string, runErr error, messages int, logger *log.Logger) Outcome {
	cfg := LoadConfig(settingsPath)
	path := StreakPath(settingsPath, key)
	var prev streakRecord
	var readErr error
	if path != "" {
		if prev, readErr = readStreak(path); readErr != nil {
			logf(logger, "the backup alert record can't be read (%v); it is treated as not failing and written again", readErr)
		}
	}
	switch result {
	case ResultFailed:
		if path != "" && readErr == nil && prev.Failing {
			return Outcome{} // this failing streak was told already
		}
		o := Outcome{Notify: true}
		if cfg.ShouldSend(ResultFailed) && !deliver(cfg, store, out, ResultFailed, runErr, 0, logger) {
			return o // not marked: the next failure tries again
		}
		if path != "" {
			if werr := writeStreak(path, streakRecord{Failing: true, Since: time.Now().UTC()}); werr != nil {
				logf(logger, "the backup alert record %s could not be written (%v), so every failed backup sends its own alert until it can", path, werr)
			}
		}
		return o
	case ResultOK:
		wasFailing := path != "" && readErr == nil && prev.Failing
		if path != "" && (wasFailing || readErr != nil) {
			if cerr := clearStreakAt(path); cerr != nil {
				logf(logger, "the backup alert record %s could not be cleared (%v)", path, cerr)
			}
		}
		if wasFailing {
			if cfg.ShouldSend(ResultRecovered) {
				deliver(cfg, store, out, ResultRecovered, nil, messages, logger)
			} else if cfg.ShouldSend(ResultOK) {
				deliver(cfg, store, out, ResultOK, nil, messages, logger)
			}
			return Outcome{Recovered: true}
		}
		if cfg.ShouldSend(ResultOK) {
			deliver(cfg, store, out, ResultOK, nil, messages, logger)
		}
		return Outcome{}
	}
	FireForRun(settingsPath, store, out, result, runErr, messages, logger)
	return Outcome{}
}

// ReportDefault is Report with the default Desktop settings path and OS secret store, for a
// scheduled run (the wizard's local job) that reads the one dashboard-saved configuration.
func ReportDefault(key, out, result string, runErr error, messages int, logger *log.Logger) Outcome {
	return Report(DefaultSettingsPath(), defaultStore(), key, out, result, runErr, messages, logger)
}

// RecordAutoPause clears the streak for key — auto-pause ends the schedule, so its failing
// streak ends too — and sends the auto-pause email, which goes whatever the streak said.
func RecordAutoPause(settingsPath string, store graphconfig.SecretStore, key, out string, why error, logger *log.Logger) {
	if cerr := ClearStreak(settingsPath, key); cerr != nil {
		logf(logger, "the backup alert record could not be cleared (%v)", cerr)
	}
	FireForRun(settingsPath, store, out, ResultPaused, why, 0, logger)
}

// RecordAutoPauseDefault is RecordAutoPause with the defaults ReportDefault uses.
func RecordAutoPauseDefault(key, out string, why error, logger *log.Logger) {
	RecordAutoPause(DefaultSettingsPath(), defaultStore(), key, out, why, logger)
}

// ClearStreak removes the streak for key beside the settings file at settingsPath: a schedule's
// create and Remove clear it, so a re-created schedule's first failure sends.
func ClearStreak(settingsPath, key string) error {
	return clearStreakAt(StreakPath(settingsPath, key))
}

// ClearStreakDefault is ClearStreak beside the default Desktop settings path.
func ClearStreakDefault(key string) error { return ClearStreak(DefaultSettingsPath(), key) }

// writeStreak writes the record atomically, owner-only (0600), through the secure helper.
func writeStreak(path string, r streakRecord) error {
	r.Version = 1
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(path, data, ".alert-streak-*.tmp")
}

func logf(logger *log.Logger, format string, args ...any) {
	if logger != nil {
		logger.Printf(format, args...)
	}
}

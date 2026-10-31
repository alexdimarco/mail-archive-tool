package backupalert

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The Thunderbird-not-synced email (design-presync-attended §5.5, slice AS6; C-29, C-35,
// C-37, C-40): when a plaintext archive's scheduled backups have run for 3 days without
// syncing Thunderbird first (the archive's sync streak, state.SyncStreak, is stale), the
// scheduled run that did not fail sends ONE email for that streak. The streak's Serial names
// it; the told-file <key>.sync-alert, beside the alert streak, holds the Serial last told.
// The softer "nothing arrived" warning sends no email (owner ruling OQ-5).

// ResultSyncStale is the Thunderbird-not-synced email's result, re-exported for callers.
const ResultSyncStale = alert.ResultSyncStale

// SyncAlertSuffix ends the told-file's name: <key>.sync-alert, beside the Desktop settings
// file. Not .json, so the job folder's scan never reads one as a job; uninstall removes them
// by this suffix, and ClearStreak removes the key's one.
const SyncAlertSuffix = ".sync-alert"

// syncAlertRecord is the told-file's content: the Serial of the last streak told.
type syncAlertRecord struct {
	Version int `json:"version"`
	ToldFor int `json:"told_for"`
}

// SyncAlertPath is where the told-file for key is kept: beside the settings file at
// settingsPath. "" when either is unknown.
func SyncAlertPath(settingsPath, key string) string {
	if strings.TrimSpace(settingsPath) == "" || key == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(settingsPath), key+SyncAlertSuffix)
}

// readSyncAlert reads the told-file: absent is "nothing told" with no error; one that is
// there and can't be read (not ours, a link, too big, damaged) is an error, and the caller
// fails loud.
func readSyncAlert(path string) (syncAlertRecord, error) {
	data, err := util.ReadSecureFile(path, "Thunderbird sync alert record", maxStreakBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return syncAlertRecord{}, nil
		}
		return syncAlertRecord{}, err
	}
	var r syncAlertRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return syncAlertRecord{}, errors.New("Thunderbird sync alert record " + path + " is damaged")
	}
	return r, nil
}

// writeSyncAlert writes the told-file atomically, owner-only (0600).
func writeSyncAlert(path string, r syncAlertRecord) error {
	r.Version = 1
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600Temp(path, data, ".sync-alert-*.tmp")
}

// ReportSyncStale sends the Thunderbird-not-synced email for the plaintext archive at out,
// when its sync streak (read back after the run) is stale and that streak was not told:
//
//   - once per streak: a streak whose Serial the told-file holds was told; a clock restart
//     inside one streak keeps its Serial, so it is never told twice (C-35);
//   - the told-file is written only after the email was delivered, or at once when alerts
//     are off or the failure option is not chosen, so a failed delivery is tried again at
//     the next stale run; a told-file that can't be read fails loud: the email sends and
//     the file is rewritten;
//   - deferred: this run sent the recovery email, so this one waits for the next stale run
//     (one email per run, §5.5), and nothing is marked told.
//
// The caller calls it only for a scheduled run that did not fail. Best effort: nothing here
// fails the backup.
func ReportSyncStale(settingsPath string, store graphconfig.SecretStore, key, out string, deferred bool, logger *log.Logger) {
	s, err := state.ReadSyncStreak(out)
	if err != nil {
		logf(logger, "the Thunderbird sync record can't be read (%v), so no Thunderbird-not-synced email was considered", err)
		return
	}
	if !s.Stale() {
		return
	}
	path := SyncAlertPath(settingsPath, key)
	if path != "" {
		told, rerr := readSyncAlert(path)
		if rerr != nil {
			logf(logger, "the Thunderbird sync alert record can't be read (%v); it is treated as not told and written again", rerr)
		} else if told.ToldFor == s.Serial {
			return // this streak was told already
		}
	}
	if deferred {
		logf(logger, "the Thunderbird-not-synced email waits for the next backup: this one sent the recovery email")
		return
	}
	cfg := LoadConfig(settingsPath)
	if cfg.ShouldSend(ResultSyncStale) && !deliverSyncStale(cfg, store, out, s, logger) {
		return // not delivered: the next stale run tries again
	}
	if path != "" {
		if werr := writeSyncAlert(path, syncAlertRecord{ToldFor: s.Serial}); werr != nil {
			logf(logger, "the Thunderbird sync alert record %s could not be written (%v), so every stale backup sends its own email until it can", path, werr)
		}
	}
}

// ReportSyncStaleDefault is ReportSyncStale with the default Desktop settings path and OS
// secret store, for the wizard's local scheduled job.
func ReportSyncStaleDefault(key, out string, deferred bool, logger *log.Logger) {
	ReportSyncStale(DefaultSettingsPath(), defaultStore(), key, out, deferred, logger)
}

// deliverSyncStale sends the email for streak s under cfg, which the caller has gated. Its
// words come only from the closed maps (C-37). It reports whether the mail was handed over;
// a failure is logged, never returned.
func deliverSyncStale(cfg alert.Config, store graphconfig.SecretStore, out string, s state.SyncStreak, logger *log.Logger) bool {
	host, _ := os.Hostname()
	phrase, remedy := health.SyncAlertWords(runtime.GOOS, s)
	ev := alert.Event{Result: ResultSyncStale, Archive: out, Host: host, When: time.Now(),
		Sync: &alert.SyncStale{Since: s.FirstMissed, Missed: s.Missed, Phrase: phrase, Remedy: remedy}}
	if err := sendFn(cfg, Password(store), ev); err != nil {
		logf(logger, "the Thunderbird-not-synced email could not be sent: %v", err)
		return false
	}
	logf(logger, "the Thunderbird-not-synced email was sent to %s", cfg.To)
	return true
}

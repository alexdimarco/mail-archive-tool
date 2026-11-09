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

// syncAlertRecord is the told-file's content: the last streak told, as the Serial and the
// RecordID of the sync record it was in. Both, because a sync record removed (the sync
// switched off) or damaged starts again at Serial 1 while this file stays (the gate-2
// adversarial pass after AS5 and AS6): the new record's streak is a new streak. Tries counts the
// failed sends in a row for the streak TriedFor and TriedRecord name (the C5 round-2 pass's
// SPAM-R2-A), under the delivery mark's id TriedSince, the latest at LastFailedTry (the round-3
// pass's CAP-R3-A, the lead's calls of 2026-11-09): at state.AlertSendTries the email is tried once
// a day, and an email delivered since (another id) starts the count again.
type syncAlertRecord struct {
	Version       int       `json:"version"`
	ToldFor       int       `json:"told_for"`
	ToldRecord    string    `json:"told_record,omitempty"`
	Tries         int       `json:"tries,omitempty"`
	TriedFor      int       `json:"tried_for,omitempty"`
	TriedRecord   string    `json:"tried_record,omitempty"`
	TriedSince    string    `json:"tried_since,omitempty"`
	LastFailedTry time.Time `json:"last_failed_try,omitzero"`
}

// toldOf reports whether r names streak s as told.
func (r syncAlertRecord) toldOf(s state.SyncStreak) bool {
	return r.ToldFor == s.Serial && r.ToldRecord == s.RecordID
}

// triesOf is how many sends of streak s's email r counts as failed in a row since the delivery id
// since: 0 for any other streak, or counts made under another id.
func (r syncAlertRecord) triesOf(s state.SyncStreak, since string) int {
	if r.TriedFor != s.Serial || r.TriedRecord != s.RecordID || r.TriedSince != since {
		return 0
	}
	return min(max(0, r.Tries), state.AlertSendTries)
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
//   - once per streak: a streak whose Serial and sync record (RecordID) the told-file holds
//     was told; a clock restart inside one streak keeps its Serial, so it is never told
//     twice (C-35), and a record started again is a new streak, told in its turn;
//   - the told-file is written only after the email was delivered, or at once when alerts
//     are off or the failure option is not chosen, so a failed delivery is tried again at
//     the next stale run; a told-file that can't be read fails loud: the email sends and
//     the file is rewritten;
//   - deferred: this run sent the recovery email, so this one waits for the next stale run
//     (one email per run, §5.5), and nothing is marked told;
//   - 5 failed tries in a row, then at most one try a day, never silent for good (the C5 round-2
//     pass's SPAM-R2-A, then the round-3 pass's CAP-R3-A, the lead's calls of 2026-11-09): each
//     failed send is counted in the told-file (never as told) under the delivery mark's id; any
//     email delivered since starts the count again, so the next stale run tries. At 5 in a row the
//     email is tried once a day, and every stale run that does not get it through adds the
//     alert_send_failed WARN to the last-run record. With no told-file path there is nowhere to
//     count, and each stale run tries again, as before.
//
// The caller calls it only for a scheduled run that did not fail. Best effort: nothing here
// fails the backup. It reports whether this run delivered the email, so the Thunderbird download
// email (ReportTBBehind) waits for the next run: one email per run.
func ReportSyncStale(settingsPath string, store graphconfig.SecretStore, key, out string, deferred bool, logger *log.Logger) (sent bool) {
	s, err := state.ReadSyncStreak(out)
	if err != nil {
		logf(logger, "the Thunderbird sync record can't be read (%v), so no Thunderbird-not-synced email was considered", err)
		return false
	}
	if !s.Stale() {
		return false
	}
	path := SyncAlertPath(settingsPath, key)
	var told syncAlertRecord
	if path != "" {
		var rerr error
		if told, rerr = readSyncAlert(path); rerr != nil {
			logf(logger, "the Thunderbird sync alert record can't be read (%v); it is treated as not told and written again", rerr)
			told = syncAlertRecord{}
		} else if told.toldOf(s) {
			return false // this streak was told already
		}
	}
	cfg := LoadConfig(settingsPath)
	want := cfg.ShouldSend(ResultSyncStale)
	since, now := deliveredID(settingsPath), alertNow()
	if want && !state.AlertTryDue(told.triesOf(s, since), told.LastFailedTry, now) {
		sendGivenUp(out, "the Thunderbird-not-synced email", logger)
		return false
	}
	if deferred {
		logf(logger, "the Thunderbird-not-synced email waits for the next backup: this one sent the recovery email")
		return false
	}
	if want {
		if !deliverSyncStale(settingsPath, cfg, store, out, s, logger) {
			syncSendFailed(path, told, s, since, now, out, logger)
			return false // not delivered: the next stale run tries again, up to 5 in a row, then daily
		}
		sent = true
	}
	if path != "" {
		if werr := writeSyncAlert(path, syncAlertRecord{ToldFor: s.Serial, ToldRecord: s.RecordID}); werr != nil {
			logf(logger, "the Thunderbird sync alert record %s could not be written (%v), so every stale backup sends its own email until it can", path, werr)
		}
	}
	return sent
}

// syncSendFailed counts a failed send of streak s's email, at now under the delivery id since, in
// the told-file at path (told is what it held), keeping what it says was told; at 5 in a row it adds
// the WARN (sendGivenUp).
func syncSendFailed(path string, told syncAlertRecord, s state.SyncStreak, since string, now time.Time, out string, logger *log.Logger) {
	if path == "" {
		return
	}
	told.Tries, told.TriedFor, told.TriedRecord = min(told.triesOf(s, since)+1, state.AlertSendTries), s.Serial, s.RecordID
	told.TriedSince, told.LastFailedTry = since, now
	if err := writeSyncAlert(path, told); err != nil {
		logf(logger, "the Thunderbird sync alert record %s could not be written (%v), so this failed send was not counted", path, err)
		return
	}
	if told.Tries >= state.AlertSendTries {
		sendGivenUp(out, "the Thunderbird-not-synced email", logger)
	}
}

// ReportSyncStaleDefault is ReportSyncStale with the default Desktop settings path and OS
// secret store, for the wizard's local scheduled job.
func ReportSyncStaleDefault(key, out string, deferred bool, logger *log.Logger) bool {
	return ReportSyncStale(DefaultSettingsPath(), defaultStore(), key, out, deferred, logger)
}

// deliverSyncStale sends the email for streak s under cfg, which the caller has gated. Its
// words come only from the closed maps (C-37). It reports whether the mail was handed over;
// a failure is logged, never returned.
func deliverSyncStale(settingsPath string, cfg alert.Config, store graphconfig.SecretStore, out string, s state.SyncStreak, logger *log.Logger) bool {
	host, _ := os.Hostname()
	phrase, remedy := health.SyncAlertWords(runtime.GOOS, s)
	ev := alert.Event{Result: ResultSyncStale, Archive: out, Host: host, When: time.Now(),
		Sync: &alert.SyncStale{Since: s.FirstMissed, Missed: s.Missed, Phrase: phrase, Remedy: remedy}}
	if err := send(settingsPath, cfg, store, ev, logger); err != nil {
		logf(logger, "the Thunderbird-not-synced email could not be sent: %v", err)
		return false
	}
	logf(logger, "the Thunderbird-not-synced email was sent to %s", cfg.To)
	return true
}

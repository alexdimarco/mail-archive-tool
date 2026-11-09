package backupalert

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// The delivery mark (the C5 adversarial pass round 3's CAP-R3-A, the lead's call of 2026-11-09):
// any email MailArchive's mail server accepts (a backup alert, the recovery, either email about
// Thunderbird, or the Backup alerts card's Send test email) writes a new random id into
// DeliveredName, beside the Desktop settings file. An email about Thunderbird counts its failed
// sends together with the id that was current when they failed (the streak record's tried_since,
// the told-file's tried_since): a different id means an email got through since, so those failures
// no longer count and the next due backup tries again. That is the reset of every failed-try count,
// in every archive, without writing into any archive folder: a working mail server is never left
// silent for the rest of a streak, and the alert_send_failed WARN, added only while the count
// stands at the cap, is gone from the next backup's record.

// DeliveredName is the delivery mark's file name, beside the Desktop settings file. Uninstall
// removes it with the settings files (MA-1291's test keeps the two in step by behaviour). Not
// .json, so the job folder's scan never reads it as a job.
const DeliveredName = "alert-delivered"

// alertNow is the clock the 5-tries rule reads (each failed send's time, and whether a day has
// passed since); a var so a test moves it.
var alertNow = time.Now

// deliveredRecord is the delivery mark's content: only the id. No address, no subject, no time.
type deliveredRecord struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
}

// DeliveredPath is the delivery mark beside the settings file at settingsPath; "" when that is
// unknown.
func DeliveredPath(settingsPath string) string {
	if strings.TrimSpace(settingsPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(settingsPath), DeliveredName)
}

// deliveredID is the delivery mark's id: "" when there is no mark, or one that can't be read (not
// ours, a link, too big, damaged). "" is then the id failures are counted under, and the next
// delivered email writes a fresh mark.
func deliveredID(settingsPath string) string {
	path := DeliveredPath(settingsPath)
	if path == "" {
		return ""
	}
	data, err := util.ReadSecureFile(path, "alert delivery record", maxStreakBytes)
	if err != nil {
		return ""
	}
	var r deliveredRecord
	if json.Unmarshal(data, &r) != nil {
		return ""
	}
	return r.ID
}

// markDelivered writes a new id into the delivery mark, atomically and owner-only (0600), so every
// failed-try count from before this email no longer counts. Best effort: a mark that can't be
// written is logged, and the counts stand until a later email writes one.
func markDelivered(settingsPath string, logger *log.Logger) {
	path := DeliveredPath(settingsPath)
	if path == "" {
		return
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		logf(logger, "the alert delivery record could not be given a new id (%v)", err)
		return
	}
	data, err := json.Marshal(deliveredRecord{Version: 1, ID: hex.EncodeToString(b[:])})
	if err == nil {
		err = util.WriteFileAtomic0600Temp(path, data, ".alert-delivered-*.tmp")
	}
	if err != nil {
		logf(logger, "the alert delivery record %s could not be written (%v), so an email about Thunderbird that failed 5 times waits a day before its next try", path, err)
	}
}

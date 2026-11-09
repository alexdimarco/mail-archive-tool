package backupalert

import (
	"log"
	"os"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/state"
)

// The Thunderbird download email (docs/design-thunderbird-completeness.md §5 with its TC-20 and M10
// folds, §9.3; slice C5): when a plaintext archive's scheduled backup finds a Thunderbird account
// still behind 3 days into its streak, one "still downloading" email; when its download has not
// grown for 3 days, one "stopped" email. The run decides which streaks are due (app.Result's
// TBNotices: only accounts it found behind, of a plaintext archive whose streak record it wrote);
// this sends them, in one email per run, and only then sets each streak's flag in the archive's own
// record (app.MarkTBNoticesSent), so each kind goes at most once per streak and a failed delivery
// is tried again at the next due run. An encrypted archive gets none (O1).

// ResultTBBehind is the Thunderbird download email's result, re-exported for callers.
const ResultTBBehind = alert.ResultTBBehind

// markTBSent sets the flags in the archive's streak record; a var so a test sees a refusal.
var markTBSent = app.MarkTBNoticesSent

// ReportTBBehind sends the Thunderbird download email for notices, the run's due streaks of the
// plaintext archive at out, and reports whether an email was delivered:
//
//   - one email for every notice of the run, its words from state.TBNoticeEmail (the store folder
//     and counts; no name, no mail folder, §9.3);
//   - the flags are set only after the email was delivered, or at once when alerts are off or the
//     failure option is not chosen (as the Thunderbird-not-synced email), so a failed delivery
//     sets nothing and the next due run tries again;
//   - deferred: this run already sent an email (the recovery or the Thunderbird-not-synced one),
//     so this one waits for the next due run (one email per run, as design-presync-attended §5.5
//     keeps it) and nothing is marked;
//   - never for an encrypted archive, even if handed a notice.
//
// The caller calls it only for a scheduled run that did not fail. Best effort: nothing here fails
// the backup.
func ReportTBBehind(settingsPath string, store graphconfig.SecretStore, out string, notices []state.TBNotice, deferred bool, logger *log.Logger) bool {
	if len(notices) == 0 || app.ArchiveEncrypted(out) {
		return false
	}
	if deferred {
		logf(logger, "the Thunderbird still downloading email waits for the next backup: this one already sent an email")
		return false
	}
	sent := false
	cfg := LoadConfig(settingsPath)
	if cfg.ShouldSend(ResultTBBehind) {
		if !deliverTBBehind(cfg, store, out, notices, logger) {
			return false // not delivered: the next due run tries again
		}
		sent = true
	}
	if err := markTBSent(out, notices); err != nil {
		logf(logger, "could not record that the Thunderbird still downloading email was dealt with (%v), so the next backup may send it again", err)
	}
	return sent
}

// ReportTBBehindDefault is ReportTBBehind with the default Desktop settings path and OS secret
// store, for the wizard's local scheduled job.
func ReportTBBehindDefault(out string, notices []state.TBNotice, deferred bool, logger *log.Logger) bool {
	return ReportTBBehind(DefaultSettingsPath(), defaultStore(), out, notices, deferred, logger)
}

// deliverTBBehind sends the email for notices under cfg, which the caller has gated. It reports
// whether the mail was handed over; a failure is logged, never returned.
func deliverTBBehind(cfg alert.Config, store graphconfig.SecretStore, out string, notices []state.TBNotice, logger *log.Logger) bool {
	host, _ := os.Hostname()
	now := time.Now()
	stopped, lines, remedy := state.TBNoticeEmail(notices, now)
	ev := alert.Event{Result: ResultTBBehind, Archive: out, Host: host, When: now,
		TB: &alert.TBBehind{Stopped: stopped, Lines: lines, Remedy: remedy}}
	what := "still downloading"
	if stopped {
		what = "stopped downloading"
	}
	if err := sendFn(cfg, Password(store), ev); err != nil {
		logf(logger, "the Thunderbird %s email could not be sent: %v", what, err)
		return false
	}
	logf(logger, "the Thunderbird %s email was sent to %s", what, cfg.To)
	return true
}

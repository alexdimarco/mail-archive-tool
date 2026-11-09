package backupalert

import (
	"fmt"
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
//   - never for an encrypted archive, even if handed a notice;
//   - never for a notice whose email the archive's record already marks sent (the C5 pass's
//     SPAM-01 second guard): a notice handed again, or a run racing the mark, sends nothing;
//   - 5 failed tries in a row, then at most one try a day, never silent for good (the C5 round-2
//     pass's SPAM-R2-A, then the round-3 pass's CAP-R3-A, the lead's calls of 2026-11-09): each
//     failed send is counted in the record beside the sent flag (app.MarkTBNoticesFailed), under
//     the delivery mark's id; once any email has been delivered since (a backup alert, another
//     email about Thunderbird, or Send test email), the count starts again and the next due run
//     tries. A notice with 5 failures in a row is tried again once a day (state.AlertTryDue). A run
//     that holds one and does not get an email through adds the alert_send_failed WARN to the
//     last-run record (sendGivenUp), so the WARN is gone from the first backup after an email gets
//     through. A failure never counts as sent.
//
// An email the mail server accepted counts as sent even when the goodbye after it failed (send,
// SPAM-01). The caller calls it only for a scheduled run that did not fail. Best effort: nothing
// here fails the backup.
func ReportTBBehind(settingsPath string, store graphconfig.SecretStore, out string, notices []state.TBNotice, deferred bool, logger *log.Logger) bool {
	if len(notices) == 0 || app.ArchiveEncrypted(out) {
		return false
	}
	s, rerr := state.ReadTBStreak(out)
	if notices = unsentTBNotices(s, rerr, notices); len(notices) == 0 {
		return false
	}
	cfg := LoadConfig(settingsPath)
	want := cfg.ShouldSend(ResultTBBehind)
	since, now := deliveredID(settingsPath), alertNow()
	var spent []state.TBNotice
	if want {
		if notices, spent = triedTBNotices(s, rerr, notices, since, now); len(notices) == 0 {
			sendGivenUp(out, "the Thunderbird download email", logger)
			return false
		}
	}
	if deferred { // this run got an email through: no WARN, and the next due run tries every notice
		logf(logger, "the Thunderbird still downloading email waits for the next backup: this one already sent an email")
		return false
	}
	sent := false
	if want {
		if !deliverTBBehind(settingsPath, cfg, store, out, notices, logger) {
			if !tbSendFailed(out, notices, since, now, logger) && len(spent) > 0 {
				sendGivenUp(out, "the Thunderbird download email", logger)
			}
			return false // not delivered: the next due run tries again, up to 5 in a row, then daily
		}
		sent = true // the delivery mark is new: the spent notices go at the next due run
	}
	if err := markTBSent(out, notices); err != nil {
		logf(logger, "could not record that the Thunderbird still downloading email was dealt with (%v), so the next backup may send it again", err)
	}
	return sent
}

// unsentTBNotices is notices less those whose email the archive's streak record s already marks
// sent. A record that could not be read (err) keeps them all: toward a repeat, never silence.
func unsentTBNotices(s state.TBStreak, err error, notices []state.TBNotice) []state.TBNotice {
	if err != nil {
		return notices
	}
	var keep []state.TBNotice
	for _, n := range notices {
		if !s.Sent(n) {
			keep = append(keep, n)
		}
	}
	return keep
}

// triedTBNotices splits notices into those tried at now and those whose sends the record s counts
// as failed state.AlertSendTries times in a row since the delivery id since, with a try less than a
// day ago (state.TBStreak.TryDue). A record that could not be read (err) tries them all.
func triedTBNotices(s state.TBStreak, err error, notices []state.TBNotice, since string, now time.Time) (try, spent []state.TBNotice) {
	for _, n := range notices {
		if err == nil && !s.TryDue(n, since, now) {
			spent = append(spent, n)
		} else {
			try = append(try, n)
		}
	}
	return try, spent
}

// tbSendFailed counts the failed send of notices, at now under the delivery id since, in the streak
// record and, when any of them has now failed state.AlertSendTries times in a row, adds the WARN
// (sendGivenUp). It reports whether it did.
func tbSendFailed(out string, notices []state.TBNotice, since string, now time.Time, logger *log.Logger) bool {
	if err := markTBFailed(out, notices, since, now); err != nil {
		logf(logger, "could not record that the Thunderbird download email failed (%v); the next backup tries it again", err)
		return false
	}
	s, err := state.ReadTBStreak(out)
	if err != nil {
		return false
	}
	for _, n := range notices {
		if s.Tries(n, since) >= state.AlertSendTries {
			sendGivenUp(out, "the Thunderbird download email", logger)
			return true
		}
	}
	return false
}

// markTBFailed counts a failed send in the archive's streak record; a var so a test sees a refusal.
var markTBFailed = app.MarkTBNoticesFailed

// SendFailedCode is the WARN an email about Thunderbird that failed state.AlertSendTries times in a
// row leaves in the last-run record (SPAM-R2-A, CAP-R3-A); `status` and Status & health show it.
const SendFailedCode = "alert_send_failed"

// sendFailedMessage is SendFailedCode's sentence (the lead's words of 2026-11-09, after the C5
// round-3 pass's FRIC-R3-1: the mail server and Send test email are on the Backup alerts card).
func sendFailedMessage() string {
	return fmt.Sprintf("MailArchive tried %d times to send you an email about Thunderbird and could not. Open MailArchive Desktop, go to Backup alerts, check the mail server details, then click Send test email. Once an email gets through, MailArchive tries this one again at the next backup.", state.AlertSendTries)
}

// sendGivenUp logs that what (an email about Thunderbird) failed state.AlertSendTries times in a
// row and adds the alert_send_failed WARN to the archive's last-run record, once per run whatever
// the email.
func sendGivenUp(out, what string, logger *log.Logger) {
	logf(logger, "%s could not be sent %d times in a row, so it is tried once a day until an email gets through; check the mail server details on MailArchive Desktop's Backup alerts card, then click Send test email", what, state.AlertSendTries)
	if err := app.RecordRunWarning(out, state.RunWarning{Code: SendFailedCode, Message: sendFailedMessage()}); err != nil {
		logf(logger, "could not add the warning that %s was given up to the last-run record (%v)", what, err)
	}
}

// ReportTBBehindDefault is ReportTBBehind with the default Desktop settings path and OS secret
// store, for the wizard's local scheduled job.
func ReportTBBehindDefault(out string, notices []state.TBNotice, deferred bool, logger *log.Logger) bool {
	return ReportTBBehind(DefaultSettingsPath(), defaultStore(), out, notices, deferred, logger)
}

// deliverTBBehind sends the email for notices under cfg, which the caller has gated. It reports
// whether the mail was handed over; a failure is logged, never returned.
func deliverTBBehind(settingsPath string, cfg alert.Config, store graphconfig.SecretStore, out string, notices []state.TBNotice, logger *log.Logger) bool {
	host, _ := os.Hostname()
	now := time.Now()
	e := state.TBNoticeEmail(notices, now)
	ev := alert.Event{Result: ResultTBBehind, Archive: out, Host: host, When: now,
		TB: &alert.TBBehind{Stopped: e.Stopped, Accounts: e.Accounts, Lines: e.Lines, Remedy: e.Remedy, Which: e.Which}}
	what := "still downloading"
	if e.Stopped {
		what = "stopped downloading"
	}
	if err := send(settingsPath, cfg, store, ev, logger); err != nil {
		logf(logger, "the Thunderbird %s email could not be sent: %v", what, err)
		return false
	}
	logf(logger, "the Thunderbird %s email was sent to %s", what, cfg.To)
	return true
}

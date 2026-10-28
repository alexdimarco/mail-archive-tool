package app

import (
	"errors"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/lockfile"
)

// busyError is a run refused because another run holds the archive's lock, in plain
// words: what is using the archive and since when, said once — never the lock's own
// diagnostics (process id, host, nonce, full lock path; P3-8 friction, B15). It still
// matches lockfile.ErrHeld.
type busyError struct {
	msg string
	err error
}

func (e *busyError) Error() string { return e.msg }
func (e *busyError) Unwrap() error { return e.err }

// busyRefusal words err, a refused lock at lockPath, for a person when another run
// holds it; any other lock error is kept as it is. The CLI refuses a busy archive
// before it asks anything; this covers a run that starts in between.
func busyRefusal(lockPath string, err error) error {
	if !errors.Is(err, lockfile.ErrHeld) {
		return err
	}
	since := ""
	if t, ok := lockStarted(lockPath); ok {
		since = " (started " + t.Local().Format("15:04 on 2006-01-02") + ")"
	}
	// The lock file is named (R5's refusal names it), so a person who finds it knows
	// what it is; the holder's own line is not.
	return &busyError{msg: "another backup or check is using this archive" + since + " — it is in use until that finishes, holding its lock file " + lockfile.Name + "; try again when it finishes", err: err}
}

// QuietSkipWindow is how long a scheduled backup that finds another holder of the archive's
// lock skips quietly (design-desktop-settings-card 3.7, R3C-12): an hourly backup that starts
// while the previous one, a verify or a rebuild is still going is not a failure. A holder that
// started longer ago than this may be hung, so the start counts as a failure and is alerted
// once per failing streak, never hidden for good.
const QuietSkipWindow = 24 * time.Hour

// ScheduledLockSkip reports whether a SCHEDULED run that ended with err — the archive's lock
// held by another run (lockfile.ErrHeld) — skips quietly: only while that holder started less
// than QuietSkipWindow before now (a few minutes of clock difference allowed). line says so in
// plain words and names the real holder (lockfile.HolderVerb). Quiet means no failure, alert,
// source streak or history entry: the lock refusal comes before any of them is written (Run and
// RunGraph take the lock before their record). A holder whose start can't be read is not
// proven recent, so the start counts as a failure.
func ScheduledLockSkip(out string, err error, now time.Time) (line string, quiet bool) {
	if err == nil || !errors.Is(err, lockfile.ErrHeld) {
		return "", false
	}
	lockPath := filepath.Join(out, lockfile.Name)
	started, ok := lockStarted(lockPath)
	if !ok {
		return "", false
	}
	if age := now.Sub(started); age < -5*time.Minute || age >= QuietSkipWindow {
		return "", false
	}
	return "This scheduled backup was skipped: " + holderWords(lockfile.HolderVerb(lockPath)) + " started at " +
		started.Local().Format("15:04 on 2006-01-02") + " and is still using this archive. The next scheduled backup runs as usual.", true
}

// holderWords names what holds the archive's lock, from the verb its holder line records, in
// plain words. An encrypted archive's lock names no verb (A18), so it reads as another task.
func holderWords(verb string) string {
	switch verb {
	case "export":
		return "an earlier backup of this archive"
	case "graph":
		return "a Microsoft 365 capture into this archive"
	case "verify":
		return "a check of this archive (verify)"
	case "reindex", "rebuild":
		return "a rebuild of this archive's search index"
	case "pause":
		return "pausing this archive"
	case "programcopies":
		return "a change to the copies of MailArchive kept in this archive"
	}
	return "another MailArchive task"
}

// lockStarted reads when the lock's holder started, from its holder line.
func lockStarted(lockPath string) (time.Time, bool) {
	for _, field := range strings.Fields(lockfile.Holder(lockPath)) {
		if v, ok := strings.CutPrefix(field, "started="); ok {
			t, err := time.Parse(time.RFC3339, v)
			return t, err == nil
		}
	}
	return time.Time{}, false
}

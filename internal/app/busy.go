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
// before it asks anything; this covers a run that starts in between. Only a backup
// (Run, RunGraph) is refused through it.
func busyRefusal(lockPath string, err error) error {
	if !errors.Is(err, lockfile.ErrHeld) {
		return err
	}
	return &busyError{msg: busyWords(lockPath, time.Now()), err: err}
}

// busyWords is a backup refused because another run holds the archive, told as the skip it is
// (design-plain-failure-words, MA-1147; the owner's report of 2026-11-05): what is running and
// since when, that this backup was skipped and nothing is wrong, and what to do — never
// "failed", and none of the lock's diagnostics. A holder that started QuietSkipWindow or more
// ago may be stuck (a scheduled backup then counts the start as a failure, R3C-12), so the words
// say so and how to end it instead of "nothing is wrong".
func busyWords(lockPath string, now time.Time) string {
	verb := lockfile.HolderVerb(lockPath)
	since, stuck := "", false
	if t, ok := lockStarted(lockPath); ok {
		since = " (started " + t.Local().Format("15:04 on 2006-01-02") + ")"
		stuck = now.Sub(t) >= QuietSkipWindow
	}
	// The lock file is named (S25, R5: the refusal names it and the holder), so a person who
	// finds it knows what it is; the holder's own line is not.
	lock := "(its lock file, " + lockfile.Name + ")"
	lead := busyLead(verb) + since + " — this backup was skipped"
	if stuck {
		return lead + ". It started more than a day ago and still keeps this archive in use " + lock + ", so it may be stuck: if it should have finished by now, restart the computer to end it; the next backup then runs as usual."
	}
	switch verb {
	case "delete":
		return lead + "; nothing is wrong. Until it finishes, it keeps this archive in use " + lock + "."
	case "pause":
		return lead + "; nothing is wrong. Until it finishes, it keeps this archive in use " + lock + "; a paused archive takes no backups until it is resumed."
	}
	return lead + "; nothing is wrong. Try again when it finishes: until then it keeps this archive in use " + lock + "."
}

// busyLead names what holds the archive's lock to the person whose backup it skipped, from the
// verb its holder line records; an encrypted archive's lock names no verb (A18).
func busyLead(verb string) string {
	switch verb {
	case "export":
		return "Another backup of this archive is already running"
	case "graph":
		return "A Microsoft 365 backup into this archive is already running"
	case "verify":
		return "A check of this archive (verify) is running"
	case "reindex", "rebuild":
		return "A rebuild of this archive's search index is running"
	case "pause":
		return "This archive is being paused"
	case "programcopies":
		return "A change to the copies of MailArchive kept in this archive is running"
	case "delete":
		return "A delete of this archive is running"
	case RestoreThunderbirdVerb:
		return "A restore of this archive into Thunderbird is running"
	}
	return "Another MailArchive task is using this archive"
}

// BusySkipped reports whether err is a backup skipped because another run holds the archive,
// with the words to tell it by: a surface says these, never as a failure (MA-1147).
func BusySkipped(err error) (string, bool) {
	var b *busyError
	if err == nil || !errors.As(err, &b) {
		return "", false
	}
	return b.msg, true
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

// RestoreThunderbirdVerb is the verb a restore into Thunderbird names in an unencrypted
// archive's lock (design-restore-thunderbird §7.1 RT-18); an encrypted archive's names none.
const RestoreThunderbirdVerb = "restore-thunderbird"

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
	case "delete":
		// A scheduled backup that meets a running delete (MA-1045, the G2 settle pass's LOW-1).
		return "a delete of this archive"
	case RestoreThunderbirdVerb:
		// A scheduled backup that meets a restore of an unencrypted archive into
		// Thunderbird (design-restore-thunderbird §7.1 RT-18, MA-1127).
		return "a restore of this archive into Thunderbird"
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

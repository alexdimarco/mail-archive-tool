package app

import (
	"errors"
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

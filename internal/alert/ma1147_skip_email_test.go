package alert

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1147, R18, S29
// A scheduled backup that met another run still holding the archive after a day is alerted as
// before (the failing streak's email), but its email says what happened first — the backup was
// skipped because the archive is still in use — never "FAILED" over a reason that says "this
// backup was skipped" (friction review of plain-failure-words, Windows lab 2026-11-07: the email
// read "MailArchive backup FAILED" above "… this backup was skipped. … it may be stuck"). When it
// is sent is unchanged. Positive twin first: a run that failed outright keeps "FAILED".
func TestASkippedBackupEmailSaysItWasSkipped(t *testing.T) {
	var c Config
	subject, body := c.subjectAndBody(Event{Result: ResultFailed, Archive: `C:\Mail\Archive`, Error: "the archive drive is full"})
	if subject != `MailArchive backup FAILED: C:\Mail\Archive` || !strings.Contains(body, "MailArchive backup result: FAILED") {
		t.Fatalf("the positive twin: %q\n%s", subject, body)
	}
	words := "Another backup of this archive is already running (started 11:31 on 2026-11-06) — this backup was skipped. It started more than a day ago and still keeps this archive in use (its lock file, .mailarchive.lock), so it may be stuck: if it should have finished by now, restart the computer to end it; the next backup then runs as usual."
	subject, body = c.subjectAndBody(Event{Result: ResultFailed, Busy: true, Archive: `C:\Mail\Archive`, Error: words})
	assure.Reached(t, subject == `MailArchive backup skipped, the archive is still in use: C:\Mail\Archive` &&
		strings.Contains(body, "MailArchive backup result: SKIPPED (THE ARCHIVE IS STILL IN USE)") && strings.Contains(body, "Reason:\r\n"+words) &&
		!strings.Contains(subject+body, "FAILED"),
		"the skipped backup's email: "+subject+"\n"+body)
	if !(Config{Enabled: true, OnFailure: true}).ShouldSend(ResultFailed) {
		t.Error("a skipped backup's email is no longer sent when failures are")
	}
}

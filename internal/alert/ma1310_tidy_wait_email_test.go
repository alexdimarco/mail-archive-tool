package alert

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1310, R45
// The one alert sent once scheduled backups have waited 3 days for another computer's tidy-up of
// the archive says they wait, never "FAILED" (the B3 UX walk's U2). Positive twin first: a run
// that failed outright keeps "FAILED".
func TestATidyUpWaitEmailSaysTheBackupsWait(t *testing.T) {
	var c Config
	if subject, _ := c.subjectAndBody(Event{Result: ResultFailed, Archive: "/m/A", Error: "full"}); subject != "MailArchive backup FAILED: /m/A" {
		t.Fatalf("positive twin: %q", subject)
	}
	subject, body := c.subjectAndBody(Event{Result: ResultFailed, Tidying: true, Archive: "/m/A", Error: "Another computer started tidying this archive"})
	assure.Reached(t, subject == "MailArchive backups are waiting for another computer: /m/A" &&
		strings.Contains(body, "SKIPPED (ANOTHER COMPUTER IS TIDYING THE ARCHIVE)") && !strings.Contains(subject+body, "FAILED"),
		"the tidy-up wait's email: "+subject+"\n"+body)
}

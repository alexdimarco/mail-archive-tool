package alert

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1146, R18, S48
// The alert email of a partial run says what happened first: its subject and its result line
// say the backup did not save everything (never "FAILED"), and its reason is the run's words.
// When it is sent is unchanged (a partial run is still a failure to the alert streak). Positive
// twin first: a run that failed outright keeps "FAILED".
func TestThePartialRunEmailSaysItDidNotSaveEverything(t *testing.T) {
	var c Config
	subject, body := c.subjectAndBody(Event{Result: ResultFailed, Archive: "/Mail/Archive", Error: "the archive drive is full"})
	if subject != "MailArchive backup FAILED: /Mail/Archive" || !strings.Contains(body, "MailArchive backup result: FAILED") {
		t.Fatalf("the positive twin: %q\n%s", subject, body)
	}
	words := "Not everything was saved. 4 messages could not be saved because of a mistake in MailArchive — your mail is fine."
	subject, body = c.subjectAndBody(Event{Result: ResultFailed, Partial: true, Archive: "/Mail/Archive", Error: words})
	assure.Reached(t, subject == "MailArchive backup did not save everything: /Mail/Archive" &&
		strings.Contains(body, "MailArchive backup result: DID NOT SAVE EVERYTHING") && strings.Contains(body, "Reason:\r\n"+words) && !strings.Contains(subject+body, "FAILED"),
		"the partial run's email: "+subject+"\n"+body)
	if !(Config{Enabled: true, OnFailure: true}).ShouldSend(ResultFailed) {
		t.Error("a partial run's email is no longer sent when failures are")
	}
}

package wizard

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/state"
)

// covers: MA-1295, R45
// The Run now result of the backup that switched an archive's Gmail account on says so, and that
// every computer then needs 1.8 (the B3 friction review's F6: the run that switched it on said
// nothing about Gmail). Positive twin first: a run that switched nothing on says nothing about
// Gmail.
func TestRunNowSaysWhenItSwitchedGmailOn(t *testing.T) {
	r := app.Result{ManifestBefore: 1, ManifestSize: 2}
	r.Stats.Exported = 1
	if s := runNowSummary(r); strings.Contains(s, "Gmail") {
		t.Fatalf("positive twin: a run that switched nothing says %q", s)
	}
	r.Gmail.On = []string{"imap.gmail.com"} // on before this run: still nothing to say
	if s := runNowSummary(r); strings.Contains(s, "Gmail") {
		t.Fatalf("a run of an archive already on says %q", s)
	}
	r.Gmail.Switched = []string{"imap.gmail.com"}
	s := runNowSummary(r)
	if !strings.Contains(s, "\n"+state.GmailSwitchedLine+" "+state.GmailNeedsLine) {
		t.Fatalf("the run that switched Gmail on does not say so: %q", s)
	}
}

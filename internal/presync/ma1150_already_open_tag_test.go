package presync

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1150, R18, S29
// The Recent backups tag of a sync that found Thunderbird already open says what the backup did
// with it — left it alone — so the row no longer reads as if the open Thunderbird made the
// backup fail (the owner's report of 2026-11-05: "Failed — partial: … — Thunderbird was already
// open"). Positive twin first: a sync that ran keeps "Thunderbird synced first".
func TestTheAlreadyOpenTagSaysThunderbirdWasLeftAlone(t *testing.T) {
	if TagPhrase(OutcomeOK.Name) != "Thunderbird synced first" {
		t.Fatalf("the positive twin: %q", TagPhrase(OutcomeOK.Name))
	}
	got := TagPhrase(OutcomeAlreadyRunning.Name)
	assure.Reached(t, got == "Thunderbird was already open, so the backup left it alone" && !strings.Contains(got, "not synced"),
		"the already-open tag: "+got)
}

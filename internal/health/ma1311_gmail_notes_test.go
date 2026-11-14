package health

import (
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1311, R45
// The Gmail lines are health's own reasons, so `status`, `status -json` and the Desktop's Status &
// health view carry them from one source (the B3 UX walk's U7): the count line, with the 1.8 line
// after it (U9), under the code gmail_counts; one waiting line per store under gmail_waiting, its
// store name shown with control characters and bidirectional overrides taken out (the B3
// adversarial pass's A5). Neither changes the posture, and an encrypted archive has neither.
// Positive twin first: an archive with no Gmail store has no Gmail reason.
func TestTheGmailLinesAreHealthsReasons(t *testing.T) {
	base := Input{Out: t.TempDir(), LastRunState: state.LastRunAbsent}
	if r := Assess(base, time.Now()); slices.Contains(r.Codes, "gmail_counts") || slices.Contains(r.Codes, "gmail_waiting") {
		t.Fatalf("positive twin: %v", r.Codes)
	}
	in := base
	in.GmailCounts = state.GmailCounts{Stores: 1, Messages: 3}
	in.GmailWaiting = []string{"imap.gmail.com", "a\u009b2Jb", "imap.‮gmail.com"}
	plain := Assess(base, time.Now())
	r := Assess(in, time.Now())
	if r.Posture != plain.Posture {
		t.Fatalf("the Gmail lines changed the posture: %s, was %s", r.Posture, plain.Posture)
	}
	joined := strings.Join(r.Reasons, "\n")
	if !strings.Contains(joined, in.GmailCounts.Line()+" "+state.GmailNeedsLine) || !slices.Contains(r.Codes, "gmail_counts") {
		t.Fatalf("no count line with the 1.8 line:\n%s", joined)
	}
	if n := strings.Count(strings.Join(r.Codes, " "), "gmail_waiting"); n != 3 || !strings.Contains(joined, state.GmailWaitingLine("imap.gmail.com")) {
		t.Fatalf("the waiting lines (%d):\n%s", n, joined)
	}
	if strings.ContainsAny(joined, "\u009b‮") {
		t.Fatalf("a store name's controls reached the reasons:\n%q", joined)
	}
	in.Encrypted = true
	if r := Assess(in, time.Now()); slices.Contains(r.Codes, "gmail_counts") || slices.Contains(r.Codes, "gmail_waiting") {
		t.Fatalf("an encrypted archive's reasons name Gmail: %v", r.Codes)
	}
}

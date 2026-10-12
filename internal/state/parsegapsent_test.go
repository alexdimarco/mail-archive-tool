package state

import (
	"strings"
	"testing"
)

// covers: MA-348, R1, R7
// F3 of the MF-S2 friction review, re-verification (cells OW-3, OW-11). A fillable corrupt-part row
// said a complete export cures a part that is "cut short or damaged", and only the boundary-less
// row said that a message malformed when it was sent stays listed. For a part whose base64 was
// damaged AS SENT (S3b) every export is the same, so the row's advice was futile and said nothing of
// it; the run summary and `status` carried the caveat and the row did not. The row says it now, for
// the mailbox-file reader (the report). Positive twin: a part the source itself would not deliver
// (terminal) and the words of a row whose writer does not know the source are unchanged.
func TestACorruptPartRowSaysAMessageDamagedAsSentStaysListed(t *testing.T) {
	const stays = "damaged when it was sent stays listed"
	attachment := "contract.pdf.partial — the data is cut short or damaged after 519 bytes: only the readable start is kept"
	body := CorruptBodyDetailPrefix + "1 is cut short or damaged after 24 bytes: only what could be decoded is shown"

	for _, detail := range []string{attachment, body} {
		r := RecordRowRemedyFor(GapKindCorruptPart, ClassFillable, detail, OriginalInSource)
		if !strings.Contains(r, stays) || !strings.Contains(r, "complete export") {
			t.Errorf("the fillable corrupt-part remedy %q does not say both what cures a cut message and that one damaged as sent stays listed", r)
		}
		if t2 := RecordRowRemedyFor(GapKindCorruptPart, ClassTerminal, detail, OriginalInSource); strings.Contains(t2, "stays listed") {
			t.Errorf("a terminal remedy %q talks of a message staying listed", t2)
		}
	}
	// The kind-only words (the writer does not know the source) say nothing of a mailbox file.
	if r := RecordRowRemedy(GapKindCorruptPart, ClassFillable); strings.Contains(r, "stays listed") || strings.Contains(r, "mailbox file") {
		t.Errorf("the kind-only remedy changed: %q", r)
	}
}

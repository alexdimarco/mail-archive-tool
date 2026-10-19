package health

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-348, R1, R18
// ESC-MS2-2 (MS2-AI5, MS2-AO10). The "cannot decode" count and its explanation are true of
// what they count. A record whose only parse gap is a fidelity fallback (kept as an earlier
// version read it) or an identity stub has no part in the attachments zip and no part this
// version failed to decode, and the same record is already counted under "Fallback"; an unsplit
// multipart is shown as text. design-mime-fidelity 4.9 specifies the count (records with at least
// one parse gap) and the sentence ("parts this version cannot decode (kept as received; see the
// report)"), and MA-348 and three surfaces' tests pin both, so correcting either is the owner's
// decision with the friction review of the S2 wording (MC39).
func TestStatusDoesNotCallAFallbackRecordUndecodableOrKeptInTheZip(t *testing.T) {
	t.Skip("ESC-MS2-2: the cannot-decode count includes fallback, identity-stub and unsplit records and says each is kept as received in the attachments zip; the count and sentence are specified by design 4.9 and pinned by MA-348's tests, so changing them is the owner's decision with the S2 friction review (invariants R1, R18)")
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.Undecodable, in.FidelityFallback = 2, 2 // every record with a parse gap is one kept as an earlier version read it
	text := strings.Join(Summary(in, Assess(in, now)), "\n")
	if strings.Contains(text, "cannot decode") || strings.Contains(text, "kept as received in the attachments zip") {
		t.Errorf("two messages kept as an earlier version read them are called undecodable and said to be in the zip:\n%s", text)
	}
}

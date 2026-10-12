package source

import (
	"strings"
	"testing"
)

// Skip encoders of the MF-S2 adversarial pass: confirmed findings whose fix needs an owner
// decision. Each body is real, so un-skipping it is red for the reason the finding gives, and
// green once the decision lands (CLAUDE.md: remove the skip, and it is a permanent guard).

// covers: MA-349, R10
// ESC-MS2-1 (MS2-AO3, the frozen half). The route's peak heap on a part whose header is
// millions of tiny fields stays a small multiple of the message. The walker's own read is
// capped before it happens (TestAPartHeaderOverTheCapIsNotReadFirst); the frozen v1 replay,
// which runs first on every message, parses the header whole, so the route peaks at about 120
// times the message (measured x120.4 on a 4 MiB message). Bounding the replay (refusing it above the cap, and giving the message
// its stub identity) moves the frozen identity path for such a message: a drift decision under
// MC28 that needs an owner ruling, with MS1-AO2 (the same replay's quadratic cost).
func TestTheFrozenReplayDoesNotReadAnOversizedPartHeaderWhole(t *testing.T) {
	t.Skip("ESC-MS2-1: the frozen v1 replay parses a part header of millions of tiny fields whole (about 120 times the message); bounding it moves the frozen identity path for that message and needs an owner ruling under MC28 (invariants R10, R27, MA-331; MS1-AO2 is the same replay)")
	useFidelity(t)
	data := hostile(`multipart/mixed; boundary="bd"`,
		"--bd\r\nContent-Type: text/plain\r\n\r\nfirst\r\n--bd\r\n"+strings.Repeat("X:\r\n", 1<<20)+"Content-Type: text/plain\r\n\r\nthe padded part\r\n--bd--\r\n")
	peak := peakHeapOf(func() { safeParseMessage(data) })
	if limit := 8 * uint64(len(data)); peak > limit {
		t.Errorf("the route's peak heap is %d KiB, %.1f times the %d KiB message; want at most 8 times", peak>>10, float64(peak)/float64(len(data)), len(data)>>10)
	}
}

// covers: MA-349, R1
// ESC-MS2-4 (MS2-AI6). "Fidelity contains v1" does not let a body v1 showed readably become
// mojibake because some note elsewhere in the message says a body was read differently on
// purpose. A text/plain part with an unknown charset and a (wrong) base64 transfer encoding is
// plain text that v1 shows as it stands; the walker decodes the base64 and shows garbage, and
// the check waives the verbatim-body rule for every body when any body-decoding note exists
// (design 4.2 step 3a: "no body-decoding note touches it" is read as "no note in the message").
// Narrowing the waiver to the body a note touches changes the check the design specifies, so
// it is the owner's decision; until then the page shows the garbage and the gap says the text
// is cut short.
func TestABodyV1ReadReadablyIsNotReplacedByGarbageUnderAnUnrelatedNote(t *testing.T) {
	t.Skip("ESC-MS2-4: holdsV1 waives the verbatim body check for every body when any body-decoding note exists (design 4.2 step 3a, MC4 'a text-decoding defect of the mojibake class' is the stated residual); narrowing it changes the specified check and needs an owner decision (invariants R1, MA-349)")
	useFidelity(t)
	const text = "Please call me about the invoice on 555-0100."
	m := safeParseMessage(hostile("text/plain; charset=\"x-unknown\"\r\nContent-Transfer-Encoding: base64", text+"\r\n"))
	requireNoStub(t, "a mislabelled body", m)
	if !strings.Contains(m.PlainBody, text) {
		t.Errorf("v1 shows %q, the page shows %q (fell back %t, notes %v)", text, m.PlainBody, m.FellBack, noteCodes(m))
	}
}

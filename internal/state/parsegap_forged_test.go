package state

import "testing"

// covers: MA-348, R1, R26
// MS2-AO4. Only the six parse-gap kinds are parse-gap labels. A label that begins with the
// separator but names another kind (one a sender's file name or an edited manifest made up)
// is an ordinary label: not a parse gap, not "cannot decode", not a kind of its own in the
// report, and not a record that probeImproves finds "improved" on every run.
func TestOnlyKnownKindsAreParseGapLabels(t *testing.T) {
	for _, kind := range []string{GapKindUndecodedPart, GapKindStructureLimit, GapKindFidelityFallback,
		GapKindIdentityStub, GapKindCorruptPart, GapKindUnsplitMultipart} {
		label := ParseGapLabel(kind, "the detail")
		if k, d, ok := ParseGapParts(label); !ok || k != kind || d != "the detail" {
			t.Errorf("ParseGapParts(%q) = %q, %q, %t; want the kind and detail back", label, k, d, ok)
		}
		if !IsParseGapLabel(label) {
			t.Errorf("the label of kind %s is not a parse-gap label", kind)
		}
	}
	for _, forged := range []string{
		"\x00verified\x00terminal\x00Inbox",
		"\x00evil\x00detail",
		"\x00\x00no kind",
		"\x00corrupt-part", // no detail separator
		"\x00Corrupt-Part\x00case",
	} {
		if k, _, ok := ParseGapParts(forged); ok || IsParseGapLabel(forged) {
			t.Errorf("%q is read as a parse gap of kind %q", forged, k)
		}
		r := Record{Terminal: []string{forged}}
		if r.hasParseGap() {
			t.Errorf("a record carrying %q counts as having a parse gap", forged)
		}
	}
}

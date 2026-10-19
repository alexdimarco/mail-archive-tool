package state

import (
	"strings"
	"testing"
)

// covers: MA-327, MA-334, R1, R7, R26
// Every parse-gap kind has its own words in the shared table (owner ruling §13.11, X2):
// a why and a remedy for each class it can carry that are not the generic fallbacks, so
// no report row appears with a bare code. A fillable kind says to download the complete
// message; a terminal one says there is nothing to do and where the bytes are; every one
// says the original is kept.
func TestEveryParseGapKindHasItsOwnWords(t *testing.T) {
	const genericWhy = "this version of mailarchive does not know why this record has a gap"
	const genericRemedy = "run again; if it is listed again, keep the original, because this version of mailarchive does not know the cause"
	kinds := map[string][]string{ // kind -> the classes it can carry
		GapKindUndecodedPart:    {ClassTerminal},
		GapKindStructureLimit:   {ClassTerminal},
		GapKindFidelityFallback: {ClassTerminal},
		GapKindIdentityStub:     {ClassTerminal},
		GapKindCorruptPart:      {ClassFillable, ClassTerminal},
		GapKindUnsplitMultipart: {ClassFillable, ClassTerminal},
	}
	for kind, classes := range kinds {
		if why := RecordRowWhy(kind); why == genericWhy || why == "" {
			t.Errorf("%s has no why of its own", kind)
		}
		for _, class := range classes {
			r := RecordRowRemedy(kind, class)
			if r == genericRemedy || r == "" {
				t.Errorf("%s (%s) has no remedy of its own", kind, class)
			}
			if !strings.Contains(r, "the original is kept") {
				t.Errorf("%s (%s): the remedy %q does not say the original is kept", kind, class, r)
			}
			if class == ClassFillable && !strings.Contains(r, "download the complete message") {
				t.Errorf("%s: a fillable remedy %q does not say how to cure it", kind, r)
			}
			if class == ClassTerminal && kind != GapKindFidelityFallback && !strings.Contains(r, "nothing to do") {
				t.Errorf("%s: a terminal remedy %q does not say there is nothing to do", kind, r)
			}
		}
	}
	if RecordRowRemedy(GapKindCorruptPart, ClassFillable) == RecordRowRemedy(GapKindCorruptPart, ClassTerminal) {
		t.Error("corrupt-part has one remedy for a fillable and a terminal source")
	}
}

package tbengine

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1124, R40
// The result's words for messages Thunderbird had marked deleted before they were backed
// up (design §4.6; the MA-1095 lab's wording note L2, settled 2026-11-07): one is "1 of
// them was deleted … keeps it hidden … The archive keeps it.", more are "N of them were
// deleted … keeps them hidden", with N in commas; none says nothing.
func TestTheDeletedInThunderbirdWordsAgreeWithTheCount(t *testing.T) {
	p := &Plan{}
	words := func(n int) string {
		r := &Result{Write: &tbrestore.WriteResult{Outcome: tbrestore.OutDone, Words: "Restored."}, DeletedInTB: n}
		return assure.Reached(t, p.resultWords(r), "the result's words")
	}
	one := words(1)
	if !strings.Contains(one, "1 of them was deleted in Thunderbird before it was backed up, so Thunderbird keeps it hidden, and may remove it") ||
		!strings.HasSuffix(one, "The archive keeps it.") || strings.Contains(one, "were deleted") {
		t.Errorf("one message: %q", one)
	}
	for n, num := range map[int]string{2: "2", 1234: "1,234"} {
		many := words(n)
		if !strings.Contains(many, num+" of them were deleted in Thunderbird before they were backed up, so Thunderbird keeps them hidden") ||
			!strings.HasSuffix(many, "The archive keeps them.") {
			t.Errorf("%d messages: %q", n, many)
		}
	}
	if w := words(0); strings.Contains(w, "deleted in Thunderbird") {
		t.Errorf("none: %q", w)
	}
}

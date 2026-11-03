package desktop

import "testing"

// covers: MA-1069, R36, S41
// The 1.5.6 friction review's note A (docs/review-delete-friction-1.5.6.md): Dismiss on a delete's
// result whose record was not saved asks "Close without saving the deletion record?", and the cursor
// sat on Close without saving, so a keyboard user pressing Enter twice lost the record. Run under
// Node (testdata/pagescript/harness.js, keepItFocused): the question opens with the cursor on Keep
// it, and Enter twice keeps the result and saves its record; Close without saving, clicked, still
// closes a result without its record (positive twin first).
func TestTheCloseWithoutSavingQuestionOpensOnKeepIt(t *testing.T) {
	if passed := runPageScenario(t, "keepItFocused"); len(passed) != 3 {
		t.Errorf("the keepItFocused scenario passed %d of its 3 checks: %v", len(passed), passed)
	}
}

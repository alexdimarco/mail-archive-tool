package desktop

import "testing"

// covers: MA-1065, R36, S41
// The 1.5.6 friction review's note B (docs/review-delete-friction-1.5.6.md, walked on the Windows VM
// 3 November 2026): after Dismiss on a delete's result and after Remove from list, the page reloads
// and the browser left the view at the Microsoft 365 card, away from Your archives, three times in
// the walk. Run under Node (testdata/pagescript/harness.js, backToArchives): the page drawn again
// after either is at Your archives with its heading focused, so a keyboard user carries on from
// there; a page drawn with nothing asked is not moved (positive twin first); and dismissing the
// result of a delete of the page's own archive still draws the page again from its top (N12).
func TestThePageComesBackToYourArchivesAfterDismissAndRemoveFromList(t *testing.T) {
	if passed := runPageScenario(t, "backToArchives"); len(passed) != 4 {
		t.Errorf("the backToArchives scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
}

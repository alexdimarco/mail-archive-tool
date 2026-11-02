package desktop

import (
	"strings"
	"testing"
)

// covers: MA-1020, R36, S41
// The delete's page layout, as the 1.5.3 friction review's backlog asks (B13, B14), run on the
// page script under Node: Save this plan says "Plan saved as FILE, in your browser's downloads.
// Nothing was deleted." on screen and sends nothing; a delete's result is brought into view;
// Remove from list's words are said again at the top of Your archives after the reload it
// starts, once (they showed for 1.5 seconds). Positive twin within: the page drawn a second time
// says them no more.
func TestThePlanSavedTheResultInViewAndRemoveFromListsWordsKept(t *testing.T) {
	if passed := runPageScenario(t, "pageLayout"); len(passed) != 3 {
		t.Errorf("the pageLayout scenario passed %d of its 3 checks: %v", len(passed), passed)
	}
}

// covers: MA-1020, R36, S41
// The rows of Your archives keep their folder readable (B14): the path keeps at least 16rem and
// the buttons wrap under it, rather than squeezing it to a few characters beside six buttons;
// "Delete not finished: click Delete again" takes a line of its own; the typed name's folder name
// never breaks across lines. Positive twin first: the row is still a wrapping flex row.
func TestTheRowsKeepTheirPathReadable(t *testing.T) {
	page := getOverview(t, Config{})
	if !strings.Contains(page, ".archive-row{display:flex;align-items:center;gap:10px;flex-wrap:wrap;") {
		t.Fatal("the positive twin: an archive row is no longer a wrapping flex row")
	}
	for _, rule := range []string{
		".archive-row .archive-path{flex:1 1 16rem;min-width:0;overflow-wrap:anywhere}",
		".archive-row[data-delete-unfinished]>.row-state{flex-basis:100%}",
		".del-name{white-space:nowrap;",
	} {
		if !strings.Contains(page, rule) {
			t.Errorf("the Overview's style lacks %q", rule)
		}
	}
	if !strings.Contains(dashboardJS, "delEl('span',p.name,'mono del-name')") {
		t.Error("the prompt does not put the folder's name in the del-name span")
	}
}

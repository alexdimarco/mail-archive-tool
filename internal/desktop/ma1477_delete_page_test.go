package desktop

import (
	"html"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// Delete waits for you, the page's half (docs/design-1.7.5-fixes.md §3.1, §3.2; the owner's report
// of 2026-11-19 and his ruling of 2026-11-20, Q1): the served dashboard.js run under Node against the
// page-script harness (testdata/pagescript/harness.js), as MA-986's tests are. The server's half is
// MA-1475 and MA-1476; the live walk is the friction review's and the Windows lab's.

// covers: MA-1477, MA-1476, R36, S41
// The panel opens at once, before the check ends, with the checking line, its minutes, the folder
// and the box; the name can be typed meanwhile; Delete when the check finishes is enabled by the
// row's name and changes the line to "checks once more, then deletes"; at the reply an armable plan
// with the plan's name typed is sent with armed: true and the panel's own row; once sent the panel
// has no Cancel and says the delete can no longer be stopped, the minutes counting through both
// checks; the result shows as before. When the check finds something, or only the name does not
// match, or the server refuses the armed delete, the plan is shown and nothing is deleted unseen.
// Positive twin first: before the delete is sent, Cancel is there and closes the panel with "Nothing
// was deleted.".
func TestTheDeletePanelOpensAtOnceAndDeletesWhenTheCheckFinishes(t *testing.T) {
	for scenario, want := range map[string]int{"armedWaits": 9, "armedFound": 3} {
		t.Run(scenario, func(t *testing.T) {
			if passed := runPageScenario(t, scenario); len(passed) != want {
				t.Errorf("%s passed %d of its %d checks: %v", scenario, len(passed), want, passed)
			}
		})
	}
}

// covers: MA-1477, MA-986, R36, S41
// An armed panel is marked data-delete-waiting from its click, so another row's Delete… leaves it
// and each check's reply fills its own panel, never one opened meanwhile (MA-986, amended); reloads
// the page starts are held while it waits, and go ahead once it ends without a delete. An un-armed
// panel made stale by a later Delete… (one that first asks about a record not saved, so no panel is
// closed for it) still opens nothing when its check answers, and its plan is discarded: only the
// last Delete… clicked opens (MA-986). Positive twin first: with nothing clicked meanwhile, the reply
// fills its panel.
func TestAnArmedPanelSurvivesAnotherDeleteAndHoldsReloads(t *testing.T) {
	for scenario, want := range map[string]int{"armedSurvives": 4, "staleAsk": 2} {
		t.Run(scenario, func(t *testing.T) {
			if passed := runPageScenario(t, scenario); len(passed) != want {
				t.Errorf("%s passed %d of its %d checks: %v", scenario, len(passed), want, passed)
			}
		})
	}
}

// covers: MA-1477, MA-1475, R36, S41
// A plan the page no longer shows is discarded (§3.1): Cancel, a panel closed for another, a check's
// late reply to a panel cancelled or closed meanwhile, and the page going away (pagehide, a keepalive
// request) post its token to /api/archive/delete/discard. Positive twin first: a panel still shown
// discards nothing.
func TestAPlanNoLongerShownIsDiscarded(t *testing.T) {
	if passed := runPageScenario(t, "discards"); len(passed) != 6 {
		t.Errorf("the discards scenario passed %d of its 6 checks: %v", len(passed), passed)
	}
}

// covers: MA-1477, MA-1475, R36, S41
// A delete refused because a backup saved mail after its check (changed) shows its held result, and
// the row's panel opens again by itself, checking, the typed name put back; once the new plan shows
// one click deletes. Positive twin first: a refusal not marked changed asks for nothing more.
func TestAChangedPlanIsAskedForAgainWithTheNameKept(t *testing.T) {
	if passed := runPageScenario(t, "changedAgain"); len(passed) != 3 {
		t.Errorf("the changedAgain scenario passed %d of its 3 checks: %v", len(passed), passed)
	}
}

// covers: MA-1477, R36, S41
// Once a delete is sent by Delete permanently (the fold check's R2), the panel has no Cancel and says
// the delete can no longer be stopped; a reply refusing it before it began gives Cancel back with its
// words; a lost connection does not. Positive twin first: before it is sent, Cancel is there.
func TestOnceSentADeleteOffersNoCancel(t *testing.T) {
	if passed := runPageScenario(t, "sentNoCancel"); len(passed) != 3 {
		t.Errorf("the sentNoCancel scenario passed %d of its 3 checks: %v", len(passed), passed)
	}
}

// covers: MA-1477, MA-982, R36, S41
// The row's Delete… button carries data-delete-name, the last element of the row's path as the page
// is drawn (no folder is read), which only enables Delete when the check finishes early; the page's
// prompt before the check is planView's typeLine for that name, word for word; and the tooltip says
// the owner's exception to "you see what would be removed first" (X14, amended). Positive twin
// first: the row is drawn with its Delete… offered.
func TestTheRowCarriesItsNameAndTheTooltipSaysTheException(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Mail Archive")
	del := rowButton(rowFor(t, e.page(), dir), "data-delete-archive")
	assure.Reached(t, del != "" && !strings.Contains(del, " disabled"), "the positive twin: the row offers Delete…")
	if want := `data-delete-name="` + filepath.Base(dir) + `"`; !strings.Contains(del, want) {
		t.Errorf("the Delete… button does not carry %s: %s", want, del)
	}
	tip := "Permanently delete this archive's files. MailArchive first checks what would be removed, and you type the folder's name to confirm. You see that list before anything is deleted, unless you choose Delete when the check finishes and nothing in the list needs your decision."
	if !strings.Contains(html.UnescapeString(del), `title="`+tip+`"`) {
		t.Errorf("the Delete… button's tooltip does not say the exception: %s", html.UnescapeString(del))
	}
	if strings.Contains(html.UnescapeString(del), "You see what would be removed first") {
		t.Error("the tooltip still says you always see what would be removed first, which an armed delete makes untrue")
	}
	// The prompt before the check is planView's, for the row's name.
	tl, _ := planView(&archdelete.DeletePlan{Name: "\x00"})["typeLine"].(string)
	parts := strings.Split(tl, "\x00")
	if len(parts) != 2 {
		t.Fatalf("planView's typeLine does not name the folder once: %q", tl)
	}
	if want := `"` + parts[0] + `"+name+"` + parts[1] + `"`; !strings.Contains(jsFunc(t, "delTypeLine"), want) {
		t.Errorf("the page's prompt before the check is not planView's typeLine (%s): %s", want, jsFunc(t, "delTypeLine"))
	}
}

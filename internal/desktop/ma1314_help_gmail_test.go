package desktop

import (
	"html"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1314, R45
// The guide "How to use MailArchive" says what the Gmail question is, in its Thunderbird part (the
// B3 UX walk's U11): asked once, on the archive's row; Yes cannot be undone and then needs 1.8 on
// every computer; Not now changes nothing and the row does not ask again, Settings showing its one
// line and "Turn on…"; a terminal backup asks too, a scheduled one never. It names the controls as
// the page draws them. Positive twin first: the Thunderbird part is there, on every platform.
func TestTheGuideSaysWhatTheGmailQuestionIs(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		page := html.UnescapeString(string(helpCard(goos, false, true, true)))
		if !strings.Contains(page, `<details id="helpThunderbird">`) {
			t.Fatalf("positive twin (%s): no Thunderbird part", goos)
		}
		at := strings.Index(page, `<p id="helpGmail">`)
		if at < 0 || at < strings.Index(page, `<details id="helpThunderbird">`) {
			t.Fatalf("%s: the guide's Thunderbird part has no Gmail paragraph", goos)
		}
		p := page[at : at+strings.Index(page[at:], "</p>")]
		for _, must := range []string{state.GmailQuestionHeading, "once", "cannot be undone", "1.8 or newer", "Not now changes nothing",
			gmailCollapsedLine, gmailCollapsedButton, "terminal asks it too", "scheduled backup never asks"} {
			if !strings.Contains(p, must) {
				t.Fatalf("%s: the Gmail paragraph does not say %q:\n%s", goos, must, p)
			}
		}
	}
}

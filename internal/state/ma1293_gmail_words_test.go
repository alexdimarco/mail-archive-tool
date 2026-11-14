package state

import (
	"strings"
	"testing"
)

// covers: MA-1293, R45
// The Gmail question's words and the tidy-up words, as the B3 friction review proposed them
// (its texts (a) and (b), F4), with the builder's truth adjustments recorded in the design's build
// notes: emails "may be" missing "because Thunderbird does not always keep a copy in each folder"
// (§2.1: Thunderbird keeps a copy in some folders and not others), and the tidy-up "started" and
// is finished "on that computer" (a stopped merge does not finish by itself, §24). The question
// says what is wrong now, that nothing is deleted, the 1.8 requirement and that Yes cannot be
// undone; it has a heading and two named buttons. Each is one constant.
func TestTheGmailQuestionSaysWhatIsWrongWhatYesDoesAndThatItCannotBeUndone(t *testing.T) {
	for name, c := range map[string]struct{ got, want string }{
		"heading":   {GmailQuestionHeading, "Gmail: show every folder?"},
		"body":      {GmailSwitchQuestion(false), "Some Gmail emails may be missing from Sent Mail and your labels in this archive, because Thunderbird does not always keep a copy in each folder. Yes lists each email in every folder it is in, and saves new emails only once. Nothing is deleted. After Yes, every computer that backs up into this archive needs MailArchive 1.8 or newer. This cannot be undone."},
		"encrypted": {GmailSwitchQuestion(true), GmailQuestion + " Emails already saved twice stay as they are: MailArchive cannot tidy an encrypted archive."},
		"yes":       {GmailYesButton, "Yes, show every folder"},
		"not now":   {GmailNotNowButton, "Not now"},
		"needs 1.8": {GmailNeedsLine, "Every computer that backs up into this archive needs MailArchive 1.8 or newer."},
		// The B3 UX walk's U8 words (MA-1313), "open it" left out: this computer can still read it.
		"tidy-up":   {NewerFormatWords(8), "Another computer started tidying this archive with a newer MailArchive (it removes extra copies of Gmail emails). Nothing is lost. Until it finishes, this computer cannot back up into it or add it. To finish it, open MailArchive on that computer, or install the newest MailArchive here and click Resume tidy-up on this archive's card."},
		"newer (9)": {NewerFormatWords(9), NewerWords},
	} {
		if c.got != c.want {
			t.Errorf("%s: %q\nwant %q", name, c.got, c.want)
		}
	}
	for _, must := range []string{"missing from Sent Mail", "Nothing is deleted.", "1.8 or newer", "cannot be undone"} {
		if !strings.Contains(GmailQuestion, must) {
			t.Errorf("the question does not say %q", must)
		}
	}
}

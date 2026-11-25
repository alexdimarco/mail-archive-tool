package wizard

import (
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-947
// The wizard's question for a Thunderbird IMAP account says truly why it offers a full
// offline download (the guide's truth review, finding T8, and the Windows lab of
// 2026-10-31). It said "Thunderbird stores mail locally only on demand", which is untrue
// as worded: in the lab, Thunderbird 153.6.0esr with a Gmail IMAP account added with no
// settings changed had no offline_download line in prefs.js (so Thunderbird's default
// applied) and saved over 36,000 messages to disk in the first half hour; "Keep messages
// for this account on this computer" is on by default for a new account. What is true is
// that Thunderbird may not have downloaded every message yet: it downloads over time, and
// only while it is open. The offer stays, with what "Yes, prepare" does. Positive twin
// first: the question still ends with the offer, and its two answers are unchanged.
func TestTheIMAPQuestionSaysWhatThunderbirdDoes(t *testing.T) {
	q := imapPrepQuestion
	assure.Reached(t, q, "the IMAP question")
	if !strings.HasPrefix(q, "This looks like an IMAP account.\n\n") || !strings.HasSuffix(q, "\n\nPrepare a full offline download first?") {
		t.Fatalf("the positive twin: the question's lead or its offer changed:\n%s", q)
	}
	for _, want := range []string{
		"Thunderbird may not have downloaded every message to this computer yet: it downloads mail over time, and only while it is open.",
		"A message or attachment it has not downloaded yet is not in this export; a later run adds it once Thunderbird has it.",
		"Yes, prepare makes sure Thunderbird keeps every message on this computer, then waits while it downloads.",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("the question does not say %q:\n%s", want, q)
		}
	}
	for _, gone := range []string{"only on demand", "stores mail locally"} {
		if strings.Contains(q, gone) {
			t.Errorf("the question still says %q", gone)
		}
	}

	// Amended 2026-11-25 (MA-1502, the owner's QA "Yes, name it"; nothing weakened): the question
	// shown is imapPrepQuestionFor's, which names the account and its number before these same words
	// and names the account in the offer. Each pin above holds for it too: the lead after the account
	// line, every sentence, the offer at the end, the old words absent.
	named := imapPrepQuestionFor(prepAsk{name: "qzsam.walker@gmail.com", i: 1, n: 3})
	if !strings.HasPrefix(named, "Account 1 of 3: qzsam.walker@gmail.com\n\nThis looks like an IMAP account.\n\n") || !strings.HasSuffix(named, "\n\nPrepare a full offline download of qzsam.walker@gmail.com first?") {
		t.Fatalf("the positive twin, named: the question's lead or its offer changed:\n%s", named)
	}
	for _, want := range []string{
		"Thunderbird may not have downloaded every message to this computer yet: it downloads mail over time, and only while it is open.",
		"A message or attachment it has not downloaded yet is not in this export; a later run adds it once Thunderbird has it.",
		"Yes, prepare makes sure Thunderbird keeps every message on this computer, then waits while it downloads.",
	} {
		if !strings.Contains(named, want) {
			t.Errorf("the named question does not say %q:\n%s", want, named)
		}
	}
	for _, gone := range []string{"only on demand", "stores mail locally"} {
		if strings.Contains(named, gone) {
			t.Errorf("the named question still says %q", gone)
		}
	}

	// Structural: prepareThunderbirdGUI's dialog shows these words (named, through the question
	// seam since MA-1502; before, zenity.Question with imapPrepQuestion), with its two answers
	// unchanged.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, wired := range []string{"choice := questionDialog(\n\t\timapPrepQuestionFor(ask),", `zenity.OKLabel("Yes, prepare")`, `zenity.CancelLabel("No, export what's cached")`} {
		if !strings.Contains(string(src), wired) {
			t.Errorf("prepareThunderbirdGUI does not show the words: %q is gone from wizard.go", wired)
		}
	}
	if strings.Contains(string(src), "only on demand") {
		t.Error("wizard.go still says Thunderbird stores mail only on demand")
	}
}

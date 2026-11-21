package state

import (
	"strings"
	"testing"
)

// covers: MA-1478, R18
// An encrypted archive's gone-mailbox sentences give advice that works (the 1.7.5 friction review's
// F1): the wizard refuses to set up an encrypted archive's folder again without its key, so the note
// never sends the person there; and Run now is greyed out while the archive is locked, so each
// sentence says to unlock it on its Encryption card first. Positive twin first: each says the date
// and that Run now shows which mailbox.
func TestTheEncryptedGoneSentencesGiveAdviceThatWorks(t *testing.T) {
	const day = "Nov 20, 2026"
	for name, s := range map[string]string{"WARN": MailboxGoneText(day), "short WARN": MailboxGoneShortText(day), "note": MailboxGoneNoteText(day)} {
		if !strings.Contains(s, "since "+day) || !strings.Contains(s, "click Run now to see which one") {
			t.Fatalf("positive twin, %s: the date and Run now: %q", name, s)
		}
		if !strings.Contains(s, "unlock it on its Encryption card in MailArchive Desktop") && !strings.Contains(s, "Unlock it on its Encryption card in MailArchive Desktop") {
			t.Errorf("%s does not say to unlock it first: %q", name, s)
		}
		if strings.Contains(s, "wizard") || strings.Contains(s, "set the archive up again") {
			t.Errorf("%s sends the person to the wizard, which refuses an encrypted archive's folder: %q", name, s)
		}
	}
	if n := MailboxGoneNoteText(day); !strings.Contains(n, "This line stays while the archive's backup still lists that mailbox.") {
		t.Errorf("the note says how long it stays: %q", n)
	}
}

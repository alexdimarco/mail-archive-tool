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
		// Amended 2026-11-20 by the 1.7.5 re-verify (RF-04), at the same strength: "to see which one,
		// unlock this archive on its Encryption card …, then click Run now" (it was "unlock it …, then
		// click Run now to see which one").
		if !strings.Contains(s, "since "+day) || !strings.Contains(s, "then click Run now") {
			t.Fatalf("positive twin, %s: the date and Run now: %q", name, s)
		}
		if !strings.Contains(s, "to see which one, unlock this archive on its Encryption card in MailArchive Desktop") && !strings.Contains(s, "To see which one, unlock this archive on its Encryption card in MailArchive Desktop") {
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

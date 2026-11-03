package delmark_test

import (
	"testing"

	"mail-archive-tool/internal/delmark"
)

// covers: MA-1049, R36
// The words for a delete the marker knows while this computer's deletion log could not be read
// (the slice-2 pass's LOW-4 (a); lead's words, owner-delegated 2026-11-02): what happened first,
// the log named as unreadable — never "has no record of it", which is not known then — no
// "again", and the remedy (check that file, then Delete…). One owner: the backup's are the same
// sentence with WhatNotBackedUp. Positive twin first: the marker-only words with a log that was
// read still say the log has no record.
func TestTheLogUnreadableWordsSayTheLogCouldNotBeRead(t *testing.T) {
	if got := delmark.OnlyMarkerWords("it was not paused"); got != "Delete not finished: a delete of this archive was begun on another computer or user account, or this computer's deletion log has no record of it, and it did not finish, so it was not paused; nothing was changed. To finish it, click Delete… on its row in MailArchive Desktop." {
		t.Fatalf("positive twin: %q", got)
	}
	for _, c := range [][2]string{
		{delmark.LogUnreadableWords("it was not paused", "CAUSE"), "Delete not finished: a delete of this archive did not finish (its folder still holds the delete's marker), and MailArchive could not read this computer's deletion log (CAUSE), so it was not paused; nothing was changed. To finish it, check that file, then click Delete… on its row in MailArchive Desktop."},
		{delmark.LogUnreadableWords(delmark.WhatNotBackedUp, "CAUSE"), "Delete not finished: a delete of this archive did not finish (its folder still holds the delete's marker), and MailArchive could not read this computer's deletion log (CAUSE), so it was not backed up; nothing was changed. To finish it, check that file, then click Delete… on its row in MailArchive Desktop."},
	} {
		if c[0] != c[1] {
			t.Errorf("words:\n got  %q\n want %q", c[0], c[1])
		}
	}
}

package delmark_test

import (
	"testing"

	"mail-archive-tool/internal/delmark"
)

// covers: MA-1048, R36
// One owner of the marker's words for every action (DM-11, DM-24, §15.4): the words for another
// action than a backup are the backup's with what was not done in its place, and for a backup they
// are exactly MsgOnlyMarker and MsgNotRegular, so no second copy can drift.
func TestTheMarkersWordsForEveryActionAreOneSentence(t *testing.T) {
	for _, c := range [][2]string{
		{delmark.OnlyMarkerWords(delmark.WhatNotBackedUp), delmark.MsgOnlyMarker},
		{delmark.NotRegularWords(delmark.WhatNotBackedUp), delmark.MsgNotRegular},
		{delmark.OnlyMarkerWords("it was not paused"), "Delete not finished: a delete of this archive was begun on another computer or user account, or this computer's deletion log has no record of it, and it did not finish, so it was not paused; nothing was changed. To finish it, click Delete… on its row in MailArchive Desktop."},
		{delmark.NotRegularWords("its scheduled backup was not installed"), "This archive's folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device), so its scheduled backup was not installed; nothing was changed. If no delete of this archive is under way, remove it by hand, then try again."},
	} {
		if c[0] != c[1] {
			t.Errorf("words:\n got  %q\n want %q", c[0], c[1])
		}
	}
}

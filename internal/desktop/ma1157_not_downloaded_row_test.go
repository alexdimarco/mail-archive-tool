package desktop

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1157, R18, S29
// Recent backups words a finished run's messages not downloaded in full by where the run read
// its mail (the 1.6.1 friction review's P7: every OK row of a plain mbox archive said "your mail
// app (such as Thunderbird) has not downloaded it"): a mail file's row says the file holds only
// part of them and names no mail app; an older entry, which names no kind, blames nobody.
// Positive twin first: a Thunderbird store's row names Thunderbird.
func TestRecentBackupsSaysWhereTheMissingMailCameFrom(t *testing.T) {
	out := t.TempDir()
	at := time.Date(2026, 11, 7, 17, 0, 0, 0, time.Local)
	for i, e := range []state.RunHistoryEntry{
		{Status: state.RunOK, Fillable: 1},
		{Status: state.RunOK, Exported: 2, Fillable: 1, FillableFrom: state.FromMailFile},
		{Status: state.RunOK, Exported: 3, Fillable: 1, FillableFrom: state.FromThunderbird},
	} {
		e.When = at.Add(time.Duration(i) * time.Hour)
		if err := state.AppendRunHistory(out, e); err != nil {
			t.Fatal(err)
		}
	}
	rows := recentRunRows(out)
	if len(rows) != 3 || rows[0].Detail != "3 messages · "+state.NotDownloadedSentence(1, state.FromThunderbird) || !strings.Contains(rows[0].Detail, "Thunderbird has not downloaded it yet") {
		t.Fatalf("the positive twin: %+v", rows)
	}
	file, older := rows[1].Detail, rows[2].Detail
	assure.Reached(t, strings.Contains(file, "the mail file holds only part of this message; if it is a copy, copy it again from its mail program") &&
		!strings.Contains(file+older, "Thunderbird") && !strings.Contains(file+older, "mail app has not downloaded"),
		"a mail file's row and an older one blame no mail app: "+file+" | "+older)
}

package desktop

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1162, R18, S29
// An Incomplete row in Recent backups also says what of the run's mail is not in the archive in
// full yet, in the sentence an OK row and Run now's note say, worded by where the run read its mail
// (the polish-161 friction walk's N4: the record had `fillable` and `fillable_from`, the row said
// nothing of them); the kinds' words and the details come first, and the report address is still a
// link. Positive twin first: an Incomplete run with nothing not downloaded keeps its words alone.
func TestAnIncompleteRowSaysWhatIsNotDownloadedToo(t *testing.T) {
	out := t.TempDir()
	at := time.Date(2026, 11, 7, 21, 17, 0, 0, time.Local)
	p := &state.PartialCounts{Mistake: state.PartialKind{Messages: 2}, ArchiveWrite: state.PartialKind{Folders: 1}}
	words := strings.Join(state.PartialSentences(*p), " ") + " " + state.PartialDetails
	for i, e := range []state.RunHistoryEntry{
		{Status: state.RunFailed, Error: state.PartialText(*p, true), Partial: p},
		{Status: state.RunFailed, Error: state.PartialText(*p, true), Partial: p, Fillable: 1, FillableFrom: state.FromMailFile},
	} {
		e.When = at.Add(time.Duration(i) * time.Hour)
		if err := state.AppendRunHistory(out, e); err != nil {
			t.Fatal(err)
		}
	}
	rows := recentRunRows(out)
	if len(rows) != 2 || rows[1].Status != "Incomplete" || rows[1].Detail != words {
		t.Fatalf("the positive twin: %+v", rows)
	}
	r := rows[0]
	want := words + " · " + state.NotDownloadedSentence(1, state.FromMailFile)
	if r.Status != "Incomplete" || !r.Failed || r.Detail != want {
		t.Errorf("the Incomplete row with a message not in full:\n got %q\nwant %q", r.Detail, want)
	}
	linked := false
	for _, part := range r.DetailParts() {
		linked = linked || part.Link
	}
	assure.Reached(t, linked && strings.Contains(r.Detail, "the mail file holds only part of this message") && !strings.Contains(r.Detail, "mail app has not downloaded"),
		"the row names the mail file, blames no mail app, and still links the report address: "+r.Detail)
}

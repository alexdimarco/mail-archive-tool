package archdelete

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/state"
)

// covers: MA-1049, R36
// A marker with this computer's deletion log unreadable (the slice-2 pass's LOW-4 (a)): the log may
// hold the delete's started line, so nothing may say it "has no record of it". The backup's and
// every action's refusal say the log could not be read, with why, and how to go on
// (delmark.LogUnreadableWords: kind delete_unfinished, nothing written); UnfinishedAt carries why
// (LogUnreadable); the row says it in MarkerRowText. Positive twins first: the same marker with a
// log that was read (another computer's, holding no such line) keeps the marker-only words.
func TestMA1049_AnUnreadableLogIsNeverSaidToHaveNoRecord(t *testing.T) {
	half, _, mbox := ma1048Half(t)
	read := filepath.Join(t.TempDir(), LogName)
	unreadable := filepath.Join(t.TempDir(), LogName)
	if err := os.Mkdir(unreadable, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	cause := "MailArchive's deletion log " + unreadable + " must be a regular file (not a symlink, pipe or device)"
	began := localStamp(time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC))

	// Positive twins: a log that was read.
	err := app.RefuseUnfinishedDelete(half, read)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+half+")"))
	withLog := func(p string) Deps { d := testDeps(); d.Log = p; return d }
	u, _ := NewRowsCheck(withLog(read)).State(half, true)
	if got := MarkerRowText(u); !strings.Contains(got, "has no record of it") {
		t.Fatalf("positive twin: the row: %q", got)
	}

	before := ma1048Names(half)
	err = ma1048Backup(half, unreadable, mbox)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1),
		assure.Names(delmark.LogUnreadableWords(delmark.WhatNotBackedUp, cause)+" (Folder: "+half+")"),
		assure.Forbid("has no record", "Delete again"),
		assure.NoSideEffect(func() bool { return slices.Equal(before, ma1048Names(half)) }))
	if k := app.FailureKind(err); k != state.FailDeleteUnfinished || !app.RefusedAsNoArchive(err) {
		t.Errorf("kind %q, taken back %v", k, app.RefusedAsNoArchive(err))
	}

	for _, done := range []string{"it was not paused", "it was not resumed", "its scheduled backup was not installed", "it was not checked"} {
		err := ActionRefusal(unreadable, half, done)
		assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.LogUnreadableWords(done, cause)), assure.Forbid("has no record"))
		if r := AsRefusal(err); r == nil || r.Case != CaseUnfinished {
			t.Errorf("%s: case %+v", done, r)
		}
	}

	u, err = UnfinishedAt(unreadable, half, "")
	if err != nil || u == nil || !u.MarkerOnly || u.LogUnreadable != cause {
		t.Fatalf("UnfinishedAt carries why the log could not be read: %+v %v", u, err)
	}
	u, _ = NewRowsCheck(withLog(unreadable)).State(half, true)
	want := "Delete not finished: a delete of this archive begun " + began + " did not finish, and MailArchive could not read this computer's deletion log (" + cause + "). Check that file, then click Delete… to finish it."
	if got := MarkerRowText(u); got != want {
		t.Fatalf("the row's words:\n got  %q\n want %q", got, want)
	}
}

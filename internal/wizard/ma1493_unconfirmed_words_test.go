package wizard

import (
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1493, MA-1496, MA-1497, S90
// The words the friction review found wrong or missing (round B: F9, F10, F11). F9: the parent
// refusal "could not confirm that … is outside every archive" gave one remedy, "Choose a folder on
// this computer's own drive", for every cause, wrong when the folder already is there; it now names
// the archive and gives each cause its own remedy (positive twin first: a refusal that names no
// archive keeps today's words). F10: the end note said "which did not finish" for an archive never
// started; it now says "which was not started". F11: when nothing runs at all (every mailbox paused,
// kept or not made), no summary is shown, as the plan said it all.
func TestTheSetsWordsSayWhatHappenedAndWhatToDo(t *testing.T) {
	p := `D:\Mail archives`
	old := setParentWords(&app.CopyRefusal{ID: "R-f2", Message: "x"}, p, "")
	if !strings.Contains(old, "outside every archive") || !strings.Contains(old, "Choose a folder on this computer's own drive.") {
		t.Fatalf("positive twin: a refusal naming no archive keeps today's words: %q", old)
	}
	a := `E:\Old archive`
	for cause, remedy := range map[string]string{
		"walk": "that archive is too large or partly unreadable to check. Nothing was made. Choose a folder on another drive; if that archive is no longer used, remove it from Your archives in MailArchive Desktop.",
		"fuse": "the folder is on an sshfs, FUSE or overlay mount, where folders cannot be compared. Nothing was made. Choose a folder on a local drive, or one mounted with your system's SMB or NFS client.",
		"here": "one of them is reached through a share on this computer, which can show one folder under two names. Nothing was made. Choose the folder by its local path instead.",
		"":     "the two are reached through different network shares, which can show one folder under two names. Nothing was made. Choose a folder on a local drive or a USB drive.",
	} {
		got := setParentWords(&app.CopyRefusal{ID: "R-f2", Archive: a, Cause: cause}, p, "")
		if want := "MailArchive could not confirm that " + p + " is outside the archive " + a + ": " + remedy; got != want {
			t.Errorf("cause %q:\n got %q\nwant %q", cause, got, want)
		}
	}
	if got := setParentWords(&app.CopyRefusal{ID: "R-f2", Archive: a, Cause: "fuse", ArchiveSide: true}, p, ""); !strings.Contains(got, "that archive is on an sshfs or other FUSE mount") || !strings.Contains(got, "mount that archive's share") {
		t.Errorf("the archive's own FUSE mount: %q", got)
	}

	// F10: an archive never started.
	notStarted := arcOf("d@x", outNew, MadeNotStarted, app.Result{})
	failed := arcOf("c@x", outNew, MadeFailed, app.Result{})
	note := setScheduleNote("/m", []*setArchive{arcOf("a@x", outNew, MadeFinished, app.Result{}), failed, notStarted}, "daily", "02:00", false, nil)
	if !strings.Contains(note, "\nNot scheduled: d@x, which was not started.\n") || !strings.HasPrefix(note, "Not scheduled: c@x, which did not finish.\n") {
		t.Errorf("the end note's not-scheduled lines:\n%s", note)
	}

	// F11: nothing runs at all, so no summary is shown.
	ownSettings(t)
	s := &flowScript{}
	s.install(t)
	paused := planItem("lee@c.example", outPaused, "lee@c.example")
	made, err := runSet(setChosen{plan: setPlan{parent: t.TempDir(), items: []setItem{paused}}}, "darwin")
	if err != nil || len(made) != 0 || len(s.questions) != 0 || len(s.notes) != 0 {
		t.Errorf("nothing ran, yet: made %+v %v, questions %q, notes %q", made, err, s.questions, s.notes)
	}
	// Its positive twin: an archive that ran gets its summary.
	s = &flowScript{summary: zenity.ErrCanceled}
	s.install(t)
	if err := showSetSummary("/m", []*setArchive{arcOf("a@x", outNew, MadeFinished, app.Result{ManifestSize: 1})}); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, s.questions, "the summary of an archive that ran")
}

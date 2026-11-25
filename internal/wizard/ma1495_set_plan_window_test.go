package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
)

// planItem is one item of a plan made by hand, for the text's own tests.
func planItem(name string, o setOutcome, folder string) setItem {
	return setItem{row: setRow{name: name, make: o != outNotMade}, outcome: o, folder: folder}
}

// covers: MA-1495
// The plan window's text (§8): the design's example, word for word (addresses invented), then the
// singular and zero forms, and every line kind §7 names: brought up to date, paused, a saved-once
// mailbox kept as it is (with Pause advice only for an exact match), another folder that also holds
// a mailbox's archive, not made (empty, a folder that could not be read, an unfinished delete, no
// number or name left), a folder that could not be read just now, not found now, folders left out,
// and the accounts asked about a full offline download.
func TestThePlanSaysEveryArchiveAndEveryLineBeforeAnythingIsWritten(t *testing.T) {
	walker := planItem("qzsam.walker@gmail.com", outNew, "qzsam.walker@gmail.com")
	walker.ask = true
	contoso := planItem("sam.walker@contoso.example", outNew, "sam.walker@contoso.example")
	contoso.ask = true
	gmail := planItem("imap.gmail.com (not in Thunderbird now)", outNew, "imap.gmail.com (not in Thunderbird now)")
	gmail.row.once, gmail.row.row.unused = true, true
	feeds := planItem("Feeds", outNotMade, "")
	feeds.why, feeds.row.empty = notMadeEmpty, true
	example := setPlan{parent: `D:\Mail archives`, items: []setItem{
		walker, contoso, planItem("Local Folders", outNew, "Local Folders"), gmail,
		planItem("sam@old.example", outContinue, "sam@old.example"), feeds,
	}}
	want := `MailArchive will make 4 archives in D:\Mail archives:

qzsam.walker@gmail.com
sam.walker@contoso.example
Local Folders
imap.gmail.com (not in Thunderbird now): saved once and not kept current, since no account uses it

To bring up to date: sam@old.example.
Not made: Feeds, which is empty.
Next you are asked whether to prepare a full offline download of: qzsam.walker@gmail.com, sam.walker@contoso.example`
	// Amended 2026-11-25 (the friction review's F11; nothing weakened): the last line is said as what
	// comes next; it read "Asked about a full offline download: …" before. The button says "Bring them
	// up to date" when the plan makes no new archive and brings some up to date ("Make the archives"
	// before), and "In this folder, not found now: NAME" says what to do.
	// Restated 2026-11-25 (the re-verify of rounds A and B, RF-07, its words; compared whole, as
	// before): the design's line "Brought up to date: sam@old.example (already in this folder)", kept
	// here, read as done before anything ran and never named the mailbox; it is now "To bring up to
	// date: FOLDER, for MAILBOX." (the design's §8 example keeps the old line: the lead's round C
	// touches the design only for RV-07).
	// Restated 2026-11-25 (the Windows lab of MA-1503, part 2; compared whole, as before): "To bring up
	// to date: sam@old.example, for sam@old.example." before; ", for MAILBOX" now follows only a folder
	// whose name is not the mailbox's (the renamed case below keeps it).
	if got := setPlanText(example); got != want {
		t.Fatalf("the design's example:\n got %q\nwant %q", got, want)
	}

	// Singular and zero forms.
	one := setPlan{parent: "/m", items: []setItem{planItem("a@x", outNew, "a@x")}}
	if got := setPlanText(one); got != "MailArchive will make 1 archive in /m:\n\na@x" {
		t.Errorf("one archive: %q", got)
	}
	none := setPlan{parent: "/m", items: []setItem{planItem("a@x", outContinue, "a@x"), planItem("b@x", outContinue, "b@x")}}
	// Restated 2026-11-25 (RF-07; compared whole, as before): "…\n\nBrought up to date: a@x (already in
	// this folder)\nBrought up to date: b@x (already in this folder)" before.
	// Restated again 2026-11-25 (the Windows lab of MA-1503, part 2; compared whole): "…To bring up to
	// date: a@x, for a@x.\nTo bring up to date: b@x, for b@x." before.
	if got := setPlanText(none); got != "No new archive: 2 brought up to date in /m.\n\nTo bring up to date: a@x.\nTo bring up to date: b@x." {
		t.Errorf("no new archive: %q", got)
	}
	// RF-07: the mailbox is named as the list showed it, also when its archive's folder has another
	// name (a renamed account's archive, found by what it holds).
	renamed := planItem("sam@new.example", outContinue, "Old account")
	renamed.row.shown = "Sam (sam@new.example)"
	if got := setPlanText(setPlan{parent: "/m", items: []setItem{renamed}}); !strings.Contains(got, "\nTo bring up to date: Old account, for Sam (sam@new.example).") {
		t.Errorf("RF-07: the line names the mailbox: %q", got)
	}
	if got := setPlanText(setPlan{parent: "/m", items: []setItem{feeds}}); got != "No new archive in /m.\n\nNot made: Feeds, which is empty." {
		t.Errorf("nothing at all: %q", got)
	}
	for p, want := range map[*setPlan]string{&example: setMakeLabel, &one: setMakeLabel, &none: "Bring them up to date", {items: []setItem{feeds}}: setMakeLabel} {
		if got := setPlanButton(*p); got != want {
			t.Errorf("the plan's button for %q: %q, want %q", outcomesOf(*p), got, want)
		}
	}

	// Every other line kind.
	older := planItem("pat@b.example (older copy)", outUnfinished, "pat@b.example (older copy)")
	older.row.once = true
	paused := planItem("lee@c.example", outPaused, "lee@c.example")
	exact := planItem("imap.old.example (not in Thunderbird now)", outKept, "Old account")
	exact.exact = true
	combined := planItem("imap.older.example (not in Thunderbird now)", outKept, "everything")
	twin := planItem("kim@d.example", outContinue, "kim@d.example")
	twin.also = []string{"kim@d.example (2)"}
	unread := planItem("sam@e.example", outNotMade, "")
	unread.why = notMadeUnread
	deleted := planItem("sam@f.example", outNotMade, "")
	deleted.why, deleted.delWhy = notMadeDelete, "Delete not finished: click Delete again. (Folder: /m/sam@f.example)"
	full := planItem("same", outNotMade, "")
	full.why = notMadeNoNumber
	alike := planItem("alike", outNotMade, "")
	alike.why = notMadeNoName
	all := setPlan{parent: "/m", items: []setItem{older, paused, exact, combined, twin, unread, deleted, full, alike},
		unread: []string{"shut"}, notFound: []string{"sam@gone.example"}, more: true}
	got := setPlanText(all)
	// Restated 2026-11-25 (the re-verify of rounds A and B, RF-07, RF-03 and RF-04, their words; each
	// line still required whole): "Brought up to date: kim@d.example (already in this folder)", "…:
	// kept as it is in Old account; Thunderbird no longer uses this folder. Click Pause on it to stop its
	// backups." and "In this folder, not found now: sam@gone.example; its mail stays in its archive;
	// click Pause on its row to stop its backups." before.
	for _, line := range []string{
		"MailArchive will make 1 archive in /m:\n\npat@b.example (older copy): saved once and not kept current, since it is an older copy\n\n",
		"To bring up to date: kim@d.example.", // "…, for kim@d.example." before (the Windows lab, part 2)
		"Paused, so lee@c.example is not being backed up now; click Resume (make writable) on its row to back it up again.",
		"imap.old.example (not in Thunderbird now): kept as it is in Old account, since Thunderbird no longer uses that mailbox's folder. To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop.",
		"imap.older.example (not in Thunderbird now): kept as it is in everything\n",
		"kim@d.example (2) also holds this mailbox's archive: left as it is",
		"sam@e.example: not made this time, since a folder here could not be read; run this again later",
		"sam@f.example: not made. Delete not finished: click Delete again. (Folder: /m/sam@f.example)",
		"same: not made, since this folder already holds folders named same (2) to (99).",
		"alike: not made, since 99 other mailboxes have its name.",
		"shut: could not be read just now",
		"In this folder, not found now: sam@gone.example. Its mail stays in that archive. To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop.",
		"Only the first 500 folders in /m were looked at; the others were left out.",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("the plan lacks %q:\n%s", line, got)
		}
	}
	if strings.Contains(got, "full offline download") || strings.Contains(setPlanText(none), "full offline download") {
		t.Errorf("no account is asked about here: %q", got)
	}
	if got := setPlanText(setPlan{parent: "/m\x1b[2J", items: []setItem{planItem("a\x07@x", outNew, "a\x07@x")}}); strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("control characters reach the plan: %q", got)
	}
}

// covers: MA-1495
// The plan step, through the dialog seams: right after the parent folder, before any other question,
// the plan is shown in a question box whose text is the plan's, with "Make the archives" and Cancel.
// Positive twin first: Make the archives gives the checked parent and the plan, and still nothing is
// written (making them is step 3b's). Cancel writes nothing: the parent folder is as it was, no
// folder is made, the made-list holds nothing and no job file is there.
func TestCancelOnThePlanWritesNothing(t *testing.T) {
	ownSettings(t)
	rows, _ := setFixture(t)
	parent := t.TempDir()
	before := treeOf(t, parent)
	nothingWritten := func() bool {
		d, _ := job.Dir()
		jobs, _ := filepath.Glob(filepath.Join(d, "*.json"))
		return reflect.DeepEqual(treeOf(t, parent), before) && len(madeLines(t)) == 0 && len(jobs) == 0
	}

	s := &setScript{folders: []string{parent}, answers: []error{nil}}
	s.install(t)
	chosen, err := chooseSet(rows, "darwin")
	if err != nil || chosen.parent == nil || chosen.parent.Parent != parent || len(chosen.plan.items) != len(rows) {
		t.Fatalf("positive twin: Make the archives: %+v %v", chosen, err)
	}
	if len(s.chooser) != 1 || len(s.questions) != 1 || len(s.lists) != 0 || len(s.notes) != 0 || s.questions[0] != setPlanText(chosen.plan) {
		t.Fatalf("the plan comes right after the parent folder: chooser %d, questions %q, lists %d, notes %q", len(s.chooser), s.questions, len(s.lists), s.notes)
	}
	if !strings.HasPrefix(s.questions[0], "MailArchive will make 6 archives in "+parent+":\n\nqz.student@mail.qzuni.example\n") {
		t.Errorf("the plan's text: %q", s.questions[0])
	}
	if !nothingWritten() {
		t.Fatalf("positive twin: agreeing to the plan writes nothing yet")
	}

	s = &setScript{folders: []string{parent}, answers: []error{zenity.ErrCanceled}}
	s.install(t)
	chosen, err = chooseSet(rows, "darwin")
	code, msg := 0, ""
	if errors.Is(err, zenity.ErrCanceled) && chosen.parent == nil && len(s.questions) == 1 {
		code, msg = 1, s.questions[0]
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(parent, "qzsam.walker@gmail.com"), assure.NoSideEffect(nothingWritten))

	// The call: through the question seam with the design's buttons.
	src, err := os.ReadFile("setwindow.go")
	if err != nil {
		t.Fatal(err)
	}
	// Amended 2026-11-25 (the friction review's F11): the OK button is setPlanButton's, "Make the
	// archives" or "Bring them up to date"; it was setMakeLabel itself before.
	if !strings.Contains(string(src), `questionDialog(setPlanText(p), zenity.Title(appTitle), zenity.OKLabel(setPlanButton(p)), zenity.CancelLabel("Cancel"))`) || setMakeLabel != "Make the archives" || setUpToDateLabel != "Bring them up to date" {
		t.Error("the plan is no longer asked through the seam with its button and Cancel")
	}
}

package archdelete

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// The words of DM-27's state (docs/design-archive-delete.md §15.4's DM-27; the slice-5 adversarial
// pass's LOW-4, docs/review-delete-marker-slice5-adversarial.md; MA-1068; lead's words,
// owner-delegated 2026-11-02): this computer's deletion log has a delete of the folder that wrote its
// `marked` line, yet the folder holds its anchor with no marker, or a marker naming another delete.
// On a rotation drive (a second drive at the same path with the same identity, the delete begun on
// the first) "click Delete again" was no way out: the delete is finished on the drive it began on.
// MailArchive cannot tell that drive from a marker removed by hand, so the words name both. The
// behaviour is unchanged: every backup and action is still refused, case delete-unfinished.

// ma1068Refusal and ma1068Row are the words this test pins.
func ma1068Refusal(done string) string {
	return "Delete not finished: a delete this computer began in this folder did not finish, but the folder does not hold the note that delete left, so " + done +
		"; nothing was changed. If this is a different drive, plug in the drive the delete began on and finish the delete there; if it is the same drive and the note was removed, click Delete… on its row in MailArchive Desktop to finish it."
}

const ma1068Row = "Delete not finished: a delete this computer began in this folder did not finish, but the folder does not hold the note that delete left. If this is a different drive, plug in the drive the delete began on and finish the delete there; if it is the same drive and the note was removed, click Delete… to finish it."

// ma1068Says asserts the words every surface of archdelete gives for dir: DM-27's when disagrees,
// else the log's own "Delete not finished: click Delete again" (and the row's own line, "").
func ma1068Says(t *testing.T, r *rig, dir string, disagrees bool) {
	t.Helper()
	const notBackedUp, notPaused, notUnlisted = "it was not backed up", "it was not paused", "it was not removed from the list"
	backup, paused, unlisted, row := ma1068Refusal(notBackedUp), ma1068Refusal(notPaused), ma1068Refusal(notUnlisted), ma1068Row
	if !disagrees {
		backup = "Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed."
		paused = "Delete not finished: click Delete again. This archive's delete did not finish, so it was not paused; nothing was changed."
		unlisted = msgUnlistUnfinished
		row = ""
	}
	names := func() []string { n := ma1044Names(dir); slices.Sort(n); return n }
	before := names()
	err := BackupRefusal(r.x.Log, dir)
	assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(backup),
		assure.NoSideEffect(func() bool { return slices.Equal(before, names()) }))
	if rr := AsRefusal(err); rr == nil || rr.Case != CaseUnfinished || errText(err) != backup {
		t.Errorf("the backup's refusal (disagrees %v): %v %q", disagrees, rr, errText(err))
	}
	if err := ActionRefusal(r.x.Log, dir, notPaused); errText(err) != paused {
		t.Errorf("Pause's refusal (disagrees %v): %q", disagrees, errText(err))
	}
	if err := UnlistRefusal(dir, r.x.Deps); errText(err) != unlisted {
		t.Errorf("Remove from list's refusal (disagrees %v): %q", disagrees, errText(err))
	}
	rc := NewRowsCheck(r.x.Deps)
	u, _ := rc.State(dir, false)
	if u == nil || MarkerRowText(u) != row || u.Disagrees != disagrees {
		t.Errorf("the row (disagrees %v): %+v %q", disagrees, u, MarkerRowText(u))
	}
	if err := rc.UnlistRefusal(dir); errText(err) != unlisted {
		t.Errorf("the row's Remove from list (disagrees %v): %q", disagrees, errText(err))
	}
	if s, err := UnfinishedToSay(r.x.Log, dir, ""); err != nil || s == nil || s.Disagrees != disagrees || s.MarkerOnly {
		t.Errorf("the status line's delete (disagrees %v): %+v %v", disagrees, s, err)
	}
	if got := ma1068Refusal(notBackedUp); DisagreesWords(notBackedUp) != got {
		t.Errorf("DisagreesWords: %q", DisagreesWords(notBackedUp))
	}
}

// covers: MA-1068, MA-1056, R36
// The marker removed by hand, and another delete's marker, after this computer's delete stopped in
// step 6. Positive twins first: with the marker left, and with the anchor and the marker both gone
// (step 6 had finished: the slice-4 pass's NoteGone), the log's own words, "click Delete again".
func TestMA1068_TheLogAndFolderDisagreeWordsNameTheDriveTheDeleteBeganOn(t *testing.T) {
	{
		dir := plainBesidePersonsFiles(t)
		r, _ := ma1050Stopped(t, dir)
		ma1068Says(t, r, dir, false)
	}
	{
		dir := plainBesidePersonsFiles(t)
		r, _ := ma1050Stopped(t, dir)
		for _, n := range []string{manifestName, delmark.Name} {
			if err := os.Remove(filepath.Join(dir, n)); err != nil {
				t.Fatal(err)
			}
		}
		ma1068Says(t, r, dir, false)
	}
	{
		dir := plainBesidePersonsFiles(t)
		r, _ := ma1050Stopped(t, dir)
		if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
			t.Fatal(err)
		}
		ma1068Says(t, r, dir, true)
	}
	{
		dir := plainBesidePersonsFiles(t)
		r, _ := ma1050Stopped(t, dir)
		writeFile(t, filepath.Join(dir, delmark.Name), `{"what":"MailArchive delete marker","version":1,"run":"6666666666666666","began":"2026-11-01T10:00:00Z","note":"x"}`)
		ma1068Says(t, r, dir, true)
	}
}

// covers: MA-1068, MA-1056, R36
// The rotation drive itself: an encrypted archive at the path, with the identity, that this computer's
// delete began on and marked (the log written by hand, another vault's record), and no marker. Positive
// twin first: the same started line with no `marked` line (the delete stopped before step 6) keeps the
// log's own words.
func TestMA1068_ARotationDriveIsToldToPlugInTheDriveTheDeleteBeganOn(t *testing.T) {
	const run = "7777777777777777"
	started := func(r *rig) {
		appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": run, "folder": r.dir, "row": r.dir, "kind": "encrypted",
			"record_id": "ffffffffffffffffffffffffffffffff", "record_spelling": filepath.Join(filepath.Dir(r.dir), "other drive's spelling"), "identity": idOf(t, r.dir)})
	}
	{
		r := encRig(t, encryptedClean(t))
		r.x.Deps.Record = recordedAt(r.dir)
		started(r)
		ma1068Says(t, r, r.dir, false)
	}
	r := encRig(t, encryptedClean(t))
	r.x.Deps.Record = recordedAt(r.dir)
	started(r)
	appendRawLog(t, r.x.Log, map[string]any{"at": "2026-11-01T10:00:01Z", "event": "marked", "run": run, "folder": r.dir, "first_run": run})
	ma1068Says(t, r, r.dir, true)
	// The words and the plan agree on the state: Delete… there is DM-27's ordinary delete.
	if p := r.plan(t); !p.Disagrees || p.Finishes != nil {
		t.Fatalf("the plan is not DM-27's: disagrees %v finishes %+v", p.Disagrees, p.Finishes)
	}
}

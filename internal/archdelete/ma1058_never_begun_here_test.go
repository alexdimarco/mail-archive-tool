package archdelete

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// ma1058Folder is an archive folder holding a regular delete marker (run 0123456789abcdef).
func ma1058Folder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Archive")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".mailarchive-manifest.json"), `{"version":1}`)
	plantMarker(t, dir)
	return dir
}

// covers: MA-1058, R36
// NeverBegunHere (docs/design-archive-delete.md §15.10 slice 6, the owner's ruling Q5, DM-16) is
// true only for a delete this computer never began: a regular marker in the folder, this
// computer's deletion log read, with no line for the folder and no started line of the marker's
// run (DM-10). A delete the log knows by the folder, one whose run the log began (begun here, the
// drive plugged in again under another name), a log that cannot be read, a marker name that is not
// a regular file, and no marker at all are false: a scheduled run refused for those never counts
// toward the auto-pause. Positive twin first: a log that never saw the delete, and MailArchive's
// own log when none is named.
func TestMA1058_NeverBegunHereIsOnlyADeleteOnlyTheMarkerKnows(t *testing.T) {
	dir := ma1058Folder(t)
	fresh := filepath.Join(t.TempDir(), LogName) // another computer's log: it never saw the delete

	// Positive twins.
	assure.Reached(t, NeverBegunHere(fresh, dir), "a delete only the marker knows, by a log that never saw it")
	assure.Reached(t, NeverBegunHere("", dir), "the same by MailArchive's own deletion log")

	// The log knows the delete by the folder.
	knows := filepath.Join(t.TempDir(), LogName)
	appendRawLog(t, knows, map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "1111111111111111", "folder": dir, "row": dir, "kind": "plain"})
	if u, err := UnfinishedAt(knows, dir, ""); err != nil || u == nil || u.MarkerOnly {
		t.Fatalf("precondition: the log knows the delete: %+v %v", u, err)
	}
	if NeverBegunHere(knows, dir) {
		t.Error("a delete this computer's log knows counted as never begun here")
	}

	// The marker's run was begun here, under another path (DM-10).
	begun := filepath.Join(t.TempDir(), LogName)
	appendRawLog(t, begun, map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "0123456789abcdef", "folder": filepath.Join(t.TempDir(), "Old name"), "kind": "plain"})
	if u, err := UnfinishedAt(begun, dir, ""); err != nil || u == nil || !u.MarkerOnly || !u.BegunHere {
		t.Fatalf("precondition: begun here: %+v %v", u, err)
	}
	if NeverBegunHere(begun, dir) {
		t.Error("a delete whose run this computer's log began counted as never begun here")
	}

	// A log that cannot be read.
	unreadable := filepath.Join(t.TempDir(), LogName)
	if err := os.Mkdir(unreadable, 0o700); err != nil {
		t.Fatal(err)
	}
	if NeverBegunHere(unreadable, dir) {
		t.Error("a log that cannot be read counted as never begun here")
	}

	// A marker name that is not a regular file, and no marker.
	odd := filepath.Join(t.TempDir(), "Odd")
	if err := os.MkdirAll(filepath.Join(odd, delmark.Name), 0o755); err != nil {
		t.Fatal(err)
	}
	if NeverBegunHere(fresh, odd) {
		t.Error("a marker name that is not a regular file counted")
	}
	if NeverBegunHere(fresh, t.TempDir()) {
		t.Error("a folder with no marker counted")
	}
}

// covers: MA-1058, R36
// The delete's step 3 removes the folder's count of refused runs (<key>.delete-refused, kept beside
// the alert streak) in MailArchive's settings folder and in the Desktop's own, as it removes the
// alert files, and keeps another archive's. The suffix is backupalert's.
func TestMA1058_Step3RemovesTheCountOfRefusedRuns(t *testing.T) {
	if deleteRefusedSuffix != backupalert.DeleteRefusedSuffix {
		t.Fatal("the count's name drifted from backupalert's")
	}
	dir := smallArchive(t, t.TempDir())
	other := smallArchive(t, t.TempDir())
	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	settings := t.TempDir()
	var mine []string
	for _, d := range []string{cfg, settings} {
		p := filepath.Join(d, schedule.DefaultNameFor(dir)+deleteRefusedSuffix)
		writeFile(t, p, "x")
		t.Cleanup(func() { os.Remove(p) })
		mine = append(mine, p)
	}
	theirs := filepath.Join(settings, schedule.DefaultNameFor(other)+deleteRefusedSuffix)
	writeFile(t, theirs, "x")

	if failed, err := removeNameFiles(nil, []string{dir}, settings); err != nil {
		t.Fatalf("%s: %v", failed, err)
	}
	for _, p := range assure.Reached(t, mine, "this archive's counts") {
		if exists(p) {
			t.Errorf("%s must be removed", p)
		}
	}
	if !exists(theirs) {
		t.Error("another archive's count is kept")
	}
}

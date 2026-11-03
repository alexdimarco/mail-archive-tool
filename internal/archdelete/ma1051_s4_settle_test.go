package archdelete

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
)

// The slice-4 adversarial pass of the delete marker, settled (docs/design-archive-delete.md §14,
// "Slice 4's adversarial pass"; MA-1051): a plan from a marker alone keeps the metadata files of
// the person's kept files (LOW-1), and a marker lookup that fails says "could not check" (LOW-5).

// covers: MA-1051, R36
// The slice-4 pass's LOW-1: a plan from a marker alone (DM-19) no longer removes operating-system
// files that belong to what it keeps. An AppleDouble file `._X` holds X's Finder tags, colour label
// and resource fork on exFAT, FAT and network drives: it goes only when X goes too (a MailArchive
// name the plan removes, the marker) or X is not there; `.VolumeIcon.icns`, `desktop.ini`,
// `.DS_Store` and `Thumbs.db` stay while the folder is kept. Positive twin first: a planted marker
// beside only MailArchive's names and such files leaves nothing — step 7 removes the litter with the
// folder.
func TestMA1051_APlanFromTheMarkerKeepsTheMetadataOfThePersonsFiles(t *testing.T) {
	{
		dir := t.TempDir()
		for _, f := range []string{".DS_Store", "Thumbs.db", ".VolumeIcon.icns", ".mailarchive-lastrun.json", "._.mailarchive-lastrun.json"} {
			writeFile(t, filepath.Join(dir, f), "x "+f)
		}
		plantMarker(t, dir)
		r := newRig(t, dir)
		if res := r.run(t, false); res.Status != StatusComplete || exists(dir) {
			t.Fatalf("positive twin: the folder there %v:\n%s", exists(dir), res.Message())
		}
	}
	dir := t.TempDir()
	keep := []string{"README.txt", "._README.txt", "Report.pdf", "._Report.pdf", ".VolumeIcon.icns", "desktop.ini", ".DS_Store", "Thumbs.db"}
	gone := []string{".mailarchive-lastrun.json", "._.mailarchive-lastrun.json", "._" + delmark.Name, "._Gone.txt"}
	for _, f := range append(append([]string(nil), keep...), gone...) {
		writeFile(t, filepath.Join(dir, f), "x "+f)
	}
	plantMarker(t, dir)
	r := newRig(t, dir)
	p := r.plan(t)
	if p.Finishes == nil || !p.Finishes.FromMarker {
		t.Fatalf("planned from the marker: %+v", p.Finishes)
	}
	for _, f := range keep {
		if contains(p.Litter, f) || contains(removeRels(p), f) {
			t.Fatalf("the plan removes %s: remove %q litter %q", f, removeRels(p), p.Litter)
		}
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	for _, f := range keep {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || string(b) != "x "+f {
			t.Errorf("%s was not kept as it was: %v", f, err)
		}
	}
	for _, f := range append(gone, delmark.Name) {
		if exists(filepath.Join(dir, f)) {
			t.Errorf("%s is left", f)
		}
	}
}

// covers: MA-1051, R36
// The slice-4 pass's LOW-5: a folder with no anchor and no line of this computer's log, whose
// marker lookup fails for another reason than "not there", is refused "could not check" — not "This
// folder no longer holds an archive. Use Remove from list.", which it cannot know — with the
// lookup's own kind (a *delmark.LookupError), and nothing changed. Positive twin first: the lookup
// working, the folder is planned from its marker.
func TestMA1051_AMarkerLookupThatFailsIsCouldNotCheck(t *testing.T) {
	dir := plantedBesidePersonsFiles(t)
	plantMarker(t, dir)
	r := newRig(t, dir)
	if p := r.plan(t); p.Finishes == nil || !p.Finishes.FromMarker {
		t.Fatalf("positive twin: %+v", p.Finishes)
	}
	marker := filepath.Join(dir, delmark.Name)
	restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		if name == marker {
			return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New("device not ready")}
		}
		return os.Lstat(name)
	})
	t.Cleanup(restore)
	before := tree(t, dir)
	_, err := Plan(dir, r.x.Deps)
	ref := AsRefusal(err)
	if ref == nil {
		t.Fatalf("not refused: %v", err)
	}
	want := "MailArchive could not check this archive's folder for a delete that did not finish (device not ready), so it was not deleted; nothing was changed. Check that the drive or folder can be read, then try again."
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(want),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) }))
	var le *delmark.LookupError
	if ref.Case != CaseCouldNotCheck || ref.Message != want || !errors.As(err, &le) {
		t.Fatalf("case %s, lookup error %v: %q", ref.Case, errors.As(err, &le), ref.Message)
	}
}

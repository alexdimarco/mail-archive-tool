package archdelete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/lockfile"
)

// The delete removes its marker last (docs/design-archive-delete.md §15.2, with the owner's ruling
// Q1 and the 10-lens review's DM-9; §15.10 slice 3; MA-1047): right after the anchor, under the
// archive lock, only when no name the archive recorded is left.

// covers: MA-1047, R36
// The marker goes right after the anchor and under the lock: the removal seam sees the manifest
// asked for, then the marker, then only the store folders and the lock file; when the marker is
// asked for the archive lock is held and the manifest gone, and the afterRelease seam (a run taking
// the lock as step 7 lets it go) finds no marker. Positive twin first: at the manifest's removal the
// marker is still there.
func TestMA1047_TheMarkerGoesRightAfterTheAnchorUnderTheLock(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	var seen []string
	thereAtAnchor, heldAtMarker, anchorAtMarker := false, false, true
	faultOn(t, &seen, func(rel string) error {
		switch rel {
		case manifestName:
			_, thereAtAnchor = ma1046Marker(dir)
		case delmark.Name:
			heldAtMarker = lockfile.Held(filepath.Join(dir, lockName))
			anchorAtMarker = exists(filepath.Join(dir, manifestName))
		}
		return nil
	})
	oldAfter := afterRelease
	t.Cleanup(func() { afterRelease = oldAfter })
	thereAfterRelease := true
	afterRelease = func(string) { _, thereAfterRelease = ma1046Marker(dir) }
	p := r.plan(t)
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete {
		t.Fatalf("%s", res.Message())
	}
	m := -1
	for i, s := range seen {
		if s == manifestName {
			m = i
		}
	}
	if m < 0 || m+1 >= len(seen) || seen[m+1] != delmark.Name {
		t.Fatalf("the marker is not the removal right after the manifest: %q", seen)
	}
	for _, s := range seen[m+2:] {
		if s != lockName && !contains(p.Folders, s) {
			t.Errorf("%s was removed after the marker", s)
		}
	}
	if !thereAtAnchor || !heldAtMarker || anchorAtMarker || thereAfterRelease {
		t.Fatalf("at the manifest: marker %v; at the marker: lock held %v, manifest there %v; after the release: marker %v",
			thereAtAnchor, heldAtMarker, anchorAtMarker, thereAfterRelease)
	}
}

// covers: MA-1047, R36
// A fault before the anchor keeps the marker with it (Q1): a fault on step 6's very first removal
// leaves the whole archive and its marker; a fault on the manifest leaves the manifest and the
// marker. Either is "Delete not finished", refused by another computer whose log never saw it
// (MA-1048's marker-only words), and Delete again finishes and removes the marker. A folder kept for
// the person's files ends complete with no marker.
func TestMA1047_AFaultBeforeTheAnchorKeepsTheMarkerAndDeleteAgainFinishes(t *testing.T) {
	for _, on := range []string{stemA + ".html", manifestName} {
		t.Run(on, func(t *testing.T) {
			dir := plainBesidePersonsFiles(t)
			r := newRig(t, dir)
			fault := true
			faultOn(t, nil, func(rel string) error {
				if fault && rel == on {
					return inUseErr(rel)
				}
				return nil
			})
			res := r.run(t, false)
			if res.Status != StatusPartial || !res.Unfinished {
				t.Fatalf("%s", res.Message())
			}
			if !exists(filepath.Join(dir, manifestName)) || !exists(filepath.Join(dir, delmark.Name)) {
				t.Fatal("the manifest and the marker stay together")
			}
			if on == stemA+".html" && !exists(filepath.Join(dir, filepath.FromSlash(stemA+".html"))) {
				t.Fatal("a fault on the first removal leaves that file")
			}
			before := ma1048Names(dir)
			err := ma1048Backup(dir, filepath.Join(t.TempDir(), LogName), ma1046Mbox(t))
			assure.Refused(t, refusalCode(err), errText(err), assure.Code(1), assure.Names(delmark.MsgOnlyMarker+" (Folder: "+dir+")"),
				assure.NoSideEffect(func() bool { return strings.Join(before, "\x00") == strings.Join(ma1048Names(dir), "\x00") }))
			fault = false
			if res := r.run(t, false); res.Status != StatusComplete {
				t.Fatalf("Delete again: %s", res.Message())
			}
			if exists(filepath.Join(dir, delmark.Name)) || exists(filepath.Join(dir, manifestName)) || !exists(filepath.Join(dir, "taxes.pdf")) {
				t.Fatal("finished: no marker, no manifest, the person's files kept")
			}
		})
	}
}

// covers: MA-1047, R36
// A marker that cannot be removed is a name the archive owns that could not be removed (§15.2):
// "Not fully deleted. 1 file could not be removed: .mailarchive-deleting.json." with §8 (a)'s
// remedy for its cause, `finished partial`, the row "Delete not finished", the manifest already
// gone. Delete again is §5.4's no-anchor re-run, which retries it and — the marker gone and only the
// person's files left — ends complete with them kept (DM-9, case (ii)): no marker, the row
// removed, the log's delete ended. Positive twin first: the same delete with the marker removable.
func TestMA1047_AMarkerThatCannotBeRemovedThenDeleteAgainEndsComplete(t *testing.T) {
	{
		twin := newRig(t, plainBesidePersonsFiles(t))
		if res := twin.run(t, false); res.Status != StatusComplete || exists(filepath.Join(twin.dir, delmark.Name)) {
			t.Fatalf("positive twin: %s", res.Message())
		}
	}
	oldPause := retryPause
	t.Cleanup(func() { retryPause = oldPause })
	retryPause = time.Millisecond
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	fault := true
	tries := 0
	faultOn(t, nil, func(rel string) error {
		if fault && rel == delmark.Name {
			tries++
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, false)
	if res.Status != StatusPartial || !res.Unfinished || res.ExitCode() != 3 ||
		!strings.HasPrefix(res.Lines[0], "Not fully deleted. 1 file could not be removed: "+delmark.Name+". ") ||
		!strings.Contains(res.Lines[0], CauseInUse.remedy(1, "")) {
		t.Fatalf("%s", res.Message())
	}
	if tries < 2 || exists(filepath.Join(dir, manifestName)) || !exists(filepath.Join(dir, delmark.Name)) || len(r.rows) != 0 {
		t.Fatalf("retried (%d tries), the manifest gone, the marker and the row kept", tries)
	}
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u == nil {
		t.Fatal("the row is Delete not finished")
	}

	fault = false
	p := r.plan(t)
	if p.Rerun != RerunNoAnchor || contains(removeRels(p), delmark.Name) || contains(p.Kept, delmark.Name) {
		t.Fatalf("Delete again is the no-anchor re-run, the marker in none of its lists: %v %q %q", p.Rerun, removeRels(p), p.Kept)
	}
	res = Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || !strings.Contains(res.Lines[0], "The folder is kept: it holds 3 files that are not the archive's: Mailbox, Photos, taxes.pdf.") {
		t.Fatalf("Delete again: %s", res.Message())
	}
	if exists(filepath.Join(dir, delmark.Name)) || !exists(filepath.Join(dir, "taxes.pdf")) || len(r.rows) != 1 {
		t.Fatal("the marker gone, the person's files kept, the row removed")
	}
	if u, _ := UnfinishedDelete(r.x.Log, dir, ""); u != nil {
		t.Fatalf("the delete is finished: %+v", u)
	}
	if ev := events(logLines(t, r.x.Log)); ev[len(ev)-1] != "finished complete" {
		t.Fatalf("log %q", ev)
	}
}

// covers: MA-1047, R36
// A ticked §4.3 copy that could not be removed does not keep the marker (§15.2): that delete removes
// the row and is not "Delete not finished", so the marker goes after the anchor as always.
func TestMA1047_ATickedCopyLeftDoesNotKeepTheMarker(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	writeFile(t, filepath.Join(dir, filepath.FromSlash(unrecA)), "an older copy")
	r := newRig(t, dir)
	faultOn(t, nil, func(rel string) error {
		if rel == unrecA {
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, true)
	if res.Status != StatusPartial || res.Unfinished || len(r.rows) != 1 {
		t.Fatalf("%s", res.Message())
	}
	if exists(filepath.Join(dir, delmark.Name)) || exists(filepath.Join(dir, manifestName)) {
		t.Fatal("the marker goes with the manifest; a ticked copy left does not keep it")
	}
}

// covers: MA-1047, R36
// The marker is never in the plan's lists, counts, saved plan or record (§15.2): a delete stopped
// with the marker in its folder is planned again without it in Remove, Kept or Litter, its count
// or its saved plan; the delete that finishes counts only the archive's files and its record never
// names the marker.
func TestMA1047_TheMarkerIsInNoListCountSavedPlanOrRecord(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	fault := true
	faultOn(t, nil, func(rel string) error {
		if fault && rel == stemA+".html" {
			return inUseErr(rel)
		}
		return nil
	})
	if res := r.run(t, false); res.Status != StatusPartial || !exists(filepath.Join(dir, delmark.Name)) {
		t.Fatalf("%s", res.Message())
	}
	fault = false
	p := r.plan(t)
	if contains(removeRels(p), delmark.Name) || contains(p.Kept, delmark.Name) || contains(p.Litter, delmark.Name) || contains(unrecordedRels(p), delmark.Name) {
		t.Fatalf("the marker in a list: remove %q kept %q litter %q", removeRels(p), p.Kept, p.Litter)
	}
	// Its count: the same folder with the marker moved aside plans the same files and bytes.
	marker, aside := filepath.Join(dir, delmark.Name), filepath.Join(dir, "Photos", "aside")
	if err := os.Rename(marker, aside); err != nil {
		t.Fatal(err)
	}
	q := r.plan(t)
	if err := os.Rename(aside, marker); err != nil {
		t.Fatal(err)
	}
	if q.Files != p.Files || q.Bytes != p.Bytes {
		t.Fatalf("the marker counted: %d files %d bytes, without it %d files %d bytes", p.Files, p.Bytes, q.Files, q.Bytes)
	}
	if saved := string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p})); strings.Contains(saved, "deleting") {
		t.Fatalf("the saved plan names the marker:\n%s", saved)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || res.Removed != len(p.Remove) || strings.Contains(string(res.Record), "deleting") || strings.Contains(res.Message(), delmark.Name) {
		t.Fatalf("removed %d of %d:\n%s\n%s", res.Removed, len(p.Remove), res.Message(), res.Record)
	}
	if exists(filepath.Join(dir, delmark.Name)) {
		t.Fatal("the marker is gone")
	}
}

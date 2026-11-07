package source

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1138, R41
// R6-04 (settled 2026-11-07, the lead's call): the skip of a restore's folder holds for a
// backup that began before the restore finished. Its skip list is read while the restore
// is still open (the folder written, no "checked" line, no parent file): the walk leaves
// the folder out. The restore then commits — its "checked" and "ended" lines, its parent
// file — and the SAME list, walking again, still leaves it out (it reads the log again,
// since it changed), as a fresh list does (the twin).
func TestASkipListReadMidRestoreStillLeavesItOutOnceCommitted(t *testing.T) {
	b := newRestoreBed(t)
	scratch := tbrestore.NewLog(filepath.Join(t.TempDir(), tbrestore.LogName))
	top := b.restore("Live", "restored mail", scratch)
	lines, err := scratch.Read()
	if err != nil {
		t.Fatal(err)
	}
	var later []tbrestore.LogLine
	for _, l := range lines {
		if l.Event == tbrestore.EvChecked || l.Event == tbrestore.EvEnded || len(later) > 0 {
			later = append(later, l)
			continue
		}
		if err := b.log.Append(l); err != nil {
			t.Fatal(err)
		}
	}
	parent := filepath.Join(b.local, top)
	if err := os.Rename(parent, parent+".hold"); err != nil {
		t.Fatal(err)
	}
	early := tbrestore.LoadSkipSet(b.log.Path)
	subj, left := b.walk(early)
	assure.Reached(t, !has(subj, "restored mail"), fmt.Sprintf("while it is open the walk leaves it out: %v (left %v)", subj, left))

	for _, l := range later {
		if err := b.log.Append(l); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(parent+".hold", parent); err != nil {
		t.Fatal(err)
	}
	fresh, _ := b.walk(tbrestore.LoadSkipSet(b.log.Path))
	assure.Reached(t, !has(fresh, "restored mail"), fmt.Sprintf("twin: a list read after the commit leaves it out: %v", fresh))
	subj, left = b.walk(early)
	assure.Reached(t, !has(subj, "restored mail") && has(subj, "kept in Trash"), fmt.Sprintf("the list read mid-restore leaves the committed folder out too: %v (left %v)", subj, left))
}

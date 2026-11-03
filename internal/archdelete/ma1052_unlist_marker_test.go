package archdelete

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1052, R36
// Remove from list is allowed for a delete only the folder's marker knows (§15.4, DM-21): on a
// computer that did not begin the delete it is the tidy-up its person may want, and the folder can
// be listed again (add-archive, Open an archive folder…). The question names this computer's
// schedule into the folder, and Yes stops it and removes the row, the folder and its marker kept
// byte for byte. A delete this computer's log knows keeps MA-1029's refusal. Positive twin first:
// the folder whose delete this computer's log knows is refused, nothing changed.
func TestMA1052_RemoveFromListIsAllowedForADeleteOnlyTheMarkerKnows(t *testing.T) {
	{
		dir := smallArchive(t, t.TempDir())
		r, x := unlistRig(t, dir)
		fault := true
		faultOn(t, nil, func(rel string) error {
			if fault && strings.HasSuffix(rel, ".html") {
				return errors.New("The process cannot access the file")
			}
			return nil
		})
		if res := r.run(t, false); res.Status != StatusPartial || !exists(filepath.Join(dir, delmark.Name)) {
			t.Fatalf("%s", res.Message())
		}
		fault = false
		before := tree(t, dir)
		un := yes(t, dir, x)
		assure.Refused(t, un.ExitCode(), strings.Join(un.Lines, "\n"), assure.Code(1), assure.Names("Delete not finished: click Delete again", "it was not removed from the list"),
			assure.NoSideEffect(func() bool { return len(r.rows) == 0 && sameTree(before, tree(t, dir)) }))
	}
	dir := smallArchive(t, t.TempDir())
	plantMarker(t, dir)
	def := schedule.DefaultNameFor(dir)
	r, x := unlistRig(t, dir)
	if err := UnlistRefusal(dir, x.Deps); err != nil {
		t.Fatalf("the route refuses: %v", err)
	}
	if err := NewRowsCheck(x.Deps).UnlistRefusal(dir); err != nil {
		t.Fatalf("the row refuses: %v", err)
	}
	q, err := PlanUnlist(dir, x)
	if err != nil || !strings.Contains(q.Question, "Its scheduled backup "+def+" will also be stopped.") {
		t.Fatalf("question %q %v", q.Question, err)
	}
	before := tree(t, dir)
	res := yes(t, dir, x)
	if !res.Removed || len(r.rows) != 1 || r.rows[0] != dir || !contains(r.sched.removed, def) {
		t.Fatalf("%v rows %q stopped %q", res.Lines, r.rows, r.sched.removed)
	}
	if !sameTree(before, tree(t, dir)) || !exists(filepath.Join(dir, delmark.Name)) {
		t.Fatal("the folder and its marker are kept as they were")
	}
}

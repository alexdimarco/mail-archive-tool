package archdelete

import (
	"errors"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1029, R36
// Remove from list is refused for a folder whose delete did not finish (the 1.5.3 friction fixes'
// adversarial pass, finding 1): its row is what Delete again finishes the delete from, and once
// the archive's own files are gone neither add-archive nor Open an archive folder… lists the
// folder again, so taking the row away left a delete that could never be finished. The route
// (UnlistRefusal, PlanUnlist, Unlist) and a page's rows (RowsCheck.UnlistRefusal) refuse in
// MA-987's words, nothing changed: no row forgotten, no schedule stopped, the folder and the
// pointers as they were. Delete again then finishes it, the row going with it. Positive twin
// first: another archive, with that unfinished delete in the same log, is removed from the list.
func TestRemoveFromListIsRefusedForADeleteNotFinished(t *testing.T) {
	// Run 1 of a delete stops part way: a mail page cannot be removed; the row stays.
	dir := smallArchive(t, t.TempDir())
	r := newRig(t, dir)
	held := true
	faultOn(t, nil, func(rel string) error {
		if held && strings.HasSuffix(rel, ".html") {
			return errors.New("The process cannot access the file")
		}
		return nil
	})
	res := r.run(t, false)
	if res.Status != StatusPartial || !res.Unfinished || len(r.rows) != 0 {
		t.Fatalf("the delete should stop part way, its row kept: %s (rows %q)", res.Message(), r.rows)
	}
	x := UnlistDeps{Deps: r.x.Deps, Scheduler: r.sched, Pointers: r.ptr, ForgetRow: r.x.ForgetRow}

	// Positive twin: another archive, the same deletions.log, is removed from the list.
	other := smallArchive(t, t.TempDir())
	if err := UnlistRefusal(other, x.Deps); err != nil {
		t.Fatalf("the positive twin is refused: %v", err)
	}
	if err := NewRowsCheck(x.Deps).UnlistRefusal(other); err != nil {
		t.Fatalf("the positive twin's row is refused: %v", err)
	}
	if res := yes(t, other, x); !res.Removed || len(r.rows) != 1 || r.rows[0] != other {
		t.Fatalf("the positive twin: %v (rows %q)", res.Lines, r.rows)
	}

	// The folder whose delete did not finish: refused at the route and on the row, in one case and
	// one set of words.
	before := tree(t, dir)
	ptrBefore := r.ptr.p
	stoppedBefore := len(r.sched.removed)
	unchanged := func() bool {
		return len(r.rows) == 1 && len(r.sched.removed) == stoppedBefore && r.ptr.p == ptrBefore && sameTree(before, tree(t, dir))
	}
	want := []string{"Delete not finished: click Delete again", "it was not removed from the list", "nothing was changed"}
	route := AsRefusal(UnlistRefusal(dir, x.Deps))
	row := AsRefusal(NewRowsCheck(x.Deps).UnlistRefusal(dir))
	if route == nil || row == nil || route.Case != CaseUnfinished || row.Case != route.Case || row.Message != route.Message {
		t.Fatalf("route %+v, row %+v: want both %q", route, row, CaseUnfinished)
	}
	assure.Refused(t, 1, route.Message, assure.Code(1), assure.Names(want...), assure.NoSideEffect(unchanged))
	if _, err := PlanUnlist(dir, x); AsRefusal(err) == nil || AsRefusal(err).Case != CaseUnfinished {
		t.Fatalf("the question is not refused: %v", err)
	}
	un := yes(t, dir, x)
	code := un.ExitCode()
	if un.Refusal == nil || un.Refusal.Case != CaseUnfinished || un.Removed {
		t.Fatalf("Remove from list: %+v", un)
	}
	assure.Refused(t, code, strings.Join(un.Lines, "\n"), assure.Code(1), assure.Names(want...), assure.NoSideEffect(unchanged))

	// Delete again finishes it, and the row goes with it.
	held = false
	if res := r.run(t, false); res.Status != StatusComplete || len(r.rows) != 2 || r.rows[1] != dir {
		t.Fatalf("Delete again: %s (rows %q)", res.Message(), r.rows)
	}
}

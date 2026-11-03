package archdelete

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The 1.5.6 friction review's notes D and E (docs/review-delete-friction-1.5.6.md; walked on the
// Windows VM 3 November 2026): two sentences that said more than the plan or the result knew. Lead's
// words, owner-delegated 2026-11-02 (docs/design-archive-delete.md §14, "The 1.5.6 friction review
// settled").

// covers: MA-1066, R36
// On the other computer, the plan's sentence after "This finishes the delete begun DATE." says what
// happened first and no longer says the log has "no record of when it began" right after the date
// (note D): "It was begun on another computer or user account: this computer's deletion log has no
// record of its start." The same sentence stays true on a later run of this computer that finishes
// such a delete (its own started line then carries from_marker, §15.4's DM-19 correction), which is
// why the review's proposed "has no record of this delete" was not taken (§15.4 had already turned
// down "has no record of it" for that reason). Positive twin first: a delete this computer's log saw
// begin says neither.
func TestMA1066_ThePlanSaysTheDeleteWasBegunElsewhereWithoutContradictingItsDate(t *testing.T) {
	const want = "It was begun on another computer or user account: this computer's deletion log has no record of its start."
	{
		dir := smallArchive(t, t.TempDir())
		r := newRig(t, dir)
		held := true
		faultOn(t, nil, func(rel string) error {
			if held && strings.HasSuffix(rel, ".html") {
				return errors.New("The process cannot access the file")
			}
			return nil
		})
		if res := r.run(t, false); res.Status == StatusComplete {
			t.Fatalf("positive twin: the first run should stop part way: %s", res.Message())
		}
		held = false
		p := r.plan(t)
		if p.Finishes == nil || p.Finishes.FromMarker || len(p.FinishesNotes()) != 0 {
			t.Fatalf("positive twin: a delete this computer began: finishes %+v notes %q", p.Finishes, p.FinishesNotes())
		}
	}
	if MsgMarkerNoRecord != want {
		t.Fatalf("the sentence is %q, want %q", MsgMarkerNoRecord, want)
	}
	dir := plainBesidePersonsFiles(t)
	plantMarker(t, dir)
	r := newRig(t, dir)
	p := r.plan(t)
	began := ma1051BeganAt(t)
	if got := p.FinishesLine(); got != "This finishes the delete begun "+localStamp(began)+"." {
		t.Fatalf("FinishesLine %q", got)
	}
	notes := p.FinishesNotes()
	if len(notes) != 1 || notes[0] != want || strings.Contains(notes[0], "when it began") {
		t.Fatalf("the plan's notes %q", notes)
	}
	// A later run here, after this computer's own run of that delete stopped part way: still said,
	// and still true (the delete's start is still on another computer's log only).
	held := true
	faultOn(t, nil, func(rel string) error {
		if held && strings.HasSuffix(rel, ".html") {
			return errors.New("The process cannot access the file")
		}
		return nil
	})
	if res := Execute(p, p.Name, false, r.x); res.Status == StatusComplete {
		t.Fatalf("this computer's run should stop part way: %s", res.Message())
	}
	held = false
	later := r.plan(t)
	if later.Finishes == nil || !later.Finishes.FromMarker {
		t.Fatalf("the later plan: finishes %+v", later.Finishes)
	}
	if n := later.FinishesNotes(); len(n) != 1 || n[0] != want {
		t.Fatalf("the later plan's notes %q", n)
	}
}

// covers: MA-1066, R36
// After "It was finished elsewhere", the result no longer calls this computer's count the whole
// delete's (note E: computer A said "29 files … removed in all" when B had removed 2 more): it says
// "This finishes the delete begun DATE: this computer's runs of it removed N files (SIZE); the rest
// were removed elsewhere." Positive twin first: a delete this computer finished itself still says
// "… removed in all." with the whole count.
func TestMA1066_FinishedElsewhereCountsOnlyThisComputersRuns(t *testing.T) {
	first := time.Date(2026, 11, 3, 17, 34, 0, 0, time.UTC)
	stopPartWay := func(t *testing.T) (*rig, *Result, *bool) {
		t.Helper()
		dir := smallArchive(t, t.TempDir())
		writeFile(t, dir+"/README.txt", "the archive's readme")
		r := newRig(t, dir)
		r.x.Now = func() time.Time { return first }
		held := true
		faultOn(t, nil, func(rel string) error {
			if held && strings.HasSuffix(rel, ".html") {
				return errors.New("The process cannot access the file")
			}
			return nil
		})
		res := r.run(t, false)
		if res.Status == StatusComplete || res.Removed == 0 {
			t.Fatalf("the first run should stop part way having removed some files: %s", res.Message())
		}
		r.x.Now = func() time.Time { return first.Add(time.Hour) }
		return r, res, &held
	}
	{
		r, res1, held := stopPartWay(t)
		*held = false
		res2 := r.run(t, false)
		want := "This finishes the delete begun " + localStamp(first) + ": " + plural(res1.Removed+res2.Removed, "file", "files") + " (" + size(res1.Bytes+res2.Bytes) + ") removed in all."
		if res2.Status != StatusComplete || !hasExactLine(res2, want) {
			t.Fatalf("positive twin, want %q:\n%s", want, res2.Message())
		}
	}
	r, res1, held := stopPartWay(t)
	*held = false
	// Another computer finished it: the folder is gone, its parent still there.
	if err := os.RemoveAll(r.dir); err != nil {
		t.Fatal(err)
	}
	deps := r.x.Deps
	deps.FinishedElsewhere = true
	p := mustPlan(t, r.dir, deps)
	if !p.Elsewhere || p.Finishes == nil || !p.Finishes.RemovedKnown {
		t.Fatalf("the plan: elsewhere %v finishes %+v", p.Elsewhere, p.Finishes)
	}
	res := Execute(p, p.Name, false, r.x)
	want := "This finishes the delete begun " + localStamp(first) + ": this computer's runs of it removed " + plural(res1.Removed+res.Removed, "file", "files") +
		" (" + size(res1.Bytes+res.Bytes) + "); the rest were removed elsewhere."
	if res.Status != StatusComplete || !hasExactLine(res, want) || hasLine(res, "removed in all") {
		t.Fatalf("want %q:\n%s", want, res.Message())
	}
}

// hasExactLine reports whether one of the result's lines is s.
func hasExactLine(r *Result, s string) bool {
	for _, l := range r.Lines {
		if l == s {
			return true
		}
	}
	return false
}

package archdelete

import (
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1011, MA-1012, R36
// Execute's re-check under the archive lock (§5.3 step 1 (a)) asks neither the sealed values nor
// the scratch listing (the core-plan-record adversarial pass, finding 5): the record states the
// plan's sealed values, so reading the sealed manifest and timeline again under the lock used the
// archive's key for nothing and held the lock longer; step 1 (c) lists the scratch folders itself.
// Positive twin first: the plan asks each once.
func TestTheRecheckUnderTheLockUsesNoKeyAndListsNoScratch(t *testing.T) {
	r := newRig(t, encryptedClean(t))
	r.x.Deps.Record = recordedAt(r.dir)
	sealed, scratch := 0, 0
	r.x.Deps.Sealed = func(string, string) (Captured, bool) { sealed++; return Captured{Messages: 1}, true }
	r.x.Deps.ScratchDir = func(spelling, id string) (string, error) {
		scratch++
		return scratchFor(r.scratch, spelling, id), nil
	}
	p := r.plan(t)
	assure.Reached(t, sealed == 1 && scratch > 0 && p.Captured.KeyOpen, "the plan's sealed values and scratch listing")

	sealed, scratch = 0, 0
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete {
		t.Fatalf("the delete: %s", res.Message())
	}
	if sealed != 0 || scratch != 0 {
		t.Fatalf("under the lock the re-check asked the sealed values %d times and Deps.ScratchDir %d times; want neither", sealed, scratch)
	}
}

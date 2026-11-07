package tbrestore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1128, R40
// R7-01 independent review F6 (settled 2026-11-07, the lead's call): when the commit's
// create of the parent file cannot be bound to the root (W-CHANGED: a folder on the way to
// Local Folders changed, macOS's bind check), the words say that, never W-TAKEN ("taken by
// another program") — also when something stands under the name — and never a raw error
// in brackets. With an entry planted under the name the cleanup removes nothing (it is not
// this restore's) and the words add what the person finds; with none, the part written is
// removed. The bind refusal comes through the fault seam here; macOS's real one is
// TestAnEntryMadeByPathIsBoundOnlyToThisUsersOwn.
func TestACommitThatCannotBeBoundIsNotCalledTaken(t *testing.T) {
	in, msgs := sampleFolders()
	for _, planted := range []bool{true, false} {
		fx := newFixture(t, in, msgs)
		bind := refuse("W-CHANGED", fill(wChanged, "WHAT", "a folder on the way to Thunderbird's Local Folders changed while the restore was writing"), errChanged)
		var before string
		f := &faultFS{inner: newRootFS(fx.root), hook: func(op, name string) error {
			if op == "create" && name == fx.top {
				if planted {
					writeFile(t, filepath.Join(fx.lf.Dir, fx.top), "not this restore's")
				}
				before = fx.local()
				return bind
			}
			return nil
		}}
		res, id := fx.write(context.Background(), f)
		ev := dump(fx.events(id))
		assure.Reached(t, res.Outcome != OutTaken && !strings.Contains(ev, EvTaken) && !strings.Contains(res.Words, "Something made a folder named"),
			"planted="+map[bool]string{true: "yes", false: "no"}[planted]+": not W-TAKEN: "+string(res.Outcome)+" "+ev)
		rc := 0
		if res.Outcome == OutFailed && !strings.Contains(ev, EvTaken) && !strings.Contains(res.Words, "could not finish (") {
			rc = 1
		}
		if planted {
			refusedAs(t, errFrom(rc, res.Words), "W-KEPT", func() bool { return keptAll(before, fx.local()) && res.Clean != nil && res.Clean.Kind == CleanParent },
				"changed since you clicked Check", "removed nothing", fx.top)
		} else {
			refusedAs(t, errFrom(rc, res.Words), "W-KEPT", func() bool { return !fileExists(filepath.Join(fx.lf.Dir, fx.top+".sbd")) },
				"changed since you clicked Check", "The part already written was removed")
		}
	}
}
